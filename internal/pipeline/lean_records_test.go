package pipeline

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1): admission of records, named APIs and joins,
// the accepted module forms, and exports with several roots.

// nsSource wraps body in a namespace, optionally as a modern module.
func nsSource(namespace, body string, modern bool, probe string) string {
	prefix := ""
	if modern {
		prefix = "module\npublic section\n"
	}
	if probe != "" {
		body += "\nopen Lean LeanGo.Module Lean.Compiler.LCNF\nrun_cmd do\n  Lean.Elab.Command.liftCoreM do\n" + probe + "\n"
	}
	return prefix + "namespace " + namespace + "\n" + body + "\nend " + namespace + "\n"
}

func recordsExport(f *leanFixture, body string, modern bool) map[string]any {
	f.t.Helper()
	f.write("Records.lean", nsSource("Records", body, modern, ""))
	return f.export("Records", "Records.run")
}

func recordsReject(f *leanFixture, body, pattern string) {
	f.t.Helper()
	f.write("Records.lean", nsSource("Records", body, false, ""))
	f.reject("Records", "Records.run", pattern)
}

// recordNodes is RecordAdmissionTests.nodes: lets and cases, not join bodies.
func recordNodes(code any) []map[string]any {
	node := obj(code)
	out := []map[string]any{node}
	switch node["op"] {
	case "let":
		out = append(out, recordNodes(node["next"])...)
	case "cases":
		for _, alt := range arr(node["alts"]) {
			out = append(out, recordNodes(obj(alt)["body"])...)
		}
	}
	return out
}

func assertSourceBoundary(t *testing.T, function map[string]any, sourceType string) {
	t.Helper()
	if !slices.Equal(typesOfParams(function["params"]), []string{sourceType}) || function["result"] != sourceType {
		t.Fatalf("%s signature %v -> %v", function["name"], typesOfParams(function["params"]), function["result"])
	}
	var unwrapped, rebuilt bool
	for _, node := range recordNodes(function["body"]) {
		if node["op"] == "cases" {
			if node["type"] == sourceType {
				unwrapped = true
			}
			if node["result"] != sourceType {
				t.Fatalf("cases result %v", node["result"])
			}
		}
		if node["op"] == "let" {
			value := obj(node["value"])
			if value["kind"] == "constructor" && value["name"] == sourceType+".mk" && obj(node["bind"])["type"] == sourceType {
				rebuilt = true
			}
		}
	}
	if !unwrapped || !rebuilt {
		t.Fatalf("%s must be unwrapped (%v) and reconstructed (%v)", sourceType, unwrapped, rebuilt)
	}
}

func assertTypedBoundaries(t *testing.T, program map[string]any) {
	t.Helper()
	constructors := map[string][]string{}
	for _, declaration := range arr(program["types"]) {
		for _, c := range arr(obj(declaration)["constructors"]) {
			var fields []string
			for _, field := range arr(obj(c)["fields"]) {
				fields = append(fields, field.(string))
			}
			constructors[str2(obj(c)["name"])] = fields
		}
	}
	functions := functionsOf(program)
	for _, function := range functions {
		all := recordNodes(function["body"])
		binders := arr(function["params"])
		for _, node := range all {
			switch node["op"] {
			case "let":
				binders = append(binders, node["bind"])
			case "cases":
				for _, alt := range arr(node["alts"]) {
					binders = append(binders, arr(obj(alt)["params"])...)
					if !slices.Equal(typesOfParams(obj(alt)["params"]), constructors[str2(obj(alt)["constructor"])]) {
						t.Fatalf("alternative %v params", obj(alt)["constructor"])
					}
				}
			}
		}
		types := map[float64]string{}
		for _, b := range binders {
			types[obj(b)["id"].(float64)] = str2(obj(b)["type"])
		}
		if len(types) != len(binders) {
			t.Fatal("adapter binders must be fresh")
		}
		for _, node := range all {
			if node["op"] != "let" {
				continue
			}
			value := obj(node["value"])
			var expected []string
			switch value["kind"] {
			case "constructor":
				expected = constructors[str2(value["name"])]
			case "call":
				target := functions[str2(value["name"])]
				expected = typesOfParams(target["params"])
				if obj(node["bind"])["type"] != target["result"] {
					t.Fatalf("call result type of %v", value["name"])
				}
			default:
				continue
			}
			var got []string
			for _, id := range ids(value["args"]) {
				got = append(got, types[id])
			}
			if !slices.Equal(got, expected) {
				t.Fatalf("%v args %v, want %v", value["name"], got, expected)
			}
		}
	}
}

func assertSourceProvenance(t *testing.T, program map[string]any, required ...string) {
	t.Helper()
	records := keysOf(provenanceOf(program))
	if !subset(required, records) || !subset(keysOf(functionsOf(program)), records) {
		t.Fatalf("provenance %v lacks %v or functions", records, required)
	}
	if program["schema"] != "lean-go-ir/v24" || program["profile"] != "pure-module/v22" {
		t.Fatal("identity")
	}
	assertTypedBoundaries(t, program)
}

