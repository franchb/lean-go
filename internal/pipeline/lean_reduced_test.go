package pipeline

import (
	"fmt"
	"slices"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1): admission of reduced arguments.

const reducedAdd = "@[noinline] def add (x y : UInt32) : UInt32 := x + y\n"
const reducedWork = `  let selected := if flag then x else y
  let first := add selected bias
  let second := add first bias
  let third := add second bias
  add third bias
`

type nsOptions struct {
	modern bool
	entry  string
	probe  string
}

func nsExport(f *leanFixture, namespace, body string, o nsOptions) map[string]any {
	f.t.Helper()
	f.write(namespace+".lean", nsSource(namespace, body, o.modern, o.probe))
	entry := o.entry
	if entry == "" {
		entry = namespace + ".run"
	}
	return f.export(namespace, entry)
}

func nsReject(f *leanFixture, namespace, body, pattern string, o nsOptions) {
	f.t.Helper()
	f.write(namespace+".lean", nsSource(namespace, body, o.modern, o.probe))
	entry := o.entry
	if entry == "" {
		entry = namespace + ".run"
	}
	f.reject(namespace, entry, pattern)
}

func reducedExport(t *testing.T, body string, o nsOptions) map[string]any {
	return nsExport(newLeanFixture(t), "Reduced", body, o)
}

func reducedReject(t *testing.T, body, pattern string, o nsOptions) {
	nsReject(newLeanFixture(t), "Reduced", body, pattern, o)
}

// assertDerivedHelper checks a compiler helper whose only dependency is its owner.
func assertDerivedHelper(t *testing.T, program map[string]any, owner, helper string, params []string, result string) map[string]any {
	t.Helper()
	function := functionsOf(program)[helper]
	if function == nil {
		t.Fatalf("helper %s missing from %v", helper, keysOf(functionsOf(program)))
	}
	if !slices.Equal(typesOfParams(function["params"]), params) || function["result"] != result {
		t.Fatalf("%s signature %v -> %v", helper, typesOfParams(function["params"]), function["result"])
	}
	provenance := provenanceOf(program)
	jsonEqual(t, provenance[helper], fmt.Sprintf(`{"name":%q,"dependencies":[%q],"rewrites":[]}`, helper, owner))
	if provenance[owner] == nil || program["schema"] != "lean-go-ir/v24" || program["profile"] != "pure-module/v22" {
		t.Fatal("owner provenance or identity")
	}
	return function
}

func assertRedArg(t *testing.T, program map[string]any, owner string, params []string, result ...string) map[string]any {
	t.Helper()
	r := "UInt32"
	if len(result) > 0 {
		r = result[0]
	}
	return assertDerivedHelper(t, program, owner, owner+"._redArg", params, r)
}

func callNames(function map[string]any) []string {
	var out []string
	for _, call := range calls(function) {
		out = append(out, str2(call["name"]))
	}
	return out
}

func callTo(function map[string]any, name string) map[string]any {
	for _, call := range calls(function) {
		if call["name"] == name {
			return call
		}
	}
	return nil
}

func TestFreshRootRetainsFullSignatureAndExportsActualHelper(t *testing.T) {
	requireModules(t)
	program := reducedExport(t, reducedAdd+"def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n"+reducedWork, nsOptions{})
	root := functionsOf(program)["Reduced.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"Bool", "UInt32", "UInt32", "UInt32", "UInt32"}) {
		t.Fatal(typesOfParams(root["params"]))
	}
	helper := assertRedArg(t, program, "Reduced.run", []string{"Bool", "UInt32", "UInt32", "UInt32"})
	if !hasString(callNames(root), str2(helper["name"])) || !hasString(typesAny(provenanceOf(program)["Reduced.run"]["dependencies"]), "Reduced.add") {
		t.Fatal("helper call or owner dependency")
	}
}

func typesAny(list any) []string {
	var out []string
	for _, v := range arr(list) {
		out = append(out, str2(v))
	}
	return out
}

