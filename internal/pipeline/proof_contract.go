package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/franchb/lean-go/internal/moduleir"
	"github.com/franchb/lean-go/internal/nativegen"
)

// Strict project contracts and generated-package binding for source proofs;
// see docs/proof-contracts.md.

const (
	ContractSchema    = "lean-go-proof-contract/v1"
	CompositionSchema = "lean-go-proof-contract/v2"
	maxDocumentBytes  = 4 * 1024 * 1024
	maxContractBytes  = 64 * 1024
	maxClaims         = 64
)

var generatedGo = []string{"api.go", "control.go", "runtime.go", "schema.go"}

// Contract is one v1 proof contract.
type Contract struct {
	Path      string
	Raw       []byte
	SourceDir string
	ProofDir  string
	Module    string
	Roots     []string
	Claims    [][2]string
}

func (c *Contract) equal(o *Contract) bool { return reflect.DeepEqual(c, o) }

// ProjectSnapshot holds runtime and proof module snapshots.
type ProjectSnapshot struct {
	Runtime map[string]Source
	Proofs  map[string]Source
}

func (p *ProjectSnapshot) modules() map[string]Source {
	out := map[string]Source{}
	for k, v := range p.Runtime {
		out[k] = v
	}
	for k, v := range p.Proofs {
		out[k] = v
	}
	return out
}

func (p *ProjectSnapshot) inputs() map[string][]byte {
	out := map[string][]byte{}
	for _, s := range p.Runtime {
		out["source/"+s.Relative] = s.Data
	}
	for _, s := range p.Proofs {
		out["proof/"+s.Relative] = s.Data
	}
	return out
}

func (p *ProjectSnapshot) equal(o *ProjectSnapshot) bool { return reflect.DeepEqual(p, o) }

