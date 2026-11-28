package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/franchb/lean-go/producer"
)

// Proof contracts and composition contracts at the protocol level; the tests
// that run Lean are in lean_proofs_test.go.

type contractFixture struct {
	t            *testing.T
	root         string
	contractPath string
	source       string
	manifestPath string
	document     map[string]any
	manifest     map[string]any
	compiler     Compiler
}

func newContractFixture(t *testing.T) *contractFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	compiler, _ := SelectedCompiler()
	f := &contractFixture{t: t, root: root, contractPath: filepath.Join(root, "proof-contract.json"),
		source: filepath.Join(root, "source", "Example.lean"), manifestPath: filepath.Join(root, "generated", "manifest.json"), compiler: compiler}
	writeText(t, f.source, "namespace Example\ndef identity (n : UInt64) := n\nend Example\n")
	writeText(t, filepath.Join(root, "proof", "ExampleProofs.lean"), "import Example\ntheorem ExampleProofs.identity (n : UInt64) : Example.identity n = n := rfl\n")
	writeText(t, filepath.Join(root, "proof", "ExampleContract.lean"), "import Example\ndef ExampleContract.identity : Prop := ∀ n : UInt64, Example.identity n = n\n")
	f.document = map[string]any{"schema": ContractSchema, "source_dir": "source", "proof_dir": "proof", "module": "Example",
		"roots": []any{"Example.identity"}, "claims": []any{map[string]any{"theorem": "ExampleProofs.identity", "expected": "ExampleContract.identity"}}}
	f.writeContract(nil)
	for _, name := range generatedGo {
		writeText(t, filepath.Join(root, "generated", name), "package extracted // "+name)
	}
	f.writeManifest()
	return f
}

