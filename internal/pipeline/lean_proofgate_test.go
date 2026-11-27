package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_PROOFS=1) attacks on the proof gate. Proof and contract
// modules are untrusted: each case below is a proof workspace that tries to
// make check-proofs certify a claim that the kernel does not accept, or to
// bind evidence to a package that the sources do not produce.

// falseStatement is a contract requirement that clamp does not satisfy.
const falseStatement = "∀ value limit : UInt64, limit < Clamp.clamp value limit"

// gateProject returns the clamp contract with claims and a copy of its
// project in which the given proof modules are added or replaced.
func gateProject(t *testing.T, claims [][2]string, modules map[string]string) (*Contract, *ProjectSnapshot, *toolInputs) {
	t.Helper()
	contract, project, tools := clampFixture(t)
	changed := &ProjectSnapshot{project.Runtime, map[string]Source{}}
	for k, v := range project.Proofs {
		changed.Proofs[k] = v
	}
	for name, text := range modules {
		changed.Proofs[name] = Source{Module: name, Relative: strings.ReplaceAll(name, ".", "/") + ".lean", Data: []byte(text)}
	}
	edited := *contract
	if claims != nil {
		edited.Claims = claims
	}
	return &edited, changed, tools
}

func gateProof(t *testing.T, claims [][2]string, modules map[string]string) ([]any, error) {
	t.Helper()
	contract, project, tools := gateProject(t, claims, modules)
	compiler, _ := SelectedCompiler()
	return prove(contract.Claims, project, tools, compiler)
}

// wrongContract adds a false expected statement next to the clamp contract.
const wrongContract = "import Clamp\nnamespace GateContract\ndef wrong : Prop := " + falseStatement + "\nend GateContract\n"

// forgedTheorem adds a theorem of the false statement whose value is True.intro.
func forgedTheorem(name, route string) string {
	return "import Clamp\nimport Lean\nopen Lean Elab Command Term Meta\nset_option debug.skipKernelTC true\n" +
		"run_cmd liftTermElabM do\n" +
		"  let ty ← elabTerm (← `(" + falseStatement + ")) (some (mkSort 0))\n" +
		"  let ty ← instantiateMVars ty\n" +
		"  let decl := Declaration.thmDecl { name := `" + name + ", levelParams := [], type := ty, value := mkConst ``True.intro }\n" +
		route + "\n"
}

// The exact reproduction of finding 1, end to end: debug.skipKernelTC and
// addDecl put a theorem of a false statement into the environment.
func TestProofGateRejectsTheoremThatSkippedTheKernel(t *testing.T) {
	clampFixture(t)
	work := t.TempDir()
	project := filepath.Join(work, "project")
	copyTree(t, "../../examples/proofs/clamp", project)
	writeText(t, filepath.Join(project, "proof", "ClampContract.lean"),
		"import Clamp\nnamespace ClampContract\n-- A false requirement: clamp always exceeds the limit.\ndef bounded : Prop := "+falseStatement+"\nend ClampContract\n")
	writeText(t, filepath.Join(project, "proof", "ClampProofs.lean"),
		"import Clamp\nimport Lean\nopen Lean Elab Command Term Meta\nset_option debug.skipKernelTC true\nrun_cmd liftTermElabM do\n"+
			"  let ty ← elabTerm (← `("+falseStatement+")) (some (mkSort 0))\n  let ty ← instantiateMVars ty\n"+
			"  addDecl (.thmDecl { name := `ClampProofs.bounded, levelParams := [], type := ty, value := mkConst ``True.intro })\n")
	writeText(t, filepath.Join(project, "proof-contract.json"), `{"schema": "lean-go-proof-contract/v1", "source_dir": "source", "proof_dir": "proof",
  "module": "Clamp", "roots": ["Clamp.clamp"], "claims": [{"theorem": "ClampProofs.bounded", "expected": "ClampContract.bounded"}]}`)
	output := filepath.Join(work, "evidence.json")
	_, err := CheckProofs(CheckProofsOptions{Contract: filepath.Join(project, "proof-contract.json"),
		Manifest: filepath.Join(project, "clampnative", "manifest.json"), Output: output})
	expectError(t, err, `Kernel rejected a declaration of workspace module ClampProofs: while replaying declaration 'ClampProofs.bounded'`)
	if exists(output) {
		t.Fatal("evidence written for a rejected claim")
	}
}

