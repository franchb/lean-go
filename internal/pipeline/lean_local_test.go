package pipeline

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1) port of scripts/test_local_admission.py.

const localAdd = "@[noinline] def add (x y : UInt32) : UInt32 := x + y\n"

var localChain = func() string {
	var b strings.Builder
	for step := 0; step < 8; step++ {
		operand := "value"
		if step == 0 {
			operand = "a"
		}
		b.WriteString("    let value := add " + operand + " bias\n")
	}
	return b.String() + "    value\n"
}()

var localScalar = "@[noinline] def run (bias x y : UInt32) : UInt32 :=\n  let bump := fun (a : UInt32) =>\n" + localChain + "  add (bump x) (bump y)\n"

var localBoolScalar = "@[noinline] def run (flag : Bool) (bias x y : UInt32) : UInt32 :=\n  let bump := fun (a : UInt32) =>\n" +
	"    let bias := if flag then bias else a\n" + localChain + "  add (bump x) (bump y)\n"

func localExport(t *testing.T, body string, o nsOptions) map[string]any {
	return nsExport(newLeanFixture(t), "Local", body, o)
}

func localReject(t *testing.T, body, pattern string, o nsOptions) {
	nsReject(newLeanFixture(t), "Local", body, pattern, o)
}

// assertLocalHelper checks the pinned compiler's observed lifted survivor.
func assertLocalHelper(t *testing.T, program map[string]any, owner string, params []string, helper string) map[string]any {
	t.Helper()
	if helper == "" {
		helper = owner + "._lam_0"
	}
	function := assertDerivedHelper(t, program, owner, helper, params, "UInt32")
	called := false
	for _, caller := range functionsOf(program) {
		if hasString(callNames(caller), helper) {
			called = true
		}
	}
	if !called {
		t.Fatal("the lifted function must have an emitted direct call")
	}
	return function
}

func callsTo(function map[string]any, name string) []map[string]any {
	var out []map[string]any
	for _, call := range calls(function) {
		if call["name"] == name {
			out = append(out, call)
		}
	}
	return out
}

func TestScalarCaptureExportsActualHelperAndSourceOwner(t *testing.T) {
	requireModules(t)
	program := localExport(t, localAdd+localScalar, nsOptions{})
	root := functionsOf(program)["Local.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"UInt32", "UInt32", "UInt32"}) || root["result"] != "UInt32" {
		t.Fatal("root signature")
	}
	helper := assertLocalHelper(t, program, "Local.run", []string{"UInt32", "UInt32"}, "")
	params := paramIDs(root["params"])
	var args [][]float64
	for _, call := range callsTo(root, str2(helper["name"])) {
		args = append(args, ids(call["args"]))
	}
	if mustJSON(args) != mustJSON([][]float64{{params[0], params[1]}, {params[0], params[2]}}) {
		t.Fatal(args)
	}
	if !hasString(typesAny(provenanceOf(program)["Local.run"]["dependencies"]), "Local.add") {
		t.Fatal("owner dependency")
	}
}

func TestBoolAndScalarCaptureOrderMatchesBothCalls(t *testing.T) {
	requireModules(t)
	program := localExport(t, localAdd+localBoolScalar, nsOptions{})
	root := functionsOf(program)["Local.run"]
	helper := assertLocalHelper(t, program, "Local.run", []string{"Bool", "UInt32", "UInt32"}, "")
	params := paramIDs(root["params"])
	var args [][]float64
	for _, call := range callsTo(root, str2(helper["name"])) {
		args = append(args, ids(call["args"]))
	}
	if mustJSON(args) != mustJSON([][]float64{{params[0], params[1], params[2]}, {params[0], params[1], params[3]}}) {
		t.Fatal(args)
	}
}