func (f *contractFixture) writeContract(document any) {
	if document == nil {
		document = f.document
	}
	data, _ := json.Marshal(document)
	if err := os.WriteFile(f.contractPath, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *contractFixture) project() (*Contract, *ProjectSnapshot, error) {
	contract, err := LoadContract(f.contractPath)
	if err != nil {
		return nil, nil, err
	}
	project, err := SnapshotProject(contract)
	return contract, project, err
}

func (f *contractFixture) writeManifest() {
	contract, project, err := f.project()
	if err != nil {
		f.t.Fatal(err)
	}
	inputs := map[string]any{}
	for _, s := range project.Runtime {
		inputs["source/"+s.Relative] = Digest(s.Data)
	}
	files := map[string]any{}
	for _, name := range generatedGo {
		files[name] = Digest([]byte(readText(f.t, filepath.Join(filepath.Dir(f.manifestPath), name))))
	}
	f.manifest = map[string]any{"schema": "lean-go-manifest/v24", "generated_files": files, "program": map[string]any{
		"schema": Schema, "profile": Profile, "stage": Stage, "module": contract.Module, "roots": contract.Roots, "features": []any{},
		"lean_version": f.compiler.Version, "lean_commit": f.compiler.Commit, "inputs": inputs}}
	f.saveManifest()
}

func (f *contractFixture) saveManifest() {
	data, _ := json.Marshal(f.manifest)
	if err := os.WriteFile(f.manifestPath, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *contractFixture) program() map[string]any { return obj(f.manifest["program"]) }

// mutate changes a manifest copy and restores the original afterwards.
func (f *contractFixture) withManifest(change func(m map[string]any), check func()) {
	original, _ := json.Marshal(f.manifest)
	change(f.manifest)
	f.saveManifest()
	check()
	json.Unmarshal(original, &f.manifest)
	f.saveManifest()
}

func (f *contractFixture) verifyManifest() ([]byte, error) {
	contract, project, err := f.project()
	if err != nil {
		return nil, err
	}
	return VerifyManifest(f.manifestPath, contract, project, f.compiler)
}

func (f *contractFixture) mustFail(label string, err error) {
	f.t.Helper()
	if err == nil {
		f.t.Errorf("%s: accepted", label)
	}
}

func sampleClaims(contract *Contract) []any {
	var out []any
	for _, claim := range contract.Claims {
		o := NewObject()
		o.Set("name", claim[0])
		o.Set("expected", claim[1])
		o.Set("statement", "checked proposition")
		o.Set("axioms", []any{"propext"})
		out = append(out, o)
	}
	return out
}

// fakeRebuild replaces the Lean re-export of generated packages
// (proof_rebuild.go; the Lean tests are in lean_proofgate_test.go).
func fakeRebuild(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := rebuildPackage
	rebuildPackage = func(string, *Contract, *ProjectSnapshot, string) error {
		*calls++
		return nil
	}
	t.Cleanup(func() { rebuildPackage = old })
	return calls
}

// fakeProve replaces the Lean proof run and package rebuild; side runs before
// returning claims.
func fakeProve(t *testing.T, side func() error) *int {
	t.Helper()
	fakeRebuild(t)
	calls := new(int)
	old := proveClaims
	proveClaims = func(claims [][2]string, project *ProjectSnapshot, tools *toolInputs, c Compiler) ([]any, error) {
		*calls++
		if side != nil {
			if err := side(); err != nil {
				return nil, err
			}
		}
		return sampleClaims(&Contract{Claims: claims}), nil
	}
	t.Cleanup(func() { proveClaims = old })
	return calls
}

func copyDocument(d map[string]any) map[string]any {
	data, _ := json.Marshal(d)
	var out map[string]any
	json.Unmarshal(data, &out)
	return out
}

func TestStrictDocumentSchemaAndDuplicateKeys(t *testing.T) {
	f := newContractFixture(t)
	documents := []any{nil, []any{}, map[string]any{}}
	changed := copyDocument(f.document)
	changed["schema"] = "other"
	documents = append(documents, changed)
	changed = copyDocument(f.document)
	changed["hooks"] = []any{}
	documents = append(documents, changed)
	for key := range f.document {
		changed := copyDocument(f.document)
		delete(changed, key)
		documents = append(documents, changed)
	}
	for _, document := range documents {
		data, _ := json.Marshal(document)
		os.WriteFile(f.contractPath, data, 0o644)
		_, err := LoadContract(f.contractPath)
		f.mustFail(string(data), err)
	}
	for _, raw := range []string{`{`, `{"schema":1,"schema":2}`, `{"schema":NaN}`} {
		os.WriteFile(f.contractPath, []byte(raw), 0o644)
		_, err := LoadContract(f.contractPath)
		f.mustFail(raw, err)
	}
}

func TestRootsAndClaimsAreBoundedUniqueAndCanonical(t *testing.T) {
	f := newContractFixture(t)
	f.document["roots"] = []any{"Example.z", "Example.identity"}
	f.document["claims"] = append([]any{map[string]any{"theorem": "ExampleProofs.z", "expected": "ExampleContract.identity"}}, arr(f.document["claims"])...)
	f.writeContract(nil)
	contract, err := LoadContract(f.contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(contract.Roots, []string{"Example.identity", "Example.z"}) || contract.Claims[0][0] != "ExampleProofs.identity" || contract.Claims[1][0] != "ExampleProofs.z" {
		t.Fatal(contract.Roots, contract.Claims)
	}
	var tooManyRoots, tooManyClaims []any
	for i := 0; i < 65; i++ {
		tooManyRoots = append(tooManyRoots, fmt.Sprintf("Example.f%d", i))
		tooManyClaims = append(tooManyClaims, map[string]any{"theorem": fmt.Sprintf("Proof.f%d", i), "expected": "Expected.p"})
	}
	claims := arr(f.document["claims"])
	for key, values := range map[string][]any{
		"roots":  {nil, []any{}, "Example.identity", []any{"Example.identity", "Example.identity"}, tooManyRoots},
		"claims": {nil, []any{}, map[string]any{}, append(append([]any{}, claims...), claims...), tooManyClaims},
	} {
		for _, value := range values {
			changed := copyDocument(f.document)
			changed[key] = value
			f.writeContract(changed)
			_, err := LoadContract(f.contractPath)
			f.mustFail(fmt.Sprintf("%s=%v", key, value), err)
		}
	}
}

func TestNamesAndClaimFieldsAreStrict(t *testing.T) {
	f := newContractFixture(t)
	for _, name := range []any{nil, 1, "", "A..b", "A.b c", "A.λ", "../A", "A.b\n"} {
		for _, field := range []string{"module", "root", "theorem", "expected"} {
			document := copyDocument(f.document)
			switch field {
			case "module":
				document["module"] = name
			case "root":
				document["roots"] = []any{name}
			default:
				obj(arr(document["claims"])[0])[field] = name
			}
			f.writeContract(document)
			_, err := LoadContract(f.contractPath)
			f.mustFail(fmt.Sprintf("%s=%v", field, name), err)
		}
	}
	for _, claim := range []any{nil, []any{}, map[string]any{"theorem": "Proof.p"}, map[string]any{"expected": "Expected.p"},
		map[string]any{"theorem": "Proof.p", "expected": "Expected.p", "axioms": []any{}}} {
		document := copyDocument(f.document)
		document["claims"] = []any{claim}
		f.writeContract(document)
		_, err := LoadContract(f.contractPath)
		f.mustFail(fmt.Sprint(claim), err)
	}
}

func TestContractDirectoriesAreCanonicalRelativeAndNonoverlapping(t *testing.T) {
	f := newContractFixture(t)
	for _, path := range []any{nil, "", ".", "..", "../source", "/tmp/source", "./source", "source/", "source//nested",
		"source/../source", `C:\source`, `source\nested`, "source\x00", "source\n", "source.", "source ", "source/*"} {
		for _, field := range []string{"source_dir", "proof_dir"} {
			document := copyDocument(f.document)
			document[field] = path
			f.writeContract(document)
			_, _, err := f.project()
			f.mustFail(fmt.Sprintf("%s=%q", field, path), err)
		}
	}
	document := copyDocument(f.document)
	document["proof_dir"] = "source"
	f.writeContract(document)
	_, _, err := f.project()
	f.mustFail("same directories", err)
	writeText(t, filepath.Join(f.root, "source", "nested", "Proof.lean"), "def nested := 0")
	document["proof_dir"] = "source/nested"
	f.writeContract(document)
	_, _, err = f.project()
	f.mustFail("nested directories", err)
}

func TestSymlinksModuleCollisionsAndReservedModulesFail(t *testing.T) {
	f := newContractFixture(t)
	linked := filepath.Join(f.root, "source", "Linked.lean")
	symlinkOrSkip(t, f.source, linked)
	_, _, err := f.project()
	f.mustFail("symlinked source", err)
	os.Remove(linked)
	collision := filepath.Join(f.root, "proof", "Example.lean")
	writeText(t, collision, readText(t, f.source))
	_, _, err = f.project()
	f.mustFail("module collision", err)
	os.Remove(collision)
	for _, directory := range []string{"source", "proof"} {
		forbidden := filepath.Join(f.root, directory, "Init.lean")
		writeText(t, forbidden, "def reserved := 0")
		_, _, err = f.project()
		f.mustFail(directory+" reserved module", err)
		os.Remove(forbidden)
	}
	target := filepath.Join(f.root, "contract-target.json")
	os.Rename(f.contractPath, target)
	symlinkOrSkip(t, target, f.contractPath)
	_, _, err = f.project()
	f.mustFail("symlinked contract", err)
}

func TestBuildFilesAndCachesAreNotProjectInputs(t *testing.T) {
	f := newContractFixture(t)
	for _, relative := range []string{"source/lakefile.lean", "proof/lakefile.lean", "source/.lake/Bad.lean", "proof/.git/Bad.lean"} {
		writeText(t, filepath.Join(f.root, relative), "poison")
	}
	_, project, err := f.project()
	if err != nil {
		t.Fatal(err)
	}
	modules, inputs := keysOf(project.modules()), keysOf(project.inputs())
	slices.Sort(modules)
	slices.Sort(inputs)
	if !slices.Equal(modules, []string{"Example", "ExampleContract", "ExampleProofs"}) ||
		!slices.Equal(inputs, []string{"proof/ExampleContract.lean", "proof/ExampleProofs.lean", "source/Example.lean"}) {
		t.Fatal(modules, inputs)
	}
}

func TestCombinedLimitsIncludeRuntimeAndProofs(t *testing.T) {
	f := newContractFixture(t)
	for _, directory := range []string{"source", "proof"} {
		for i := 0; i < 128; i++ {
			writeText(t, filepath.Join(f.root, directory, fmt.Sprintf("%s%d.lean", strings.ToUpper(directory[:1])+directory[1:], i)), "-- file")
		}
	}
	_, _, err := f.project()
	expectError(t, err, "limit|exceeds|bound")
	g := newContractFixture(t)
	big := "--" + strings.Repeat("x", 4*1024*1024)
	writeText(t, g.source, big)
	writeText(t, filepath.Join(g.root, "proof", "ExampleProofs.lean"), big)
	_, _, err = g.project()
	expectError(t, err, "limit|exceeds|bound")
}

func TestContractSizeAndMissingRuntimeModuleAreRejected(t *testing.T) {
	f := newContractFixture(t)
	os.WriteFile(f.contractPath, []byte(strings.Repeat(" ", 64*1024+1)), 0o644)
	_, err := LoadContract(f.contractPath)
	expectError(t, err, "limit")
	document := copyDocument(f.document)
	document["module"] = "Missing"
	f.writeContract(document)
	_, _, err = f.project()
	expectError(t, err, "module")
}

func TestOnlyCurrentManifestIRAndProfileAreAccepted(t *testing.T) {
	f := newContractFixture(t)
	for _, multi := range []bool{false, true} {
		if multi {
			f.document["roots"] = append(arr(f.document["roots"]), "Example.another")
			f.writeContract(nil)
			f.writeManifest()
		}
		raw, err := f.verifyManifest()
		if err != nil || string(raw) != readText(t, f.manifestPath) {
			t.Fatal(err)
		}
		for version := 1; version < 26; version++ {
			if version == 24 {
				continue
			}
			stale := fmt.Sprintf("lean-go-manifest/v%d", version)
			f.withManifest(func(m map[string]any) { m["schema"] = stale }, func() {
				_, err := f.verifyManifest()
				expectError(t, err, stale+".*regenerate")
			})
			f.withManifest(func(m map[string]any) { obj(m["program"])["schema"] = fmt.Sprintf("lean-go-ir/v%d", version) }, func() {
				_, err := f.verifyManifest()
				f.mustFail("ir", err)
			})
			if version != 22 {
				f.withManifest(func(m map[string]any) { obj(m["program"])["profile"] = fmt.Sprintf("pure-module/v%d", version) }, func() {
					_, err := f.verifyManifest()
					f.mustFail("profile", err)
				})
			}
		}
		for _, schema := range []any{nil, []any{}, map[string]any{}, 8, true} {
			f.withManifest(func(m map[string]any) { m["schema"] = schema }, func() {
				_, err := f.verifyManifest()
				f.mustFail(fmt.Sprint(schema), err)
			})
		}
	}
}

func TestSingleAndMultiRootManifestProfiles(t *testing.T) {
	f := newContractFixture(t)
	if _, err := f.verifyManifest(); err != nil {
		t.Fatal(err)
	}
	f.document["roots"] = append(arr(f.document["roots"]), "Example.another")
	f.writeContract(nil)
	f.writeManifest()
	if _, err := f.verifyManifest(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		key   string
		value any
	}{{"roots", []any{"Example.identity"}}, {"root", "Example.identity"}, {"lean_commit", "other"}, {"lean_version", "4.0.0"},
		{"profile", "pure-module/v23"}, {"schema", "lean-go-ir/v25"}, {"module", "Other"}} {
		f.withManifest(func(m map[string]any) { obj(m["program"])[change.key] = change.value }, func() {
			_, err := f.verifyManifest()
			f.mustFail(change.key, err)
		})
	}
}

func TestSingleRootUsesExactRootsArray(t *testing.T) {
	f := newContractFixture(t)
	for _, change := range []struct {
		key   string
		value any
	}{{"root", "Example.identity"}, {"roots", "Example.identity"}, {"roots", []any{"Example.other"}}, {"roots", []any{}}} {
		f.withManifest(func(m map[string]any) { obj(m["program"])[change.key] = change.value }, func() {
			_, err := f.verifyManifest()
			f.mustFail(fmt.Sprint(change), err)
		})
	}
}

func TestSourceHashAndCompleteRuntimeInventoryAreBound(t *testing.T) {
	f := newContractFixture(t)
	injected := map[string]any{"source/Injected.lean": strings.Repeat("0", 64)}
	for k, v := range obj(f.program()["inputs"]) {
		injected[k] = v
	}
	for _, entries := range []map[string]any{{}, {"source/Example.lean": strings.Repeat("0", 64)}, injected} {
		f.withManifest(func(m map[string]any) { obj(m["program"])["inputs"] = entries }, func() {
			_, err := f.verifyManifest()
			expectError(t, err, "source|runtime")
		})
	}
	writeText(t, filepath.Join(f.root, "source", "New.lean"), "def extra := 1")
	_, err := f.verifyManifest()
	expectError(t, err, "source|runtime")
}

func TestGeneratedInventoryHashesAndSymlinksAreChecked(t *testing.T) {
	f := newContractFixture(t)
	for _, name := range []string{"extra.go", "../runtime.go", "/tmp/runtime.go"} {
		f.withManifest(func(m map[string]any) { obj(m["generated_files"])[name] = strings.Repeat("0", 64) }, func() {
			_, err := f.verifyManifest()
			expectError(t, err, "inventory")
		})
	}
	dir := filepath.Dir(f.manifestPath)
	for _, name := range []string{"injected.go", "injected.s", "extra.json"} {
		writeText(t, filepath.Join(dir, name), "unlisted")
		_, err := f.verifyManifest()
		expectError(t, err, "inventory")
		os.Remove(filepath.Join(dir, name))
	}
	generated := filepath.Join(dir, "api.go")
	original := readText(t, generated)
	writeText(t, generated, "corrupted")
	_, err := f.verifyManifest()
	expectError(t, err, "hash")
	writeText(t, generated, original)
	target := filepath.Join(f.root, "outside.go")
	os.Rename(generated, target)
	symlinkOrSkip(t, target, generated)
	_, err = f.verifyManifest()
	expectError(t, err, "symlink|regular")
}

func TestMalformedManifestFailsClosed(t *testing.T) {
	f := newContractFixture(t)
	for _, raw := range []string{`[]`, `null`, `{`, `{"schema":1,"schema":2}`} {
		os.WriteFile(f.manifestPath, []byte(raw), 0o644)
		_, err := f.verifyManifest()
		f.mustFail(raw, err)
	}
}

func TestManifestCannotReuseGeneratedGoFilename(t *testing.T) {
	f := newContractFixture(t)
	raw := readText(t, f.manifestPath)
	os.Remove(f.manifestPath)
	f.manifestPath = filepath.Join(filepath.Dir(f.manifestPath), "api.go")
	writeText(t, f.manifestPath, raw)
	_, err := f.verifyManifest()
	f.mustFail("manifest named api.go", err)
}

func claimsValue(t *testing.T, claims []any) []any {
	t.Helper()
	data, _ := EncodeCompactOrdered(claims)
	return parseValue(t, string(data)).([]any)
}

func TestClaimRecordsMatchNamesExpectationsAndAxiomPolicy(t *testing.T) {
	f := newContractFixture(t)
	contract, _, _ := f.project()
	if _, err := validateClaims(claimsValue(t, sampleClaims(contract)), contract.Claims); err != nil {
		t.Fatal(err)
	}
	for field, values := range map[string][]any{"name": {"Other.claim"}, "expected": {"Other.expected"}, "statement": {"", nil},
		"axioms": {"propext", []any{nil}, []any{"propext", "propext"}, []any{"sorryAx"}, []any{"Custom.trust"}, []any{"Lean.ofReduceBool"}, []any{"Lean.trustCompiler"}}} {
		for _, value := range values {
			claims := claimsValue(t, sampleClaims(contract))
			claims[0].(*Object).Set(field, value)
			// Round trip so values have their decoded JSON types.
			_, err := validateClaims(claimsValue(t, claims), contract.Claims)
			f.mustFail(fmt.Sprintf("%s=%v", field, value), err)
		}
	}
	for _, claims := range [][]any{nil, {}, append(sampleClaims(contract), sampleClaims(contract)...)} {
		var value any
		if claims != nil {
			value = claimsValue(t, claims)
		}
		_, err := validateClaims(value, contract.Claims)
		f.mustFail(fmt.Sprint(len(claims)), err)
	}
}

func TestEvidenceIsDeterministicAndBindsProjectToolAndManifest(t *testing.T) {
	f := newContractFixture(t)
	fakeProve(t, nil)
	raw, _, err := Evidence(f.contractPath, f.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	again, _, _ := Evidence(f.contractPath, f.manifestPath)
	if string(raw) != string(again) {
		t.Fatal("evidence is not deterministic")
	}
	var evidence map[string]any
	json.Unmarshal(raw, &evidence)
	_, project, _ := f.project()
	if evidence["lean_version"] != f.compiler.Version || evidence["lean_commit"] != f.compiler.Commit || evidence["schema"] != EvidenceSchema {
		t.Fatal(evidence)
	}
	if evidence["contract_sha256"] != Digest([]byte(readText(t, f.contractPath))) || evidence["generated_manifest_sha256"] != Digest([]byte(readText(t, f.manifestPath))) {
		t.Fatal("contract or manifest digest")
	}
	jsonEqual(t, evidence["project_inputs"], mustJSON(digestInputs(project.inputs())))
	tools, _ := proofTools()
	jsonEqual(t, evidence["tool_inputs"], mustJSON(tools.digests))
	for _, key := range []string{ToolInput, ProofToolInput, "producer/LeanGo/Proofs/Check.lean", "producer/lean-toolchain"} {
		if _, ok := obj(evidence["tool_inputs"])[key]; !ok {
			t.Errorf("tool input %s missing", key)
		}
	}
	if strings.Contains(string(raw), f.root) || strings.Contains(string(raw), "/home/") {
		t.Fatal("evidence contains machine paths")
	}
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestProjectContractAndGeneratedMutationsDuringProvingFail(t *testing.T) {
	f := newContractFixture(t)
	for _, target := range []string{f.source, filepath.Join(f.root, "proof", "ExampleProofs.lean"), f.contractPath, f.manifestPath,
		filepath.Join(filepath.Dir(f.manifestPath), "api.go")} {
		original := readText(t, target)
		suffix := "\n-- changed"
		if strings.HasSuffix(target, ".json") {
			suffix = "\n"
		}
		fakeProve(t, func() error { writeText(t, target, original+suffix); return nil })
		_, _, err := Evidence(f.contractPath, f.manifestPath)
		f.mustFail(filepath.Base(target), err)
		writeText(t, target, original)
	}
}

func TestProofToolIdentityIsPinnedAndEmbedded(t *testing.T) {
	// The checker and the pin are embedded in the binary, so what can be
	// checked here is the pin and the identity.
	tools, err := proofTools()
	if err != nil {
		t.Fatal(err)
	}
	pin, _ := os.ReadFile("../../producer/lean-toolchain")
	checker, _ := os.ReadFile("../../producer/LeanGo/Proofs/Check.lean")
	if tools.digests["producer/lean-toolchain"] != Digest(pin) || tools.digests["producer/LeanGo/Proofs/Check.lean"] != Digest(checker) {
		t.Fatal("embedded proof tools differ from the checkout")
	}
	if string(pin) != string(DefaultCompiler.PinFile()) {
		t.Fatal("checkout pin differs from the default compiler")
	}
}

func evidenceOptions(f *contractFixture, mode, path string) CheckProofsOptions {
	opts := CheckProofsOptions{Contract: f.contractPath, Manifest: f.manifestPath}
	if mode == "output" {
		opts.Output = path
	} else {
		opts.Evidence = path
	}
	return opts
}

func TestCLIRechecksStaleEvidenceAndPreservesOutputOnFailure(t *testing.T) {
	f := newContractFixture(t)
	fakeProve(t, nil)
	output := filepath.Join(f.root, "proof-evidence.json")
	if _, err := CheckProofs(evidenceOptions(f, "output", output)); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckProofs(evidenceOptions(f, "evidence", output)); err != nil {
		t.Fatal(err)
	}
	original := readText(t, output)
	for _, raw := range []string{`{}`, `{`, `{"schema":1,"schema":2}`, strings.Replace(original, "propext", "badAx", 1)} {
		writeText(t, output, raw)
		_, err := CheckProofs(evidenceOptions(f, "evidence", output))
		f.mustFail(raw[:min(len(raw), 20)], err)
		if readText(t, output) != raw {
			t.Fatal("verification changed evidence")
		}
	}
	writeText(t, output, original)
	old := proveClaims
	proveClaims = func([][2]string, *ProjectSnapshot, *toolInputs, Compiler) ([]any, error) {
		return nil, exportError("proof failed")
	}
	_, err := CheckProofs(evidenceOptions(f, "output", output))
	proveClaims = old
	expectError(t, err, "proof failed")
	if readText(t, output) != original {
		t.Fatal("failed check changed evidence")
	}
}

func TestCLIOutputCannotReplaceInputsOrEnterGeneratedPackage(t *testing.T) {
	f := newContractFixture(t)
	calls := fakeProve(t, nil)
	dir := filepath.Dir(f.manifestPath)
	for _, output := range []string{f.contractPath, f.source, filepath.Join(f.root, "proof", "ExampleProofs.lean"), f.manifestPath,
		filepath.Join(dir, "api.go"), filepath.Join(dir, "evidence.json"), filepath.Join(dir, "nested", "evidence.json")} {
		before, _ := os.ReadFile(output)
		_, err := CheckProofs(evidenceOptions(f, "output", output))
		f.mustFail(output, err)
		after, _ := os.ReadFile(output)
		if string(before) != string(after) {
			t.Fatalf("%s changed", output)
		}
	}
	if *calls != 0 {
		t.Fatal("proofs ran for a rejected output path")
	}
	alias := filepath.Join(f.root, "input-alias.json")
	symlinkOrSkip(t, f.source, alias)
	_, err := CheckProofs(evidenceOptions(f, "output", alias))
	f.mustFail("symlinked output", err)
}

// fakeProofLean stands in for Lean during prove().
func fakeProofLean(t *testing.T, f *contractFixture, onRun func(command []string, workspace string) (string, bool)) *[][]string {
	t.Helper()
	commands := new([][]string)
	oldRun, oldLook := run, lookPath
	lookPath = func(name string) (string, bool) { return name, true }
	run = func(command []string, workspace string, env []string) (string, error) {
		*commands = append(*commands, command)
		if out, handled := onRun(command, workspace); handled {
			return out, nil
		}
		if command[len(command)-1] == "--version" {
			os.WriteFile(filepath.Join(workspace, "lake-manifest.json"), []byte(`{"packages":[]}`), 0o644)
			return fmt.Sprintf("Lean (version %s, test, commit %s, Release)", f.compiler.Version, f.compiler.Commit), nil
		}
		if slices.Contains(command, "build") {
			// Lake writes one .olean per requested module.
			for _, module := range command[slices.Index(command, "build")+1:] {
				writeText(t, filepath.Join(workspace, ".lake", "build", "lib", "lean", strings.ReplaceAll(module, ".", string(filepath.Separator))+".olean"), "olean "+module)
			}
		}
		return "", nil
	}
	t.Cleanup(func() { run, lookPath = oldRun, oldLook })
	return commands
}

func TestFreshWorkspaceUsesSnapshotsAndIgnoresProjectCaches(t *testing.T) {
	f := newContractFixture(t)
	writeText(t, filepath.Join(f.root, "lakefile.lean"), "unsafe project build script")
	writeText(t, filepath.Join(f.root, ".lake", "build", "lib", "lean", "Example.olean"), "stale compiled proof")
	contract, project, _ := f.project()
	t.Setenv("LEAN_PATH", "/poison")
	t.Setenv("LAKE_HOME", "/poison")
	claimsJSON, _ := EncodeCompactOrdered(sampleClaims(contract))
	oldRun := run
	tools, _ := proofTools()
	var built string
	commands := fakeProofLean(t, f, func(command []string, workspace string) (string, bool) {
		if len(command) > 3 && command[1] == "--run" {
			// The checker is staged from the embedded producer after the
			// build, outside the workspace, and runs there.
			checker, lib := command[2], command[3]
			if built == "" || isRelativeTo(workspace, built) || isRelativeTo(checker, built) || !isRelativeTo(checker, workspace) {
				t.Errorf("checker %s runs in %s, built workspace %s", checker, workspace, built)
			}
			if readText(t, checker) != string(tools.checker) || lib != filepath.Join(built, ".lake", "build", "lib", "lean") {
				t.Errorf("checker %s or library %s is not the staged one", checker, lib)
			}
			want := append([]string{}, sortedKeys(project.modules())...)
			want = append(want, "--", contract.Claims[0][0], contract.Claims[0][1])
			if !slices.Equal(command[4:], want) {
				t.Errorf("checker arguments %v", command[4:])
			}
			return string(claimsJSON), true
		}
		if workspace == f.root || exists(filepath.Join(workspace, "lakefile.lean")) || !slices.Contains(command, "build") && exists(filepath.Join(workspace, ".lake")) {
			t.Errorf("workspace %s is not fresh", workspace)
		}
		built = workspace
		for _, s := range project.modules() {
			if readText(t, filepath.Join(workspace, s.Relative)) != string(s.Data) {
				t.Errorf("%s not staged from the snapshot", s.Relative)
			}
		}
		if exists(filepath.Join(workspace, filepath.FromSlash(producer.CheckerFile))) {
			t.Error("checker staged into the workspace")
		}
		return "", false
	})
	// Check the child environment the real runner would receive.
	env := strings.Join(childEnvironment(f.compiler), "\n")
	if strings.Contains(env, "LEAN_PATH=") || strings.Contains(env, "LAKE_HOME=") || !strings.Contains(env, "ELAN_TOOLCHAIN="+f.compiler.Toolchain()) {
		t.Fatal("child environment keeps Lean overrides")
	}
	_ = oldRun
	claims, err := prove(contract.Claims, project, tools, f.compiler)
	if err != nil {
		t.Fatal(err)
	}
	// Versions, the runtime modules alone, every module, then the checker.
	if len(claims) != 1 || len(*commands) != 5 || !slices.Equal((*commands)[2][1:], []string{"build", "Example"}) ||
		!slices.Equal((*commands)[3][1:], []string{"build", "Example", "ExampleContract", "ExampleProofs"}) || (*commands)[4][1] != "--run" {
		t.Fatalf("claims %v commands %v", claims, *commands)
	}
}

func TestStagedInputAndDependencyChangesAreRejected(t *testing.T) {
	f := newContractFixture(t)
	contract, project, _ := f.project()
	tools, _ := proofTools()
	for _, target := range []string{"Example.lean", "LeanGo/Proofs/Check.lean", "lean-toolchain", "lakefile.toml", "lakefile.lean", "Injected.lean", "lake-manifest.json"} {
		fakeProofLean(t, f, func(command []string, workspace string) (string, bool) {
			if slices.Contains(command, "build") {
				data := "changed input"
				if target == "lake-manifest.json" {
					data = `{"packages":[{"name":"unexpected"}]}`
				}
				writeText(t, filepath.Join(workspace, target), data)
			}
			return "", false
		})
		_, err := prove(contract.Claims, project, tools, f.compiler)
		if err == nil || !strings.Contains(err.Error(), "workspace") && !strings.Contains(err.Error(), "dependenc") && !strings.Contains(err.Error(), "staged") {
			t.Errorf("%s: %v", target, err)
		}
	}
}

// Composition contracts.

type compositionFixture struct {
	root, contract, output string
	manifests              [][2]string
}

func newCompositionFixture(t *testing.T) *compositionFixture {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	source := "../../examples/proofs"
	c := &compositionFixture{root: root, contract: filepath.Join(root, "contract.json"), output: filepath.Join(root, "evidence.json")}
	copyTree(t, filepath.Join(source, "checkedframe-contract.json"), c.contract)
	copyTree(t, filepath.Join(source, "checkedframe", "proof"), filepath.Join(root, "checkedframe", "proof"))
	for _, name := range []string{"crc16", "wire16"} {
		for _, sub := range []string{"source", "proof", name} {
			copyTree(t, filepath.Join(source, name, sub), filepath.Join(root, name, sub))
		}
		copyTree(t, filepath.Join(source, name, "proof-contract.json"), filepath.Join(root, name, "proof-contract.json"))
		c.manifests = append(c.manifests, [2]string{name, filepath.Join(root, name, name, "manifest.json")})
	}
	t.Setenv("LEAN_GO_TOOLCHAIN", "4.36.0")
	return c
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeText(t, filepath.Join(to, rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (c *compositionFixture) check(mode, output string) error {
	if output == "" {
		output = c.output
	}
	opts := CheckProofsOptions{Contract: c.contract, ComponentManifests: c.manifests}
	if mode == "output" {
		opts.Output = output
	} else {
		opts.Evidence = output
	}
	_, err := CheckProofs(opts)
	return err
}

func noProofs(t *testing.T, side func()) *int {
	t.Helper()
	fakeRebuild(t)
	calls := new(int)
	old := proveClaims
	proveClaims = func([][2]string, *ProjectSnapshot, *toolInputs, Compiler) ([]any, error) {
		*calls++
		if side != nil {
			side()
		}
		return []any{}, nil
	}
	t.Cleanup(func() { proveClaims = old })
	return calls
}

func TestCompositionChecksOnceAndDetectsStaleManifestOnEitherSide(t *testing.T) {
	c := newCompositionFixture(t)
	calls := noProofs(t, nil)
	if err := c.check("output", ""); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("proved %d times", *calls)
	}
	var data map[string]any
	json.Unmarshal([]byte(readText(t, c.output)), &data)
	components := keysOf(obj(data["components"]))
	slices.Sort(components)
	if !slices.Equal(components, []string{"crc16", "wire16"}) {
		t.Fatal(components)
	}
	slices.Reverse(c.manifests)
	if err := c.check("evidence", ""); err != nil {
		t.Fatal(err)
	}
	for _, pair := range c.manifests {
		original := readText(t, pair[1])
		writeText(t, pair[1], original+" ")
		expectError(t, c.check("evidence", ""), "stale")
		writeText(t, pair[1], original)
	}
}

func TestCompositionRejectsInvalidBindingsAndMismatchedSources(t *testing.T) {
	c := newCompositionFixture(t)
	calls := noProofs(t, nil)
	contract, err := LoadComposition(c.contract)
	if err != nil {
		t.Fatal(err)
	}
	pairs := c.manifests
	for _, bad := range [][][2]string{pairs[:1], append(append([][2]string{}, pairs...), pairs[0]),
		append(append([][2]string{}, pairs...), [2]string{"extra", c.output}), {{pairs[0][0], pairs[0][1]}, {pairs[1][0], pairs[0][1]}}} {
		if _, err := ManifestBindings(contract, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	c.manifests = [][2]string{{pairs[0][0], pairs[1][1]}, {pairs[1][0], pairs[0][1]}}
	if c.check("output", "") == nil {
		t.Fatal("swapped manifests accepted")
	}
	if *calls != 0 {
		t.Fatal("proofs ran")
	}
}

func TestCompositionSourceCollisionAndProtectedOutputs(t *testing.T) {
	c := newCompositionFixture(t)
	calls := noProofs(t, nil)
	for _, path := range []string{c.contract, filepath.Join(c.root, "checkedframe", "proof", "output"), c.manifests[1][1],
		filepath.Join(c.root, "crc16", "proof-contract.json"), filepath.Join(c.root, "crc16", "source", "output")} {
		if c.check("output", path) == nil {
			t.Errorf("%s accepted as output", path)
		}
	}
	writeText(t, filepath.Join(c.root, "checkedframe", "proof", "Crc16.lean"), "-- duplicate module")
	expectError(t, c.check("output", ""), "overlap")
	if *calls != 0 {
		t.Fatal("proofs ran")
	}
}

func TestCompositionChangeDuringProofDoesNotPublish(t *testing.T) {
	c := newCompositionFixture(t)
	writeText(t, c.output, "prior evidence")
	api := filepath.Join(filepath.Dir(c.manifests[0][1]), "api.go")
	noProofs(t, func() { writeText(t, api, readText(t, api)+" ") })
	expectError(t, c.check("output", ""), "generated file hash")
	if readText(t, c.output) != "prior evidence" {
		t.Fatal("evidence published")
	}
}