func setOf(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)
	return slices.Compact(out)
}

func TestRejectionHelperRefusesMalformedReplacementFixture(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	f.write("Records.lean", nsSource("Records", "def run (a b : UInt64) := replacement a b\n", false, ""))
	_, err := Generate(GenerateOptions{SourceDir: f.source, Module: "Records", Roots: []string{"Records.run"}, Out: f.pkg, IROutput: f.ir, Cache: testCache(t)})
	if err == nil || checkRejection(err, "replacement") == nil || !strings.Contains(checkRejection(err, "replacement").Error(), "Regex matched") {
		t.Fatalf("an elaboration failure passed as a rejection: %v", err)
	}
}

func TestRejectionGuardCompilerErrorsCannotMasquerade(t *testing.T) {
	for _, message := range []string{"error(lean.unknownIdentifier): Unknown identifier `replacement`", "error(lean.typeMismatch): replacement",
		"error: Unknown attribute replacement", "error: Unknown constant replacement", "error: Type mismatch for replacement",
		"error: unexpected token replacement"} {
		err := checkRejection(exportError(message), "replacement")
		if err == nil || !strings.Contains(err.Error(), "Regex matched") {
			t.Errorf("%q passed as an audit rejection", message)
		}
	}
	if err := checkRejection(exportError("Unsupported compiler replacement"), "replacement"); err != nil {
		t.Fatal(err)
	}
}

func TestSingleFieldParameterReturnAndNoinlineCallKeepSourceTypes(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), `structure Count where
  value : UInt32
@[noinline] def increment (x : Count) : Count := ⟨x.value + 1⟩
def run (x : Count) : Count := increment x
`, false)
	functions := functionsOf(program)
	if !slices.Equal(setOf(keysOf(functions)), []string{"Records.increment", "Records.run"}) {
		t.Fatal(keysOf(functions))
	}
	for _, function := range functions {
		assertSourceBoundary(t, function, "Records.Count")
	}
	assertSourceProvenance(t, program, "Records.run", "Records.increment", "Records.Count.value")
	jsonEqual(t, typesOf(program)["Records.Count"]["constructors"], `[{"name":"Records.Count.mk","tag":0,"fields":["UInt32"]}]`)
}

func TestNestedNominalBoolFieldsAndRecordUpdateKeepOriginalSchema(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), `structure Count where
  value : UInt32
structure Other where
  value : UInt32
structure Nested where
  value : Count
structure Flag where
  value : Bool
structure Pair where
  count : Count
  other : Other
  nested : Nested
  enabled : Flag
@[noinline] def combine (x : Count) (y : Other) : Other := ⟨x.value + y.value⟩
@[noinline] def bumpNested (x : Nested) : Nested := ⟨⟨x.value.value + 3⟩⟩
@[noinline] def toggle (x : Flag) : Flag := ⟨!x.value⟩
def run (x : Pair) : Pair :=
  { x with
    other := combine x.count x.other
    nested := bumpNested x.nested
    enabled := toggle x.enabled }
`, false)
	types := typesOf(program)
	expected := map[string][]string{
		"Records.Count": {"UInt32"}, "Records.Other": {"UInt32"}, "Records.Nested": {"Records.Count"},
		"Records.Flag": {"Bool"}, "Records.Pair": {"Records.Count", "Records.Other", "Records.Nested", "Records.Flag"},
	}
	if !slices.Equal(setOf(keysOf(types)), setOf(append(keysOf(expected), "Bool"))) {
		t.Fatal(keysOf(types))
	}
	for name, fields := range expected {
		jsonEqual(t, types[name]["constructors"], fmt.Sprintf(`[{"name":%q,"tag":0,"fields":%s}]`, name+".mk", mustJSON(fields)))
	}
	functions := functionsOf(program)
	required := []string{"Records.run", "Records.combine", "Records.bumpNested", "Records.toggle"}
	if !subset(required, keysOf(functions)) || !subset(keysOf(functions), append(required, "Bool.Internal.not")) {
		t.Fatal(keysOf(functions))
	}
	assertSourceBoundary(t, functions["Records.bumpNested"], "Records.Nested")
	assertSourceBoundary(t, functions["Records.toggle"], "Records.Flag")
	helper := functions["Records.combine"]
	if !slices.Equal(typesOfParams(helper["params"]), []string{"Records.Count", "Records.Other"}) || helper["result"] != "Records.Other" {
		t.Fatal("combine signature")
	}
	root := functions["Records.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"Records.Pair"}) || root["result"] != "Records.Pair" {
		t.Fatal("run signature")
	}
	var reconstructed []string
	var pairCase map[string]any
	for _, node := range recordNodes(root["body"]) {
		if node["op"] == "cases" && node["type"] == "Records.Pair" && pairCase == nil {
			pairCase = node
		}
		if node["op"] == "let" && obj(node["value"])["kind"] == "constructor" {
			reconstructed = append(reconstructed, str2(obj(node["value"])["name"]))
		}
	}
	if pairCase == nil || !slices.Equal(typesOfParams(obj(arr(pairCase["alts"])[0])["params"]), expected["Records.Pair"]) {
		t.Fatal("pair case")
	}
	for name := range expected {
		if !hasString(reconstructed, name+".mk") {
			t.Fatalf("%s not reconstructed", name)
		}
	}
	assertSourceProvenance(t, program, "Records.run", "Records.combine", "Records.Count.value", "Records.Other.value",
		"Records.Nested.value", "Records.Flag.value", "Records.Pair.count", "Records.Pair.other", "Records.Pair.nested", "Records.Pair.enabled")
}