func snapshotMap(s Snapshot) map[string]Source {
	out := map[string]Source{}
	for _, source := range s {
		out[source.Module] = source
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func baseName(path string) string { return filepath.Base(path) }

// readRegular reads a canonical, symlink-free regular file within a size limit.
func readRegular(path string, limit int64) ([]byte, error) {
	path, err := absolutePath(path)
	if err != nil {
		return nil, err
	}
	if !symlinkFree(path) || !isFile(path) {
		return nil, exportErrorf("missing, noncanonical or symlinked input: %s", baseName(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, exportErrorf("input exceeds byte limit: %s", baseName(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, exportErrorf("input exceeds byte limit: %s", baseName(path))
	}
	return data, nil
}

func projectPath(root string, value any) (string, error) {
	s, ok := value.(string)
	invalid := exportError("contract directories must be canonical portable relative paths")
	if !ok || s == "" || strings.HasPrefix(s, "/") {
		return "", invalid
	}
	for _, r := range s {
		if strings.ContainsRune(`\:<>"|?*`, r) || r < 32 || r == 127 || (r >= 0xD800 && r <= 0xDFFF) || r == utf8.RuneError {
			return "", invalid
		}
	}
	parts := strings.Split(s, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
			return "", invalid
		}
	}
	return root + string(filepath.Separator) + filepath.Join(parts...), nil
}

func projectDirectory(root string, value any) (string, error) {
	directory, err := projectPath(root, value)
	if err != nil {
		return "", err
	}
	if !symlinkFree(directory) || !isDir(directory) {
		return "", exportError("contract directory is missing or contains a symlink")
	}
	return directory, nil
}

func claimNames(value any) ([][2]string, error) {
	items, ok := value.([]any)
	if !ok || len(items) < 1 || len(items) > maxClaims {
		return nil, exportErrorf("expected between 1 and %d claims", maxClaims)
	}
	var pairs [][2]string
	for _, item := range items {
		claim, ok := item.(*Object)
		if !ok || !claim.HasExactly("theorem", "expected") {
			return nil, exportError("claim requires exactly theorem and expected names")
		}
		theorem, ok1 := claim.Values["theorem"].(string)
		expected, ok2 := claim.Values["expected"].(string)
		if !ok1 || !ok2 {
			return nil, exportError("claim names must be strings")
		}
		for _, name := range []string{theorem, expected} {
			if err := ValidateName(name); err != nil {
				return nil, err
			}
		}
		pairs = append(pairs, [2]string{theorem, expected})
	}
	seen := map[string]bool{}
	for _, pair := range pairs {
		seen[pair[0]] = true
	}
	if len(seen) != len(pairs) {
		return nil, exportError("duplicate theorem claims are not allowed")
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	return pairs, nil
}

// LoadContract reads and validates a v1 proof contract.
func LoadContract(path string) (*Contract, error) {
	path, err := absolutePath(path)
	if err != nil {
		return nil, err
	}
	raw, err := readRegular(path, maxContractBytes)
	if err != nil {
		return nil, err
	}
	value, err := ParseJSON(raw)
	if err != nil {
		return nil, exportError(err.Error())
	}
	doc, ok := value.(*Object)
	if !ok || !doc.HasExactly("schema", "source_dir", "proof_dir", "module", "roots", "claims") || doc.Values["schema"] != ContractSchema {
		return nil, exportError("unexpected proof contract schema or fields")
	}
	module, ok := doc.Values["module"].(string)
	rawRoots, isList := doc.Values["roots"].([]any)
	if !ok || !isList {
		return nil, exportError("contract module must be a name and roots must be a list")
	}
	if err := ValidateName(module); err != nil {
		return nil, err
	}
	if len(rawRoots) < 1 || len(rawRoots) > MaxRoots {
		return nil, exportErrorf("expected between 1 and %d roots", MaxRoots)
	}
	var names []string
	for _, root := range rawRoots {
		s, ok := root.(string)
		if !ok {
			return nil, exportError("root names must be strings")
		}
		if err := ValidateName(s); err != nil {
			return nil, err
		}
		names = append(names, s)
	}
	roots, err := SelectRoots(names)
	if err != nil {
		return nil, err
	}
	claims, err := claimNames(doc.Values["claims"])
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	source, err := projectDirectory(parent, doc.Values["source_dir"])
	if err != nil {
		return nil, err
	}
	proofs, err := projectDirectory(parent, doc.Values["proof_dir"])
	if err != nil {
		return nil, err
	}
	if isRelativeTo(source, proofs) || isRelativeTo(proofs, source) {
		return nil, exportError("runtime and proof directories must be disjoint")
	}
	return &Contract{path, raw, source, proofs, module, roots, claims}, nil
}

// SnapshotProject reads the runtime and proof modules of a contract.
func SnapshotProject(c *Contract) (*ProjectSnapshot, error) {
	runtime, err := SourceSnapshot(c.SourceDir)
	if err != nil {
		return nil, err
	}
	proofs, err := SourceSnapshot(c.ProofDir)
	if err != nil {
		return nil, err
	}
	if _, ok := runtime.Lookup(c.Module); !ok {
		return nil, exportError("configured runtime module does not exist")
	}
	for _, proof := range proofs {
		if _, ok := runtime.Lookup(proof.Module); ok {
			return nil, exportError("runtime and proof module names overlap")
		}
	}
	project := &ProjectSnapshot{snapshotMap(runtime), snapshotMap(proofs)}
	total := 0
	for _, s := range project.modules() {
		total += len(s.Data)
	}
	if len(project.modules()) > MaxFiles || total > MaxSourceBytes {
		return nil, exportError("combined proof project exceeds file or byte limit")
	}
	return project, nil
}

func pyReprValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case string:
		return pythonRepr(t)
	case bool:
		if t {
			return "True"
		}
		return "False"
	case json.Number:
		return string(t)
	case *Object:
		return "{...}"
	case []any:
		return "[...]"
	}
	return "?"
}

// Generated package modes the checker binds.
const (
	MeteredMode = "metered"
	NativeMode  = "native"
)

// maxNativeFiles bounds the Go file inventory a native manifest may list.
const maxNativeFiles = 16

// packageLayout returns the mode and generated Go file names that a manifest's
// schema fixes. A metered package is always the four files in generatedGo. A
// native package lists its files in generated_files; each must be a plain,
// non-test Go file name, and the manifest must record mode "native" and the
// current native generator.
func packageLayout(manifest *Object) (string, []string, error) {
	switch manifest.Values["schema"] {
	case moduleir.ManifestSchema:
		return MeteredMode, generatedGo, nil
	case nativegen.ManifestSchema:
	default:
		return "", nil, unsupportedManifest(manifest.Values["schema"])
	}
	if manifest.Values["mode"] != NativeMode || manifest.Values["generator"] != nativegen.Version {
		return "", nil, exportErrorf("native manifest must record mode %q and generator %s", NativeMode, nativegen.Version)
	}
	files, ok := manifest.Values["generated_files"].(*Object)
	if !ok || len(files.Keys) < 1 || len(files.Keys) > maxNativeFiles {
		return "", nil, exportError("generated file inventory differs")
	}
	names := make([]string, 0, len(files.Keys))
	for _, name := range files.Keys {
		if !plainGoFile(name) {
			return "", nil, exportError("generated file inventory differs")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return NativeMode, names, nil
}

func unsupportedManifest(found any) error {
	return exportErrorf("unsupported generated manifest schema %s; regenerate the package with the current lean-go tool (%s or %s)",
		pyReprValue(found), moduleir.ManifestSchema, nativegen.ManifestSchema)
}

// plainGoFile accepts lower-case portable names such as types.go.
func plainGoFile(name string) bool {
	stem, ok := strings.CutSuffix(name, ".go")
	if !ok || stem == "" || strings.HasSuffix(stem, "_test") || stem[0] < 'a' || stem[0] > 'z' {
		return false
	}
	for _, r := range stem {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// VerifyManifest binds a generated package to the contract and returns its bytes.
func VerifyManifest(path string, c *Contract, project *ProjectSnapshot, compiler Compiler) ([]byte, error) {
	raw, _, err := verifyPackage(path, c, project, compiler)
	return raw, err
}

// verifyPackage binds a metered or native generated package to the contract
// and returns the manifest bytes and the package mode. The manifest schema
// fixes the layout and the manifest lists the files; the directory must hold
// exactly the manifest and those files, each matching its recorded SHA-256.
func verifyPackage(path string, c *Contract, project *ProjectSnapshot, compiler Compiler) ([]byte, string, error) {
	path, err := absolutePath(path)
	if err != nil {
		return nil, "", err
	}
	for _, name := range generatedGo {
		if baseName(path) == name {
			return nil, "", exportError("manifest must be separate from generated Go files")
		}
	}
	raw, err := readRegular(path, maxDocumentBytes)
	if err != nil {
		return nil, "", err
	}
	value, err := ParseJSON(raw)
	if err != nil {
		return nil, "", exportError(err.Error())
	}
	manifest, ok := value.(*Object)
	if !ok {
		return nil, "", unsupportedManifest(nil)
	}
	mode, names, err := packageLayout(manifest)
	if err != nil {
		return nil, "", err
	}
	if slices.Contains(names, baseName(path)) {
		return nil, "", exportError("manifest must be separate from generated Go files")
	}
	program, ok := manifest.Values["program"].(*Object)
	mismatch := exportError("generated program, roots or Lean pin differ from contract")
	if !ok {
		return nil, "", mismatch
	}
	if _, hasRoot := program.Values["root"]; hasRoot {
		return nil, "", mismatch
	}
	for key, want := range map[string]string{"schema": Schema, "profile": Profile, "stage": Stage, "module": c.Module,
		"lean_version": compiler.Version, "lean_commit": compiler.Commit} {
		if program.Values[key] != want {
			return nil, "", mismatch
		}
	}
	roots, ok := program.Values["roots"].([]any)
	if !ok || len(roots) != len(c.Roots) {
		return nil, "", mismatch
	}
	for i, root := range c.Roots {
		if roots[i] != root {
			return nil, "", mismatch
		}
	}
	inputs, ok := program.Values["inputs"].(*Object)
	runtime := map[string]string{}
	for _, s := range project.Runtime {
		runtime["source/"+s.Relative] = Digest(s.Data)
	}
	recorded := map[string]any{}
	if ok {
		for _, key := range inputs.Keys {
			if strings.HasPrefix(key, "source/") {
				recorded[key] = inputs.Values[key]
			}
		}
	}
	same := ok && len(recorded) == len(runtime)
	for key, digest := range runtime {
		same = same && recorded[key] == digest
	}
	if !same {
		return nil, "", exportError("runtime source inventory or hashes differ from generated manifest")
	}
	files, ok := manifest.Values["generated_files"].(*Object)
	if !ok || !files.HasExactly(names...) {
		return nil, "", exportError("generated file inventory differs")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil, "", err
	}
	expected := map[string]bool{baseName(path): true}
	for _, name := range names {
		expected[name] = true
	}
	found := map[string]bool{}
	for _, entry := range entries {
		found[entry.Name()] = true
	}
	if !reflect.DeepEqual(found, expected) {
		return nil, "", exportError("generated directory inventory differs")
	}
	for _, name := range names {
		data, err := readRegular(filepath.Join(filepath.Dir(path), name), maxDocumentBytes)
		if err != nil {
			return nil, "", err
		}
		if files.Values[name] != Digest(data) {
			return nil, "", exportErrorf("generated file hash differs: %s", name)
		}
	}
	return raw, mode, nil
}

// Composition is a v2 contract binding one proof project to several packages.
type Composition struct {
	Path       string
	Raw        []byte
	ProofDir   string
	Components map[string]*Contract
	Claims     [][2]string
}

func (c *Composition) equal(o *Composition) bool { return reflect.DeepEqual(c, o) }

// LoadComposition reads and validates a v2 composition contract.
func LoadComposition(path string) (*Composition, error) {
	path, err := absolutePath(path)
	if err != nil {
		return nil, err
	}
	raw, err := readRegular(path, maxContractBytes)
	if err != nil {
		return nil, err
	}
	value, err := ParseJSON(raw)
	if err != nil {
		return nil, exportError(err.Error())
	}
	doc, ok := value.(*Object)
	if !ok || !doc.HasExactly("schema", "components", "proof_dir", "claims") || doc.Values["schema"] != CompositionSchema {
		return nil, exportError("expected a v2 composition contract with components, proof_dir and claims")
	}
	specs, ok := doc.Values["components"].(*Object)
	if !ok || len(specs.Keys) < 2 || len(specs.Keys) > 64 {
		return nil, exportError("a composition requires between 2 and 64 named components")
	}
	components := map[string]*Contract{}
	paths := map[string]bool{}
	parent := filepath.Dir(path)
	for _, name := range sortedKeys(specs.Values) {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		file, err := projectPath(parent, specs.Values[name])
		if err != nil {
			return nil, err
		}
		component, err := LoadContract(file)
		if err != nil {
			return nil, err
		}
		components[name] = component
		paths[component.Path] = true
	}
	if len(paths) != len(components) {
		return nil, exportError("duplicate component contract")
	}
	proofDir, err := projectDirectory(parent, doc.Values["proof_dir"])
	if err != nil {
		return nil, err
	}
	claims, err := claimNames(doc.Values["claims"])
	if err != nil {
		return nil, err
	}
	return &Composition{path, raw, proofDir, components, claims}, nil
}

// ManifestBindings maps each named component to exactly one manifest path.
func ManifestBindings(c *Composition, pairs [][2]string) (map[string]string, error) {
	paths := map[string]string{}
	for _, pair := range pairs {
		path, err := absolutePath(pair[1])
		if err != nil {
			return nil, err
		}
		paths[pair[0]] = path
	}
	if len(paths) != len(pairs) || !reflect.DeepEqual(sortedKeys(paths), sortedKeys(c.Components)) {
		return nil, exportError("provide exactly one manifest for each named component")
	}
	unique := map[string]bool{}
	for _, path := range paths {
		unique[path] = true
	}
	if len(unique) != len(paths) {
		return nil, exportError("duplicate component manifest path")
	}
	return paths, nil
}

func compositionInputs(c *Composition, bindings map[string]string, compiler Compiler) (*ProjectSnapshot, *Object, error) {
	proofs, err := SourceSnapshot(c.ProofDir)
	if err != nil {
		return nil, nil, err
	}
	combined := &ProjectSnapshot{map[string]Source{}, snapshotMap(proofs)}
	count := len(combined.Proofs)
	size := 0
	for _, s := range combined.Proofs {
		size += len(s.Data)
	}
	identities := NewObject()
	for _, name := range sortedKeys(c.Components) {
		component := c.Components[name]
		project, err := SnapshotProject(component)
		if err != nil {
			return nil, nil, err
		}
		existing := combined.modules()
		for module := range project.modules() {
			if _, overlap := existing[module]; overlap {
				return nil, nil, exportErrorf("overlapping Lean module names in composition: %s", name)
			}
		}
		count += len(project.modules())
		for _, s := range project.modules() {
			size += len(s.Data)
		}
		if count > MaxFiles || size > MaxSourceBytes {
			return nil, nil, exportError("combined proof project exceeds file or byte limit")
		}
		manifest, mode, err := verifyPackage(bindings[name], component, project, compiler)
		if err != nil {
			return nil, nil, err
		}
		identity := NewObject()
		identity.Set("contract_sha256", Digest(component.Raw))
		identity.Set("generated_manifest_sha256", Digest(manifest))
		// Metered components keep their original fields; other modes are recorded.
		if mode != MeteredMode {
			identity.Set("generated_mode", mode)
		}
		identity.Set("module", component.Module)
		identity.Set("roots", component.Roots)
		identities.Set(name, identity)
		for k, v := range project.Runtime {
			combined.Runtime[k] = v
		}
		for k, v := range project.Proofs {
			combined.Proofs[k] = v
		}
	}
	return combined, identities, nil
}
