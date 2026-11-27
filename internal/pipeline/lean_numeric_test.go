package pipeline

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1) ports of scripts/test_numeric_admission.py
// and scripts/test_uint64_admission.py, with the fixture shared by the other
// numeric ports (lean_unsigned_test.go, lean_bytes_test.go,
// lean_bitwise_test.go and lean_widths_test.go).

// numericOptions are NumericAdmissionTests.export's options; probe is Lean
// inserted into the export harness right before the export command, after
// the source module is imported (UInt64AdmissionTests.export).
type numericOptions struct {
	modern bool
	probe  string
}

func probeHarness(probe string) func(string, []string) string {
	if probe == "" {
		return nil
	}
	return func(module string, roots []string) string {
		return strings.Replace(exportHarness(module, roots), "#lean_go_export", probe+"\n#lean_go_export", 1)
	}
}

func numericFixture(t *testing.T, body string, o numericOptions) *leanFixture {
	t.Helper()
	f := newLeanFixture(t)
	f.harness = probeHarness(o.probe)
	f.write("Numeric.lean", nsSource("Numeric", body, o.modern, ""))
	return f
}

// numericExport extracts the root Numeric.run of body in namespace Numeric.
func numericExport(t *testing.T, body string, o numericOptions) map[string]any {
	t.Helper()
	return numericFixture(t, body, o).export("Numeric", "Numeric.run")
}

// numericReject requires the producer to reject Numeric.run with a
// diagnostic matching pattern that is no elaboration failure, leaves
// existing outputs alone, and was raised before emission (the Python suites
// rejected in export_module, which never emits Go).
func numericReject(t *testing.T, body, pattern string, o numericOptions) {
	t.Helper()
	err := numericFixture(t, body, o).reject("Numeric", "Numeric.run", pattern)
	if item := arr(Report(err).Values["diagnostics"])[0].(*Object); item.Values["stage"] == "emission" {
		t.Fatalf("rejected only during emission: %v", err)
	}
}

// numericExportRoots writes source verbatim as Numeric.lean and extracts roots.
func numericExportRoots(t *testing.T, source string, roots ...string) map[string]any {
	t.Helper()
	f := newLeanFixture(t)
	f.write("Numeric.lean", source)
	return f.export("Numeric", roots...)
}

// emittedPrimitives collects executable primitive leaves, ignoring
// provenance and ordinary call names (export_module.emitted_primitives).
func emittedPrimitives(program map[string]any) []string {
	var out []string
	for _, function := range arr(program["functions"]) {
		for _, node := range nodes(obj(function)["body"]) {
			if node["op"] == "let" && obj(node["value"])["kind"] == "primitive" {
				out = append(out, str2(obj(node["value"])["name"]))
			}
		}
	}
	return setOf(out)
}

func letValues(program map[string]any) []map[string]any {
	var out []map[string]any
	for _, function := range arr(program["functions"]) {
		for _, node := range nodes(obj(function)["body"]) {
			if node["op"] == "let" {
				out = append(out, obj(node["value"]))
			}
		}
	}
	return out
}

func featuresOf(program map[string]any) []string { return typesAny(program["features"]) }

func provenanceNames(program map[string]any) []string { return names(program["provenance"]) }

func assertSet(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(setOf(got), setOf(want)) {
		t.Fatalf("%s: got %v, want %v", what, setOf(got), setOf(want))
	}
}