func TestUnusedPositionsAndMultipleDropsKeepSourceOrder(t *testing.T) {
	requireModules(t)
	for position, c := range map[string]struct {
		params   string
		retained []int
	}{
		"first":   {"(_a : UInt32) (flag : Bool) (x y bias : UInt32)", []int{1, 2, 3, 4}},
		"middle":  {"(flag : Bool) (x : UInt32) (_a : UInt32) (y bias : UInt32)", []int{0, 1, 3, 4}},
		"last":    {"(flag : Bool) (x y bias : UInt32) (_a : UInt32)", []int{0, 1, 2, 3}},
		"several": {"(_a : UInt32) (flag : Bool) (_b : Bool) (x y bias : UInt32) (_c : UInt32)", []int{1, 3, 4, 5}},
	} {
		t.Run(position, func(t *testing.T) {
			program := reducedExport(t, reducedAdd+"def run "+c.params+" : UInt32 :=\n"+reducedWork, nsOptions{})
			helper := assertRedArg(t, program, "Reduced.run", []string{"Bool", "UInt32", "UInt32", "UInt32"})
			root := functionsOf(program)["Reduced.run"]
			call := callTo(root, str2(helper["name"]))
			params := paramIDs(root["params"])
			var want []float64
			for _, i := range c.retained {
				want = append(want, params[i])
			}
			if !slices.Equal(ids(call["args"]), want) {
				t.Fatal(ids(call["args"]), want)
			}
			wantLen := 5
			if position == "several" {
				wantLen = 7
			}
			if len(params) != wantLen {
				t.Fatal(len(params))
			}
		})
	}
}

func TestAllUnusedUsesZeroRuntimeParametersAndNullaryOwnerWorks(t *testing.T) {
	requireModules(t)
	work := "  let first := add 1 2\n  let second := add first 3\n  let third := add second 4\n  add third 5\n"
	for _, params := range []string{"(_a : UInt32) (_b : Bool)", ""} {
		program := reducedExport(t, reducedAdd+"def run "+params+" : UInt32 :=\n"+work, nsOptions{})
		root := functionsOf(program)["Reduced.run"]
		if params == "" {
			if len(arr(root["params"])) != 0 {
				t.Fatal("nullary owner has parameters")
			}
			continue
		}
		if !slices.Equal(typesOfParams(root["params"]), []string{"UInt32", "Bool"}) {
			t.Fatal(typesOfParams(root["params"]))
		}
		helper := assertRedArg(t, program, "Reduced.run", nil)
		if len(arr(callTo(root, str2(helper["name"]))["args"])) != 0 {
			t.Fatal("all-unused helper call has arguments")
		}
	}
}

func TestSmallOwnerWithoutForwardingWrapperStillWorks(t *testing.T) {
	requireModules(t)
	program := reducedExport(t, "def run (_unused : UInt32) (value : UInt32) : UInt32 := value\n", nsOptions{})
	functions := functionsOf(program)
	if !slices.Equal(keysOf(functions), []string{"Reduced.run"}) || len(arr(functions["Reduced.run"]["params"])) != 2 {
		t.Fatal(keysOf(functions))
	}
	jsonEqual(t, functions["Reduced.run"]["body"], fmt.Sprintf(`{"op":"return","var":%v}`, paramIDs(functions["Reduced.run"]["params"])[1]))
}

const chooseOwner = "@[noinline] def choose (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n"
const chooseCaller = "def run (flag : Bool) (x y bias : UInt32) : UInt32 := choose flag x x y bias\n"

func TestDirectNoinlineCallEmitsHelperAndUnchangedOwnerProvenance(t *testing.T) {
	requireModules(t)
	program := reducedExport(t, reducedAdd+chooseOwner+reducedWork+chooseCaller, nsOptions{})
	helper := assertRedArg(t, program, "Reduced.choose", []string{"Bool", "UInt32", "UInt32", "UInt32"})
	if !hasString(callNames(functionsOf(program)["Reduced.run"]), str2(helper["name"])) {
		t.Fatal("helper not called")
	}
	deps := typesAny(provenanceOf(program)["Reduced.choose"]["dependencies"])
	if !hasString(deps, "Reduced.add") || hasString(deps, str2(helper["name"])) {
		t.Fatal(deps)
	}
}

func TestRecursiveHelperCallsItself(t *testing.T) {
	requireModules(t)
	program := reducedExport(t, reducedAdd+`inductive Chain where
  | done
  | next (rest : Chain)
@[noinline] def descend (chain : Chain) (_unused : UInt32) (value : UInt32) : UInt32 :=
  match chain with
  | .done => add value 1
  | .next rest =>
    let first := add value 2
    let second := add first 3
    let third := add second 4
    descend rest _unused (add third 5)
def run (chain : Chain) (value : UInt32) : UInt32 := descend chain value value
`, nsOptions{})
	helper := assertRedArg(t, program, "Reduced.descend", []string{"Reduced.Chain", "UInt32"})
	if !hasString(callNames(helper), str2(helper["name"])) {
		t.Fatal("helper does not recurse")
	}
}