func TestNestedRecordCapturesPreserveSourceRootSignature(t *testing.T) {
	requireModules(t)
	program := localExport(t, localAdd+`structure Count where
  value : UInt32
structure Nested where
  count : Count
structure Config where
  nested : Nested
  flag : Bool
@[noinline] def run (config : Config) (x y : Count) : Count :=
  let bump := fun (a : UInt32) =>
    let bias := if config.flag then config.nested.count.value else a
`+localChain+"  ⟨add (bump x.value) (bump y.value)⟩\n", nsOptions{})
	root := functionsOf(program)["Local.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"Local.Config", "Local.Count", "Local.Count"}) || root["result"] != "Local.Count" {
		t.Fatal("root signature")
	}
	assertLocalHelper(t, program, "Local.run", []string{"Local.Config", "UInt32"}, "")
	types := typesOf(program)
	for name, fields := range map[string]string{"Local.Config": `["Local.Nested","Bool"]`, "Local.Nested": `["Local.Count"]`, "Local.Count": `["UInt32"]`} {
		jsonEqual(t, types[name]["constructors"], fmt.Sprintf(`[{"name":%q,"tag":0,"fields":%s}]`, name+".mk", fields))
	}
}

func TestShadowedCaptureIsTheSameValueAtMultipleCalls(t *testing.T) {
	requireModules(t)
	body := strings.Replace(localScalar, "  let bump :=", "  let bias := add bias 1\n  let bump :=", 1)
	program := localExport(t, localAdd+body, nsOptions{})
	root := functionsOf(program)["Local.run"]
	helper := assertLocalHelper(t, program, "Local.run", []string{"UInt32", "UInt32"}, "")
	var capture float64 = -1
	for _, node := range nodes(root["body"]) {
		if node["op"] == "let" && obj(node["value"])["kind"] == "call" && obj(node["value"])["name"] == "Local.add" {
			capture = obj(node["bind"])["id"].(float64)
			break
		}
	}
	helperCalls := callsTo(root, str2(helper["name"]))
	if len(helperCalls) != 2 || ids(helperCalls[0]["args"])[0] != capture || ids(helperCalls[1]["args"])[0] != capture || capture == paramIDs(root["params"])[0] {
		t.Fatal(helperCalls, capture)
	}
}

func TestLocalClassicAndModernImportsRetainHelperOwnership(t *testing.T) {
	requireModules(t)
	for _, modern := range []bool{false, true} {
		f := newLeanFixture(t)
		model, caller := modelCaller(modern, "namespace Model\n"+localAdd+localScalar+"end Model\n",
			"namespace Caller\ndef run (bias x y : UInt32) : UInt32 := Model.run bias x y\nend Caller\n")
		f.write("Model.lean", model)
		f.write("Caller.lean", caller)
		program := f.export("Caller", "Caller.run")
		assertLocalHelper(t, program, "Model.run", []string{"UInt32", "UInt32"}, "")
		if !hasKeys(obj(program["inputs"]), "source/Model.lean", "source/Caller.lean") {
			t.Fatal("inputs")
		}
	}
}

func TestAuthoredSuffixAndUnrelatedUnsupportedLocalAreSourceScoped(t *testing.T) {
	requireModules(t)
	program := localExport(t, `@[noinline] def safe_lam_0 (x : UInt32) : UInt32 := x + 1
@[noinline] def ordinary._lam_0 (x : UInt32) : UInt32 := safe_lam_0 x
def unrelated (x : UInt32) : UInt32 :=
  let localProof := fun (a : UInt32) (_proof : True) => a
  localProof x True.intro
def run (x : UInt32) : UInt32 := ordinary._lam_0 x
`, nsOptions{})
	if functionsOf(program)["Local.ordinary._lam_0"] == nil {
		t.Fatal("authored helper missing")
	}
	provenance := provenanceOf(program)
	deps := typesAny(provenance["Local.ordinary._lam_0"]["dependencies"])
	if !hasString(deps, "Local.safe_lam_0") || hasString(deps, "Local.ordinary") || provenance["Local.unrelated"] != nil {
		t.Fatal(deps)
	}
}

