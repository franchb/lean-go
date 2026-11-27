package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Protocol checks of proof_rebuild.go that need no Lean; the rebuild itself
// is tested in lean_proofgate_test.go.

// emittedPackage emits the tracked clamp IR and returns the files and the
// package directory they were published to.
func emittedPackage(t *testing.T, mode string) (map[string][]byte, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "proofs", "clamp", "clamp.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Emit(data, EmitOptions{Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "package")
	if err := PublishPackage(dir, files, false); err != nil {
		t.Fatal(err)
	}
	return files, dir
}

func TestRebuiltPackageMustMatchByteForByte(t *testing.T) {
	for _, mode := range []string{MeteredMode, NativeMode} {
		files, dir := emittedPackage(t, mode)
		if err := samePackage(dir, "manifest.json", files); err != nil {
			t.Fatalf("%s: untouched package: %v", mode, err)
		}
		if err := sameGenerator(mustObject(t, files["manifest.json"]), files["manifest.json"]); err != nil {
			t.Fatal(err)
		}
		// The first Go file in name order, before manifest.json.
		name := sortedKeys(files)[0]
		path := filepath.Join(dir, name)
		writeText(t, path, string(files[name])+"\n// edited\n")
		rehash(t, filepath.Join(dir, "manifest.json"), name)
		expectError(t, samePackage(dir, "manifest.json", files), "generated file differs from a fresh emission of its IR: "+name)
		writeText(t, path, string(files[name]))
		writeText(t, filepath.Join(dir, "extra.go"), "package extra\n")
		expectError(t, samePackage(dir, "manifest.json", files), "inventory differs")
	}
}

func mustObject(t *testing.T, data []byte) *Object {
	t.Helper()
	value, err := ParseJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*Object)
}

func TestPackageOfAnotherVersionIsRefusedWithAHint(t *testing.T) {
	files, _ := emittedPackage(t, NativeMode)
	for _, key := range []string{"generator", "generator_source_sha256", "validator_source_sha256"} {
		var m map[string]any
		json.Unmarshal(files["manifest.json"], &m)
		m[key] = "lean-go-other"
		data, _ := json.Marshal(m)
		expectError(t, sameGenerator(mustObject(t, data), files["manifest.json"]), "another lean-go version .*"+key+".*regenerate")
	}
	// Producer and pipeline inputs are compared before Lean is run.
	contract, manifest := emitProject(t, "clamp", NativeMode)
	c, err := LoadContract(contract)
	if err != nil {
		t.Fatal(err)
	}
	project, err := SnapshotProject(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{ToolInput, "producer/LeanGo/Module/Export.lean"} {
		var m map[string]any
		original := readText(t, manifest)
		json.Unmarshal([]byte(original), &m)
		obj(obj(m["program"])["inputs"])[key] = strings.Repeat("0", 64)
		data, _ := json.Marshal(m)
		writeText(t, manifest, string(data))
		expectError(t, rebuildFromSources(manifest, c, project, NativeMode), "produced by another lean-go version.*regenerate it with this lean-go")
		writeText(t, manifest, original)
	}
}
