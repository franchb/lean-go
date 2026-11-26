package pipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/franchb/lean-go/producer"
)

// Fresh source-proof checking with evidence bound to generated Go.

const (
	EvidenceSchema            = "lean-go-proof-evidence/v1"
	CompositionEvidenceSchema = "lean-go-proof-evidence/v2"
	assurance                 = "Kernel-checked source statements; extraction and Go execution remain tested and trusted."
	checkerInput              = "producer/" + producer.CheckerFile
	toolchainInput            = "producer/" + producer.ToolchainFile
	harnessName               = "LeanGo/Proofs/Harness.lean"
)

var approvedAxioms = map[string]bool{"propext": true, "Classical.choice": true, "Quot.sound": true}

// toolInputs are the proof tool's identities: embedded checker and pin
// digests plus the Go tool source digests.
type toolInputs struct {
	checker []byte
	digests map[string]string
}

func proofTools() (*toolInputs, error) {
	pin, err := producer.Read(producer.ToolchainFile)
	if err != nil {
		return nil, err
	}
	if string(pin) != string(DefaultCompiler.PinFile()) {
		return nil, exportError("proof toolchain configuration differs from the pin")
	}
	checker, err := producer.Read(producer.CheckerFile)
	if err != nil {
		return nil, err
	}
	return &toolInputs{checker, map[string]string{
		checkerInput: Digest(checker), toolchainInput: Digest(pin),
		ToolInput: SourceDigest(), ProofToolInput: ProofSourceDigest(),
	}}, nil
}

func validateClaims(value any, claims [][2]string) ([]any, error) {
	items, ok := value.([]any)
	if !ok || len(items) != len(claims) {
		return nil, exportError("unexpected checked theorem inventory")
	}
	for i, pair := range claims {
		claim, ok := items[i].(*Object)
		if !ok || !claim.HasExactly("name", "expected", "statement", "axioms") ||
			claim.Values["name"] != pair[0] || claim.Values["expected"] != pair[1] {
			return nil, exportError("malformed checked theorem evidence")
		}
		if statement, ok := claim.Values["statement"].(string); !ok || statement == "" {
			return nil, exportError("malformed checked theorem evidence")
		}
		axioms, ok := claim.Values["axioms"].([]any)
		bad := !ok
		var names []string
		for _, axiom := range axioms {
			s, isString := axiom.(string)
			if !isString || !approvedAxioms[s] {
				bad = true
				break
			}
			names = append(names, s)
		}
		if !bad {
			sorted := append([]string(nil), names...)
			sort.Strings(sorted)
			for j := range sorted {
				if j > 0 && sorted[j] == sorted[j-1] || sorted[j] != names[j] {
					bad = true
				}
			}
		}
		if bad {
			return nil, exportError("unapproved or malformed theorem axiom evidence")
		}
	}
	return items, nil
}

func proofHarness(claims [][2]string, modules []string) []byte {
	var imports strings.Builder
	for _, module := range modules {
		fmt.Fprintf(&imports, "import %s\n", module)
	}
	pairs := make([]string, len(claims))
	for i, claim := range claims {
		pairs[i] = fmt.Sprintf("(``%s, ``%s)", claim[0], claim[1])
	}
	return []byte("import LeanGo.Proofs.Check\n" + imports.String() + "\nopen Lean Elab Command\n" +
		"elab \"#lean_go_proof_check\" : command => do\n" +
		"  let claims ← liftTermElabM <| LeanGo.Proofs.checkTheorems [\n    " + strings.Join(pairs, ",\n    ") + "]\n" +
		"  IO.FS.writeFile \"claims.json\" ((toJson claims).pretty ++ \"\\n\")\n\n" +
		"#lean_go_proof_check\n")
}