func TestWrapperAroundRecursiveSumKeepsSumAndWrapperTypes(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), `inductive Tree where
  | leaf (value : UInt32)
  | branch (left right : Tree)
structure Box where
  value : Tree
@[noinline] def transform (x : Box) : Box :=
  match x.value with
  | .leaf value => ⟨.leaf (value + 1)⟩
  | .branch left right => ⟨.branch right left⟩
def run (x : Box) : Box := transform x
`, false)
	types := typesOf(program)
	if !slices.Equal(setOf(keysOf(types)), []string{"Records.Box", "Records.Tree"}) {
		t.Fatal(keysOf(types))
	}
	jsonEqual(t, types["Records.Box"]["constructors"], `[{"name":"Records.Box.mk","tag":0,"fields":["Records.Tree"]}]`)
	jsonEqual(t, types["Records.Tree"]["constructors"], `[{"name":"Records.Tree.leaf","tag":0,"fields":["UInt32"]},{"name":"Records.Tree.branch","tag":1,"fields":["Records.Tree","Records.Tree"]}]`)
	functions := functionsOf(program)
	if !slices.Equal(setOf(keysOf(functions)), []string{"Records.run", "Records.transform"}) {
		t.Fatal(keysOf(functions))
	}
	for _, function := range functions {
		assertSourceBoundary(t, function, "Records.Box")
	}
	var treeCase map[string]any
	for _, node := range recordNodes(functions["Records.transform"]["body"]) {
		if node["op"] == "cases" && node["type"] == "Records.Tree" {
			treeCase = node
			break
		}
	}
	if treeCase == nil || treeCase["result"] != "Records.Box" {
		t.Fatal("tree case")
	}
	var constructors []string
	for _, alt := range arr(treeCase["alts"]) {
		constructors = append(constructors, str2(obj(alt)["constructor"]))
	}
	if !slices.Equal(constructors, []string{"Records.Tree.leaf", "Records.Tree.branch"}) {
		t.Fatal(constructors)
	}
	assertSourceProvenance(t, program, "Records.run", "Records.transform", "Records.Box.value")
}

func TestRecursiveSingleFieldStaysAnOrdinaryRecursiveADT(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), "inductive Loop where\n  | next : Loop → Loop\ndef run (x : Loop) : Loop := x\n", false)
	jsonEqual(t, program["types"], `[{"name":"Records.Loop","constructors":[{"name":"Records.Loop.next","tag":0,"fields":["Records.Loop"]}]}]`)
	function := obj(arr(program["functions"])[0])
	if !slices.Equal(typesOfParams(function["params"]), []string{"Records.Loop"}) || function["result"] != "Records.Loop" {
		t.Fatal("signature")
	}
	jsonEqual(t, function["body"], fmt.Sprintf(`{"op":"return","var":%v}`, paramIDs(function["params"])[0]))
}

func modelCaller(modern bool, model, caller string) (string, string) {
	prefix, header := "", "import Model\n"
	if modern {
		prefix, header = "module\npublic section\n", "module\npublic import Model\npublic section\n"
	}
	return prefix + model, header + caller
}

func TestClassicAndModernImportedRecordHelpersKeepSourceProvenance(t *testing.T) {
	requireModules(t)
	for _, modern := range []bool{false, true} {
		f := newLeanFixture(t)
		model, caller := modelCaller(modern, `namespace Model
structure Count where
  value : UInt32
@[noinline] def increment (x : Count) : Count := ⟨x.value + 1⟩
end Model
`, `namespace Caller
def run (x : Model.Count) : Model.Count := Model.increment x
end Caller
`)
		f.write("Model.lean", model)
		f.write("Caller.lean", caller)
		program := f.export("Caller", "Caller.run")
		functions := functionsOf(program)
		if !slices.Equal(setOf(keysOf(functions)), []string{"Caller.run", "Model.increment"}) {
			t.Fatal(keysOf(functions))
		}
		for _, function := range functions {
			assertSourceBoundary(t, function, "Model.Count")
		}
		jsonEqual(t, program["types"], `[{"name":"Model.Count","constructors":[{"name":"Model.Count.mk","tag":0,"fields":["UInt32"]}]}]`)
		if !hasKeys(obj(program["inputs"]), "source/Model.lean", "source/Caller.lean") {
			t.Fatal("inputs")
		}
		assertSourceProvenance(t, program, "Caller.run", "Model.increment", "Model.Count.value")
	}
}

