package pipeline

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1) ports of BitwiseAdmissionTests in
// scripts/test_bitwise.py and ShiftLeftAdmissionTests in
// scripts/test_shift_left.py. The native comparisons of both files are in
// integration/bitwise_native_test.go.

var bitwiseTestOperations = []string{"land", "lor", "xor", "complement", "shiftRight"}

func bitwisePrimitive(name string) bool {
	operation := name[strings.IndexByte(name, '.')+1:]
	for _, w := range unsignedWidths {
		if strings.HasPrefix(name, fmt.Sprintf("UInt%d.", w)) {
			for _, op := range append([]string{"shiftLeft"}, bitwiseTestOperations...) {
				if operation == op {
					return true
				}
			}
		}
	}
	return false
}

func TestBitwiseNotationAndDirectCallsHaveExactSignatures(t *testing.T) {
	requireModules(t)
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprintf("direct=%v", direct), func(t *testing.T) {
			var fields, values, want []string
			for _, width := range []int{32, 64} {
				a, b := fmt.Sprintf("a%d", width), fmt.Sprintf("b%d", width)
				expressions := []string{a + " &&& " + b, a + " ||| " + b, a + " ^^^ " + b, "~~~" + a, a + " >>> " + b}
				for i, op := range bitwiseTestOperations {
					fields = append(fields, fmt.Sprintf("  %s%d : UInt%d", op, width, width))
					if direct {
						value := fmt.Sprintf("UInt%d.%s %s", width, op, a)
						if op != "complement" {
							value += " " + b
						}
						values = append(values, value)
					} else {
						values = append(values, expressions[i])
					}
					want = append(want, fmt.Sprintf("UInt%d.%s", width, op))
				}
			}
			source := "structure Result where\n" + strings.Join(fields, "\n") + "\n" +
				"def run (a32 b32 : UInt32) (a64 b64 : UInt64) : Result :=\n  ⟨" + strings.Join(values, ", ") + "⟩\n"
			program := numericExport(t, source, numericOptions{modern: direct})
			assertList(t, "features", featuresOf(program), "bitwise", "uint32", "uint64")
			var leaves []string
			for _, value := range letValues(program) {
				if value["kind"] != "primitive" {
					continue
				}
				name := str2(value["name"])
				leaves = append(leaves, name)
				arity := 2
				if strings.HasSuffix(name, ".complement") {
					arity = 1
				}
				if len(arr(value["args"])) != arity {
					t.Fatalf("%s has %d arguments", name, len(arr(value["args"])))
				}
			}
			assertSet(t, "leaves", leaves, want...)
		})
	}
}

func TestBitwiseSourceOnlyAndMultiRootFeatures(t *testing.T) {
	requireModules(t)
	program := numericExport(t, "def run (a b : UInt32) : UInt32 :=\n  let _unused := a &&& b\n  a\n", numericOptions{})
	assertList(t, "features", featuresOf(program), "uint32")
	// Provenance stops at the trusted wrapper; its pristine closure audits land.
	assertContains(t, "provenance", provenanceNames(program), "instAndOpUInt32")
	for _, name := range emittedPrimitives(program) {
		if bitwisePrimitive(name) {
			t.Fatalf("bitwise leaf %s emitted", name)
		}
	}
	multi := numericExportRoots(t, "namespace Numeric\ndef run (a : UInt32) := ~~~a\ndef second (a : UInt64) := a >>> 64\nend Numeric\n", "Numeric.second", "Numeric.run")
	assertList(t, "multi-root features", featuresOf(multi), "bitwise", "uint32", "uint64")
	assertList(t, "roots", typesAny(multi["roots"]), "Numeric.run", "Numeric.second")
}