func workspaceSources(workspace string) (map[string]bool, error) {
	out := map[string]bool{}
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var dirs []string
		for _, entry := range entries {
			full := filepath.Join(dir, entry.Name())
			info, err := os.Stat(full)
			if err == nil && info.IsDir() {
				if isSymlink(full) {
					return exportError("proof workspace source directory is symlinked")
				}
				dirs = append(dirs, entry.Name())
			} else if strings.HasSuffix(entry.Name(), ".lean") {
				rel, err := filepath.Rel(workspace, full)
				if err != nil {
					return err
				}
				out[filepath.ToSlash(rel)] = true
			}
		}
		for _, name := range dirs {
			if dir == workspace && name == ".lake" {
				continue
			}
			if err := walk(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
		return nil
	}
	return out, walk(workspace)
}

func verifyProofWorkspace(workspace string, files map[string][]byte) error {
	found, err := workspaceSources(workspace)
	if err != nil {
		return err
	}
	expected := map[string]bool{}
	for name := range files {
		if strings.HasSuffix(name, ".lean") {
			expected[name] = true
		}
	}
	if !reflect.DeepEqual(found, expected) {
		return exportError("proof workspace source inventory changed")
	}
	for _, relative := range sortedKeys(files) {
		data, err := readRegular(filepath.Join(workspace, filepath.FromSlash(relative)), MaxSourceBytes)
		if err != nil {
			return err
		}
		if string(data) != string(files[relative]) {
			return exportErrorf("proof workspace input changed: %s", relative)
		}
	}
	lock, err := readRegular(filepath.Join(workspace, "lake-manifest.json"), maxDocumentBytes)
	if err != nil {
		return err
	}
	value, err := ParseJSON(lock)
	if err != nil {
		return exportError(err.Error())
	}
	manifest, ok := value.(*Object)
	packages, isList := manifest.Values["packages"].([]any)
	if !ok || !isList || len(packages) != 0 {
		return exportError("proof workspace must have no Lake dependencies")
	}
	return nil
}

// prove compiles only snapshots in fixed, dependency-free scaffolding.
func prove(claims [][2]string, project *ProjectSnapshot, tools *toolInputs, compiler Compiler) ([]any, error) {
	env := childEnvironment(compiler)
	lean, okLean := lookPath("lean")
	lake, okLake := lookPath("lake")
	if !okLean || !okLake {
		return nil, exportError("pinned Lean and Lake executables must be available on PATH")
	}
	directory, err := os.MkdirTemp("", "lean-go-source-proofs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	workspace, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	modules := project.modules()
	files := map[string][]byte{}
	for _, s := range modules {
		files[s.Relative] = s.Data
	}
	files["lean-toolchain"] = compiler.PinFile()
	files["lakefile.toml"] = LakeConfig(sortedKeys(modules))
	files[producer.CheckerFile] = tools.checker
	for _, relative := range sortedKeys(files) {
		if err := writeFile(filepath.Join(workspace, filepath.FromSlash(relative)), files[relative]); err != nil {
			return nil, err
		}
	}
	for _, command := range [][]string{{lean, "--version"}, {lake, "env", "lean", "--version"}} {
		text, err := run(command, workspace, env)
		if err != nil {
			return nil, err
		}
		if err := compiler.CheckVersion(text); err != nil {
			return nil, err
		}
	}
	if err := verifyProofWorkspace(workspace, files); err != nil {
		return nil, err
	}
	if _, err := run(append([]string{lake, "build", "LeanGo.Proofs.Check"}, sortedKeys(modules)...), workspace, env); err != nil {
		return nil, err
	}
	if err := verifyProofWorkspace(workspace, files); err != nil {
		return nil, err
	}
	files[harnessName] = proofHarness(claims, sortedKeys(modules))
	// User elaboration may have written files; never follow or replace the harness.
	harness, err := os.OpenFile(filepath.Join(workspace, filepath.FromSlash(harnessName)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil, exportError("proof workspace reserved harness path already exists")
	}
	if err != nil {
		return nil, err
	}
	if _, err := harness.Write(files[harnessName]); err != nil {
		harness.Close()
		return nil, err
	}
	if err := harness.Close(); err != nil {
		return nil, err
	}
	if _, err := run([]string{lake, "env", "lean", harnessName}, workspace, env); err != nil {
		return nil, err
	}
	if err := verifyProofWorkspace(workspace, files); err != nil {
		return nil, err
	}
	data, err := readRegular(filepath.Join(workspace, "claims.json"), maxDocumentBytes)
	if err != nil {
		return nil, err
	}
	value, err := ParseJSON(data)
	if err != nil {
		return nil, exportError(err.Error())
	}
	return validateClaims(value, claims)
}

func digestInputs(inputs map[string][]byte) map[string]string {
	out := map[string]string{}
	for name, data := range inputs {
		out[name] = Digest(data)
	}
	return out
}

func encodeEvidence(record *Object) ([]byte, error) {
	data, err := EncodeIndentedSorted(record)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Evidence freshly checks a v1 contract and returns deterministic evidence.
func Evidence(contractPath, manifestPath string) ([]byte, *Contract, error) {
	compiler, err := SelectedCompiler()
	if err != nil {
		return nil, nil, err
	}
	contract, err := LoadContract(contractPath)
	if err != nil {
		return nil, nil, err
	}
	project, err := SnapshotProject(contract)
	if err != nil {
		return nil, nil, err
	}
	tools, err := proofTools()
	if err != nil {
		return nil, nil, err
	}
	manifest, err := VerifyManifest(manifestPath, contract, project, compiler)
	if err != nil {
		return nil, nil, err
	}
	claims, err := prove(contract.Claims, project, tools, compiler)
	if err != nil {
		return nil, nil, err
	}
	changed := exportError("proof inputs or generated package changed during checking")
	again, err := LoadContract(contractPath)
	if err != nil || !again.equal(contract) {
		return nil, nil, changed
	}
	if fresh, err := SnapshotProject(contract); err != nil || !fresh.equal(project) {
		return nil, nil, changed
	}
	if fresh, err := VerifyManifest(manifestPath, contract, project, compiler); err != nil || string(fresh) != string(manifest) {
		return nil, nil, changed
	}
	record := NewObject()
	record.Set("schema", EvidenceSchema)
	record.Set("lean_version", compiler.Version)
	record.Set("lean_commit", compiler.Commit)
	record.Set("module", contract.Module)
	record.Set("roots", contract.Roots)
	record.Set("contract_sha256", Digest(contract.Raw))
	record.Set("project_inputs", digestInputs(project.inputs()))
	record.Set("tool_inputs", tools.digests)
	record.Set("generated_manifest_sha256", Digest(manifest))
	record.Set("claims", claims)
	record.Set("assurance", assurance)
	data, err := encodeEvidence(record)
	return data, contract, err
}

// CompositionEvidence freshly checks a v2 composition contract.
func CompositionEvidence(c *Composition, bindings map[string]string) ([]byte, error) {
	compiler, err := SelectedCompiler()
	if err != nil {
		return nil, err
	}
	tools, err := proofTools()
	if err != nil {
		return nil, err
	}
	project, components, err := compositionInputs(c, bindings, compiler)
	if err != nil {
		return nil, err
	}
	claims, err := prove(c.Claims, project, tools, compiler)
	if err != nil {
		return nil, err
	}
	changed := exportError("composition inputs or generated packages changed during checking")
	fresh, err := LoadComposition(c.Path)
	if err != nil || !fresh.equal(c) {
		return nil, changed
	}
	again, againComponents, err := compositionInputs(fresh, bindings, compiler)
	if err != nil || !again.equal(project) || !reflect.DeepEqual(againComponents, components) {
		return nil, changed
	}
	record := NewObject()
	record.Set("schema", CompositionEvidenceSchema)
	record.Set("lean_version", compiler.Version)
	record.Set("lean_commit", compiler.Commit)
	record.Set("contract_sha256", Digest(c.Raw))
	record.Set("components", components)
	record.Set("claims", claims)
	record.Set("project_inputs", digestInputs(project.inputs()))
	record.Set("tool_inputs", tools.digests)
	record.Set("assurance", assurance)
	return encodeEvidence(record)
}

func validateEvidencePath(path string, c *Contract, manifest string) (string, error) {
	output, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	resolved, err := resolvePath(output)
	if err != nil {
		return "", exportError("invalid evidence path")
	}
	if resolved != output || (exists(output) && !isFile(output)) {
		return "", exportError("evidence path must be a regular file without symlinks")
	}
	if contractPath, err := resolvePath(c.Path); err == nil && output == contractPath {
		return "", exportError("evidence output must not overwrite proof or tool inputs")
	}
	manifestPath, err := absolutePath(manifest)
	if err != nil {
		return "", err
	}
	for _, directory := range []string{c.SourceDir, c.ProofDir, filepath.Dir(manifestPath)} {
		resolvedDir, err := resolvePath(directory)
		if err == nil && isRelativeTo(output, resolvedDir) {
			return "", exportError("evidence must stay outside source, proof and generated directories")
		}
	}
	return output, nil
}

func validateCompositionOutput(path string, c *Composition, bindings map[string]string) (string, error) {
	var output string
	for _, name := range sortedKeys(c.Components) {
		var err error
		output, err = validateEvidencePath(path, c.Components[name], bindings[name])
		if err != nil {
			return "", err
		}
	}
	if output == c.Path || isRelativeTo(output, c.ProofDir) {
		return "", exportError("evidence must stay outside the composition inputs")
	}
	return output, nil
}

// CheckProofsOptions selects a contract, its manifests and the evidence mode.
type CheckProofsOptions struct {
	Contract string
	// Manifest binds a v1 contract; ComponentManifests bind a v2 composition.
	Manifest           string
	ComponentManifests [][2]string
	// Exactly one of Output (write fresh evidence) or Evidence (verify).
	Output, Evidence string
}

// CheckProofs runs one check and returns its summary line.
func CheckProofs(opts CheckProofsOptions) (string, error) {
	path := opts.Output
	if path == "" {
		path = opts.Evidence
	}
	var output string
	var fresh []byte
	var claims int
	if len(opts.ComponentManifests) > 0 {
		contract, err := LoadComposition(opts.Contract)
		if err != nil {
			return "", err
		}
		bindings, err := ManifestBindings(contract, opts.ComponentManifests)
		if err != nil {
			return "", err
		}
		if output, err = validateCompositionOutput(path, contract, bindings); err != nil {
			return "", err
		}
		if fresh, err = CompositionEvidence(contract, bindings); err != nil {
			return "", err
		}
		if again, err := LoadComposition(opts.Contract); err != nil || !again.equal(contract) {
			return "", exportError("composition contract changed during checking")
		}
		if _, err = validateCompositionOutput(output, contract, bindings); err != nil {
			return "", err
		}
		claims = len(contract.Claims)
	} else {
		contract, err := LoadContract(opts.Contract)
		if err != nil {
			return "", err
		}
		if output, err = validateEvidencePath(path, contract, opts.Manifest); err != nil {
			return "", err
		}
		if fresh, _, err = Evidence(opts.Contract, opts.Manifest); err != nil {
			return "", err
		}
		if again, err := LoadContract(opts.Contract); err != nil || !again.equal(contract) {
			return "", exportError("proof contract changed during checking")
		}
		if _, err = validateEvidencePath(output, contract, opts.Manifest); err != nil {
			return "", err
		}
		claims = len(contract.Claims)
	}
	if opts.Output != "" {
		if err := PublishFile(output, fresh, false); err != nil {
			return "", err
		}
		return fmt.Sprintf("Checked %d source theorems and wrote bound proof evidence.", claims), nil
	}
	tracked, err := readRegular(output, maxDocumentBytes)
	if err != nil {
		return "", err
	}
	if _, err := ParseJSON(tracked); err != nil {
		return "", exportError(err.Error())
	}
	if string(tracked) != string(fresh) {
		return "", exportError("proof evidence is stale; regenerate with --output")
	}
	return fmt.Sprintf("Checked %d source theorems; evidence and generated files match.", claims), nil
}