func TestUnsupportedSourceFieldsPreserveOutput(t *testing.T) {
	requireModules(t)
	for name, c := range map[string][2]string{
		"proof_field":       {"structure Checked where\n  value : UInt32\n  witness : value = value\ndef run (x : Checked) : Checked := x\n", "Unsupported|unsupported|proof|source field|wrapper"},
		"generic":           {"structure Generic (α : Type) where\n  value : α\ndef run (x : Generic UInt32) : Generic UInt32 := x\n", "monomorphic|Unsupported|unsupported|parameter|universe"},
		"dependent":         {"structure Dependent where\n  count : Nat\n  value : Fin count\ndef run (x : Dependent) : Dependent := x\n", "monomorphic|Dependent|dependent|Unsupported|unsupported"},
		"unsupported_width": {"structure Wide where\n  value : USize\ndef run (x : Wide) : Wide := x\n", "USize|Unsupported|unsupported"},
	} {
		t.Run(name, func(t *testing.T) { recordsReject(newLeanFixture(t), c[0], c[1]) })
	}
}

func TestWrapperDepthBoundaryUsesBoundedAdmission(t *testing.T) {
	requireModules(t)
	body := "set_option maxRecDepth 4096\n"
	field := "UInt32"
	for depth := 0; depth < 64; depth++ {
		body += fmt.Sprintf("structure Wrapper%d where\n  value : %s\n", depth, field)
		field = fmt.Sprintf("Wrapper%d", depth)
	}
	f := newLeanFixture(t)
	program := recordsExport(f, body+fmt.Sprintf("def run (x : %s) : %s := x\n", field, field), false)
	if len(arr(program["types"])) != 64 {
		t.Fatal(len(arr(program["types"])))
	}
	assertSourceBoundary(t, obj(arr(program["functions"])[0]), "Records.Wrapper63")
	assertSourceProvenance(t, program, "Records.run")
	body += fmt.Sprintf("structure Wrapper64 where\n  value : %s\n", field)
	body += "def run (x : Wrapper64) : Wrapper64 := x\n"
	recordsReject(f, body, "[Ww]rapper.*(depth|limit)|representation.*(depth|limit)")
}

func TestForeignAndAxiomaticRecordHelpersPreserveOutput(t *testing.T) {
	requireModules(t)
	record := "structure Count where\n  value : UInt32\n"
	for name, c := range map[string][2]string{
		"foreign": {"@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32\n@[noinline] def helper (x : Count) : Count := ⟨foreign x.value⟩\n", "extern|External function|opaque"},
		"axiom":   {"axiom forged : True\n@[noinline] def helper (x : Count) : Count :=\n  let _proof : True := forged\n  x\n", "[Aa]xiom|provenance"},
	} {
		t.Run(name, func(t *testing.T) {
			recordsReject(newLeanFixture(t), record+c[0]+"def run (x : Count) : Count := helper x\n", c[1])
		})
	}
}

// rewriteContexts places a root under local, scoped or late csimp rewrites.
func rewriteContexts(root string) map[string]string {
	return map[string]string{
		"local":  "section\nattribute [local csimp] replacement_eq\n" + root + "end\n",
		"scoped": "namespace RewriteScope\nattribute [scoped csimp] replacement_eq\nend RewriteScope\nopen scoped RewriteScope\n" + root,
		"late":   root + "attribute [csimp] replacement_eq\n",
	}
}

func TestContextSensitiveAccessorAndHelperRewritesAreRejected(t *testing.T) {
	requireModules(t)
	record := "structure Count where\n  value : UInt32\n"
	for _, target := range []string{"Count.value", "helper"} {
		definitions := record
		if target == "helper" {
			definitions += "@[noinline] def helper (x : Count) : UInt32 := x.value\n"
		}
		definitions += fmt.Sprintf("@[noinline] def replacement (x : Count) : UInt32 := %s x\ntheorem replacement_eq : %s = replacement := rfl\n", target, target)
		root := fmt.Sprintf("def run (x : Count) : UInt32 := %s x\n", target)
		for context, body := range rewriteContexts(root) {
			t.Run(target+"/"+context, func(t *testing.T) {
				recordsReject(newLeanFixture(t), definitions+body, "rewrite|replacement|repeated compilation")
			})
		}
	}
}