func assertList(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

func assertSubset(t *testing.T, what string, small, large []string) {
	t.Helper()
	if !subset(small, large) {
		t.Fatalf("%s: %v is not a subset of %v", what, small, large)
	}
}

func assertContains(t *testing.T, what string, list []string, item string) {
	t.Helper()
	if !hasString(list, item) {
		t.Fatalf("%s: %q missing from %v", what, item, list)
	}
}

// importedAttributeProbe changes an attribute of an imported declaration in
// the actual post-import environment: Lean forbids source attributes on
// imported declarations, so this is how the producer's own checks are tested.
func importedAttributeProbe(attribute, name, value string) string {
	return fmt.Sprintf(`open Lean
run_cmd do
  Lean.Elab.Command.liftCoreM do
    let name := "%s".toName
    let some index := (← getEnv).getModuleIdxFor? name
      | throwError "Test requires an imported numeric boundary"
    modifyEnv fun env => %s.ext.toEnvExtension.modifyState env fun state =>
      let entries := (state.importedEntries[index]!).filter (·.1 != name)
      let entries := entries.push (name, %s)
      let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
      { state with importedEntries := state.importedEntries.set! index entries }
`, name, attribute, value)
}

const untrustedExtern = "{ entries := [.standard `all \"untrusted\"] }"

// definitionBaselineProbe isolates the exact-name definition from imported
// constants and axiom caches.
func definitionBaselineProbe(name, safety string) string {
	return fmt.Sprintf("open Lean LeanGo.Module\nrun_cmd Lean.Elab.Command.liftCoreM do\n"+
		"  withEnv (← mkEmptyEnvironment) do\n"+
		"    addDecl <| .axiomDecl { name := `Forged, levelParams := [], type := .sort .zero, isUnsafe := false }\n"+
		"    addDecl <| .defnDecl { name := ``%s, levelParams := [], type := .sort .zero, value := .const `Forged [], hints := .opaque, safety := .%s }\n"+
		"    validateNumericDefinition ``%s\n", name, safety, name)
}

// rewriteSources places the same valid replacement at each relevant
// compilation boundary: local, scoped, late, and late after erasure.
func rewriteSources(prelude, call, erased string) []string {
	return []string{prelude + "section\nattribute [local csimp] replacement_eq\n" + call + "end\n",
		prelude + "namespace Scope\nattribute [scoped csimp] replacement_eq\nend Scope\nopen scoped Scope\n" + call,
		prelude + call + "attribute [csimp] replacement_eq\n",
		prelude + erased + "attribute [csimp] replacement_eq\n"}
}

type historyPrimitive struct {
	name, operand string
	unary         bool
}

// checkPrimitiveHistory rejects every rewrite context of each primitive and
// admits a closed local scope that no longer applies (check_primitive_history).
func checkPrimitiveHistory(t *testing.T, primitives []historyPrimitive, feature string) {
	t.Helper()
	for _, p := range primitives {
		arguments := "a b"
		if p.unary {
			arguments = "a"
		}
		prelude := fmt.Sprintf("@[noinline] def replacement (%s : %s) := %s %s\ntheorem replacement_eq : %s = replacement := rfl\n",
			arguments, p.operand, p.name, arguments, p.name)
		call := fmt.Sprintf("def run (%s : %s) := %s %s\n", arguments, p.operand, p.name, arguments)
		erased := fmt.Sprintf("def run (%s : %s) :=\n  let _unused := %s %s\n  a\n", arguments, p.operand, p.name, arguments)
		for i, source := range rewriteSources(prelude, call, erased) {
			t.Run(fmt.Sprintf("%s/%d", p.name, i), func(t *testing.T) {
				numericReject(t, source, "rewrite|replacement|repeated compilation", numericOptions{})
			})
		}
		clean := numericExport(t, prelude+"section\nattribute [local csimp] replacement_eq\nend\n"+call, numericOptions{})
		assertContains(t, p.name+" closed scope features", featuresOf(clean), feature)
	}
}

func noPrivatePackage(t *testing.T, provenance []string) {
	t.Helper()
	for _, name := range provenance {
		if strings.HasPrefix(name, "_private.Init.Data.UInt.Package.") {
			t.Fatalf("private order package helper %s in provenance", name)
		}
	}
}

// scripts/test_numeric_admission.py

func TestNumericLiteralsPrimitivesAndDecidableBoolLowering(t *testing.T) {
	requireModules(t)
	program := numericExport(t, `inductive Result where
  | mk (arithmetic : UInt32) (eq lt le : Bool)
def run (a b : UInt32) : Result :=
  .mk (((a + 4294967297) - b) * 2) (decide (a = b)) (decide (a < b)) (decide (a ≤ b))
`, numericOptions{modern: true})
	// Only the straight-line let chain of each function, as in the Python test.
	var values []map[string]any
	for _, function := range arr(program["functions"]) {
		code := obj(obj(function)["body"])
		for code["op"] == "let" {
			values = append(values, obj(code["value"]))
			code = obj(code["next"])
		}
	}
	found := false
	var primitives []string
	for _, value := range values {
		if mustJSON(value) == `{"args":[],"kind":"uint32","name":"1"}` {
			found = true
		}
		if value["kind"] == "primitive" {
			primitives = append(primitives, str2(value["name"]))
		}
	}
	if !found {
		t.Fatal("the folded literal 4294967297 must lower to the UInt32 literal 1")
	}
	assertSet(t, "primitives", primitives, "UInt32.add", "UInt32.sub", "UInt32.shiftLeft", "UInt32.decEq", "UInt32.decLt", "UInt32.decLe")
	for _, name := range primitives {
		if functionsOf(program)[name] != nil {
			t.Fatalf("primitive %s exported as a function", name)
		}
	}
	var constructors []string
	for _, ctor := range arr(typesOf(program)["Bool"]["constructors"]) {
		constructors = append(constructors, fmt.Sprintf("%v/%v", obj(ctor)["name"], obj(ctor)["tag"]))
	}
	assertList(t, "Bool constructors", constructors, "Bool.false/0", "Bool.true/1")
}

func TestNumericConditionAndOrderPackageEquality(t *testing.T) {
	requireModules(t)
	for _, modern := range []bool{false, true} {
		program := numericExport(t, "def run (a b : UInt32) : UInt32 :=\n  if a == b then 4294967295 else if a < b then a + b else a - b\n", numericOptions{modern: modern})
		if obj(arr(program["functions"])[0])["result"] != "UInt32" {
			t.Fatal("result type")
		}
		assertContains(t, "types", names(program["types"]), "Bool")
		assertContains(t, "provenance", provenanceNames(program), "instLinearOrderPackageUInt32")
		noPrivatePackage(t, provenanceNames(program))
	}
}

func TestNumericCustomOperatorInstanceFollowsItsAuditedImplementation(t *testing.T) {
	requireModules(t)
	program := numericExport(t, "instance : Add UInt32 := ⟨fun a b => UInt32.sub a b⟩\ndef run (a b : UInt32) : UInt32 := a + b\n", numericOptions{})
	value := obj(obj(functionsOf(program)["Numeric.run"]["body"])["value"])
	if value["kind"] != "primitive" || value["name"] != "UInt32.sub" {
		t.Fatalf("custom instance lowered to %v", value)
	}
}

func TestNumericUnsupportedSourcesPreserveOutputEvenWhenFolded(t *testing.T) {
	requireModules(t)
	for name, body := range map[string]string{
		"other_width": "def run (a : USize) : USize := a\n",
		"custom_foreign_instance": "@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32 → UInt32\n" +
			"instance : Add UInt32 := ⟨foreign⟩\ndef run (a b : UInt32) : UInt32 := a + b\n",
		"signed_division":   "def run (a b : Int32) : Int32 := a / b\n",
		"natural_remainder": "def run (a b : UInt32) : UInt32 := (a.toNat % b.toNat).toUInt32\n",
		"foreign":           "@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32\ndef run (a : UInt32) : UInt32 := foreign a\n",
		"axiom":             "axiom forged : UInt32\ndef run : UInt32 := forged\n",
	} {
		t.Run(name, func(t *testing.T) {
			numericReject(t, body, "extern|axiom|opaque|provenance|Unsupported|not supported by code generator", numericOptions{})
		})
	}
}

func TestNumericContextSensitivePrimitiveAndWrapperReplacementsAreRejected(t *testing.T) {
	requireModules(t)
	addition := "@[noinline] def replacement (a b : UInt32) := UInt32.add a b\ntheorem replacement_eq : UInt32.add = replacement := rfl\n"
	equality := "@[noinline] def replacement (a b : UInt32) : Decidable (a = b) := UInt32.decEq a b\ntheorem replacement_eq : UInt32.decEq = replacement := rfl\n"
	wrapper := "def replacement : Add UInt32 := instAddUInt32\ntheorem replacement_eq : instAddUInt32 = replacement := rfl\n"
	for name, body := range map[string]string{
		"local_primitive":                addition + "section\nattribute [local csimp] replacement_eq\ndef run (a b : UInt32) := a + b\nend\n",
		"scoped_primitive":               addition + "namespace Scope\nattribute [scoped csimp] replacement_eq\nend Scope\nopen scoped Scope\ndef run (a b : UInt32) := a + b\n",
		"late_primitive":                 addition + "def run (a b : UInt32) := a + b\nattribute [csimp] replacement_eq\n",
		"local_order_package_dependency": equality + "section\nattribute [local csimp] replacement_eq\ndef run (a b : UInt32) := a == b\nend\n",
		"late_order_package_dependency":  equality + "def run (a b : UInt32) := a == b\nattribute [csimp] replacement_eq\n",
		"local_notation_wrapper":         wrapper + "section\nattribute [local csimp] replacement_eq\ndef run (a b : UInt32) := a + b\nend\n",
		"late_pristine_rewrite_dependency": "@[noinline] def replacement := @Nat.recCompiled\ntheorem replacement_eq : @Nat.recCompiled = @replacement := rfl\n" +
			"def run (a b : UInt32) := a == b\nattribute [csimp] replacement_eq\n",
		"late_extern":         "def run (a b : UInt32) := a + b\nattribute [extern \"lean_go_untrusted\"] UInt32.add\n",
		"late_implemented_by": addition + "def run (a b : UInt32) := a + b\nattribute [implemented_by replacement] UInt32.add\n",
	} {
		t.Run(name, func(t *testing.T) {
			numericReject(t, body, "rewrite|extern|implemented_by|replacement|repeated compilation", numericOptions{})
		})
	}
}

// scripts/test_uint64_admission.py

func TestUInt64ExactLiteralsAndSevenPrimitiveSignatures(t *testing.T) {
	requireModules(t)
	program := numericExport(t, `structure Result where
  sum : UInt64
  difference : UInt64
  product : UInt64
  shifted : UInt64
  aboveExact : UInt64
  maximum : UInt64
  wrapped : UInt64
  eq : Bool
  lt : Bool
  le : Bool
def run (a b : UInt64) : Result :=
  ⟨a + b, a - b, a * b, a * 8, 9007199254740993,
   18446744073709551615, 18446744073709551617,
   decide (a = b), decide (a < b), decide (a ≤ b)⟩
`, numericOptions{modern: true})
	if program["schema"] != "lean-go-ir/v24" || program["profile"] != "pure-module/v22" {
		t.Fatal("schema or profile")
	}
	if _, ok := obj(program["api"])["records"]; !ok {
		t.Fatal("api has no records")
	}
	var literals, primitives []string
	for _, value := range letValues(program) {
		switch value["kind"] {
		case "uint64":
			literals = append(literals, str2(value["name"]))
		case "primitive":
			primitives = append(primitives, str2(value["name"]))
		}
	}
	assertSubset(t, "literals", []string{"9007199254740993", "18446744073709551615", "1"}, literals)
	assertSet(t, "primitives", primitives, "UInt64.add", "UInt64.sub", "UInt64.mul", "UInt64.shiftLeft", "UInt64.decEq", "UInt64.decLt", "UInt64.decLe")
	for _, name := range primitives {
		if functionsOf(program)[name] != nil {
			t.Fatalf("primitive %s exported as a function", name)
		}
	}
	if hasString(names(program["types"]), "UInt64") {
		t.Fatal("UInt64 exported as a type")
	}
	root := functionsOf(program)["Numeric.run"]
	assertList(t, "root params", typesOfParams(root["params"]), "UInt64", "UInt64")
	for _, node := range nodes(root["body"]) {
		if node["op"] == "let" && obj(node["value"])["kind"] == "primitive" {
			name := str2(obj(node["value"])["name"])
			expected := "UInt64"
			if strings.Contains(name, ".dec") {
				expected = "Bool"
			}
			if obj(node["bind"])["type"] != expected {
				t.Fatalf("%s binds %v", name, obj(node["bind"])["type"])
			}
		}
	}
}

func TestUInt64MixedWidthRecordsWrappersAndJoinKeepSourceTypes(t *testing.T) {
	requireModules(t)
	program := numericExport(t, `structure Narrow where
  value : UInt32
structure Wide where
  value : UInt64
structure State where
  small : Narrow
  large : Wide
@[noinline] def add (a b : UInt64) : UInt64 := a + b
def run (flag : Bool) (state : State) (delta : Wide) : State :=
  let selected := if flag then delta else state.large
  { small := ⟨state.small.value + 1⟩,
    large := ⟨add selected.value state.large.value⟩ }
`, numericOptions{})
	types := typesOf(program)
	for name, fields := range map[string][]string{"Narrow": {"UInt32"}, "Wide": {"UInt64"}, "State": {"Numeric.Narrow", "Numeric.Wide"}} {
		assertList(t, name+" fields", typesAny(obj(arr(types["Numeric."+name]["constructors"])[0])["fields"]), fields...)
	}
	root := functionsOf(program)["Numeric.run"]
	assertList(t, "root params", typesOfParams(root["params"]), "Bool", "Numeric.State", "Numeric.Wide")
	if root["result"] != "Numeric.State" {
		t.Fatal("root result")
	}
	var joins []map[string]any
	for _, node := range nodes(root["body"]) {
		if node["op"] == "join" {
			joins = append(joins, obj(node["join"]))
		}
	}
	if len(joins) != 1 {
		t.Fatalf("the source must retain a compiler join, got %d", len(joins))
	}
	assertList(t, "join params", typesOfParams(joins[0]["params"]), "UInt64")
	assertSet(t, "api records", keysOf(obj(obj(program["api"])["records"])), "Numeric.Narrow", "Numeric.Wide", "Numeric.State")
}

func TestUInt64CapturedAndReducedHelpersSurviveWithWideTypes(t *testing.T) {
	requireModules(t)
	wide := func(s string) string { return strings.ReplaceAll(s, "UInt32", "UInt64") }
	for name, c := range map[string]struct {
		body, suffix string
		params       []string
	}{
		"capture": {localAdd + localScalar, "._lam_0", []string{"UInt64", "UInt64"}},
		"capture_in_reduced_owner": {localAdd + strings.Replace(localScalar, "(bias x y : UInt32)", "(_unused : UInt32) (bias x y : UInt32)", 1),
			"._redArg._lam_0", []string{"UInt64", "UInt64"}},
		"reduced": {reducedAdd + "def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n" + reducedWork,
			"._redArg", []string{"Bool", "UInt64", "UInt64", "UInt64"}},
	} {
		t.Run(name, func(t *testing.T) {
			program := numericExport(t, wide(c.body), numericOptions{})
			helper := functionsOf(program)["Numeric.run"+c.suffix]
			if helper == nil {
				t.Fatalf("helper missing from %v", keysOf(functionsOf(program)))
			}
			assertList(t, "helper params", typesOfParams(helper["params"]), c.params...)
			if helper["result"] != "UInt64" {
				t.Fatal("helper result")
			}
			jsonEqual(t, provenanceOf(program)[str2(helper["name"])], fmt.Sprintf(`{"name":%q,"dependencies":["Numeric.run"],"rewrites":[]}`, helper["name"]))
			called := false
			for _, function := range functionsOf(program) {
				called = called || hasString(callNames(function), str2(helper["name"]))
			}
			if !called {
				t.Fatal("the compiler helper must have a surviving emitted call")
			}
		})
	}
}

func TestUInt64ClassicAndModernImportsPreserveMixedSourceRecords(t *testing.T) {
	requireModules(t)
	model := `namespace Model
structure Packet where
  narrow : UInt32
  wide : UInt64
@[noinline] def update (packet : Packet) : Packet :=
  ⟨packet.narrow + 1, packet.wide + 9007199254740993⟩
end Model
`
	for _, modern := range []bool{false, true} {
		f := newLeanFixture(t)
		modelText, callerText := modelCaller(modern, model, "def run (packet : Model.Packet) : Model.Packet := Model.update packet\n")
		f.write("Model.lean", modelText)
		f.write("Caller.lean", callerText)
		program := f.export("Caller", "run")
		jsonEqual(t, obj(program["api"])["records"], `{"Model.Packet":["narrow","wide"]}`)
		assertList(t, "Packet fields", typesAny(obj(arr(typesOf(program)["Model.Packet"]["constructors"])[0])["fields"]), "UInt32", "UInt64")
		if functionsOf(program)["Model.update"] == nil || provenanceOf(program)["Model.update"] == nil {
			t.Fatal("Model.update must be exported with provenance")
		}
	}
}

func TestUInt64MixedRecordCaptureRetainsCompleteSourceFieldClosure(t *testing.T) {
	requireModules(t)
	body := `structure Settings where
  narrow : UInt32
  wide : UInt64
@[noinline] def run (settings : Settings) (x y : UInt64) : UInt64 :=
  let bump := fun (a : UInt64) =>
    let bias := if settings.narrow == 0 then settings.wide else a
` + strings.ReplaceAll(localChain, "UInt32", "UInt64") + "  add (bump x) (bump y)\n"
	program := numericExport(t, strings.ReplaceAll(localAdd, "UInt32", "UInt64")+body, numericOptions{})
	helper := functionsOf(program)["Numeric.run._lam_0"]
	if helper == nil {
		t.Fatalf("helper missing from %v", keysOf(functionsOf(program)))
	}
	assertList(t, "helper params", typesOfParams(helper["params"]), "Numeric.Settings", "UInt64")
	assertList(t, "Settings fields", typesAny(obj(arr(typesOf(program)["Numeric.Settings"]["constructors"])[0])["fields"]), "UInt32", "UInt64")
	if !hasString(callNames(functionsOf(program)["Numeric.run"]), str2(helper["name"])) {
		t.Fatal("helper not called by its owner")
	}
}

func TestUInt64CustomAddInstanceFollowsItsActualImplementation(t *testing.T) {
	requireModules(t)
	program := numericExport(t, `structure Result where
  custom : UInt64
  direct : UInt64
instance : Add UInt64 := ⟨fun a b => UInt64.sub a b⟩
def run (a b : UInt64) : Result := ⟨a + b, UInt64.add a b⟩
`, numericOptions{})
	var operations []string
	for _, node := range nodes(functionsOf(program)["Numeric.run"]["body"]) {
		if node["op"] == "let" && obj(node["value"])["kind"] == "primitive" {
			operations = append(operations, str2(obj(node["value"])["name"]))
		}
	}
	assertList(t, "operations", operations, "UInt64.sub", "UInt64.add")
}

func TestUInt64OrderPackageAndDirectShiftAreAdmitted(t *testing.T) {
	requireModules(t)
	for _, modern := range []bool{false, true} {
		program := numericExport(t, "def run (a b : UInt64) : UInt64 :=\n  if a == b then UInt64.shiftLeft a b else if a < b then a + b else a - b\n", numericOptions{modern: modern})
		assertContains(t, "provenance", provenanceNames(program), "instLinearOrderPackageUInt64")
		noPrivatePackage(t, provenanceNames(program))
		if functionsOf(program)["Numeric.run"]["result"] != "UInt64" {
			t.Fatal("result type")
		}
	}
}

func TestUInt64UnsupportedNumericDependenciesRejectEvenAfterErasure(t *testing.T) {
	requireModules(t)
	for name, body := range map[string]string{
		"signed_division":          "def run (a b : Int64) : Int64 := a / b\n",
		"erased_natural_remainder": "def run (a b : UInt64) : UInt64 :=\n  let _unused := a.toNat % b.toNat\n  a\n",
		"platform_conversion":      "def run (a : UInt64) : USize := a.toUSize\n",
		"custom_foreign_instance": "@[extern \"lean_go_untrusted\"] opaque foreign : UInt64 → UInt64 → UInt64\n" +
			"instance : Add UInt64 := ⟨foreign⟩\ndef run (a b : UInt64) : UInt64 := a + b\n",
	} {
		t.Run(name, func(t *testing.T) {
			numericReject(t, body, "extern|axiom|opaque|provenance|Unsupported|unsupported", numericOptions{})
		})
	}
}

func TestUInt64ContextualAndLatePrimitiveAndSupportMutationsReject(t *testing.T) {
	requireModules(t)
	addition := "@[noinline] def replacement (a b : UInt64) := UInt64.add a b\ntheorem replacement_eq : UInt64.add = replacement := rfl\n"
	equality := "@[noinline] def replacement (a b : UInt64) : Decidable (a = b) := UInt64.decEq a b\ntheorem replacement_eq : UInt64.decEq = replacement := rfl\n"
	wrapper := "def replacement : Add UInt64 := instAddUInt64\ntheorem replacement_eq : instAddUInt64 = replacement := rfl\n"
	call := "def run (a b : UInt64) := a + b\n"
	erased := "def run (a b : UInt64) :=\n  let _unused := a + b\n  a\n"
	for name, body := range map[string]string{
		"local_primitive":        addition + "section\nattribute [local csimp] replacement_eq\n" + call + "end\n",
		"scoped_primitive":       addition + "namespace Scope\nattribute [scoped csimp] replacement_eq\nend Scope\nopen scoped Scope\n" + call,
		"late_primitive":         addition + call + "attribute [csimp] replacement_eq\n",
		"erased_late_primitive":  addition + erased + "attribute [csimp] replacement_eq\n",
		"local_wrapper":          wrapper + "section\nattribute [local csimp] replacement_eq\n" + call + "end\n",
		"late_order_dependency":  equality + "def run (a b : UInt64) := a == b\nattribute [csimp] replacement_eq\n",
		"local_order_dependency": equality + "section\nattribute [local csimp] replacement_eq\ndef run (a b : UInt64) := a == b\nend\n",
		"late_pristine_rewrite_dependency": "@[noinline] def replacement := @Nat.recCompiled\ntheorem replacement_eq : @Nat.recCompiled = @replacement := rfl\n" +
			"def run (a b : UInt64) := a == b\nattribute [csimp] replacement_eq\n",
	} {
		t.Run(name, func(t *testing.T) {
			numericReject(t, body, "rewrite|extern|implemented_by|replacement|initializ|repeated compilation", numericOptions{})
		})
	}
}

func TestUInt64ImportedPrimitiveAttributesRejectEvenAfterErasure(t *testing.T) {
	requireModules(t)
	prefix := "@[noinline] def replacement (a b : UInt64) := UInt64.add a b\ndef initializeAdd : IO (UInt64 → UInt64 → UInt64) := pure UInt64.add\n"
	for name, c := range map[string][2]string{
		"extern":         {"externAttr", "{ entries := [.standard `all \"lean_go_untrusted\"] }"},
		"implemented_by": {"Compiler.implementedByAttr", "`Numeric.replacement"},
		"initializer":    {"regularInitAttr", "`Numeric.initializeAdd"},
	} {
		for _, erased := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/erased=%v", name, erased), func(t *testing.T) {
				body := "def run (a b : UInt64) := a + b\n"
				if erased {
					body = "def run (a b : UInt64) := \n  let _unused := a + b\n  a\n"
				}
				numericReject(t, prefix+body, "Numeric source .*changed|numeric primitive",
					numericOptions{probe: importedAttributeProbe(c[0], "UInt64.add", c[1])})
			})
		}
	}
}

func TestUInt64LiteralAndPublicOrderPackageMutationsReject(t *testing.T) {
	requireModules(t)
	for name, body := range map[string]string{
		"UInt64.ofNat":                 "def run : UInt64 := 9007199254740993\n",
		"instLinearOrderPackageUInt64": "def run (a b : UInt64) : Bool := a == b\n",
		"instLinearOrderPackageUInt32": "def run (a b : UInt32) : Bool := a == b\n",
	} {
		t.Run(name, func(t *testing.T) {
			numericReject(t, body, "Numeric source extern changed",
				numericOptions{probe: importedAttributeProbe("externAttr", name, "{ entries := [.standard `all \"lean_go_untrusted\"] }")})
		})
	}
}