func TestBitwiseExactPrimitiveAndWrapperMutationsReject(t *testing.T) {
	requireModules(t)
	for _, width := range []int{32, 64} {
		for _, c := range [][2]string{
			{fmt.Sprintf("UInt%d.land", width), "a &&& b"}, {fmt.Sprintf("UInt%d.complement", width), "~~~a"},
			{fmt.Sprintf("UInt%d.shiftRight", width), "a >>> b"}, {fmt.Sprintf("instAndOpUInt%d", width), "a &&& b"},
			{fmt.Sprintf("instComplementUInt%d", width), "~~~a"}, {fmt.Sprintf("instShiftRightUInt%d", width), "a >>> b"},
		} {
			t.Run(c[0], func(t *testing.T) {
				numericReject(t, fmt.Sprintf("def run (a b : UInt%d) := %s\n", width, c[1]), "Numeric source extern changed|numeric primitive",
					numericOptions{probe: importedAttributeProbe("externAttr", c[0], untrustedExtern)})
			})
		}
	}
}

func TestBitwiseLocalScopedLateAndErasedReplacementsReject(t *testing.T) {
	requireModules(t)
	for _, width := range []int{32, 64} {
		primitive := fmt.Sprintf("@[noinline] def replacement (a : UInt%d) := UInt%d.complement a\ntheorem replacement_eq : UInt%d.complement = replacement := rfl\n", width, width, width)
		wrapper := fmt.Sprintf("def replacement : AndOp UInt%d := instAndOpUInt%d\ntheorem replacement_eq : instAndOpUInt%d = replacement := rfl\n", width, width, width)
		for _, c := range [][2]string{{primitive, "~~~a"}, {wrapper, "a &&& b"}} {
			call := fmt.Sprintf("def run (a b : UInt%d) := %s\n", width, c[1])
			erased := fmt.Sprintf("def run (a b : UInt%d) :=\n  let _unused := %s\n  a\n", width, c[1])
			for i, source := range rewriteSources(c[0], call, erased) {
				t.Run(fmt.Sprintf("%d/%s/%d", width, c[1], i), func(t *testing.T) {
					numericReject(t, source, "rewrite|replacement|repeated compilation", numericOptions{})
				})
			}
		}
	}
}

func TestBitwiseUnsupportedSignedAndPlatformWidthsAndForgedAxiom(t *testing.T) {
	requireModules(t)
	for _, source := range []string{"def run (a b : Int16) := a &&& b\n",
		"def run (a : USize) := (~~~a).toUInt16\n",
		"def run (a b : Int32) := (a &&& b) / b\n",
		"def run (a b : UInt64) := ((a ||| b) % b).toUSize\n",
		"axiom forged : UInt32\ndef run := ~~~forged\n"} {
		t.Run(source, func(t *testing.T) {
			numericReject(t, source, "extern|axiom|opaque|Unsupported|unsupported|not supported by code generator", numericOptions{})
		})
	}
}

// scripts/test_shift_left.py ShiftLeftAdmissionTests

var shiftLeftWrappers = []string{"HShiftLeft.hShiftLeft", "instHShiftLeftOfShiftLeft", "ShiftLeft.shiftLeft",
	"instShiftLeftUInt32", "instShiftLeftUInt64"}

func TestShiftLeftDirectAndNotationMatchAtBothWidthsAndModuleModes(t *testing.T) {
	requireModules(t)
	body := "structure Result where\n  small : UInt32\n  large : UInt64\ndef run (a b : UInt32) (c d : UInt64) : Result := ⟨%s, %s⟩\n"
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprintf("modern=%v", modern), func(t *testing.T) {
			direct := numericExport(t, fmt.Sprintf(body, "UInt32.shiftLeft a b", "UInt64.shiftLeft c d"), numericOptions{modern: modern})
			notation := numericExport(t, fmt.Sprintf(body, "a <<< b", "c <<< d"), numericOptions{modern: modern})
			for _, key := range []string{"types", "functions", "roots", "features", "api", "schema", "profile", "claims", "rewrites", "stage"} {
				if !reflect.DeepEqual(direct[key], notation[key]) {
					t.Fatalf("%s differs: %s vs %s", key, mustJSON(direct[key]), mustJSON(notation[key]))
				}
			}
			assertList(t, "features", featuresOf(notation), "bitwise", "uint32", "uint64")
			assertSet(t, "primitives", emittedPrimitives(notation), "UInt32.shiftLeft", "UInt64.shiftLeft")
			for _, wrapper := range shiftLeftWrappers {
				if wrapper != "ShiftLeft.shiftLeft" {
					assertContains(t, "provenance", provenanceNames(notation), wrapper)
				}
			}
		})
	}
}