// Named APIs.

func TestNamedNestedInheritedFieldsAndEnums(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), `structure Base where
  first : UInt32
  second : UInt32
structure Nested where
  base : Base
structure Child extends Base where
  nested : Nested
  enabled : Bool
inductive Mode where
  | ready
  | done
structure Envelope where
  child : Child
  mode : Mode
def run (x : Envelope) : Envelope := x
`, false)
	if program["schema"] != "lean-go-ir/v24" || program["profile"] != "pure-module/v22" {
		t.Fatal("identity")
	}
	jsonEqual(t, program["api"], `{"records":{"Records.Base":["first","second"],"Records.Nested":["base"],"Records.Child":["toBase","nested","enabled"],"Records.Envelope":["child","mode"]}}`)
	types := typesOf(program)
	jsonEqual(t, types["Records.Child"]["constructors"], `[{"name":"Records.Child.mk","tag":0,"fields":["Records.Base","Records.Nested","Bool"]}]`)
	jsonEqual(t, types["Records.Base"]["constructors"], `[{"name":"Records.Base.mk","tag":0,"fields":["UInt32","UInt32"]}]`)
	if !slices.Equal(setOf(names(types["Records.Mode"]["constructors"])), []string{"Records.Mode.done", "Records.Mode.ready"}) {
		t.Fatal("mode constructors")
	}
	if _, ok := obj(obj(program["api"])["records"])["Bool"]; ok {
		t.Fatal("Bool has record metadata")
	}
}

func TestNumericBooleanRecursiveSumKeepsOriginalAdmission(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), `inductive Tree where
  | leaf (value : UInt32) (flag : Bool)
  | branch (left right : Tree)
@[noinline] def run (x : Tree) : Tree :=
  match x with
  | .leaf value flag => .leaf (value + 1) (!flag)
  | .branch left right => .branch (run left) (run right)
`, true)
	jsonEqual(t, program["api"], `{"records":{}}`)
	types := typesOf(program)
	if !slices.Equal(setOf(keysOf(types)), []string{"Bool", "Records.Tree"}) {
		t.Fatal(keysOf(types))
	}
	jsonEqual(t, types["Records.Tree"]["constructors"], `[{"name":"Records.Tree.leaf","tag":0,"fields":["UInt32","Bool"]},{"name":"Records.Tree.branch","tag":1,"fields":["Records.Tree","Records.Tree"]}]`)
	root := functionsOf(program)["Records.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"Records.Tree"}) || root["result"] != "Records.Tree" {
		t.Fatal("signature")
	}
}

func TestSingleConstructorInductiveIsNotRecordMetadata(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), "inductive Box where\n  | mk (value : UInt32)\ndef run (x : Box) : Box := x\n", false)
	jsonEqual(t, program["api"], `{"records":{}}`)
	jsonEqual(t, program["types"], `[{"name":"Records.Box","constructors":[{"name":"Records.Box.mk","tag":0,"fields":["UInt32"]}]}]`)
}

// Joins.

const joinPrelude = `structure Count where
  value : UInt32
structure State where
  count : Count
  enabled : Bool
@[noinline] def add (x y : UInt32) : UInt32 := x + y
`

func joinsOf(function map[string]any) []map[string]any {
	var out []map[string]any
	for _, node := range nodes(function["body"]) {
		if node["op"] == "join" {
			out = append(out, obj(node["join"]))
		}
	}
	return out
}

func assertUniqueIDs(t *testing.T, function map[string]any) {
	t.Helper()
	all := paramIDs(function["params"])
	for _, node := range nodes(function["body"]) {
		switch node["op"] {
		case "let":
			all = append(all, obj(node["bind"])["id"].(float64))
		case "join":
			join := obj(node["join"])
			all = append(all, join["id"].(float64))
			all = append(all, paramIDs(join["params"])...)
		case "cases":
			for _, alt := range arr(node["alts"]) {
				all = append(all, paramIDs(obj(alt)["params"])...)
			}
		}
	}
	seen := map[float64]bool{}
	for _, id := range all {
		if id <= 0 || seen[id] {
			t.Fatalf("join and value IDs must be positive and share one namespace: %v", all)
		}
		seen[id] = true
	}
}