func TestRetainedAndDiscardedWrappersKeepSourceTypesAndResult(t *testing.T) {
	requireModules(t)
	program := reducedExport(t, reducedAdd+`structure Count where
  value : UInt32
structure Flag where
  value : Bool
structure Discarded where
  value : UInt32
@[noinline] def choose (flag : Flag) (_unused : Discarded) (x y bias : Count) : Count :=
  let selected := if flag.value then x.value else y.value
  let first := add selected bias.value
  let second := add first bias.value
  let third := add second bias.value
  ⟨add third bias.value⟩
def run (flag : Flag) (x y bias : Count) : Count := choose flag ⟨x.value⟩ x y bias
`, nsOptions{})
	helper := assertRedArg(t, program, "Reduced.choose", []string{"Reduced.Flag", "Reduced.Count", "Reduced.Count", "Reduced.Count"}, "Reduced.Count")
	if hasString(names(program["types"]), "Reduced.Discarded") {
		t.Fatal("discarded wrapper exported")
	}
	rebuilt := false
	for _, node := range nodes(helper["body"]) {
		if node["op"] == "let" && obj(node["value"])["kind"] == "constructor" && obj(node["value"])["name"] == "Reduced.Count.mk" {
			rebuilt = true
		}
	}
	if !rebuilt {
		t.Fatal("result wrapper not rebuilt")
	}
}

func TestReducedClassicAndModernImportsRetainCompilerEvidence(t *testing.T) {
	requireModules(t)
	for _, modern := range []bool{false, true} {
		f := newLeanFixture(t)
		model, caller := modelCaller(modern, "namespace Model\n"+reducedAdd+chooseOwner+reducedWork+"end Model\n",
			"namespace Caller\ndef run (flag : Bool) (x y bias : UInt32) : UInt32 := Model.choose flag x x y bias\nend Caller\n")
		f.write("Model.lean", model)
		f.write("Caller.lean", caller)
		program := f.export("Caller", "Caller.run")
		helper := assertRedArg(t, program, "Model.choose", []string{"Bool", "UInt32", "UInt32", "UInt32"})
		if !hasString(callNames(functionsOf(program)["Caller.run"]), str2(helper["name"])) || !hasKeys(obj(program["inputs"]), "source/Model.lean", "source/Caller.lean") {
			t.Fatal("import evidence")
		}
	}
}

func TestOrdinarySourceNameContainingRedArgHasNoSpecialPrivilege(t *testing.T) {
	requireModules(t)
	program := reducedExport(t, "@[noinline] def safe_redArg (x : UInt32) : UInt32 := x + 1\n"+
		"@[noinline] def ordinary._redArg (x : UInt32) : UInt32 := safe_redArg x\n"+
		"def run (x : UInt32) : UInt32 := ordinary._redArg x\n", nsOptions{})
	functions := functionsOf(program)
	if functions["Reduced.safe_redArg"] == nil || functions["Reduced.ordinary._redArg"] == nil {
		t.Fatal(keysOf(functions))
	}
	provenance := provenanceOf(program)
	if mustJSON(provenance["Reduced.safe_redArg"]["dependencies"]) == `["Reduced.safe"]` {
		t.Fatal("authored _redArg name treated as a derived helper")
	}
	deps := typesAny(provenance["Reduced.ordinary._redArg"]["dependencies"])
	if !hasString(deps, "Reduced.safe_redArg") || hasString(deps, "Reduced.ordinary") {
		t.Fatal(deps)
	}
}

func TestKernelCollisionsBeforeAndAfterOwnerPreserveOutput(t *testing.T) {
	requireModules(t)
	owner := reducedAdd + chooseOwner + reducedWork
	collision := "def choose._redArg (flag : Bool) (x y bias : UInt32) : UInt32 := x\n"
	for order, body := range map[string]string{"before": collision + owner + chooseCaller, "after": owner + collision + chooseCaller} {
		t.Run(order, func(t *testing.T) {
			reducedReject(t, body, "[Cc]ollision|reduced[- ]argument|[Ii]nvalid reduced", nsOptions{})
		})
	}
}

func TestOverwrittenAuthoredHelperSelectedAsRootIsRejected(t *testing.T) {
	requireModules(t)
	reducedReject(t, "def choose._redArg (value : UInt32) : UInt32 := value + 7\ndef choose (_unused : UInt32) (value : UInt32) : UInt32 := value\n",
		"[Cc]ollision|reduced[- ]argument|[Ii]nvalid reduced", nsOptions{entry: "Reduced.choose._redArg"})
}

func TestConstructorNameCollisionCannotBypassCallResolution(t *testing.T) {
	requireModules(t)
	reducedReject(t, `inductive Foo where
  | run._redArg (value : UInt32)
  | other (value : UInt32)
namespace Foo
@[noinline] def run (value _unused : UInt32) : Foo := .other (value + value)
end Foo
def run (value unused : UInt32) : Foo := Foo.run value unused
`, "[Cc]ollision", nsOptions{})
}