// A theorem put into the environment without addDecl at all.
func TestProofGateRejectsDeclarationsAddedWithoutAddDecl(t *testing.T) {
	routes := map[string]string{
		// The elaborator's environment, without the kernel check.
		"GateProofs.unchecked": "  modifyEnv fun env => match env.addDeclCore 0 0 decl none (doCheck := false) with\n    | .ok env => env\n    | .error _ => env",
		// The kernel environment's own unchecked insertion, then a new
		// elaborator environment around it.
		"GateProofs.kernel": "  match (← getEnv).toKernelEnv.addDeclWithoutChecking decl with\n    | .ok kenv => setEnv (Environment.ofKernelEnv kenv)\n    | .error _ => throwError \"failed\"",
	}
	for name, route := range routes {
		module := forgedTheorem(name, route)
		_, err := gateProof(t, [][2]string{{name, "GateContract.wrong"}}, map[string]string{"GateContract": wrongContract, "GateProofs": module})
		expectError(t, err, `Kernel rejected a declaration of workspace module GateProofs: while replaying declaration '`+name+`'`)
	}
}

// A unification hint added to the hint table without validation makes
// Meta.isDefEq, which the previous checker used, equate a trivial
// proposition with the false statement; the kernel does not.
func TestProofGateIgnoresForgedUnificationHints(t *testing.T) {
	hint := "import Clamp\nimport GateContract\nimport Lean\nopen Lean Meta Elab Command\nnamespace GateProofs\n" +
		"inductive Trivially : UInt64 → UInt64 → Prop\n  | intro (value limit : UInt64) : Trivially value limit\n" +
		"theorem wrong : ∀ value limit : UInt64, Trivially value limit := Trivially.intro\n" +
		"def hint (value limit : UInt64) (r : Prop) : Prop := Trivially value limit = r\nend GateProofs\n" +
		"run_cmd liftTermElabM do\n  let value ← mkFreshExprMVar (mkConst ``UInt64)\n  let limit ← mkFreshExprMVar (mkConst ``UInt64)\n" +
		"  let keys ← DiscrTree.mkPath (mkApp2 (mkConst ``GateProofs.Trivially) value limit)\n" +
		"  unificationHintExtension.add { keys, val := ``GateProofs.hint }\n" +
		// The hint is effective for Meta.isDefEq: the build fails if it is not.
		"run_meta do\n  let some (.thmInfo t) := (← getEnv).find? ``GateProofs.wrong | throwError \"missing\"\n" +
		"  unless ← isDefEq t.type (← getConstInfo ``GateContract.wrong).value! do throwError \"hint not effective\"\n"
	_, err := gateProof(t, [][2]string{{"GateProofs.wrong", "GateContract.wrong"}}, map[string]string{"GateContract": wrongContract, "GateProofs": hint})
	expectError(t, err, `producer process failed \(1\): Theorem type changed: GateProofs.wrong\n`)
}

