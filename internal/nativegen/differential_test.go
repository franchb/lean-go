package nativegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildHarness writes the differential module: for every tracked program a
// metered package, a native package and a generated driver, plus the
// hand-written checks and benchmarks under testdata/harness.
func buildHarness(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	if keep := os.Getenv("LEAN_GO_NATIVE_HARNESS"); keep != "" {
		dir = keep
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	harnessModule(t, dir, Options{})
	small, err := Generate(loadIR(t, filepath.Join(repoRoot(t), "testdata/trees.ir.json")), Options{MaxDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	writePackage(t, filepath.Join(dir, "Trees/natsmall"), small)
	extras, err := filepath.Glob("testdata/harness/*/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range extras {
		data, err := os.ReadFile(extra)
		if err != nil {
			t.Fatal(err)
		}
		rel := strings.TrimPrefix(filepath.ToSlash(extra), "testdata/harness/")
		writePackage(t, filepath.Join(dir, filepath.Dir(rel)), map[string][]byte{filepath.Base(rel): data})
	}
	return dir
}

// TestDifferential compares the native and metered packages of every tracked
// IR file on boundary values, seeded random inputs, zero values and the byte
// lengths listed in docs/native-mode.md, then runs the independent checks,
// depth-guard tests and fuzz seed corpora.
func TestDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs generated packages")
	}
	dir := buildHarness(t)
	out := goCommand(t, dir, "vet", "./...")
	out += goCommand(t, dir, "test", "-count=1", "./...")
	t.Log(out)
	if fuzz := os.Getenv("LEAN_GO_NATIVE_FUZZ"); fuzz != "" {
		for _, target := range []struct{ pkg, name string }{{"./Adler32", "FuzzAdler32"}, {"./Trees", "FuzzMirror"}} {
			t.Log(goCommand(t, dir, "test", "-run", "^$", "-fuzz", "^"+target.name+"$", "-fuzztime", fuzz, target.pkg))
		}
	}
	if bench := os.Getenv("LEAN_GO_NATIVE_BENCH"); bench != "" {
		t.Log(goCommand(t, dir, "test", "-run", "^$", "-bench", bench, "-benchmem", "-cpu", "1", "-count", "5", "./bench"))
	}
}