func TestErasedUnsupportedLocalParametersPreserveOutput(t *testing.T) {
	requireModules(t)
	for _, c := range [][3]string{{"proof", "True", "True.intro"}, {"usize", "USize", "0"}, {"higher_order", "UInt32 → UInt32", "(fun z => z)"}} {
		t.Run(c[0], func(t *testing.T) {
			body := strings.Replace(localScalar, "fun (a : UInt32) =>", fmt.Sprintf("fun (a : UInt32) (_unused : %s) =>", c[1]), 1)
			body = strings.Replace(strings.Replace(body, "(bump x)", "(bump x "+c[2]+")", 1), "(bump y)", "(bump y "+c[2]+")", 1)
			localReject(t, localAdd+body, "[Ll]ocal.*(type|parameter|signature)|Unsupported|unsupported|proof", nsOptions{})
		})
	}
	for _, alias := range []bool{false, true} {
		t.Run(fmt.Sprintf("conditional_function_type_alias_%v", alias), func(t *testing.T) {
			body := "structure Bad where\n  value : UInt32\n  proof : True\n"
			signature := "Bad → UInt32"
			if alias {
				body += "abbrev BadFunction := Bad → UInt32\n"
				signature = "BadFunction"
			}
			body += "def run (flag : Bool) (x : UInt32) : UInt32 :=\n" +
				fmt.Sprintf("  let ignored : %s := if flag then (fun _ => 1) else (fun _ => 2)\n", signature) + "  x\n"
			localReject(t, body, "Unsupported|unsupported|proof|not an inductive type", nsOptions{})
		})
	}
}

func TestErasedUnsupportedCapturesPreserveOutput(t *testing.T) {
	requireModules(t)
	for _, c := range [][3]string{{"proof", "True", "True.intro"}, {"usize", "USize", "0"}, {"closure", "UInt32 → UInt32", "(fun z => z)"}} {
		t.Run(c[0], func(t *testing.T) {
			record := fmt.Sprintf("structure Settings where\n  bias : UInt32\n  hidden : %s\n", c[1])
			body := strings.Replace(localScalar, "  let bump :=", fmt.Sprintf("  let settings : Settings := ⟨bias, %s⟩\n  let bump :=", c[2]), 1)
			body = strings.Replace(body, "    let value := add a bias", "    let bias := settings.bias\n    let value := add a bias", 1)
			localReject(t, localAdd+record+body, "[Ll]ocal.*(capture|field|type)|Unsupported|unsupported|proof", nsOptions{})
		})
	}
	body := strings.Replace(localScalar, "  let bump :=", "  let transform := fun (z : UInt32) => z\n  let bump :=", 1)
	body = strings.Replace(body, "    let value := add a bias", "    let bias := transform bias\n    let value := add a bias", 1)
	localReject(t, localAdd+body, "Unsupported first-order monomorphic type|function-valued capture", nsOptions{})
}

func TestReturnedFunctionEtaExpandsToExistingDataSignature(t *testing.T) {
	requireModules(t)
	body := "def run (bias : UInt32) : UInt32 → UInt32 :=\n  let bump := fun (a : UInt32) =>\n" + localChain + "  bump\n"
	program := localExport(t, localAdd+body, nsOptions{})
	root := functionsOf(program)["Local.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"UInt32", "UInt32"}) || root["result"] != "UInt32" {
		t.Fatal("signature")
	}
	all := calls(root)
	params := paramIDs(root["params"])
	if len(all) != 8 || !slices.Equal(ids(all[0]["args"]), []float64{params[1], params[0]}) {
		t.Fatal(all)
	}
	for _, call := range all {
		if call["name"] != "Local.add" || ids(call["args"])[1] != params[0] {
			t.Fatal(call)
		}
	}
}