func TestErasedSourceProofAndForeignDependenciesPreserveOutput(t *testing.T) {
	requireModules(t)
	for boundary, c := range map[string][2]string{
		"proof":   {"axiom forged : True\n" + reducedAdd + chooseOwner + "  let _proof : True := forged\n" + reducedWork + chooseCaller, "[Aa]xiom|proof|provenance"},
		"foreign": {"@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32\n" + reducedAdd + chooseOwner + "  let _erased := foreign x\n" + reducedWork + chooseCaller, "extern|External function|opaque"},
	} {
		t.Run(boundary, func(t *testing.T) { reducedReject(t, c[0], c[1], nsOptions{}) })
	}
}

func TestContextualAndLateSourceChangesPreserveOutput(t *testing.T) {
	requireModules(t)
	definitions := reducedAdd + "@[noinline] def original (x : UInt32) : UInt32 := x + 1\n" +
		"@[noinline] def replacement (x : UInt32) : UInt32 := original x\ntheorem replacement_eq : original = replacement := rfl\n"
	owner := chooseOwner + "  let _erased := original x\n" + reducedWork
	for context, body := range map[string]string{
		"local":               "section\nattribute [local csimp] replacement_eq\n" + owner + "end\n" + chooseCaller,
		"scoped":              "namespace RewriteScope\nattribute [scoped csimp] replacement_eq\nend RewriteScope\nopen scoped RewriteScope\n" + owner + chooseCaller,
		"late_csimp":          owner + chooseCaller + "attribute [csimp] replacement_eq\n",
		"late_extern":         owner + chooseCaller + "attribute [extern \"lean_go_untrusted\"] original\n",
		"implemented_by":      "attribute [implemented_by replacement] original\n" + owner + chooseCaller,
		"late_implemented_by": owner + chooseCaller + "attribute [implemented_by replacement] original\n",
	} {
		t.Run(context, func(t *testing.T) {
			reducedReject(t, definitions+body, "rewrite|replacement|repeated compilation|extern|implemented_by", nsOptions{})
		})
	}
}

func TestDiscardedUnsupportedSourceFieldsPreserveOutput(t *testing.T) {
	requireModules(t)
	for field, typ := range map[string]string{"usize": "USize", "closure": "UInt32 → UInt32", "proof": "True", "dependent": "Fin 7"} {
		t.Run(field, func(t *testing.T) {
			body := reducedAdd + "inductive Discarded where\n  | empty\n" + fmt.Sprintf("  | boxed (value : %s)\n", typ) +
				"@[noinline] def choose (flag : Bool) (_unused : Discarded) (x y bias : UInt32) : UInt32 :=\n" + reducedWork +
				"def run (flag : Bool) (x y bias : UInt32) : UInt32 :=\n  choose flag .empty x y bias\n"
			reducedReject(t, body, "Unsupported|unsupported|monomorphic|proof|source field|wrapper", nsOptions{})
		})
	}
}

func TestReducedRetainedClosureRemainsRejected(t *testing.T) {
	requireModules(t)
	reducedReject(t, `@[noinline] def apply (f : UInt32 → UInt32) (_unused : UInt32)
    (value : UInt32) : UInt32 := f value
def run (bias value : UInt32) : UInt32 := apply (fun x => x + bias) bias value
`, "Unsupported first-order monomorphic type", nsOptions{})
}

func reducedEvidence(mutation, helper string) string {
	if helper == "" {
		helper = "Reduced.run._redArg"
	}
	return fmt.Sprintf(`    let name := `+"`"+`%s
    let some entry := (reducedExt.getState (← getEnv)).find? name
      | throwError "Test fixture requires compiler evidence"
    let some evidence := entry.evidence?
      | throwError "Test fixture requires successful compiler evidence"
%s
`, helper, mutation)
}

const reducedRunBody = reducedAdd + "def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=\n" + reducedWork

