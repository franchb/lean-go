package pipeline

import (
	"fmt"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1) port of WidthAdmissionTests in
// scripts/test_width_conversions.py.

var widthCasts = [][3]string{{"UInt32.toUInt64", "UInt32", "UInt64"}, {"UInt64.toUInt32", "UInt64", "UInt32"}}

func TestWidthExactUnaryLeavesAndErasedSelection(t *testing.T) {
	requireModules(t)
	for _, c := range widthCasts {
		name, operand, result := c[0], c[1], c[2]
		for _, modern := range []bool{false, true} {
			program := numericExport(t, fmt.Sprintf("def run (a : %s) : %s := %s a\n", operand, result, name), numericOptions{modern: modern})
			assertList(t, "features", featuresOf(program), "uint32", "uint64", "width_conversion")
			function := obj(arr(program["functions"])[0])
			assertList(t, "params", typesOfParams(function["params"]), operand)
			if function["result"] != result {
				t.Fatal("result")
			}
			var values []string
			for _, node := range nodes(function["body"]) {
				if node["op"] == "let" {
					value := obj(node["value"])
					values = append(values, fmt.Sprintf("%v %v %d", value["kind"], value["name"], len(arr(value["args"]))))
				}
			}
			assertList(t, "lets", values, "primitive "+name+" 1")
		}
		program := numericExport(t, fmt.Sprintf("def run (a : %s) :=\n  let _unused := %s a\n  a\n", operand, name), numericOptions{})
		assertList(t, "erased features", featuresOf(program), strings.ToLower(operand))
		assertContains(t, "erased provenance", provenanceNames(program), name)
		for _, leaf := range emittedPrimitives(program) {
			if strings.Contains(leaf, ".toUInt") {
				t.Fatalf("conversion %s emitted", leaf)
			}
		}
	}
}

func TestWidthPristineClosureIsAuditedWithoutRuntimeHelperAdmission(t *testing.T) {
	requireModules(t)
	for _, c := range [][3]string{{"UInt32.toNat", "UInt32.toUInt64", "UInt32"}, {"Nat.toUInt32", "UInt64.toUInt32", "UInt64"}, {"UInt64.toNat", "UInt64.toUInt32", "UInt64"}} {
		t.Run(c[0], func(t *testing.T) {
			numericReject(t, fmt.Sprintf("def run (a : %s) := %s a\n", c[2], c[1]), "Numeric source extern changed",
				numericOptions{probe: importedAttributeProbe("externAttr", c[0], untrustedExtern)})
		})
	}
	for _, body := range []string{"def run (a : UInt64) := a.toNat\n", "def run (a : UInt32) := a.toUSize\n",
		"def run (a : UInt16) := a.toInt16\n", "axiom forged : UInt64\ndef run := forged.toUInt32\n",
		"unsafe def run (a : UInt64) := a.toUInt32\n"} {
		t.Run(body, func(t *testing.T) {
			numericReject(t, body, "extern|axiom|unsafe|Unsafe|Unsupported|unsupported|not supported by code generator|provenance", numericOptions{})
		})
	}
}

func monoMutationProbe(name, fields string) string {
	return fmt.Sprintf("open Lean LeanGo.Module Lean.Compiler.LCNF\nrun_cmd Lean.Elab.Command.liftCoreM do\n"+
		"  let some decl ← getMonoDecl? ``%[1]s | throwError \"missing test primitive\"\n"+
		"  let changed := { decl with %[2]s }\n"+
		"  let some index := (← getEnv).getModuleIdxFor? ``%[1]s\n"+
		"    | throwError \"test requires imported primitive\"\n"+
		"  modifyEnv fun env => monoExt.toEnvExtension.modifyState env fun state =>\n"+
		"    let entries := (state.importedEntries[index]!).map fun entry =>\n"+
		"      if entry.name == ``%[1]s then changed else entry\n"+
		"    { state with importedEntries := state.importedEntries.set! index entries }\n"+
		"  let some observed ← getMonoDecl? ``%[1]s | throwError \"missing mutated primitive\"\n"+
		"  unless observed == changed do throwError \"test mutation was not observed\"\n", name, fields)
}

func TestWidthImportedAttributesAndMonoSignaturesReject(t *testing.T) {
	requireModules(t)
	for _, c := range widthCasts {
		name, operand, result := c[0], c[1], c[2]
		prefix := fmt.Sprintf("@[noinline] def replacement (a : %s) := %s a\ndef initializeCast : IO (%s → %s) := pure %s\n", operand, name, operand, result, name)
		for _, attribute := range [][2]string{{"externAttr", untrustedExtern}, {"Compiler.implementedByAttr", "`Numeric.replacement"},
			{"regularInitAttr", "`Numeric.initializeCast"}} {
			for _, erased := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/erased=%v", name, attribute[0], erased), func(t *testing.T) {
					body := fmt.Sprintf("def run (a : %s) := %s a\n", operand, name)
					if erased {
						body = fmt.Sprintf("def run (a : %s) := \n  let _unused := %s a\n  a\n", operand, name)
					}
					numericReject(t, prefix+body, "Numeric source .*changed|numeric primitive",
						numericOptions{probe: importedAttributeProbe(attribute[0], name, attribute[1])})
				})
			}
		}
		for _, fields := range []string{"safe := false", "params := #[]", "levelParams := [`u]",
			fmt.Sprintf("type := .forallE `a (.const ``%s []) (.const ``%s []) .default", result, result),
			fmt.Sprintf("type := .forallE `a (.const ``%s []) (.const ``%s []) .default", operand, operand),
			"value := .extern " + untrustedExtern} {
			t.Run(name+"/"+fields, func(t *testing.T) {
				numericReject(t, fmt.Sprintf("def run (a : %s) := %s a\n", operand, name),
					"Invalid numeric primitive|Numeric primitive signature/safety mismatch|Numeric mono extern mismatch",
					numericOptions{probe: monoMutationProbe(name, fields)})
			})
		}
	}
}

func TestWidthExactDefinitionSafetyAndAxiomChecks(t *testing.T) {
	requireModules(t)
	// Isolated exact-name definitions: no imported constant or cached axiom
	// result can hide these deliberate baseline corruptions.
	for _, c := range widthCasts {
		for _, s := range [][2]string{{"unsafe", "Unsafe numeric boundary"}, {"safe", "Unexpected numeric boundary axioms"}} {
			t.Run(c[0]+"/"+s[0], func(t *testing.T) {
				numericReject(t, fmt.Sprintf("def run (a : %s) := %s a\n", c[1], c[0]), s[1], numericOptions{probe: definitionBaselineProbe(c[0], s[0])})
			})
		}
	}
}

func TestWidthLocalScopedLateErasedAndClosedScopeHistory(t *testing.T) {
	requireModules(t)
	checkPrimitiveHistory(t, []historyPrimitive{{widthCasts[0][0], widthCasts[0][1], true}, {widthCasts[1][0], widthCasts[1][1], true}}, "width_conversion")
}