func TestRetainedClosurePreservesOutput(t *testing.T) {
	requireModules(t)
	body := "@[noinline] def apply (f : UInt32 → UInt32) (x y : UInt32) : UInt32 :=\n  add (f x) (f y)\n" +
		strings.Replace(localScalar, "  add (bump x) (bump y)", "  apply bump x y", 1)
	localReject(t, localAdd+body, "[Ee]scap|closure|Local helper.*(call|function)|Unsupported first-order", nsOptions{})
	// An anonymous argument has no authenticated source local-let witness.
	anonymous := "@[inline] def twice (f : UInt32 → UInt32) (x y : UInt32) : UInt32 :=\n  add (f x) (f y)\n" +
		"def run (bias x y : UInt32) : UInt32 :=\n  twice (fun (a : UInt32) =>\n" + localChain + "  ) x y\n"
	localReject(t, localAdd+anonymous, `Runtime function lacks compilation provenance: Local\.run\._lam_0`, nsOptions{})
	// Compiler-cache reuse cannot turn an anonymous lambda into an approved local let.
	prior := localAdd + strings.Replace(localScalar, "def run ", "def first ", 1)
	program := localExport(t, prior+anonymous, nsOptions{entry: "Local.first"})
	assertLocalHelper(t, program, "Local.first", []string{"UInt32", "UInt32"}, "")
	for _, reducedOwner := range []bool{false, true} {
		t.Run(fmt.Sprintf("anonymous_cache_reduced_owner_%v", reducedOwner), func(t *testing.T) {
			owner := anonymous
			if reducedOwner {
				owner = strings.Replace(owner, "(bias x y : UInt32)", "(_unused : UInt32) (bias x y : UInt32)", 1)
			}
			localReject(t, prior+owner, "anonymous|unowned|untraced|source identity|ambiguous cache reuse", nsOptions{})
		})
	}
	// The inner anonymous lambda can be lifted into an outer helper before
	// its cached target is visible in the source owner's final declaration.
	var nested strings.Builder
	for _, line := range strings.SplitAfter(localChain, "\n") {
		if line != "" {
			nested.WriteString("  " + line)
		}
	}
	nestedChain := strings.ReplaceAll(nested.String(), " bias", " z")
	nestedFirst := "@[noinline] def first (z x y : UInt32) : UInt32 :=\n  let bump := fun (a : UInt32) =>\n" + nestedChain + "  add (bump x) (bump y)\n"
	nestedAnonymous := "set_option compiler.small 4\n@[noinline] def add (a b : UInt32) : UInt32 := UInt32.add a b\n" + nestedFirst +
		"@[inline] def twice (f : UInt32 → UInt32) (x y : UInt32) := add (f x) (f y)\n" +
		"def run (bias x y : UInt32) : UInt32 :=\n  twice (fun (z : UInt32) =>\n    twice (fun (a : UInt32) =>\n" + nestedChain + "    ) z y\n  ) x y\n"
	program = localExport(t, nestedAnonymous, nsOptions{entry: "Local.first"})
	assertLocalHelper(t, program, "Local.first", []string{"UInt32", "UInt32"}, "")
	for _, reducedOwner := range []bool{true, false} {
		t.Run(fmt.Sprintf("nested_anonymous_reduced_owner_%v", reducedOwner), func(t *testing.T) {
			owner := nestedAnonymous
			if !reducedOwner {
				owner = strings.Replace(owner, "  ) x y\n", "  ) (add bias x) y\n", 1)
			}
			localReject(t, owner, "ambiguous cache reuse|Nested local helper lacks original source identity", nsOptions{})
		})
	}
}

func TestNestedLocalsRejectWithoutRejectingExistingKernelRecursion(t *testing.T) {
	requireModules(t)
	nested := strings.Replace(localScalar, "    let value := add a bias", "    let inner := fun (b : UInt32) => add b bias\n    let value := inner a", 1)
	localReject(t, localAdd+nested, "Local helper.*nested|[Nn]ested local", nsOptions{})
	program := localExport(t, localAdd+`inductive Steps where
  | done
  | more (tail : Steps)
def run (steps : Steps) (bias : UInt32) : UInt32 :=
  let rec bump (remaining : Steps) : UInt32 :=
    match remaining with
    | .done => bias
    | .more tail => add (bump tail) bias
  add (bump steps) bias
`, nsOptions{})
	helper := functionsOf(program)["Local.run.bump"]
	if !slices.Equal(typesOfParams(helper["params"]), []string{"UInt32", "Local.Steps"}) || !hasString(callNames(helper), "Local.run.bump") ||
		!hasString(names(program["provenance"]), "Local.run.bump") {
		t.Fatal("kernel recursion helper")
	}
}

func TestCustomPassAndErasedForeignOrAxiomaticDependencyPreserveOutput(t *testing.T) {
	requireModules(t)
	for boundary, c := range map[string][2]string{
		"custom_pass": {"open Lean.Compiler.LCNF\n@[cpass] meta def noOpPass : PassInstaller where\n  phase := .base\n  install passes := pure passes\n" + localAdd + localScalar, "[Cc]ustom compiler passes"},
		"foreign":     {"@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32\n" + localAdd + strings.Replace(localScalar, "  let bump :=", "  let _erased := foreign bias\n  let bump :=", 1), "extern|External function|opaque"},
		"axiom":       {"axiom forged : True\n" + localAdd + strings.Replace(localScalar, "  let bump :=", "  let _proof : True := forged\n  let bump :=", 1), "[Aa]xiom|proof"},
	} {
		t.Run(boundary, func(t *testing.T) { localReject(t, c[0], c[1], nsOptions{}) })
	}
}

