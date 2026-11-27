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
	// v3 and v4 replace v1 and v2: statements are rendered by the checker
	// from the kernel term of the expected proposition, and the generated
	// package is rebuilt from the sources before it is bound.
	EvidenceSchema            = "lean-go-proof-evidence/v3"
	CompositionEvidenceSchema = "lean-go-proof-evidence/v4"
	assurance                 = "Kernel-replayed source statements; extraction and Go execution remain tested and trusted."
	checkerInput              = "producer/" + producer.CheckerFile
	toolchainInput            = "producer/" + producer.ToolchainFile
	// maxOleanBytes bounds one built runtime module read for hashing.
	maxOleanBytes = 256 * 1024 * 1024
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

// checkerCommand runs the staged checker program: lean --run CHECKER LIB
// MODULE... -- THEOREM EXPECTED ... Its only output is the claims JSON.
func checkerCommand(lean, checker, lib string, modules []string, claims [][2]string) []string {
	command := append([]string{lean, "--run", checker, lib}, modules...)
	command = append(command, "--")
	for _, claim := range claims {
		command = append(command, claim[0], claim[1])
	}
	return command
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

// prove builds only snapshots in fixed, dependency-free scaffolding and then
// decides the claims with the checker program, which runs in its own process
// outside the workspace and executes no user code (see
// producer/LeanGo/Proofs/Check.lean). proveClaims is a variable so protocol
// tests can stand in for Lean.
var proveClaims = prove

// oleanDigests hashes the built .olean of each module.
func oleanDigests(lib string, modules map[string]Source) (map[string]string, error) {
	out := map[string]string{}
	for name, s := range modules {
		path := filepath.Join(lib, filepath.FromSlash(strings.TrimSuffix(s.Relative, ".lean")+".olean"))
		data, err := readRegular(path, maxOleanBytes)
		if err != nil {
			return nil, exportErrorf("proof workspace module was not built: %s", name)
		}
		out[name] = Digest(data)
	}
	return out, nil
}

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
	// The trusted runtime modules are built first and alone; their .olean
	// files must be unchanged once the untrusted proof modules have been built.
	lib := filepath.Join(workspace, ".lake", "build", "lib", "lean")
	if _, err := run(append([]string{lake, "build"}, sortedKeys(project.Runtime)...), workspace, env); err != nil {
		return nil, err
	}
	if err := verifyProofWorkspace(workspace, files); err != nil {
		return nil, err
	}
	runtime, err := oleanDigests(lib, project.Runtime)
	if err != nil {
		return nil, err
	}
	if _, err := run(append([]string{lake, "build"}, sortedKeys(modules)...), workspace, env); err != nil {
		return nil, err
	}
	if err := verifyProofWorkspace(workspace, files); err != nil {
		return nil, err
	}
	if again, err := oleanDigests(lib, project.Runtime); err != nil || !reflect.DeepEqual(again, runtime) {
		return nil, exportError("proof workspace runtime build changed while proof modules were built")
	}
	// Code run by the build cannot reach the checker: it is staged only now,
	// from the embedded producer, in a new directory outside the workspace.
	stage, err := os.MkdirTemp("", "lean-go-proof-checker-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	checker := filepath.Join(stage, filepath.Base(producer.CheckerFile))
	if err := writeExclusive(checker, tools.checker); err != nil {
		return nil, err
	}
	output, err := run(checkerCommand(lean, checker, lib, sortedKeys(modules), claims), stage, env)
	if err != nil {
		return nil, err
	}
	if err := verifyProofWorkspace(workspace, files); err != nil {
		return nil, err
	}
	if len(output) > maxDocumentBytes {
		return nil, exportError("proof checker output exceeds byte limit")
	}
	value, err := ParseJSON([]byte(output))
	if err != nil {
		return nil, exportError("malformed proof checker output: " + err.Error())
	}
	return validateClaims(value, claims)
}

// writeExclusive creates a new file and never follows or replaces one.
func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return exportError("proof checker path already exists")
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
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
	manifest, mode, err := verifyPackage(manifestPath, contract, project, compiler)
	if err != nil {
		return nil, nil, err
	}
	if err := rebuildPackage(manifestPath, contract, project, mode); err != nil {
		return nil, nil, err
	}
	claims, err := proveClaims(contract.Claims, project, tools, compiler)
	if err != nil {
		return nil, nil, err
	}
	changed := exportError("proof inputs or generated package changed during checking")
	// Like the Python checker, a failing recheck reports its own error.
	again, err := LoadContract(contractPath)
	if err != nil {
		return nil, nil, err
	}
	if !again.equal(contract) {
		return nil, nil, changed
	}
	freshProject, err := SnapshotProject(contract)
	if err != nil {
		return nil, nil, err
	}
	if !freshProject.equal(project) {
		return nil, nil, changed
	}
	freshManifest, err := VerifyManifest(manifestPath, contract, project, compiler)
	if err != nil {
		return nil, nil, err
	}
	if string(freshManifest) != string(manifest) {
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
	// Metered evidence keeps its original fields; other modes are recorded.
	if mode != MeteredMode {
		record.Set("generated_mode", mode)
	}
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
	project, components, err := compositionInputs(c, bindings, compiler, true)
	if err != nil {
		return nil, err
	}
	claims, err := proveClaims(c.Claims, project, tools, compiler)
	if err != nil {
		return nil, err
	}
	changed := exportError("composition inputs or generated packages changed during checking")
	fresh, err := LoadComposition(c.Path)
	if err != nil {
		return nil, err
	}
	if !fresh.equal(c) {
		return nil, changed
	}
	again, againComponents, err := compositionInputs(fresh, bindings, compiler, false)
	if err != nil {
		return nil, err
	}
	if !again.equal(project) || !reflect.DeepEqual(againComponents, components) {
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
	if !SymlinkFree(output) || (exists(output) && !isFile(output)) {
		return "", exportError("evidence path must be a regular file without symlinks")
	}
	// Compare resolved forms with each other: on Windows the resolved form
	// also expands short names, so it can differ from a link-free path.
	if contractPath, err := resolvePath(c.Path); err == nil && resolved == contractPath {
		return "", exportError("evidence output must not overwrite proof or tool inputs")
	}
	manifestPath, err := absolutePath(manifest)
	if err != nil {
		return "", err
	}
	for _, directory := range []string{c.SourceDir, c.ProofDir, filepath.Dir(manifestPath)} {
		resolvedDir, err := resolvePath(directory)
		if err == nil && isRelativeTo(resolved, resolvedDir) {
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
		again, err := LoadComposition(opts.Contract)
		if err != nil {
			return "", err
		}
		if !again.equal(contract) {
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
		again, err := LoadContract(opts.Contract)
		if err != nil {
			return "", err
		}
		if !again.equal(contract) {
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