func TestShiftLeftMultiRootHelperAndCases(t *testing.T) {
	requireModules(t)
	program := numericExportRoots(t, `namespace Numeric
@[noinline] def helper (a b : UInt64) : UInt64 := a <<< b
def run (flag : Bool) (a b : UInt64) := if flag then helper a b else helper b a
def second (a b : UInt32) := a <<< b
end Numeric
`, "Numeric.second", "Numeric.run")
	assertSubset(t, "features", []string{"bitwise", "uint32", "uint64"}, featuresOf(program))
	assertList(t, "roots", typesAny(program["roots"]), "Numeric.run", "Numeric.second")
	assertSet(t, "primitives", emittedPrimitives(program), "UInt32.shiftLeft", "UInt64.shiftLeft")
	cases := false
	for _, function := range arr(program["functions"]) {
		for _, node := range nodes(obj(function)["body"]) {
			cases = cases || node["op"] == "cases"
		}
	}
	if !cases {
		t.Fatal("no cases node")
	}
	assertContains(t, "functions", names(program["functions"]), "Numeric.helper")
}

func TestShiftLeftErasedAndDeadUsesDoNotActivateACapability(t *testing.T) {
	requireModules(t)
	for _, width := range []int{32, 64} {
		program := numericExport(t, fmt.Sprintf("def dead (a b : UInt%d) := a <<< b\ndef run (a b : UInt%d) :=\n  let _unused := a <<< b\n  a\n", width, width), numericOptions{})
		assertList(t, "features", featuresOf(program), fmt.Sprintf("uint%d", width))
		assertList(t, "primitives", emittedPrimitives(program))
		assertContains(t, "provenance", provenanceNames(program), fmt.Sprintf("instShiftLeftUInt%d", width))
		if hasString(names(program["functions"]), "Numeric.dead") {
			t.Fatal("dead function exported")
		}
	}
}

func TestShiftLeftCustomInstanceFollowsAdditionInsteadOfSyntax(t *testing.T) {
	requireModules(t)
	program := numericExport(t, "instance : ShiftLeft UInt32 := ⟨fun a b => UInt32.add a b⟩\ndef run (a b : UInt32) := a <<< b\n", numericOptions{})
	assertList(t, "primitives", emittedPrimitives(program), "UInt32.add")
}

func shiftLeftWidth(name string) int {
	if strings.HasSuffix(name, "64") || strings.HasPrefix(name, "UInt64.") {
		return 64
	}
	return 32
}

func TestShiftLeftExactWrapperAndLeafExternMutationsReject(t *testing.T) {
	requireModules(t)
	for _, name := range append(slicesClone(shiftLeftWrappers), "UInt32.shiftLeft", "UInt64.shiftLeft") {
		t.Run(name, func(t *testing.T) {
			numericReject(t, fmt.Sprintf("def run (a b : UInt%d) := a <<< b\n", shiftLeftWidth(name)), "Numeric source extern changed|numeric primitive",
				numericOptions{probe: importedAttributeProbe("externAttr", name, untrustedExtern)})
		})
	}
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

func TestShiftLeftImplementedByAndInitializationRejectAfterErasureToo(t *testing.T) {
	requireModules(t)
	for _, width := range []int{32, 64} {
		for _, c := range [][2]string{{fmt.Sprintf("UInt%d.shiftLeft", width), fmt.Sprintf("UInt%d → UInt%d → UInt%d", width, width, width)},
			{fmt.Sprintf("instShiftLeftUInt%d", width), fmt.Sprintf("ShiftLeft UInt%d", width)}} {
			name, typ := c[0], c[1]
			prefix := fmt.Sprintf("def replacement : %s := %s\ndef initializeShift : IO (%s) := pure %s\n", typ, name, typ, name)
			for _, attribute := range [][2]string{{"Compiler.implementedByAttr", "`Numeric.replacement"}, {"regularInitAttr", "`Numeric.initializeShift"}} {
				for _, erased := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/erased=%v", name, attribute[0], erased), func(t *testing.T) {
						body := fmt.Sprintf("def run (a b : UInt%d) := a <<< b\n", width)
						if erased {
							body = fmt.Sprintf("def run (a b : UInt%d) := \n  let _unused := a <<< b\n  a\n", width)
						}
						numericReject(t, prefix+body, "Numeric source .*changed|numeric primitive",
							numericOptions{probe: importedAttributeProbe(attribute[0], name, attribute[1])})
					})
				}
			}
		}
	}
}