func TestOriginalOwnerHistorySurvivesErasedDependency(t *testing.T) {
	requireModules(t)
	definitions := localAdd + "@[noinline] def original (x : UInt32) : UInt32 := x + 1\n" +
		"@[noinline] def replacement (x : UInt32) : UInt32 := original x\ntheorem replacement_eq : original = replacement := rfl\n"
	owner := strings.Replace(localScalar, "  let bump :=", "  let _erased := original bias\n  let bump :=", 1)
	contexts := rewriteContexts(owner)
	contexts["late_csimp"] = contexts["late"]
	delete(contexts, "late")
	contexts["late_extern"] = owner + "attribute [extern \"lean_go_untrusted\"] original\n"
	contexts["late_implemented_by"] = owner + "attribute [implemented_by replacement] original\n"
	for context, body := range contexts {
		t.Run(context, func(t *testing.T) {
			localReject(t, definitions+body, "rewrite|replacement|repeated compilation|extern|implemented_by", nsOptions{})
		})
	}
}

func TestPreexistingNameIsAvoidedButLateKernelCollisionRejects(t *testing.T) {
	requireModules(t)
	collision := "def run._lam_0 (bias x : UInt32) : UInt32 := add bias x\n"
	program := localExport(t, localAdd+collision+localScalar, nsOptions{})
	assertLocalHelper(t, program, "Local.run", []string{"UInt32", "UInt32"}, "Local.run._lam_1")
	if functionsOf(program)["Local.run._lam_0"] != nil {
		t.Fatal("authored colliding name exported")
	}
	localReject(t, localAdd+localScalar+collision, "[Cc]ollision|Local helper.*(identity|invalid|rejected)", nsOptions{})
}

func localEvidence(mutation string) string {
	return "    let name := `Local.run._lam_0\n" +
		"    let some entry := (localExt.getState (← getEnv)).find? name\n      | throwError \"Test fixture requires compiler evidence\"\n" +
		"    let some evidence := entry.evidence?\n      | throwError \"Test fixture requires successful compiler evidence\"\n" + mutation + "\n"
}

func corruptLocalEvidence(fields string) string {
	return localEvidence("    modifyEnv (localExt.addEntry · { entry with\n      evidence? := some { evidence with " + fields + " } })")
}

func TestAuxiliaryCacheReuseRejectsOnlyTheAmbiguousOwner(t *testing.T) {
	requireModules(t)
	body := localAdd + strings.Replace(localScalar, "def run ", "def first ", 1) + strings.Replace(localScalar, "def run ", "def second ", 1)
	program := localExport(t, body, nsOptions{entry: "Local.first"})
	assertLocalHelper(t, program, "Local.first", []string{"UInt32", "UInt32"}, "")
	localReject(t, body, "[Aa]mbiguous|[Aa]uxiliary|[Ll]ocal.*reuse", nsOptions{entry: "Local.second"})
	samePass := localAdd + "@[noinline] def run (initial other x y : UInt32) : UInt32 :=\n  let bias := add initial 1\n  let first := fun (a : UInt32) =>\n" +
		localChain + "  let bias := add other 1\n  let second := fun (a : UInt32) =>\n" + localChain +
		"  add (add (first x) (first y)) (add (second x) (second y))\n"
	localReject(t, samePass, "ambiguous cache reuse", nsOptions{probe: localEvidence("    pure ()")})
}

func TestStaleFinalAndPreLiftingSnapshotsPreserveOutput(t *testing.T) {
	requireModules(t)
	for snapshot, c := range map[string][2]string{
		"container":           {"containerDecl := { evidence.containerDecl with recursive := !evidence.containerDecl.recursive }", "declaration snapshot mismatch"},
		"helper":              {"helperDecl := { evidence.helperDecl with recursive := !evidence.helperDecl.recursive }", "declaration snapshot mismatch"},
		"pre_lifting":         {"beforeDecl := { evidence.beforeDecl with value := .code (.return evidence.beforeDecl.params[0]!.fvarId) }", "declaration or direct call mapping mismatch|pre-lifting local missing"},
		"source_capture_link": {"sourceCaptures := #[]", "source snapshot mapping mismatch"},
		"typed_capture_link":  {"typedCaptures := #[]", "typed source mapping mismatch"},
	} {
		t.Run(snapshot, func(t *testing.T) {
			localReject(t, localAdd+localScalar, c[1], nsOptions{probe: corruptLocalEvidence(c[0])})
		})
	}
}