func TestMalformedEvidenceSnapshotsIdentityAndMappingPreserveOutput(t *testing.T) {
	requireModules(t)
	for corruption, c := range map[string][2]string{
		"owner_snapshot":   {"ownerDecl := { evidence.ownerDecl with recursive := !evidence.ownerDecl.recursive }", "declaration snapshot mismatch"},
		"helper_snapshot":  {"helperDecl := { evidence.helperDecl with recursive := !evidence.helperDecl.recursive }", "declaration snapshot mismatch"},
		"wrong_module":     {"sourceModule := `OtherModule", "module mismatch"},
		"wrong_owner":      {"owner := `Reduced.add", "identity or kernel collision"},
		"wrong_helper":     {"helper := `Reduced.absent", "identity or kernel collision"},
		"nonordered":       {"retained := #[2, 0, 3, 4]", "forwarding mapping mismatch"},
		"duplicate":        {"retained := #[0, 2, 2, 4]", "forwarding mapping mismatch"},
		"out_of_range":     {"retained := #[0, 2, 3, 5]", "forwarding mapping mismatch"},
		"false_all_unused": {"retained := #[], allUnused := true", "forwarding mapping mismatch"},
	} {
		t.Run(corruption, func(t *testing.T) {
			probe := reducedEvidence("    modifyEnv (reducedExt.addEntry · { entry with\n      evidence? := some { evidence with "+c[0]+" } })", "")
			reducedReject(t, reducedRunBody, c[1], nsOptions{probe: probe})
		})
	}
}

func TestMissingEvidenceAndUncompiledOrUnsafeSourceOwnerPreserveOutput(t *testing.T) {
	requireModules(t)
	body := reducedAdd + chooseOwner + reducedWork + chooseCaller
	source := "    let some source := (provenanceExt.getState (← getEnv)).find? evidence.owner\n      | throwError \"Test fixture requires source owner\"\n"
	for corruption, c := range map[string][2]string{
		"missing_evidence":    {"    modifyEnv (reducedExt.addEntry · { entry with evidence? := none })", "Missing reduced helper evidence"},
		"uncompiled_owner":    {source + "    modifyEnv (provenanceExt.addEntry · { source with compiled := false })", "source owner rejected|fresh source provenance"},
		"unsafe_owner":        {source + "    modifyEnv (provenanceExt.addEntry · { source with safeDefinition := false })", "source owner rejected|source provenance|safe source"},
		"wrong_source_module": {source + "    modifyEnv (provenanceExt.addEntry · { source with sourceModule := `OtherModule })", "source owner rejected|source provenance|safe source"},
	} {
		t.Run(corruption, func(t *testing.T) {
			reducedReject(t, body, c[1], nsOptions{probe: reducedEvidence(c[0], "Reduced.choose._redArg")})
		})
	}
}

func TestChangedOwnerBodyCannotEraseEvidenceAudit(t *testing.T) {
	requireModules(t)
	mutation := `    let owner := { evidence.ownerDecl with
      value := .code (.return evidence.ownerDecl.params[2]!.fvarId) }
    modifyEnv (saveMonoDeclCore · owner)
`
	for corruption, c := range map[string][2]string{
		"stale_snapshot":        {mutation, "declaration snapshot mismatch"},
		"nonforwarding_wrapper": {mutation + "    modifyEnv (reducedExt.addEntry · { entry with evidence? := some { evidence with ownerDecl := owner } })", "forwarding mapping mismatch"},
	} {
		t.Run(corruption, func(t *testing.T) {
			reducedReject(t, reducedRunBody, c[1], nsOptions{probe: reducedEvidence(c[0], "")})
		})
	}
}

func TestAllUnusedRejectsNonErasedCallAndDummyDataUse(t *testing.T) {
	requireModules(t)
	body := reducedAdd + "def run (_unused : UInt32) : UInt32 :=\n  let first := add 1 2\n  let second := add first 3\n  let third := add second 4\n  add third 5\n"
	for corruption, c := range map[string][2]string{
		"non_erased_argument": {`    let .code (.let call continuation) := evidence.ownerDecl.value
      | throwError "Test fixture requires owner forwarding wrapper"
    let .const name levels _ _ := call.value
      | throwError "Test fixture requires helper call"
    let call := { call with value := .const name levels #[.fvar evidence.ownerDecl.params[0]!.fvarId] }
    let owner := { evidence.ownerDecl with value := .code (.let call continuation) }
    modifyEnv (saveMonoDeclCore · owner)
    modifyEnv (reducedExt.addEntry · { entry with evidence? := some { evidence with ownerDecl := owner } })`, "forwarding mapping mismatch"},
		"dummy_data_use": {`    let helper := { evidence.helperDecl with
      value := .code (.return evidence.helperDecl.params[0]!.fvarId) }
    modifyEnv (saveMonoDeclCore · helper)
    modifyEnv (reducedExt.addEntry · { entry with evidence? := some { evidence with helperDecl := helper } })`, "Unbound variable|dummy|Dummy"},
	} {
		t.Run(corruption, func(t *testing.T) {
			reducedReject(t, body, c[1], nsOptions{probe: reducedEvidence(c[0], "")})
		})
	}
}