func TestShiftLeftExactWrapperSafetyAndAxiomBaselines(t *testing.T) {
	requireModules(t)
	for _, name := range append(slicesClone(shiftLeftWrappers), "UInt32.shiftLeft", "UInt64.shiftLeft") {
		for _, c := range [][2]string{{"unsafe", "Unsafe numeric boundary"}, {"safe", "Unexpected numeric boundary axioms"}} {
			t.Run(name+"/"+c[0], func(t *testing.T) {
				numericReject(t, "def run (a b : UInt32) := a <<< b\n", c[1], numericOptions{probe: definitionBaselineProbe(name, c[0])})
			})
		}
	}
}

func TestShiftLeftLocalScopedLateErasedAndClosedScopeRewriteHistory(t *testing.T) {
	requireModules(t)
	for _, width := range []int{32, 64} {
		for _, c := range [][2]string{{fmt.Sprintf("UInt%d.shiftLeft", width), fmt.Sprintf("UInt%d → UInt%d → UInt%d", width, width, width)},
			{fmt.Sprintf("instShiftLeftUInt%d", width), fmt.Sprintf("ShiftLeft UInt%d", width)}} {
			name, typ := c[0], c[1]
			prelude := fmt.Sprintf("@[noinline] def replacement : %s := %s\ntheorem replacement_eq : %s = replacement := rfl\n", typ, name, name)
			call := fmt.Sprintf("def run (a b : UInt%d) := a <<< b\n", width)
			erased := fmt.Sprintf("def run (a b : UInt%d) :=\n  let _unused := a <<< b\n  a\n", width)
			for i, body := range rewriteSources(prelude, call, erased) {
				t.Run(fmt.Sprintf("%s/%d", name, i), func(t *testing.T) {
					numericReject(t, body, "rewrite|replacement|repeated compilation", numericOptions{})
				})
			}
			clean := numericExport(t, prelude+"section\nattribute [local csimp] replacement_eq\nend\n"+call, numericOptions{})
			assertList(t, "closed scope primitives", emittedPrimitives(clean), fmt.Sprintf("UInt%d.shiftLeft", width))
		}
	}
}

func TestShiftLeftPristineClosureAndUnsupportedWidthsAreNotRuntimeAdmission(t *testing.T) {
	requireModules(t)
	for _, width := range []int{32, 64} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			numericReject(t, fmt.Sprintf("def run (a b : UInt%d) := a <<< b\n", width), "Numeric source extern changed",
				numericOptions{probe: importedAttributeProbe("externAttr", fmt.Sprintf("UInt%d.mod", width), untrustedExtern)})
			// UInt.mod is now an admitted leaf; its natural-number helper is not.
			numericReject(t, fmt.Sprintf("def run (a b : UInt%d) := (Nat.mod a.toNat b.toNat).toUInt%d\n", width, width),
				"extern|Unsupported|unsupported|provenance", numericOptions{})
		})
	}
	numericReject(t, "def run (a b : USize) := a <<< b\n", "extern|axiom|opaque|Unsupported|unsupported|not supported by code generator", numericOptions{})
	numericReject(t, "axiom forged : UInt32\ndef run (b : UInt32) := forged <<< b\n", "axiom|not supported by code generator", numericOptions{})
}
