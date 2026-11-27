package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

// Program identity written into every module IR.
const (
	Schema  = moduleir.Schema
	Profile = "pure-module/v22"
	Stage   = "pure-mono"
)

const (
	diagnosticProtocol = "lean-go-diagnostic.json"
	maxProtocolBytes   = 1024 * 1024
	buildHint          = "Check the Lean build error and the pinned toolchain; this is not an admission rejection."
)

// ExportOptions selects one module extraction.
type ExportOptions struct {
	SourceDir string
	Module    string
	Roots     []string
	// Cache, when non-nil, reuses a verified producer build.
	Cache *Cache
	// Validation, when non-nil, translation-validates the exported IR in a
	// separate Lean process after the export is complete (validate_run.go).
	Validation *ValidationOptions

	// harness replaces the generated export command in tests.
	harness func(module string, roots []string) string
}

// ExportResult is the complete, canonical IR and how the producer was built.
type ExportResult struct {
	IR      []byte
	Sources Snapshot
	Cache   CacheStatus
	// Validation is set when ExportOptions.Validation was.
	Validation *ValidationResult
}

// LakeConfig is the fixed dependency-free Lake configuration for a snapshot.
func LakeConfig(modules []string) []byte {
	roots := map[string]bool{"LeanGo": true}
	for _, name := range modules {
		roots[strings.SplitN(name, ".", 2)[0]] = true
	}
	names := make([]string, 0, len(roots))
	for name := range roots {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("name = \"lean_go_module_producer\"\nversion = \"0.0.0\"\n")
	for _, name := range names {
		fmt.Fprintf(&b, "\n[[lean_lib]]\nname = \"%s\"\n", name)
	}
	return []byte(b.String())
}

func exportHarness(module string, roots []string) string {
	var command string
	if len(roots) == 1 {
		command = fmt.Sprintf("#lean_go_export \"ir.json\" %s \"%s\"\n", roots[0], module)
	} else {
		command = fmt.Sprintf("#lean_go_export_roots \"ir.json\" [%s] \"%s\"\n", strings.Join(roots, ", "), module)
	}
	return "import LeanGo.Module.Export\nimport " + module + "\n" + command
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// stageSources writes instrumented snapshots and checks every import.
func stageSources(sources Snapshot, workspace, prefix string) ([]mappedSource, error) {
	var mapped []mappedSource
	for _, source := range sources {
		mapping, imports, err := instrumentSource(source.Data)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, mappedSource{source.Module, source.Relative, mapping})
		for _, imported := range imports {
			if _, local := sources.Lookup(imported); local {
				continue
			}
			top := strings.SplitN(imported, ".", 2)[0]
			olean := filepath.Join(prefix, "lib", "lean", filepath.FromSlash(strings.ReplaceAll(imported, ".", "/")+".olean"))
			if (top != "Lean" && top != "Init" && top != "Std") || !isFile(olean) {
				return nil, exportErrorf("unsupported or missing local import: %s", imported)
			}
		}
		if err := writeFile(filepath.Join(workspace, filepath.FromSlash(source.Relative)), mapping.Instrumented); err != nil {
			return nil, err
		}
	}
	return mapped, nil
}

// runCompiler interprets the producer's explicit failure protocol.
func runCompiler(command []string, workspace string, env []string, sources []mappedSource, admission bool) error {
	protocol := filepath.Join(workspace, diagnosticProtocol)
	if admission {
		os.Remove(protocol)
	}
	_, err := run(command, workspace, env)
	if err == nil {
		return nil
	}
	if admission && isFile(protocol) {
		item, invalid := readProducerDiagnostic(protocol, sources)
		if invalid != nil {
			return diagnosticError(newDiagnostic("invalid producer diagnostic: "+invalid.Error(), "build", "producer_protocol", nil, nil, nil, nil), err)
		}
		return diagnosticError(item, err)
	}
	return diagnosticError(newDiagnostic(err.Error(), "build", "producer_failed", nil, nil,
		compilerLocation(err.Error(), sources, workspace), str(buildHint)), err)
}

func readProducerDiagnostic(path string, sources []mappedSource) (*Object, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxProtocolBytes {
		return nil, errors.New("producer diagnostic exceeds limit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := ParseJSON(data)
	if err != nil {
		return nil, err
	}
	item, err := producerDiagnostic(value)
	if err != nil {
		return nil, err
	}
	item.Set("location", mapLocation(item.Values["location"], sources))
	return item, nil
}

// leanTools finds Lean and Lake or reports a toolchain failure.
func leanTools() (string, string, error) {
	lean, okLean := lookPath("lean")
	lake, okLake := lookPath("lake")
	if !okLean || !okLake {
		return "", "", staged("toolchain", "toolchain_unavailable", exportError("pinned Lean and Lake executables must be available on PATH"))
	}
	return lean, lake, nil
}

// ExportModule extracts one module from a fresh, private Lake workspace.
func ExportModule(opts ExportOptions) (*ExportResult, error) {
	compiler, err := SelectedCompiler()
	if err != nil {
		return nil, staged("toolchain", "toolchain_mismatch", err)
	}
	if err := ValidateName(opts.Module); err != nil {
		return nil, err
	}
	roots, err := SelectRoots(opts.Roots)
	if err != nil {
		return nil, err
	}
	sources, err := SourceSnapshot(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	if _, ok := sources.Lookup(opts.Module); !ok {
		return nil, exportErrorf("requested module does not exist: %s", opts.Module)
	}
	env := childEnvironment(compiler)
	lean, lake, err := leanTools()
	if err != nil {
		return nil, err
	}
	workspace, err := os.MkdirTemp("", "lean-go-module-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workspace)
	if err := os.WriteFile(filepath.Join(workspace, "lean-toolchain"), compiler.PinFile(), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(workspace, "lakefile.toml"), LakeConfig(sources.Modules()), 0o644); err != nil {
		return nil, err
	}
	versionText, err := run([]string{lean, "--version"}, workspace, env)
	if err == nil {
		err = compiler.CheckVersion(versionText)
	}
	if err != nil {
		return nil, staged("toolchain", "toolchain_mismatch", err)
	}
	prefixText, err := run([]string{lean, "--print-prefix"}, workspace, env)
	if err != nil {
		return nil, staged("toolchain", "toolchain_mismatch", err)
	}
	mapped, err := stageSources(sources, workspace, strings.TrimSpace(prefixText))
	if err != nil {
		return nil, err
	}
	validating := opts.Validation != nil
	for name, data := range producerFiles(validating) {
		if err := writeFile(filepath.Join(workspace, filepath.FromSlash(name)), data); err != nil {
			return nil, err
		}
	}
	text, err := run([]string{lake, "env", "lean", "--version"}, workspace, env)
	if err == nil {
		err = compiler.CheckVersion(text)
	}
	if err != nil {
		return nil, staged("toolchain", "toolchain_mismatch", err)
	}
	if err := checkLakeManifest(workspace); err != nil {
		return nil, err
	}
	cached, status := opts.Cache.prepareProducer(lake, versionText, compiler, env, validating)
	if cached != nil {
		if err := writeTree(filepath.Join(workspace, ".lake", "build"), cached); err != nil {
			return nil, err
		}
	}
	build := append(append([]string{lake, "build"}, producerTargets(validating)...), sources.Modules()...)
	if err := runCompiler(build, workspace, env, mapped, false); err != nil {
		return nil, err
	}
	harness := exportHarness
	if opts.harness != nil {
		harness = opts.harness
	}
	harnessPath := filepath.Join(workspace, "LeanGo", "Module", "Harness.lean")
	if err := os.WriteFile(harnessPath, []byte(harness(opts.Module, roots)), 0o644); err != nil {
		return nil, err
	}
	if err := runCompiler([]string{lake, "env", "lean", "LeanGo/Module/Harness.lean"}, workspace, env, mapped, true); err != nil {
		return nil, err
	}
	result, err := completeOutput(workspace, opts.Module, roots, InputInventory(sources), compiler)
	if err != nil {
		return nil, staged("build", "producer_protocol", err)
	}
	if err := verifySources(opts.SourceDir, sources); err != nil {
		return nil, err
	}
	exported := &ExportResult{IR: result, Sources: sources, Cache: status}
	if validating {
		exported.Validation, err = runValidation(validationRun{
			workspace: workspace, lake: lake, lean: lean, env: env, module: opts.Module, roots: roots,
			ir: result, compiler: compiler, options: *opts.Validation,
		})
		if err != nil {
			return nil, err
		}
	}
	return exported, nil
}

func checkLakeManifest(workspace string) error {
	data, err := os.ReadFile(filepath.Join(workspace, "lake-manifest.json"))
	if err != nil {
		return err
	}
	value, err := ParseJSON(data)
	if err != nil {
		return exportError(err.Error())
	}
	manifest, ok := value.(*Object)
	if !ok {
		return exportError("producer dependency lock must have no packages")
	}
	packages, ok := manifest.Values["packages"].([]any)
	if !ok || len(packages) != 0 {
		return exportError("producer dependency lock must have no packages")
	}
	return nil
}

func completeOutput(workspace, module string, roots []string, inputs map[string]string, compiler Compiler) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(workspace, "ir.json"))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxIRBytes {
		return nil, exportError("producer IR exceeds 4 MiB")
	}
	program, err := completeProgram(raw, module, roots, inputs, compiler)
	if err != nil {
		return nil, err
	}
	result, err := EncodeCompactSorted(program)
	if err != nil {
		return nil, err
	}
	result = append(result, '\n')
	if len(result) > MaxIRBytes {
		return nil, exportError("complete IR exceeds 4 MiB")
	}
	return result, nil
}

func completeProgram(raw []byte, module string, roots []string, inputs map[string]string, compiler Compiler) (*Object, error) {
	value, err := ParseJSON(raw)
	if err != nil {
		return nil, exportError(err.Error())
	}
	program, ok := value.(*Object)
	if !ok || !program.HasExactly("module", "roots", "types", "functions", "claims", "rewrites", "provenance", "api") {
		return nil, exportError("unexpected module producer result fields")
	}
	gotRoots, ok := program.Values["roots"].([]any)
	same := ok && len(gotRoots) == len(roots)
	for i := 0; same && i < len(roots); i++ {
		same = gotRoots[i] == roots[i]
	}
	if program.Values["module"] != module || !same {
		return nil, exportError("module producer returned a different module or roots")
	}
	features, err := programFeatures(program)
	if err != nil {
		return nil, exportError("unexpected module producer executable fields")
	}
	program.Set("schema", Schema)
	program.Set("profile", Profile)
	program.Set("stage", Stage)
	program.Set("features", features)
	program.Set("lean_version", compiler.Version)
	program.Set("lean_commit", compiler.Commit)
	program.Set("inputs", inputs)
	return program, nil
}

// verifySources requires that the snapshot did not change during extraction.
func verifySources(sourceDir string, sources Snapshot) error {
	current, err := SourceSnapshot(sourceDir)
	if err != nil || !reflect.DeepEqual(InputInventory(current), InputInventory(sources)) {
		return exportError("module producer inputs changed after export")
	}
	return nil
}

// VerifyInputs checks an IR's inputs against the current sources and tools.
func VerifyInputs(sourceDir string, program *Object) error {
	current, err := SourceSnapshot(sourceDir)
	if err != nil {
		return exportError("module producer inputs changed after export")
	}
	inputs, ok := program.Values["inputs"].(*Object)
	expected := InputInventory(current)
	if !ok || len(inputs.Keys) != len(expected) {
		return exportError("module producer inputs changed after export")
	}
	for key, digest := range expected {
		if inputs.Values[key] != digest {
			return exportError("module producer inputs changed after export")
		}
	}
	return nil
}
