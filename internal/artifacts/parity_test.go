package artifacts

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

// These tests need the pinned Lean toolchain. They regenerate every tracked
// module artifact with the in-process Go pipeline and compare the result with
// the checked-in bytes. Generated Go files must be byte-identical; IR,
// manifests and evidence must be identical except for the provenance
// inventory (IR "inputs", the manifest's copy of it and its IR digest, and
// evidence fields derived from them or from the tool identity). Once the
// tracked artifacts carry the Go-era inventory, every byte must match.

func requireLean(t *testing.T) {
	t.Helper()
	if os.Getenv("LEAN_GO_PARITY") != "1" {
		t.Skip("set LEAN_GO_PARITY=1 to compare fresh Go-pipeline artifacts with the tracked ones")
	}
}

func checkoutRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func parseObject(t *testing.T, label string, data []byte) *pipeline.Object {
	t.Helper()
	value, err := pipeline.ParseJSON(data)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	object, ok := value.(*pipeline.Object)
	if !ok {
		t.Fatalf("%s is not a JSON object", label)
	}
	return object
}

func canonical(t *testing.T, value *pipeline.Object) string {
	t.Helper()
	data, err := pipeline.EncodeCompactSorted(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func goEra(inputs *pipeline.Object) bool {
	_, ok := inputs.Values[pipeline.ToolInput]
	return ok
}

// compareInputs requires the fresh inventory to be exactly the Go-era
// inventory for the tracked source digests.
func compareInputs(t *testing.T, label string, fresh, tracked *pipeline.Object) {
	t.Helper()
	expected := pipeline.ProducerInventory()
	for _, key := range tracked.Keys {
		if strings.HasPrefix(key, "source/") {
			expected[key] = tracked.Values[key].(string)
		}
	}
	if len(fresh.Keys) != len(expected) {
		t.Errorf("%s: inventory keys %v, want %v", label, fresh.Keys, sortedKeys(expected))
		return
	}
	for key, digest := range expected {
		if fresh.Values[key] != digest {
			t.Errorf("%s: inventory %s = %v, want %s", label, key, fresh.Values[key], digest)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CompareIR reports differences outside the provenance inventory.
func compareIR(t *testing.T, label string, fresh, tracked []byte) {
	t.Helper()
	f, k := parseObject(t, label, fresh), parseObject(t, label, tracked)
	fi, _ := f.Values["inputs"].(*pipeline.Object)
	ki, _ := k.Values["inputs"].(*pipeline.Object)
	if fi == nil || ki == nil {
		t.Fatalf("%s: missing inputs", label)
	}
	compareInputs(t, label, fi, ki)
	if goEra(ki) && !bytes.Equal(fresh, tracked) {
		t.Errorf("%s differs from the tracked Go-era IR", label)
	}
	f.Delete("inputs")
	k.Delete("inputs")
	if canonical(t, f) != canonical(t, k) {
		t.Errorf("%s differs from the tracked IR outside the inventory", label)
	}
	if !bytes.HasSuffix(fresh, []byte("\n")) || bytes.Count(fresh, []byte("\n")) != 1 {
		t.Errorf("%s is not one compact JSON line", label)
	}
}

func compareManifest(t *testing.T, label string, fresh, tracked []byte) {
	t.Helper()
	f, k := parseObject(t, label, fresh), parseObject(t, label, tracked)
	fp, _ := f.Values["program"].(*pipeline.Object)
	kp, _ := k.Values["program"].(*pipeline.Object)
	if fp == nil || kp == nil {
		t.Fatalf("%s: missing program", label)
	}
	ki, _ := kp.Values["inputs"].(*pipeline.Object)
	if ki != nil && goEra(ki) && !bytes.Equal(fresh, tracked) {
		t.Errorf("%s differs from the tracked Go-era manifest", label)
	}
	fp.Delete("inputs")
	kp.Delete("inputs")
	f.Delete("ir_sha256")
	k.Delete("ir_sha256")
	if canonical(t, f) != canonical(t, k) {
		t.Errorf("%s differs from the tracked manifest outside the inventory", label)
	}
}

func compareEvidence(t *testing.T, label string, fresh, tracked []byte) {
	t.Helper()
	f, k := parseObject(t, label, fresh), parseObject(t, label, tracked)
	ti, _ := k.Values["tool_inputs"].(*pipeline.Object)
	if ti != nil {
		if _, ok := ti.Values[pipeline.ToolInput]; ok && !bytes.Equal(fresh, tracked) {
			t.Errorf("%s differs from the tracked Go-era evidence", label)
		}
	}
	for _, o := range []*pipeline.Object{f, k} {
		o.Delete("tool_inputs")
		o.Delete("generated_manifest_sha256")
		if components, ok := o.Values["components"].(*pipeline.Object); ok {
			for _, name := range components.Keys {
				components.Values[name].(*pipeline.Object).Delete("generated_manifest_sha256")
			}
		}
	}
	if canonical(t, f) != canonical(t, k) {
		t.Errorf("%s differs from the tracked evidence outside inventory-derived fields", label)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// comparePackage requires identical Go files and an equivalent manifest.
func comparePackage(t *testing.T, label, fresh, tracked string) {
	t.Helper()
	freshFiles, err := tree(fresh)
	if err != nil {
		t.Fatal(err)
	}
	trackedFiles, err := tree(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshFiles) != len(trackedFiles) {
		t.Errorf("%s: file sets differ", label)
	}
	for name, data := range freshFiles {
		want, ok := trackedFiles[name]
		switch {
		case !ok:
			t.Errorf("%s/%s is not tracked", label, name)
		case name == "manifest.json":
			compareManifest(t, label+"/manifest.json", data, want)
		case !bytes.Equal(data, want):
			t.Errorf("%s/%s differs from the tracked file", label, name)
		}
	}
}

func TestTrackedArtifactParity(t *testing.T) {
	requireLean(t)
	root := checkoutRoot(t)
	stage := t.TempDir()
	cache := &pipeline.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	for _, spec := range Packages {
		t.Run(spec.Name(), func(t *testing.T) {
			if err := GeneratePackage(root, stage, spec, cache); err != nil {
				t.Fatal(err)
			}
			comparePackage(t, spec.Package, at(stage, spec.Package), at(root, spec.Package))
			compareIR(t, spec.IR, readFile(t, at(stage, spec.IR)), readFile(t, at(root, spec.IR)))
			if !bytes.Equal(readFile(t, at(stage, spec.Validation())), readFile(t, at(root, spec.Validation()))) {
				t.Errorf("%s differs from the tracked validation report", spec.Validation())
			}
			if spec.Evidence != "" {
				compareEvidence(t, spec.Evidence, readFile(t, at(stage, spec.Evidence)), readFile(t, at(root, spec.Evidence)))
			}
		})
	}
	t.Run("checkedframe", func(t *testing.T) {
		for _, name := range Frame.Components {
			if _, err := os.Stat(at(stage, "examples/proofs/"+name+"/"+name+"/manifest.json")); err != nil {
				t.Skip("component packages were not generated in this run")
			}
		}
		if err := CheckFrame(root, stage, stage); err != nil {
			t.Fatal(err)
		}
		compareEvidence(t, Frame.Evidence, readFile(t, at(stage, Frame.Evidence)), readFile(t, at(root, Frame.Evidence)))
	})
	formats, err := FormatNames(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range formats {
		t.Run("format-"+name, func(t *testing.T) {
			if err := GenerateFormat(root, stage, name, cache); err != nil {
				t.Fatal(err)
			}
			base := FormatsDir + "/" + name
			files, err := tree(at(stage, base))
			if err != nil {
				t.Fatal(err)
			}
			for file, data := range files {
				label := base + "/" + file
				want := readFile(t, at(root, label))
				switch {
				case file == name+".ir.json":
					compareIR(t, label, data, want)
				case file == "proof-evidence.json":
					compareEvidence(t, label, data, want)
				case strings.HasSuffix(file, "/manifest.json"):
					compareManifest(t, label, data, want)
				case !bytes.Equal(data, want):
					t.Errorf("%s differs from the tracked file", label)
				}
			}
		})
	}
}

// A cached producer build must give byte-identical IR and Go to a cold build.
func TestCachedProducerMatchesColdBuild(t *testing.T) {
	requireLean(t)
	root := checkoutRoot(t)
	cache := &pipeline.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	for _, spec := range []Package{Packages[1], Packages[len(Packages)-1]} {
		t.Run(spec.Name(), func(t *testing.T) {
			var trees []map[string][]byte
			for i, c := range []struct {
				cache *pipeline.Cache
				want  pipeline.CacheStatus
			}{{nil, pipeline.CacheDisabled}, {cache, ""}, {cache, pipeline.CacheHit}} {
				out := filepath.Join(t.TempDir(), "run")
				result, err := pipeline.Generate(pipeline.GenerateOptions{
					SourceDir: at(root, spec.SourceDir), Module: spec.Module, Roots: spec.Roots,
					Out: filepath.Join(out, spec.Name()), IROutput: filepath.Join(out, spec.Name()+".ir.json"), Cache: c.cache,
				})
				if err != nil {
					t.Fatal(err)
				}
				if c.want != "" && result.Cache != c.want {
					t.Fatalf("run %d: cache status %s, want %s", i, result.Cache, c.want)
				}
				if i == 1 && result.Cache != pipeline.CacheStored && result.Cache != pipeline.CacheHit {
					t.Fatalf("second run did not use the cache: %s", result.Cache)
				}
				files, err := tree(out)
				if err != nil {
					t.Fatal(err)
				}
				trees = append(trees, files)
			}
			for i := 1; i < len(trees); i++ {
				if !sameTree(trees[0], trees[i]) {
					t.Fatalf("run %d differs from the uncached build", i)
				}
			}
		})
	}
}
