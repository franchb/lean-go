package pipeline

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Lean-gated ports: RealProofContractTests of scripts/test_proof_contracts.py
// (LEAN_GO_PROOFS=1) and the script JSON case of
// scripts/test_diagnostics_lean.py (LEAN_GO_MODULES=1). The other
// test_diagnostics_lean.py cases are covered with exact report equality by
// TestAdmissionDiagnosticsMatchRecordedPythonReports (LEAN_GO_PARITY=1) and by
// TestDiagnosticsLeanAssertions below.

func clampFixture(t *testing.T) (*Contract, *ProjectSnapshot, *toolInputs) {
	t.Helper()
	gate(t, "LEAN_GO_PROOFS")
	contract, err := LoadContract("../../examples/proofs/clamp/proof-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	project, err := SnapshotProject(contract)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := proofTools()
	if err != nil {
		t.Fatal(err)
	}
	return contract, project, tools
}

func replaceProofDeclaration(t *testing.T, project *ProjectSnapshot, module, startMarker, endMarker, declaration string) *ProjectSnapshot {
	t.Helper()
	changed := &ProjectSnapshot{project.Runtime, map[string]Source{}}
	for k, v := range project.Proofs {
		changed.Proofs[k] = v
	}
	source := changed.Proofs[module]
	text := string(source.Data)
	start := strings.Index(text, startMarker)
	end := start + strings.Index(text[start:], endMarker)
	source.Data = []byte(text[:start] + declaration + "\n\n" + text[end:])
	changed.Proofs[module] = source
	return changed
}

func expectProofError(t *testing.T, contract *Contract, project *ProjectSnapshot, tools *toolInputs, pattern string) {
	t.Helper()
	compiler, _ := SelectedCompiler()
	_, err := prove(contract.Claims, project, tools, compiler)
	expectError(t, err, pattern)
}

func TestExpectedDeclarationKindIsChecked(t *testing.T) {
	contract, project, tools := clampFixture(t)
	for _, declaration := range []string{"axiom bounded : Prop", "theorem bounded : True := True.intro", "opaque bounded : Prop := True"} {
		expectProofError(t, contract, replaceProofDeclaration(t, project, "ClampContract", "def bounded : Prop", "def preserves : Prop", declaration),
			tools, "Expected statement is not a definition")
	}
}

func TestExpectedDefinitionMustBeClosedPropWithoutUniverses(t *testing.T) {
	contract, project, tools := clampFixture(t)
	for _, declaration := range []string{"def bounded : Nat := 0", "def bounded (p : Prop) : Prop := p", "def bounded.{u} : Prop := ∀ (_ : Sort u), True"} {
		expectProofError(t, contract, replaceProofDeclaration(t, project, "ClampContract", "def bounded : Prop", "def preserves : Prop", declaration),
			tools, "closed nonpolymorphic Prop")
	}
}

func TestChangedExpectedPropositionIsRejected(t *testing.T) {
	contract, project, tools := clampFixture(t)
	expectProofError(t, contract, replaceProofDeclaration(t, project, "ClampContract", "def bounded : Prop", "def preserves : Prop", "def bounded : Prop := True"),
		tools, "Theorem type changed")
}

func TestPolymorphicTheoremIsRejected(t *testing.T) {
	contract, project, tools := clampFixture(t)
	changed := &ProjectSnapshot{project.Runtime, map[string]Source{}}
	for k, v := range project.Proofs {
		changed.Proofs[k] = v
	}
	source := changed.Proofs["ClampProofs"]
	source.Data = append(append([]byte{}, source.Data...), []byte("\ntheorem ClampProofs.poly.{u} (A : Sort u) (x : A) : x = x := rfl\n")...)
	changed.Proofs["ClampProofs"] = source
	poly := *contract
	poly.Claims = [][2]string{{"ClampProofs.poly", "ClampContract.bounded"}}
	expectProofError(t, &poly, changed, tools, "Polymorphic theorem is unsupported")
}

func TestCompilationCannotModifySnapshottedRuntimeSource(t *testing.T) {
	contract, project, tools := clampFixture(t)
	changed := &ProjectSnapshot{project.Runtime, map[string]Source{}}
	for k, v := range project.Proofs {
		changed.Proofs[k] = v
	}
	source := changed.Proofs["ClampProofs"]
	source.Data = []byte("import Lean\n" + string(source.Data) + "\nrun_cmd IO.FS.writeFile \"Clamp.lean\" \"-- altered runtime source\\n\"\n")
	changed.Proofs["ClampProofs"] = source
	expectProofError(t, contract, changed, tools, "workspace|staged")
}

func TestExternalProjectExtractsProvesAndRunsWithGoOnly(t *testing.T) {
	clampFixture(t)
	work := t.TempDir()
	project := filepath.Join(work, "project")
	copyTree(t, "../../examples/proofs/clamp", project)
	writeText(t, filepath.Join(project, "lakefile.lean"), "this project build script must never execute")
	writeText(t, filepath.Join(project, ".lake", "build", "lib", "lean", "ClampProofs.olean"), "stale proof cache must never load")
	generate := func(out string) {
		t.Helper()
		if _, err := Generate(GenerateOptions{SourceDir: filepath.Join(project, "source"), Module: "Clamp", Roots: []string{"Clamp.clamp"},
			Out: out, IROutput: out + ".ir.json", Emit: EmitOptions{Mode: "metered"}, Cache: testCache(t)}); err != nil {
			t.Fatal(err)
		}
	}
	pkg, repeated := filepath.Join(work, "generated"), filepath.Join(work, "regenerated")
	generate(pkg)
	contract, manifest := filepath.Join(project, "proof-contract.json"), filepath.Join(pkg, "manifest.json")
	evidence, again := filepath.Join(work, "evidence.json"), filepath.Join(work, "again.json")
	check := func(manifest, mode, path string) error {
		opts := CheckProofsOptions{Contract: contract, Manifest: manifest}
		if mode == "output" {
			opts.Output = path
		} else {
			opts.Evidence = path
		}
		_, err := CheckProofs(opts)
		return err
	}
	if err := check(manifest, "output", evidence); err != nil {
		t.Fatal(err)
	}
	if err := check(manifest, "evidence", evidence); err != nil {
		t.Fatal(err)
	}
	generate(repeated)
	for _, name := range append(generatedGo, "manifest.json") {
		if readText(t, filepath.Join(pkg, name)) != readText(t, filepath.Join(repeated, name)) {
			t.Fatalf("%s is not reproducible", name)
		}
	}
	if err := check(filepath.Join(repeated, "manifest.json"), "output", again); err != nil {
		t.Fatal(err)
	}
	if readText(t, evidence) != readText(t, again) || strings.Contains(readText(t, evidence), work) {
		t.Fatal("evidence differs or contains machine paths")
	}
	runClampConsumer(t, work, pkg)
	assertWorkspaceReplacementsRejected(t, project, manifest, evidence)
	before := readText(t, evidence)
	for _, target := range []string{filepath.Join(pkg, "api.go"), filepath.Join(project, "source", "Clamp", "Bounds.lean")} {
		original := readText(t, target)
		writeText(t, target, original+"\n-- mismatch")
		err := check(manifest, "output", evidence)
		expectError(t, err, "hash")
		if readText(t, evidence) != before {
			t.Fatal("failed check changed evidence")
		}
		writeText(t, target, original)
	}
	var document map[string]any
	json.Unmarshal([]byte(readText(t, contract)), &document)
	document["roots"] = []any{"Clamp.missing"}
	data, _ := json.Marshal(document)
	writeText(t, contract, string(data))
	expectError(t, check(manifest, "output", evidence), "roots")
	if readText(t, evidence) != before {
		t.Fatal("failed check changed evidence")
	}
}

func assertWorkspaceReplacementsRejected(t *testing.T, project, manifest, output string) {
	t.Helper()
	contract, err := LoadContract(filepath.Join(project, "proof-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := SnapshotProject(contract)
	altered := replaceProofDeclaration(t, original, "ClampProofs", "theorem bounded ", "theorem preserves ",
		"theorem bounded (value limit : UInt64) : Clamp.clamp value limit ≤ limit := by sorry")
	var forged []map[string]any
	for _, claim := range contract.Claims {
		forged = append(forged, map[string]any{"name": claim[0], "expected": claim[1], "statement": "FORGED unchecked statement", "axioms": []any{}})
	}
	forgedJSON, _ := json.Marshal(forged)
	quoted, _ := json.Marshal(string(forgedJSON))
	replacement := "import Lean\nrun_cmd IO.FS.writeFile \"claims.json\" " + string(quoted) + "\n"
	replacementQuoted, _ := json.Marshal(replacement)
	// The directory of the former harness no longer exists; create it so the
	// write succeeds and the replacement reaches the workspace.
	injection := "\nrun_cmd do\n  IO.FS.createDirAll \"LeanGo/Proofs\"\n  IO.FS.writeFile \"LeanGo/Proofs/Harness.lean\" " + string(replacementQuoted) + "\n"
	lakefile := "import Lake\nopen Lake DSL\npackage replaced\nlean_lib LeanGo\nlean_lib Clamp\nlean_lib ClampProofs\nlean_lib ClampContract\n"
	lakefileQuoted, _ := json.Marshal(lakefile)
	admitted := altered.Proofs["ClampProofs"]
	scripts := []string{string(admitted.Data) + injection,
		string(original.Proofs["ClampProofs"].Data) + "\nrun_cmd IO.FS.writeFile \"lakefile.lean\" " + string(lakefileQuoted) + "\n"}
	target := filepath.Join(project, "proof", admitted.Relative)
	before := readText(t, output)
	for _, body := range scripts {
		writeText(t, target, "import Lean\n"+body)
		_, err := CheckProofs(CheckProofsOptions{Contract: contract.Path, Manifest: manifest, Output: output})
		expectError(t, err, "Unapproved theorem axiom|workspace|harness|staged")
		if readText(t, output) != before {
			t.Fatal("evidence changed")
		}
	}
	writeText(t, target, string(original.Proofs["ClampProofs"].Data))
}

func runClampConsumer(t *testing.T, work, pkg string) {
	t.Helper()
	consumer := filepath.Join(work, "consumer")
	for _, name := range generatedGo {
		writeText(t, filepath.Join(consumer, "clamp", name), readText(t, filepath.Join(pkg, name)))
	}
	writeText(t, filepath.Join(consumer, "go.mod"), "module consumer\n\ngo 1.27.4\n")
	writeText(t, filepath.Join(consumer, "main.go"), `package main
import("fmt";"os/exec";clamp "consumer/clamp")
func main(){
for _,tool:=range []string{"lean","lake","uv","python","python3","cc","gcc","clang"}{if _,err:=exec.LookPath(tool);err==nil{panic("producer available")}}
for _,pair:=range [][2]uint64{{0,0},{0,^uint64(0)},{^uint64(0),0},{^uint64(0),^uint64(0)},{9007199254740993,9007199254740992},{42,99}}{
want:=pair[0];if want>pair[1]{want=pair[1]};got,err:=clamp.RunTyped(pair[0],pair[1],clamp.DefaultLimits());if err!=nil||got!=want{panic("clamp mismatch")}
}
fmt.Println("proof-contract Go-only consumer: OK")
}
`)
	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	cmd := exec.Command(goBinary, "run", "-buildvcs=false", ".")
	cmd.Dir = consumer
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=", "PATH="+filepath.Dir(goBinary))
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "proof-contract Go-only consumer: OK\n" {
		t.Fatalf("%v\n%s", err, out)
	}
}

// scripts/test_diagnostics_lean.py: the individual assertions of each case,
// on top of the exact-report parity test.

func diagnosticOf(t *testing.T, err error) map[string]any {
	t.Helper()
	data, _ := EncodeCompactOrdered(Report(err))
	var report map[string]any
	json.Unmarshal(data, &report)
	return obj(arr(report["diagnostics"])[0])
}

func TestDiagnosticsLeanAssertions(t *testing.T) {
	requireModules(t)
	reject := func(t *testing.T, files map[string]string, module, root string, harness string) map[string]any {
		t.Helper()
		f := newLeanFixture(t)
		for name, text := range files {
			f.write(name, text)
		}
		if harness != "" {
			f.harness = func(string, []string) string { return harness }
		}
		return diagnosticOf(t, f.reject(module, root, ""))
	}
	csimp := readText(t, "../../compatibility/reproducers/CustomCsimpDiagnostic.lean")
	t.Run("source_audit_names_imported_foreign_dependency", func(t *testing.T) {
		item := reject(t, map[string]string{
			"Helper.lean":  "module\r\npublic section\r\n@[extern \"lean_go_untrusted\"] opaque foreign : UInt32 → UInt32\r\n",
			"Example.lean": "module\npublic import Helper\npublic def run (x : UInt32) : UInt32 := foreign x\n"}, "Example", "run", "")
		location := obj(item["location"])
		if item["stage"] != "admission" || item["code"] != "source_audit" || item["declaration"] != "foreign" ||
			!strings.Contains(str2(item["message"]), "extern source dependency: foreign") || location["path"] != "Helper.lean" ||
			obj(location["start"])["line"] != 3.0 || location["declaration"] != "foreign" {
			t.Fatal(item)
		}
	})
	t.Run("custom_csimp_is_explicit_source_audit_rejection", func(t *testing.T) {
		item := reject(t, map[string]string{"Example.lean": csimp}, "Example", "run", "")
		location := obj(item["location"])
		if item["stage"] != "admission" || item["code"] != "source_audit" || item["declaration"] != "original" || item["compiler_form"] != "csimp" ||
			!strings.Contains(str2(item["message"]), "Unsupported historical compiler rewrite: original -> replacement via replacement_eq") ||
			location["path"] != "Example.lean" || location["declaration"] != "original" || obj(location["start"])["line"] != 1.0 {
			t.Fatal(item)
		}
	})
	t.Run("malformed_custom_csimp_remains_build_failure", func(t *testing.T) {
		item := reject(t, map[string]string{"Example.lean": csimp + "def broken : UInt32 := )\n"}, "Example", "run", "")
		if item["stage"] != "build" || item["code"] != "producer_failed" || item["declaration"] != nil || item["compiler_form"] != nil ||
			obj(item["location"])["path"] != "Example.lean" || obj(obj(item["location"])["start"])["line"] != 6.0 {
			t.Fatal(item)
		}
	})
	t.Run("rewrite_witness_integrity_failure_remains_build_failure", func(t *testing.T) {
		item := reject(t, map[string]string{"Example.lean": "def run (x : UInt32) : UInt32 := x\n"}, "Example", "run",
			"import LeanGo.Module.Export\nimport Example\nopen Lean LeanGo.Module\nrun_cmd Lean.Elab.Command.liftCoreM do\n"+
				"  withEnv (← mkEmptyEnvironment) do\n    addDecl <| .axiomDecl { name := ``Bool.not_eq_internalNot, levelParams := [], type := .sort .zero, isUnsafe := false }\n"+
				"    discard <| reportAdmission <| rewriteJson { source := ``Bool.not, target := ``Bool.Internal.not, theoremName := ``Bool.not_eq_internalNot }\n")
		if item["stage"] != "build" || item["code"] != "producer_failed" || !strings.Contains(str2(item["message"]), "Compiler rewrite witness is not a theorem") ||
			item["declaration"] != nil || item["compiler_form"] != nil {
			t.Fatal(item)
		}
	})
	t.Run("polymorphic_signature_is_explicit_type_rejection", func(t *testing.T) {
		item := reject(t, map[string]string{"Example.lean": "def run {α : Type} (x : α) : α := x\n"}, "Example", "run", "")
		if item["stage"] != "admission" || item["code"] != "unsupported_type" || item["declaration"] != "run" ||
			!strings.Contains(str2(item["message"]), "Unsupported first-order monomorphic type") ||
			obj(obj(item["location"])["start"])["column"] != 4.0 || item["compiler_form"] != nil {
			t.Fatal(item)
		}
	})
	t.Run("unsupported_constructor_field_has_its_known_declaration_range", func(t *testing.T) {
		item := reject(t, map[string]string{"Example.lean": "inductive Choice where\n  | number (x : UInt32)\n  | fn (f : UInt32 → UInt32)\ndef run (x : Choice) : Choice := x\n"}, "Example", "run", "")
		location := obj(item["location"])
		if item["stage"] != "admission" || item["code"] != "unsupported_type" || item["declaration"] != "Choice.fn" || location["path"] != "Example.lean" ||
			location["declaration"] != "Choice.fn" || obj(location["start"])["line"] != 3.0 || obj(location["start"])["column"] != 4.0 {
			t.Fatal(item)
		}
	})
	for _, c := range []struct {
		expression string
		column     int
	}{{")", 32}, {"missing", 33}} {
		t.Run("syntax_failure_"+c.expression, func(t *testing.T) {
			item := reject(t, map[string]string{"Example.lean": "def run (x : UInt32) : UInt32 := " + c.expression + "\n"}, "Example", "run", "")
			if item["stage"] != "build" || item["code"] != "producer_failed" || item["declaration"] != nil {
				t.Fatal(item)
			}
			point := map[string]int{"line": 1, "column": c.column, "utf16_column": c.column, "byte_offset": c.column}
			jsonEqual(t, item["location"], mustJSON(map[string]any{"path": "Example.lean", "kind": "compiler", "declaration": nil, "start": point, "end": point}))
		})
	}
	for _, c := range [][3]string{
		{"encode (.unreach (.const ``UInt32 [])) [] [] \"UInt32\"", "unsupported_lcnf_control", "LCNF.Code.unreach"},
		{"encode (.let { fvarId := ⟨`x⟩, binderName := `x, type := .const ``UInt32 [], value := .erased } (.return ⟨`x⟩)) [] [] \"UInt32\"", "unsupported_lcnf_value", "LCNF.LetValue.erased"},
	} {
		t.Run("encoder_"+c[2], func(t *testing.T) {
			harness := "import LeanGo.Module.Export\nimport Example\nopen Lean LeanGo.Module Lean.Compiler.LCNF\nrun_cmd Lean.Elab.Command.liftCoreM do\n" +
				"  discard <| reportAdmission <| withDeclaration `run do\n    let (result, _) ← (" + c[0] + ").run {}\n    return result\n"
			item := reject(t, map[string]string{"Example.lean": "def run (x : UInt32) : UInt32 := x\n"}, "Example", "run", harness)
			if item["stage"] != "admission" || item["code"] != c[1] || item["compiler_form"] != c[2] || item["declaration"] != "run" {
				t.Fatal(item)
			}
		})
	}
	for _, c := range [][2]string{{"Local.run._lam_0", "Local.run"}, {"Unknown.run._lam_0", ""}} {
		t.Run("generated_helper_"+c[0], func(t *testing.T) {
			harness := "import LeanGo.Module.Export\nimport Local\nopen Lean LeanGo.Module\nrun_cmd Lean.Elab.Command.liftCoreM do\n" +
				"  discard <| reportAdmission <| withDeclaration `" + c[0] + " do\n    reject \"unsupported_lcnf_control\" \"Use first-order calls.\" m!\"test encoder rejection\"\n"
			item := reject(t, map[string]string{"Local.lean": "namespace Local\n" + localAdd + localScalar + "end Local\n"}, "Local", "Local.run", harness)
			if item["declaration"] != c[0] {
				t.Fatal(item)
			}
			if c[1] == "" {
				if item["location"] != nil {
					t.Fatal(item["location"])
				}
			} else if obj(item["location"])["kind"] != "source_owner" || obj(item["location"])["declaration"] != c[1] {
				t.Fatal(item["location"])
			}
		})
	}
}
