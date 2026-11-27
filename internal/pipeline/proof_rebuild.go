package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Rebuilding a generated package from the hash-bound sources before proof
// evidence is bound to it. verifyPackage alone checks the manifest against
// itself; this ties the IR in the manifest to the Lean sources and the Go
// files to that IR.

// rebuildPackage is a variable so protocol tests can stand in for Lean.
var rebuildPackage = rebuildFromSources

func anotherVersion(detail string) error {
	return exportErrorf("generated package was produced by another lean-go version (%s); regenerate it with this lean-go", detail)
}

// rebuildFromSources requires that exporting the contract's module and roots
// again from the runtime sources gives exactly the manifest's program, and
// that emitting from that program with the manifest's mode and options gives
// exactly the package directory, manifest.json included, byte for byte.
func rebuildFromSources(manifestPath string, c *Contract, project *ProjectSnapshot, mode string) error {
	manifestPath, err := absolutePath(manifestPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(manifestPath)
	raw, err := readRegular(manifestPath, maxDocumentBytes)
	if err != nil {
		return err
	}
	value, err := ParseJSON(raw)
	if err != nil {
		return exportError(err.Error())
	}
	manifest, ok := value.(*Object)
	if !ok {
		return unsupportedManifest(nil)
	}
	program, ok := manifest.Values["program"].(*Object)
	if !ok {
		return exportError("generated program, roots or Lean pin differ from contract")
	}
	// The producer and pipeline identities are recorded in the IR inputs; a
	// difference means another lean-go built the package.
	inputs, ok := program.Values["inputs"].(*Object)
	if !ok {
		return exportError("runtime source inventory or hashes differ from generated manifest")
	}
	tools := map[string]any{}
	for _, key := range inputs.Keys {
		if !isSourceInput(key) {
			tools[key] = inputs.Values[key]
		}
	}
	current := map[string]any{}
	for key, digest := range ProducerInventory() {
		current[key] = digest
	}
	if !reflect.DeepEqual(tools, current) {
		return anotherVersion("its producer or pipeline inputs differ")
	}
	emit := EmitOptions{Mode: mode}
	if mode == NativeMode {
		depth, err := manifestDepth(manifest)
		if err != nil {
			return err
		}
		emit.NativeMaxDepth = depth
	}
	exported, err := ExportModule(ExportOptions{SourceDir: c.SourceDir, Module: c.Module, Roots: c.Roots, Cache: DefaultCache()})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(InputInventory(exported.Sources), runtimeInventory(project)) {
		return exportError("proof inputs or generated package changed during checking")
	}
	recorded, err := EncodeCompactSorted(program)
	if err != nil {
		return err
	}
	if string(append(recorded, '\n')) != string(exported.IR) {
		return exportError("generated IR differs from a fresh export of the contract's sources; regenerate the package")
	}
	// The emitted manifest.json carries ir_sha256 and is compared byte for byte.
	files, err := Emit(exported.IR, emit)
	if err != nil {
		return exportErrorf("generated package cannot be re-emitted: %v", err)
	}
	if fresh, ok := files["manifest.json"]; ok {
		if err := sameGenerator(manifest, fresh); err != nil {
			return err
		}
	}
	return samePackage(dir, baseName(manifestPath), files)
}

func isSourceInput(key string) bool { return strings.HasPrefix(key, "source/") }

// runtimeInventory is the IR input inventory of the project's runtime sources.
func runtimeInventory(project *ProjectSnapshot) map[string]string {
	var sources Snapshot
	for _, name := range sortedKeys(project.Runtime) {
		sources = append(sources, project.Runtime[name])
	}
	return InputInventory(sources)
}

func manifestDepth(manifest *Object) (int, error) {
	number, ok := manifest.Values["max_depth"].(json.Number)
	if !ok {
		return 0, exportError("native manifest must record an integer max_depth")
	}
	depth, err := number.Int64()
	if err != nil || depth < 1 || depth > 1<<24 {
		return 0, exportError("native manifest must record an integer max_depth")
	}
	return int(depth), nil
}

// sameGenerator reports a generator identity difference as a version mismatch.
func sameGenerator(manifest *Object, fresh []byte) error {
	value, err := ParseJSON(fresh)
	if err != nil {
		return err
	}
	emitted, ok := value.(*Object)
	if !ok {
		return exportError("re-emitted manifest is malformed")
	}
	for _, key := range []string{"schema", "mode", "generator", "generator_source_sha256", "validator_source_sha256"} {
		if manifest.Values[key] != emitted.Values[key] {
			return anotherVersion("its " + key + " differs")
		}
	}
	return nil
}

// samePackage requires the directory to hold exactly the emitted files.
func samePackage(dir, manifestName string, files map[string][]byte) error {
	if manifestName != "manifest.json" {
		return exportError("generated manifest must be named manifest.json to be rebuilt")
	}
	found, err := sortedEntries(dir)
	if err != nil {
		return err
	}
	expected := sortedKeys(files)
	if !reflect.DeepEqual(found, expected) {
		return exportError("generated directory inventory differs from a fresh emission; regenerate the package")
	}
	for _, name := range expected {
		data, err := readRegular(filepath.Join(dir, name), maxDocumentBytes)
		if err != nil {
			return err
		}
		if string(data) != string(files[name]) {
			return exportErrorf("generated file differs from a fresh emission of its IR: %s; regenerate the package", name)
		}
	}
	return nil
}

func sortedEntries(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