func TestConditionalRecordUpdateKeepsCapturedState(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), joinPrelude+`def run (b : Bool) (state : State) (delta : Count) : State :=
  let count := if b then delta else state.count
  { state with count := ⟨add count.value state.count.value⟩ }
`, false)
	function := functionsOf(program)["Records.run"]
	joins := joinsOf(function)
	if len(joins) != 1 {
		t.Fatalf("the example must exercise one compiler join, got %d", len(joins))
	}
	join := joins[0]
	if !slices.Equal(typesOfParams(join["params"]), []string{"UInt32"}) || function["result"] != "Records.State" {
		t.Fatal("join or result types")
	}
	captured := paramIDs(function["params"])[1]
	body := nodes(join["body"])
	var casesState, callsAdd bool
	for _, node := range body {
		if node["op"] == "cases" && node["var"] == captured {
			casesState = true
		}
		if node["op"] == "let" && obj(node["value"])["kind"] == "call" && obj(node["value"])["name"] == "Records.add" {
			callsAdd = true
		}
	}
	jumps := 0
	for _, node := range nodes(function["body"]) {
		if node["op"] == "jump" && node["target"] == join["id"] {
			jumps++
		}
	}
	if !casesState || !callsAdd || jumps != 2 {
		t.Fatal(casesState, callsAdd, jumps)
	}
	if !hasString(names(program["functions"]), "Records.add") || !hasString(names(program["provenance"]), "Records.add") {
		t.Fatal("add")
	}
	assertUniqueIDs(t, function)
}

func TestConditionalAndMatchResultsRebuildSourceWrapper(t *testing.T) {
	requireModules(t)
	for name, c := range map[string][2]string{
		"conditional": {"(b : Bool) (x y bias : Count)", "if b then x else y"},
		"match":       {"(choice : Selection) (bias : Count)", "match choice with | .left value => value | .right value => value"},
	} {
		t.Run(name, func(t *testing.T) {
			program := recordsExport(newLeanFixture(t), joinPrelude+fmt.Sprintf(`inductive Selection where
  | left (value : Count)
  | right (value : Count)
def run %s : Count :=
  let selected := %s
  let first := add selected.value bias.value
  let second := add first bias.value
  let third := add second bias.value
  ⟨add third bias.value⟩
`, c[0], c[1]), false)
			function := functionsOf(program)["Records.run"]
			joins := joinsOf(function)
			if len(joins) != 1 || !slices.Equal(typesOfParams(joins[0]["params"]), []string{"UInt32"}) || function["result"] != "Records.Count" {
				t.Fatal("join")
			}
			for _, node := range nodes(function["body"]) {
				if node["op"] == "cases" && node["result"] != "Records.Count" {
					t.Fatal("cases result")
				}
			}
			body := nodes(joins[0]["body"])
			var wrappers []map[string]any
			for _, node := range body {
				if node["op"] == "let" && obj(node["value"])["name"] == "Records.Count.mk" {
					wrappers = append(wrappers, node)
				}
			}
			if len(wrappers) != 1 {
				t.Fatal(len(wrappers))
			}
			jsonEqual(t, body[len(body)-1], fmt.Sprintf(`{"op":"return","var":%v}`, obj(wrappers[0]["bind"])["id"]))
			assertUniqueIDs(t, function)
		})
	}
}

func TestNestedJoinsCanJumpToPriorTargetsAndCaptureValues(t *testing.T) {
	requireModules(t)
	program := recordsExport(newLeanFixture(t), joinPrelude+`def run (a b : Bool) (x y z bias : UInt32) : UInt32 :=
  let first := if a then x else y
  let second := if b then first else z
  let one := add second bias
  let two := add one bias
  let three := add two bias
  add three bias
`, false)
	function := functionsOf(program)["Records.run"]
	joins := joinsOf(function)
	if len(joins) != 2 {
		t.Fatal(len(joins))
	}
	outer, inner := joins[0], joins[1]
	jumpsOuter := false
	for _, node := range nodes(inner["body"]) {
		if node["op"] == "jump" && node["target"] == outer["id"] {
			jumpsOuter = true
		}
	}
	if !jumpsOuter {
		t.Fatal("inner join does not jump to the outer target")
	}
	params := paramIDs(function["params"])
	bias := params[len(params)-1]
	var lets []map[string]any
	for _, node := range nodes(outer["body"]) {
		if node["op"] == "let" {
			lets = append(lets, obj(node["value"]))
		}
	}
	if len(lets) != 4 {
		t.Fatal(len(lets))
	}
	for _, call := range lets {
		if call["name"] != "Records.add" || !slices.Contains(ids(call["args"]), bias) {
			t.Fatal(call)
		}
	}
	assertUniqueIDs(t, function)
}

