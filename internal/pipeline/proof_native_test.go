package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Proof binding for native packages: the manifest fixes the file set and
// digests, the mode is recorded, and any missing, extra or changed file is
// rejected, exactly as for metered packages.

// emitProject copies a tracked proof example and emits its tracked IR in mode
// into a fresh package directory next to it. It returns the contract and
// manifest paths.
func emitProject(t *testing.T, name, mode string) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("..", "..", "examples", "proofs", name)
	for _, sub := range []string{"source", "proof"} {
		copyTree(t, filepath.Join(source, sub), filepath.Join(root, sub))
	}
	copyTree(t, filepath.Join(source, "proof-contract.json"), filepath.Join(root, "proof-contract.json"))
	data, err := os.ReadFile(filepath.Join(source, name+".ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Emit(data, EmitOptions{Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishPackage(filepath.Join(root, "generated"), files, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEAN_GO_TOOLCHAIN", DefaultCompiler.Version)
	return filepath.Join(root, "proof-contract.json"), filepath.Join(root, "generated", "manifest.json")
}

func bindPackage(t *testing.T, contractPath, manifestPath string) ([]byte, string, error) {
	t.Helper()
	contract, err := LoadContract(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	project, err := SnapshotProject(contract)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := SelectedCompiler()
	if err != nil {
		t.Fatal(err)
	}
	return verifyPackage(manifestPath, contract, project, compiler)
}

func TestBothModesOfOneProgramBind(t *testing.T) {
	for _, mode := range []string{MeteredMode, NativeMode} {
		contract, manifest := emitProject(t, "clamp", mode)
		raw, got, err := bindPackage(t, contract, manifest)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if got != mode || string(raw) != readText(t, manifest) {
			t.Fatalf("%s: bound as %q", mode, got)
		}
	}
}

func editManifest(t *testing.T, path string, change func(map[string]any)) func() {
	t.Helper()
	original := readText(t, path)
	var manifest map[string]any
	if err := json.Unmarshal([]byte(original), &manifest); err != nil {
		t.Fatal(err)
	}
	change(manifest)
	data, _ := json.Marshal(manifest)
	writeText(t, path, string(data))
	return func() { writeText(t, path, original) }
}

func TestNativeFileSetComesFromTheManifest(t *testing.T) {
	contract, manifest := emitProject(t, "clamp", NativeMode)
	dir := filepath.Dir(manifest)
	fail := func(label, pattern string) {
		t.Helper()
		_, _, err := bindPackage(t, contract, manifest)
		if err == nil {
			t.Errorf("%s: accepted", label)
			return
		}
		expectError(t, err, pattern)
	}
	// Missing, extra and changed files.
	for _, name := range []string{"functions.go", "native.go", "types.go"} {
		path := filepath.Join(dir, name)
		original := readText(t, path)
		os.Remove(path)
		fail("missing "+name, "inventory|missing")
		writeText(t, path, original+"// changed\n")
		fail("changed "+name, "hash differs: "+name)
		writeText(t, path, original)
	}
	for _, name := range []string{"extra.go", "native_test.go", "api.go", "README.md", "go.mod"} {
		path := filepath.Join(dir, name)
		writeText(t, path, "package extracted\n")
		fail("extra "+name, "inventory")
		os.Remove(path)
	}
	// The manifest's own inventory: unsafe, test, missing or extra names.
	for _, name := range []string{"../functions.go", "sub/x.go", "x_test.go", "X.go", "x.s", "manifest.json", "extra.go"} {
		restore := editManifest(t, manifest, func(m map[string]any) { m["generated_files"].(map[string]any)[name] = strings.Repeat("0", 64) })
		fail("listed "+name, "inventory")
		restore()
	}
	restore := editManifest(t, manifest, func(m map[string]any) { delete(m["generated_files"].(map[string]any), "types.go") })
	fail("unlisted types.go", "inventory")
	restore()
	for _, files := range []any{map[string]any{}, nil, []any{"native.go"}, "native.go"} {
		restore := editManifest(t, manifest, func(m map[string]any) { m["generated_files"] = files })
		fail("generated_files", "inventory")
		restore()
	}
	// The mode and generator are part of the native layout.
	for key, value := range map[string]any{"mode": "metered", "generator": "lean-go-nativegen/v0"} {
		restore := editManifest(t, manifest, func(m map[string]any) { m[key] = value })
		fail(key, "mode")
		restore()
		restore = editManifest(t, manifest, func(m map[string]any) { delete(m, key) })
		fail("no "+key, "mode")
		restore()
	}
	// A native package is not accepted under the metered schema, nor the reverse.
	restore = editManifest(t, manifest, func(m map[string]any) { m["schema"] = "lean-go-manifest/v24" })
	fail("metered schema", "inventory")
	restore()
	for _, schema := range []any{"lean-go-native-manifest/v2", "lean-go-native-manifest/v0", nil, 1} {
		restore := editManifest(t, manifest, func(m map[string]any) { m["schema"] = schema })
		fail("schema", "unsupported generated manifest schema")
		restore()
	}
	// Program, roots and source inventory are checked as for metered packages.
	for key, value := range map[string]any{"module": "Other", "roots": []any{"Clamp.other"}, "lean_commit": "0", "profile": "pure-module/v1"} {
		restore := editManifest(t, manifest, func(m map[string]any) { m["program"].(map[string]any)[key] = value })
		fail(key, "differ")
		restore()
	}
	writeText(t, filepath.Join(filepath.Dir(contract), "source", "Extra.lean"), "def extra := 1\n")
	fail("new source", "source")
	os.Remove(filepath.Join(filepath.Dir(contract), "source", "Extra.lean"))
	// The manifest cannot stand in for one of the files it lists.
	raw := readText(t, manifest)
	os.Remove(manifest)
	renamed := filepath.Join(dir, "types.go")
	writeText(t, renamed, raw)
	if _, _, err := bindPackage(t, contract, renamed); err == nil {
		t.Error("manifest named types.go accepted")
	}
	// types.go now holds the manifest bytes, so its hash no longer matches.
	writeText(t, manifest, raw)
	if _, _, err := bindPackage(t, contract, manifest); err == nil {
		t.Error("corrupted types.go accepted")
	}
}

func TestNativeEvidenceRecordsTheMode(t *testing.T) {
	fakeProve(t, nil)
	for _, mode := range []string{MeteredMode, NativeMode} {
		contract, manifest := emitProject(t, "clamp", mode)
		raw, _, err := Evidence(contract, manifest)
		if err != nil {
			t.Fatal(err)
		}
		var evidence map[string]any
		json.Unmarshal(raw, &evidence)
		recorded, ok := evidence["generated_mode"]
		if mode == MeteredMode && ok {
			t.Fatalf("metered evidence gained generated_mode %v", recorded)
		}
		if mode == NativeMode && recorded != NativeMode {
			t.Fatalf("native evidence records mode %v", recorded)
		}
		if evidence["generated_manifest_sha256"] != Digest([]byte(readText(t, manifest))) {
			t.Fatal("manifest digest")
		}
		// Stale evidence is refused after any change to a native file.
		output := filepath.Join(filepath.Dir(contract), "evidence.json")
		if _, err := CheckProofs(CheckProofsOptions{Contract: contract, Manifest: manifest, Output: output}); err != nil {
			t.Fatal(err)
		}
		if _, err := CheckProofs(CheckProofsOptions{Contract: contract, Manifest: manifest, Evidence: output}); err != nil {
			t.Fatal(err)
		}
		writeText(t, manifest, readText(t, manifest)+" ")
		_, err = CheckProofs(CheckProofsOptions{Contract: contract, Manifest: manifest, Evidence: output})
		expectError(t, err, "stale")
	}
}

func TestCompositionBindsANativeComponent(t *testing.T) {
	c := newCompositionFixture(t)
	noProofs(t, nil)
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "proofs", "crc16", "crc16.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Emit(data, EmitOptions{Mode: NativeMode})
	if err != nil {
		t.Fatal(err)
	}
	var native string
	for _, pair := range c.manifests {
		if pair[0] == "crc16" {
			native = filepath.Dir(pair[1])
		}
	}
	if err := PublishPackage(native, files, true); err != nil {
		t.Fatal(err)
	}
	if err := c.check("output", ""); err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	json.Unmarshal([]byte(readText(t, c.output)), &evidence)
	components := obj(evidence["components"])
	if obj(components["crc16"])["generated_mode"] != NativeMode {
		t.Fatalf("crc16 identity %v", components["crc16"])
	}
	if _, ok := obj(components["wire16"])["generated_mode"]; ok {
		t.Fatalf("metered wire16 identity %v", components["wire16"])
	}
	if err := c.check("evidence", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"functions.go", "extra.go"} {
		path := filepath.Join(native, name)
		original, _ := os.ReadFile(path)
		writeText(t, path, string(original)+"// changed\n")
		if c.check("evidence", "") == nil {
			t.Errorf("changed %s accepted", name)
		}
		if original == nil {
			os.Remove(path)
		} else {
			writeText(t, path, string(original))
		}
	}
	if keys := keysOf(components); !slices.Contains(keys, "crc16") || !slices.Contains(keys, "wire16") {
		t.Fatal(keys)
	}
}