func TestCaptureOrderAndTypeCorruptionPreserveOutput(t *testing.T) {
	requireModules(t)
	for corruption, c := range map[string][2]string{
		"capture_order": {"captures := evidence.captures.reverse", "capture mapping mismatch|declaration or direct call mapping mismatch"},
		"capture_type":  {"captures := evidence.captures.modify 0 (fun param => { param with type := mkConst ``UInt32 })", "lifted body mismatch|declaration or direct call mapping mismatch"},
	} {
		t.Run(corruption, func(t *testing.T) {
			localReject(t, localAdd+localBoolScalar, c[1], nsOptions{probe: corruptLocalEvidence(c[0])})
		})
	}
}

func TestMissingEvidenceWrongOwnerAndModulePreserveOutput(t *testing.T) {
	requireModules(t)
	for corruption, c := range map[string][2]string{
		"missing": {localEvidence("    modifyEnv (localExt.addEntry · { entry with evidence? := none })"), "Missing local helper evidence"},
		"owner":   {corruptLocalEvidence("sourceOwner := `Local.add"), "owner derivation|owner mismatch|source owner"},
		"module":  {corruptLocalEvidence("sourceModule := `OtherModule"), "module mismatch"},
	} {
		t.Run(corruption, func(t *testing.T) {
			localReject(t, localAdd+localScalar, c[1], nsOptions{probe: c[0]})
		})
	}
}

func TestChangedOwnerCannotHideEvidenceByErasingHelperCalls(t *testing.T) {
	requireModules(t)
	erase := "    let container := { evidence.containerDecl with\n      value := .code (.return evidence.containerDecl.params[0]!.fvarId) }\n    modifyEnv (saveMonoDeclCore · container)\n"
	for corruption, c := range map[string][2]string{
		"stale_snapshot":   {"", "declaration snapshot mismatch"},
		"missing_evidence": {"    modifyEnv (localExt.addEntry · { entry with evidence? := none })", "Missing local helper evidence"},
		"wrong_owner":      {"    modifyEnv (localExt.addEntry · { entry with evidence? := some { evidence with sourceOwner := `Local.add } })", "owner derivation|owner mismatch|owner association|source owner"},
	} {
		t.Run(corruption, func(t *testing.T) {
			localReject(t, localAdd+localScalar, c[1], nsOptions{probe: localEvidence(erase + c[0])})
		})
	}
}

func TestReducedOwnerCompositionIsAuthenticatedOrExplicitlyUnsupported(t *testing.T) {
	requireModules(t)
	body := localAdd + strings.Replace(localScalar, "(bias x y : UInt32)", "(_unused : UInt32) (bias x y : UInt32)", 1)
	f := newLeanFixture(t)
	f.write("Local.lean", nsSource("Local", body, false, ""))
	program, err := f.tryExport("Local", "Local.run")
	if err != nil {
		if !regexpMatch("Unsupported local helper.*(owner|derivation|reduced)|unsupported.*local.*reduced|unsupported.*reduced.*local", err.Error()) {
			t.Fatal(err)
		}
		f.reject("Local", "Local.run", "")
		return
	}
	root := functionsOf(program)["Local.run"]
	if !slices.Equal(typesOfParams(root["params"]), []string{"UInt32", "UInt32", "UInt32", "UInt32"}) {
		t.Fatal("root signature")
	}
	var derived []string
	for _, item := range arr(program["provenance"]) {
		name := str2(obj(item)["name"])
		if mustJSON(obj(item)["dependencies"]) == `["Local.run"]` && strings.Contains(name, "._lam_") {
			derived = append(derived, name)
		}
	}
	if len(derived) != 1 {
		t.Fatalf("composition must retain one authenticated local helper: %v", derived)
	}
	assertLocalHelper(t, program, "Local.run", []string{"UInt32", "UInt32"}, derived[0])
}
