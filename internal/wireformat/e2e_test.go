package wireformat

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/franchb/lean-go/internal/pipeline"
)

// TestEndToEnd regenerates every example from its spec with Lean: render the
// sources, extract, check the proof contract, compare every resulting file
// with the tracked example byte for byte, then build and test the fresh copy
// together with the example's hand-written tests. It needs the pinned Lean
// toolchain on PATH.
func TestEndToEnd(t *testing.T) {
	if os.Getenv("LEAN_GO_FORMATS") != "1" {
		t.Skip("set LEAN_GO_FORMATS=1 to run extraction and the proof checker")
	}
	specs := map[string]string{}
	for _, name := range exampleNames(t) {
		specs[name] = filepath.Join(examplesDir, name, "spec.json")
	}
	if extra := os.Getenv("LEAN_GO_FORMATS_EXTRA"); extra != "" {
		// Optional extra specs, e.g. testdata/mix.json, are generated and
		// tested but have no tracked copy to compare with.
		for _, path := range filepath.SplitList(extra) {
			specs["extra:"+path] = path
		}
	}
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f, err := Load(specs[name])
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), f.Name)
			start := time.Now()
			if err := Render(f, dir, pipeline.DefaultCache()); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: rendered, extracted and proof-checked %d claims in %s", f.Name, f.ClaimCount(), time.Since(start).Round(time.Second))
			fresh := listFiles(t, dir)
			if !isExtra(name) {
				tracked := filepath.Join(examplesDir, name)
				for _, path := range fresh {
					got, err := os.ReadFile(filepath.Join(dir, path))
					if err != nil {
						t.Fatal(err)
					}
					want, err := os.ReadFile(filepath.Join(tracked, path))
					if err != nil {
						t.Fatalf("generated %s is not tracked: %v", path, err)
					}
					if string(got) != string(want) {
						t.Errorf("%s differs from the tracked example", path)
					}
				}
				// Bring in the hand-written tests, then test the fresh module.
				for _, path := range listFiles(t, tracked) {
					if slices.Contains(fresh, path) || filepath.Ext(path) != ".go" {
						continue
					}
					data, err := os.ReadFile(filepath.Join(tracked, path))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, path), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			runGo(t, dir, "vet", "./...")
			runGo(t, dir, "test", "-count=1", "./...")
		})
	}
}

func isExtra(name string) bool { return len(name) > 6 && name[:6] == "extra:" }