func TestForeignAxiomAndRewriteInsideContinuationPreserveOutput(t *testing.T) {
	requireModules(t)
	continuation := `def run (b : Bool) (x y bias : UInt32) : UInt32 :=
  let selected := if b then x else y
  let first := add selected bias
  let second := add first bias
  let third := add second bias
  helper third
`
	helper := "@[noinline] def helper (x : UInt32) : UInt32 := x\n"
	baseline := functionsOf(recordsExport(newLeanFixture(t), joinPrelude+helper+continuation, false))["Records.run"]
	joins := joinsOf(baseline)
	found := false
	for _, node := range nodes(joins[0]["body"]) {
		if node["op"] == "let" && obj(node["value"])["name"] == "Records.helper" {
			found = true
		}
	}
	if !found {
		t.Fatal("helper call is not inside the join")
	}
	for name, c := range map[string][2]string{
		"foreign": {"@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32\n@[noinline] def helper (x : UInt32) : UInt32 := foreign x\n" + continuation, "extern|External function|opaque"},
		"axiom":   {"axiom forged : True\n@[noinline] def helper (x : UInt32) : UInt32 :=\n  let _proof : True := forged\n  x\n" + continuation, "[Aa]xiom|provenance"},
		"local_rewrite": {helper + "@[noinline] def replacement (x : UInt32) : UInt32 := helper x\n" + "theorem replacement_eq : helper = replacement := rfl\n" +
			"section\nattribute [local csimp] replacement_eq\n" + continuation + "end\n", "rewrite|replacement|repeated compilation"},
		"late_foreign": {helper + continuation + "attribute [extern \"lean_go_untrusted\"] helper\n", "extern"},
	} {
		t.Run(name, func(t *testing.T) { recordsReject(newLeanFixture(t), joinPrelude+c[0], c[1]) })
	}
}

func TestCapturedClosureRemainsRejected(t *testing.T) {
	requireModules(t)
	recordsReject(newLeanFixture(t), joinPrelude+`@[noinline] def apply (f : UInt32 → UInt32) (value : UInt32) : UInt32 :=
  f value
def run (bias value : UInt32) : UInt32 :=
  let captured := fun x => x + bias
  apply captured value
`, "Local helper requires saturated direct calls")
}

// Accepted module forms.

func modernSources(f *leanFixture) {
	f.write("Modern/Model.lean", `module
public section
namespace Modern
inductive Signal where
  | yes : Bool → Signal
  | no
@[noinline] def negate (b : Bool) : Bool := !b
end Modern
`)
	f.write("Modern/Transform.lean", `module
public import Modern.Model
public section
namespace Modern
def choose (flag : Bool) : Signal := .yes (negate flag)
def constant : Signal := .no
def select (left : Signal) (right : Signal) : Signal :=
  match left with
  | .yes _ => right
  | .no => .no
end Modern
`)
}

const boolRewrite = `{"source":"Bool.not","target":"Bool.Internal.not","theorem":"Bool.not_eq_internalNot","axioms":["Quot.sound"]}`

func containsJSON(t *testing.T, list any, item string) bool {
	t.Helper()
	for _, element := range arr(list) {
		if mustJSON(element) == mustJSON(parseAny(t, item)) {
			return true
		}
	}
	return false
}

