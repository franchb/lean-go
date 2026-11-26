package nativegen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// trackedIR lists every tracked module IR file the native emitter must accept.
func trackedIR(t testing.TB) []string {
	t.Helper()
	root := repoRoot(t)
	var files []string
	for _, pattern := range []string{"testdata/*.ir.json", "examples/proofs/*/*.ir.json"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	slices.Sort(files)
	if len(files) < 17 {
		t.Fatalf("found only %d tracked IR files", len(files))
	}
	return files
}

func loadIR(t testing.TB, path string) *moduleir.Program {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := moduleir.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return p
}

func programName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".ir.json")
}

func writePackage(t testing.TB, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func goCommand(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out.String())
	}
	return out.String()
}

func TestGenerateDeterministic(t *testing.T) {
	for _, path := range trackedIR(t) {
		first, err := Generate(loadIR(t, path), Options{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		second, err := Generate(loadIR(t, path), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(first) != len(second) {
			t.Fatalf("%s: file sets differ", path)
		}
		for name, data := range first {
			if !bytes.Equal(data, second[name]) {
				t.Fatalf("%s: %s differs between runs", path, name)
			}
		}
	}
}

// TestGeneratedPackagesVet builds every tracked program's native package in a
// fresh module and requires gofmt-clean, go vet-clean, stdlib-only output.
func TestGeneratedPackagesVet(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated packages")
	}
	dir := t.TempDir()
	if dump := os.Getenv("LEAN_GO_NATIVE_DUMP"); dump != "" {
		dir = dump
	}
	writePackage(t, dir, map[string][]byte{"go.mod": []byte("module nativecheck\n\ngo 1.27\n")})
	for _, path := range trackedIR(t) {
		files, err := Generate(loadIR(t, path), Options{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for name, data := range files {
			if strings.HasSuffix(name, ".go") && strings.Contains(string(data), "\nimport (") {
				t.Fatalf("%s/%s imports more than one package", path, name)
			}
		}
		writePackage(t, filepath.Join(dir, programName(path)), files)
	}
	goCommand(t, dir, "vet", "./...")
	if out := goCommand(t, dir, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."); strings.Count(out, "\n") != len(trackedIR(t)) {
		t.Fatalf("unexpected non-standard dependencies:\n%s", out)
	}
	out, err := exec.Command("gofmt", "-l", dir).CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("gofmt -l: %v\n%s", err, out)
	}
}

func TestNameCollisions(t *testing.T) {
	files, err := Generate(collidingTrees(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var api API
	manifestAPI(t, files, &api)
	if got := api.Types["Other.MirrorRed"].Go; got != "MirrorRed" {
		t.Fatalf("tree type %q", got)
	}
	if got := api.Types["Trees.Mirror"].Go; got != "Mirror" {
		t.Fatalf("color type %q", got)
	}
	if got := api.Types["Trees.Mirror"].Constructors["Trees.Color.red"]; got != "MirrorRed2" {
		t.Fatalf("constructor %q", got)
	}
	if got := api.Functions["Trees.mirror"].Go; got != "Mirror2" {
		t.Fatalf("root %q", got)
	}
	functions := string(files["functions.go"])
	if !strings.Contains(functions, "func fnv1(") || !strings.Contains(functions, "// fnv1 implements the Lean function Trees.v1.") {
		t.Fatalf("internal name:\n%s", functions)
	}
}

func TestExportedNames(t *testing.T) {
	for lean, want := range map[string]string{
		"Adler32.update": "Update", "List UInt8": "List_UInt8", "Foo.«weird name»": "X_weird_name_",
		"A.αβ": "Αβ", "A.中文": "X中文", "A._x": "X_x", "A.": "X", "A.type": "Type",
	} {
		if got := exportedName(lean); got != want {
			t.Errorf("exportedName(%q) = %q, want %q", lean, got, want)
		}
	}
	a := newInternalAllocator()
	for _, c := range []struct{ preferred, want string }{{"len", "len2"}, {"v1", "fnv1"}, {"step", "step"}, {"errors", "errors2"}, {"step", "step2"}} {
		if got := a.allocateInternal(c.preferred); got != c.want {
			t.Errorf("allocateInternal(%q) = %q, want %q", c.preferred, got, c.want)
		}
	}
}