// Initializers, delaborators and native reduction would print a forged but
// valid checker result and exit 0 if the checker ran any of them.
func TestProofGateRunsNoUserCode(t *testing.T) {
	forged := `[{"name":"GateProofs.native","expected":"GateProofs.nativeExpected","statement":"forged","axioms":[]},` +
		`{"name":"GateProofs.wrong","expected":"GateContract.wrong","statement":"forged","axioms":[]}]`
	exit := "  IO.println GateProofs.forged\n  IO.Process.exit 0\n"
	prelude := "import Clamp\nimport Lean\nopen Lean Elab Command Term Meta PrettyPrinter Delaborator\n" +
		"def GateProofs.forged : String := " + jsonString(forged) + "\n" +
		"initialize do\n" + exit +
		"@[delab app.Clamp.clamp] def GateProofs.delab : Delab := do\n" + exit +
		"@[delab app.LT.lt] def GateProofs.delabLt : Delab := do\n" + exit +
		// Lean 4.36 has no Lean.reduceBool; one declared here must not make the
		// kernel evaluate code either.
		"namespace Lean\nopaque reduceBool (b : Bool) : Bool := b\nend Lean\n" +
		"unsafe def GateProofs.nativeImpl : Bool :=\n  (unsafeIO (do\n  " + strings.ReplaceAll(strings.TrimSuffix(exit, "\n"), "\n", "\n  ") + " : IO Bool)).toOption.getD false\n" +
		"@[implemented_by GateProofs.nativeImpl] def GateProofs.nativeValue : Bool := false\n" +
		"def GateProofs.nativeExpected : Prop := Lean.reduceBool GateProofs.nativeValue = true\n" +
		"set_option debug.skipKernelTC true\nrun_cmd liftTermElabM do\n"
	native := "  addDecl (.thmDecl { name := `GateProofs.native, levelParams := [], type := mkConst ``GateProofs.nativeExpected, " +
		"value := mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Bool) (mkConst ``Bool.true) })\n"
	wrong := "  let ty ← elabTerm (← `(" + falseStatement + ")) (some (mkSort 0))\n  let ty ← instantiateMVars ty\n" +
		"  addDecl (.thmDecl { name := `GateProofs.wrong, levelParams := [], type := ty, value := mkConst ``True.intro })\n"
	claims := [][2]string{{"GateProofs.native", "GateProofs.nativeExpected"}, {"GateProofs.wrong", "GateContract.wrong"}}
	_, err := gateProof(t, claims, map[string]string{"GateContract": wrongContract, "GateProofs": prelude + native + wrong})
	expectError(t, err, `Kernel rejected a declaration of workspace module GateProofs: while replaying declaration 'GateProofs\.(native|wrong)'`)
	// The native theorem alone is rejected by the kernel, not evaluated.
	_, err = gateProof(t, claims, map[string]string{"GateContract": wrongContract, "GateProofs": prelude + native})
	expectError(t, err, `while replaying declaration 'GateProofs.native'`)
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// A theorem about a definition that shadows the extracted function proves a
// different statement; a contract that names a shadowing definition is
// accepted, and the recorded statement names it in full.
func TestProofGateShadowingDefinitions(t *testing.T) {
	contract := "import Clamp\nnamespace GateContract\nnamespace Clamp\ndef clamp (_ _ : UInt64) : UInt64 := 0\nend Clamp\n" +
		"def shadow : Prop := ∀ value limit : UInt64, Clamp.clamp value limit ≤ limit\nend GateContract\n"
	proofs := "import Clamp\nimport GateContract\nnamespace GateProofs\nnamespace Clamp\ndef clamp (_ _ : UInt64) : UInt64 := 0\nend Clamp\n" +
		"theorem bounded (value limit : UInt64) : Clamp.clamp value limit ≤ limit := by simp [Clamp.clamp]\n" +
		"theorem shadow (value limit : UInt64) : GateContract.Clamp.clamp value limit ≤ limit := by simp [GateContract.Clamp.clamp]\nend GateProofs\n"
	modules := map[string]string{"GateContract": contract, "GateProofs": proofs}
	_, err := gateProof(t, [][2]string{{"GateProofs.bounded", "ClampContract.bounded"}}, modules)
	expectError(t, err, `Theorem type changed: GateProofs.bounded`)
	claims, err := gateProof(t, [][2]string{{"GateProofs.shadow", "GateContract.shadow"}}, modules)
	if err != nil {
		t.Fatal(err)
	}
	statement := claims[0].(*Object).Values["statement"]
	if statement != "∀ (value : UInt64) (limit : UInt64), LE.le UInt64 instLEUInt64 (GateContract.Clamp.clamp value limit) limit" {
		t.Fatalf("statement %v", statement)
	}
}

// replay skips unsafe and partial constants; a claim cannot depend on one.
func TestProofGateClaimCannotUseSkippedConstants(t *testing.T) {
	module := "import Clamp\nimport Lean\nopen Lean Elab Command Term Meta\n" +
		"unsafe def GateProofs.cheat : " + falseStatement + " := GateProofs.cheat\n" +
		"set_option debug.skipKernelTC true\nrun_cmd liftTermElabM do\n" +
		"  let ty ← elabTerm (← `(" + falseStatement + ")) (some (mkSort 0))\n  let ty ← instantiateMVars ty\n" +
		"  addDecl (.defnDecl { name := `GateProofs.partialCheat, levelParams := [], type := ty, value := mkConst ``GateProofs.cheat, hints := .opaque, safety := .partial })\n" +
		"  addDecl (.thmDecl { name := `GateProofs.%s, levelParams := [], type := ty, value := mkConst `GateProofs.%s })\n"
	for _, route := range [][2]string{{"viaUnsafe", "cheat"}, {"viaPartial", "partialCheat"}} {
		text := strings.ReplaceAll(strings.Replace(module, "%s", route[0], 1), "%s", route[1])
		_, err := gateProof(t, [][2]string{{"GateProofs." + route[0], "GateContract.wrong"}}, map[string]string{"GateContract": wrongContract, "GateProofs": text})
		expectError(t, err, `while replaying declaration 'GateProofs.`+route[0]+`':\n\(kernel\) unknown constant 'GateProofs.`+route[1]+`'`)
	}
	// A claim naming the skipped constant itself is not a checked theorem.
	text := strings.ReplaceAll(strings.Replace(module, "%s", "viaUnsafe", 1), "%s", "cheat")
	text = strings.Replace(text, "  addDecl (.thmDecl", "  if false then addDecl (.thmDecl", 1)
	_, err := gateProof(t, [][2]string{{"GateProofs.cheat", "GateContract.wrong"}}, map[string]string{"GateContract": wrongContract, "GateProofs": text})
	expectError(t, err, `Claim is not a theorem: GateProofs.cheat`)
}

// sorry, native_decide and declared axioms are not approved.
func TestProofGateRejectsUnapprovedAxioms(t *testing.T) {
	module := "import Clamp\naxiom GateProofs.ax : ∀ value limit : UInt64, Clamp.clamp value limit ≤ limit\n" +
		"theorem GateProofs.viaAxiom : ∀ value limit : UInt64, Clamp.clamp value limit ≤ limit := GateProofs.ax\n" +
		"theorem GateProofs.viaSorry : ∀ value limit : UInt64, Clamp.clamp value limit ≤ limit := sorry\n" +
		"def GateProofs.small : Prop := (3 : UInt64) ≤ 5\ntheorem GateProofs.viaNative : GateProofs.small := by unfold GateProofs.small; native_decide\n"
	for claim, pattern := range map[[2]string]string{
		{"GateProofs.viaAxiom", "ClampContract.bounded"}: `Unapproved theorem axiom: GateProofs.viaAxiom: GateProofs.ax\n`,
		{"GateProofs.viaSorry", "ClampContract.bounded"}: `Unapproved theorem axiom: GateProofs.viaSorry: sorryAx\n`,
		{"GateProofs.viaNative", "GateProofs.small"}:     `Unapproved theorem axiom: GateProofs.viaNative: GateProofs.viaNative._native.native_decide.ax`,
	} {
		_, err := gateProof(t, [][2]string{claim}, map[string]string{"GateProofs": module})
		expectError(t, err, pattern)
	}
}

// Modules using the Lean module system are refused with a diagnostic.
func TestProofGateRefusesTheModuleSystem(t *testing.T) {
	_, err := gateProof(t, nil, map[string]string{"GateLemmas": "module\n\npublic theorem GateLemmas.trivial : True := True.intro\n"})
	expectError(t, err, "Workspace module uses the Lean module system, which check-proofs does not support; remove `module` from GateLemmas")
}

// Code run by the build cannot replace the checker or the toolchain it reads,
// and cannot replace the built runtime modules.
func TestProofGateBuildCannotReplaceCheckerToolchainOrRuntime(t *testing.T) {
	forge := "import Clamp\nimport Lean\nrun_cmd do\n" +
		"  IO.FS.createDirAll \".lake/build/lib/lean/LeanGo/Proofs\"\n" +
		"  IO.FS.writeFile \".lake/build/lib/lean/LeanGo/Proofs/Check.olean\" \"forged checker\"\n" +
		"  IO.FS.writeFile \".lake/build/lib/lean/LeanGo/Proofs/Check.lean\" \"forged checker\"\n" +
		"  IO.FS.createDirAll \".lake/build/lib/lean/Lean\"\n" +
		"  IO.FS.writeFile \".lake/build/lib/lean/Lean/Replay.olean\" \"forged toolchain module\"\n" +
		"  IO.FS.writeFile \".lake/build/lib/lean/Lean.olean\" \"forged toolchain module\"\n"
	claims, err := gateProof(t, nil, map[string]string{"GateForge": forge})
	if err != nil || len(claims) != 3 {
		t.Fatalf("claims %v error %v", claims, err)
	}
	// Imports the other proof modules so that it is built after them.
	tamper := "import Clamp\nimport ClampContract\nimport ClampProofs\nimport Lean\nrun_cmd do\n  let path := \".lake/build/lib/lean/Clamp/Bounds.olean\"\n" +
		"  IO.FS.writeBinFile path ((← IO.FS.readBinFile path).push 0)\n"
	_, err = gateProof(t, nil, map[string]string{"GateTamper": tamper})
	expectError(t, err, "proof workspace runtime build changed while proof modules were built")
}

// Finding 2: evidence binds only a package that a fresh export and emission
// of the hash-bound sources reproduce byte for byte.

// packageProject copies the clamp example and returns its contract path.
func packageProject(t *testing.T) (string, string) {
	t.Helper()
	clampFixture(t)
	work := t.TempDir()
	project := filepath.Join(work, "project")
	copyTree(t, "../../examples/proofs/clamp", project)
	return work, filepath.Join(project, "proof-contract.json")
}

func bindEvidence(work, contract, manifest string) error {
	_, err := CheckProofs(CheckProofsOptions{Contract: contract, Manifest: manifest, Output: filepath.Join(work, "evidence.json")})
	return err
}

// rehash records a file's new digest in its package manifest.
func rehash(t *testing.T, manifest, name string) {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal([]byte(readText(t, manifest)), &document); err != nil {
		t.Fatal(err)
	}
	obj(document["generated_files"])[name] = Digest([]byte(readText(t, filepath.Join(filepath.Dir(manifest), name))))
	data, _ := json.MarshalIndent(document, "", "  ")
	writeText(t, manifest, string(data)+"\n")
}

func TestProofGateBindsOnlyRebuiltPackages(t *testing.T) {
	work, contract := packageProject(t)
	project := filepath.Dir(contract)
	for _, pkg := range []string{"clamp", "clampnative"} {
		if err := bindEvidence(work, contract, filepath.Join(project, pkg, "manifest.json")); err != nil {
			t.Fatalf("untouched %s: %v", pkg, err)
		}
	}
	// Edited Go with a re-hashed manifest.
	native := filepath.Join(project, "clampnative")
	original := readText(t, filepath.Join(native, "functions.go"))
	edited := strings.Replace(original, "if v1 <= v2 {", "if v1 >= v2 {", 1)
	if edited == original {
		t.Fatal("clamp comparison not found")
	}
	writeText(t, filepath.Join(native, "functions.go"), edited)
	rehash(t, filepath.Join(native, "manifest.json"), "functions.go")
	expectError(t, bindEvidence(work, contract, filepath.Join(native, "manifest.json")),
		`generated file differs from a fresh emission of its IR: functions.go`)
	// Edited IR with Go re-emitted from it: clamp returns the limit whenever it may.
	var ir map[string]any
	if err := json.Unmarshal([]byte(readText(t, filepath.Join(project, "clamp.ir.json"))), &ir); err != nil {
		t.Fatal(err)
	}
	alts := arr(obj(obj(obj(arr(ir["functions"])[0])["body"])["next"])["alts"])
	obj(obj(alts[0])["body"])["var"], obj(obj(alts[1])["body"])["var"] = 1.0, 2.0
	data, _ := json.Marshal(ir)
	for _, mode := range []string{MeteredMode, NativeMode} {
		files, err := Emit(data, EmitOptions{Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(work, "edited-"+mode)
		if err := PublishPackage(out, files, false); err != nil {
			t.Fatal(err)
		}
		expectError(t, bindEvidence(work, contract, filepath.Join(out, "manifest.json")),
			`generated IR differs from a fresh export of the contract's sources`)
	}
	// A native manifest bound to the metered package's Go files.
	other := filepath.Join(work, "other-mode")
	copyTree(t, filepath.Join(project, "clamp"), other)
	os.Remove(filepath.Join(other, "manifest.json"))
	var manifest map[string]any
	json.Unmarshal([]byte(readText(t, filepath.Join(project, "clampnative", "manifest.json"))), &manifest)
	files := map[string]any{}
	for _, name := range generatedGo {
		files[name] = Digest([]byte(readText(t, filepath.Join(other, name))))
	}
	manifest["generated_files"] = files
	data, _ = json.MarshalIndent(manifest, "", "  ")
	writeText(t, filepath.Join(other, "manifest.json"), string(data)+"\n")
	if _, _, err := bindPackage(t, contract, filepath.Join(other, "manifest.json")); err != nil {
		t.Fatalf("the self-consistent manifest should pass the manifest check: %v", err)
	}
	expectError(t, bindEvidence(work, contract, filepath.Join(other, "manifest.json")),
		`generated directory inventory differs from a fresh emission`)
}

func TestProofGateRebuildsEveryCompositionComponent(t *testing.T) {
	clampFixture(t)
	c := newCompositionFixture(t)
	if err := c.check("output", ""); err != nil {
		t.Fatal(err)
	}
	for _, pair := range c.manifests {
		dir := filepath.Dir(pair[1])
		original := readText(t, filepath.Join(dir, "api.go"))
		manifest := readText(t, pair[1])
		writeText(t, filepath.Join(dir, "api.go"), original+"\n// edited\n")
		rehash(t, pair[1], "api.go")
		expectError(t, c.check("output", filepath.Join(c.root, "again.json")), `component `+pair[0]+`: generated file differs from a fresh emission of its IR: api.go`)
		writeText(t, filepath.Join(dir, "api.go"), original)
		writeText(t, pair[1], manifest)
	}
}