func parseAny(t *testing.T, text string) any {
	var v any
	if err := jsonUnmarshal(text, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestModernModuleLocalImportAndBoolRewriteSurviveOlean(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	modernSources(f)
	program := f.export("Modern.Transform", "Modern.choose")
	if program["module"] != "Modern.Transform" || mustJSON(program["roots"]) != `["Modern.choose"]` {
		t.Fatal("module or roots")
	}
	if !subset([]string{"Bool", "Modern.Signal"}, names(program["types"])) || !hasKeys(obj(program["inputs"]), "source/Modern/Model.lean") {
		t.Fatal("types or inputs")
	}
	if !containsJSON(t, program["rewrites"], boolRewrite) {
		t.Fatal("Bool.not rewrite missing")
	}
	records := provenanceOf(program)
	if records["Modern.negate"] == nil || !containsJSON(t, records["Modern.negate"]["rewrites"], boolRewrite) {
		t.Fatal("negate provenance")
	}
}

func TestNullaryEnumAndTwoArgumentSelectorRoots(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	modernSources(f)
	for entry, count := range map[string]int{"Modern.constant": 0, "Modern.select": 2} {
		program := f.export("Modern.Transform", entry)
		function := functionsOf(program)[entry]
		if len(arr(function["params"])) != count || function["result"] != "Modern.Signal" || !hasString(names(program["types"]), "Modern.Signal") {
			t.Fatal(entry)
		}
	}
}

func TestBooleanPatternMatchRoot(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	f.write("Boolean.lean", "def Boolean.flip (b : Bool) : Bool :=\n  match b with\n  | true => false\n  | false => true\n")
	program := f.export("Boolean", "Boolean.flip")
	if mustJSON(names(program["types"])) != `["Bool"]` || mustJSON(program["roots"]) != `["Boolean.flip"]` {
		t.Fatal(program["types"], program["roots"])
	}
}

func TestSafeUserRewriteRejectedAfterLocalScopeHasEnded(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	f.write("Historical.lean", `namespace Historical
@[noinline] def original (b : Bool) : Bool := b
@[noinline] def replacement (b : Bool) : Bool := b
theorem equivalent : original = replacement := rfl
section
attribute [local csimp] equivalent
@[noinline] def helper (b : Bool) : Bool := original b
end
@[noinline] def run (b : Bool) : Bool := helper b
end Historical
`)
	err := f.reject("Historical", "Historical.run", "Unsupported historical compiler rewrite: Historical.original")
	if !strings.Contains(err.Error(), "Historical.equivalent") || regexpMatch("sorry|unknown identifier|unexpected token", err.Error()) {
		t.Fatal(err)
	}
}

func TestSafeLocalRewriteInImportedInlineHelperIsRejected(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	f.write("Helper.lean", `namespace Helper
@[noinline] def original (b : Bool) : Bool := b
@[noinline] def replacement (b : Bool) : Bool := b
theorem equivalent : original = replacement := rfl
section
attribute [local csimp] equivalent
@[inline] def helper (b : Bool) : Bool := original b
end
end Helper
`)
	f.write("Caller.lean", "import Helper\nnamespace Caller\n@[noinline] def run (b : Bool) : Bool := Helper.helper b\nend Caller\n")
	err := f.reject("Caller", "Caller.run", "Unsupported historical compiler rewrite: Helper.original")
	if !strings.Contains(err.Error(), "Helper.equivalent") || regexpMatch("sorry|unknown identifier|unexpected token", err.Error()) {
		t.Fatal(err)
	}
}

// Several roots.

func sharedSources(f *leanFixture) {
	f.write("Shared.lean", `namespace Shared
structure Box where
  value : UInt64
@[noinline] def bump (n : UInt64) : UInt64 := n + 1
def advance (s : Box) (delta : UInt64) : Box := { value := bump (s.value + delta) }
def read (s : Box) : UInt64 := bump s.value
def enabled (flag : Bool) : Bool := !flag
end Shared
`)
	f.write("Example.lean", "import Shared\n")
}

func TestUnionClosureImportsAndRequestOrderAreDeterministic(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	sharedSources(f)
	roots := []string{"Shared.read", "Shared.enabled", "Shared.advance"}
	program := f.export("Example", roots...)
	original := string(f.exported)
	reversed := slices.Clone(roots)
	slices.Reverse(reversed)
	f.export("Example", reversed...)
	if string(f.exported) != original {
		t.Fatal("request order changed the IR")
	}
	if mustJSON(program["roots"]) != mustJSON(setOf(roots)) || program["schema"] != "lean-go-ir/v24" {
		t.Fatal("roots or schema")
	}
	if _, ok := program["root"]; ok {
		t.Fatal("root field")
	}
	count := func(list any, name string) int {
		n := 0
		for _, item := range names(list) {
			if item == name {
				n++
			}
		}
		return n
	}
	if count(program["functions"], "Shared.bump") != 1 || count(program["types"], "Shared.Box") != 1 {
		t.Fatal("shared declarations duplicated")
	}
	functions := functionsOf(program)
	if !slices.Equal(typesOfParams(functions["Shared.advance"]["params"]), []string{"Shared.Box", "UInt64"}) ||
		functions["Shared.advance"]["result"] != "Shared.Box" || typesOfParams(functions["Shared.read"]["params"])[0] != "Shared.Box" ||
		functions["Shared.enabled"]["result"] != "Bool" {
		t.Fatal("signatures")
	}
	jsonEqual(t, obj(obj(program["api"])["records"])["Shared.Box"], `["value"]`)
	if !subset(append(roots, "Shared.bump"), keysOf(provenanceOf(program))) || !hasKeys(obj(program["inputs"]), "source/Shared.lean") {
		t.Fatal("provenance or inputs")
	}
}

func TestOneRootUsesTheSameRootsArray(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	sharedSources(f)
	program := f.export("Example", "Shared.read")
	if mustJSON(program["roots"]) != `["Shared.read"]` || program["schema"] != "lean-go-ir/v24" || program["profile"] != "pure-module/v22" {
		t.Fatal(program["roots"])
	}
	if _, ok := program["root"]; ok {
		t.Fatal("root field")
	}
}

func TestBadSecondRootNeverPublishesPartialOutput(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	f.write("Example.lean", `namespace Example
@[extern "unavailable_external"] opaque externalValue (n : UInt64) : UInt64
def aGood (n : UInt64) : UInt64 := n
def zBad (n : UInt64) : UInt64 := externalValue n
end Example
`)
	for _, c := range []struct {
		roots   []string
		message string
	}{{[]string{"Example.aGood", "Example.zBad"}, "extern source dependency"}, {[]string{"Example.aGood", "Example.zMissing"}, "Unknown constant"}} {
		writeText(t, f.ir, "existing output")
		_, err := Generate(GenerateOptions{SourceDir: f.source, Module: "Example", Roots: c.roots, Out: f.pkg, IROutput: f.ir, Cache: testCache(t)})
		if err == nil || !strings.Contains(err.Error(), c.message) || readText(t, f.ir) != "existing output" {
			t.Fatal(c.roots, err)
		}
	}
}
