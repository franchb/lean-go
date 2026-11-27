package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
	"testing"
)

// Shared helpers for the tests ported from the retired Python suites in scripts/.

var (
	sharedCacheOnce sync.Once
	sharedCacheDir  string
)

func TestMain(m *testing.M) {
	if os.Getenv(helperProcess) == "1" {
		fmt.Print("compiler stdout")
		fmt.Fprint(os.Stderr, "precise compiler stderr")
		os.Exit(9)
	}
	code := m.Run()
	if sharedCacheDir != "" {
		os.RemoveAll(sharedCacheDir)
	}
	os.Exit(code)
}

// testCache is one producer cache for every Lean-gated test in this binary.
func testCache(t *testing.T) *Cache {
	t.Helper()
	sharedCacheOnce.Do(func() {
		dir, err := os.MkdirTemp("", "lean-go-test-cache-")
		if err == nil {
			sharedCacheDir = dir
		}
	})
	if sharedCacheDir == "" {
		return nil
	}
	return &Cache{Dir: filepath.Join(sharedCacheDir, "producer")}
}

func gate(t *testing.T, name string) {
	t.Helper()
	if os.Getenv(name) != "1" {
		t.Skipf("set %s=1 for fresh Lean builds", name)
	}
}

func requireModules(t *testing.T) { gate(t, "LEAN_GO_MODULES") }

func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// elaborationFailure matches compiler errors that must never pass as audit rejections.
var elaborationFailure = regexp.MustCompile(`(?i)unexpected token|unknown (?:identifier|constant|attribute)|` +
	`unsolved goals|type mismatch|maximum recursion depth|stack overflow|RecursionError|` +
	`error\(lean\.(?:unknownIdentifier|unknownConstant|unknownAttribute|typeMismatch|unsolvedGoals|unexpectedToken)\)`)

// leanFixture is a fresh source directory with outputs next to it.
type leanFixture struct {
	t        *testing.T
	dir      string
	source   string
	ir       string
	pkg      string
	harness  func(string, []string) string
	exported []byte
}

func newLeanFixture(t *testing.T) *leanFixture {
	t.Helper()
	dir := t.TempDir()
	f := &leanFixture{t: t, dir: dir, source: filepath.Join(dir, "source"), ir: filepath.Join(dir, "program.json"), pkg: filepath.Join(dir, "package")}
	if err := os.Mkdir(f.source, 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *leanFixture) write(name, text string) { writeText(f.t, filepath.Join(f.source, name), text) }

// export extracts and decodes the IR, failing the test on error.
func (f *leanFixture) export(module string, roots ...string) map[string]any {
	f.t.Helper()
	program, err := f.tryExport(module, roots...)
	if err != nil {
		f.t.Fatalf("export %s %v: %v", module, roots, err)
	}
	return program
}

func (f *leanFixture) tryExport(module string, roots ...string) (map[string]any, error) {
	f.t.Helper()
	result, err := ExportModule(ExportOptions{SourceDir: f.source, Module: module, Roots: roots, Cache: testCache(f.t), harness: f.harness})
	if err != nil {
		return nil, err
	}
	f.exported = result.IR
	var program map[string]any
	if err := json.Unmarshal(result.IR, &program); err != nil {
		f.t.Fatal(err)
	}
	return program, nil
}

// reject runs the full generate command over existing outputs and requires a
// rejection matching pattern that is not a compiler elaboration error, with
// both outputs preserved.
func (f *leanFixture) reject(module, root, pattern string) error {
	f.t.Helper()
	writeText(f.t, f.ir, "previous output\n")
	writeText(f.t, filepath.Join(f.pkg, "previous.go"), "previous package")
	_, err := Generate(GenerateOptions{SourceDir: f.source, Module: module, Roots: []string{root}, Out: f.pkg, IROutput: f.ir, Cache: testCache(f.t), harness: f.harness})
	if err == nil {
		f.t.Fatalf("%s was accepted, want rejection matching %q", root, pattern)
	}
	if pattern == "" {
		// Any failure, build failures included (test_diagnostics_lean.py).
	} else if err := checkRejection(err, pattern); err != nil {
		f.t.Fatal(err)
	}
	if readText(f.t, f.ir) != "previous output\n" || readText(f.t, filepath.Join(f.pkg, "previous.go")) != "previous package" {
		f.t.Fatal("rejection changed existing output")
	}
	return err
}

// checkRejection mirrors RecordAdmissionTests.assert_rejected.
func checkRejection(err error, pattern string) error {
	if !regexp.MustCompile(pattern).MatchString(err.Error()) {
		return fmt.Errorf("rejection %q does not match %q", err, pattern)
	}
	if elaborationFailure.MatchString(err.Error()) {
		return fmt.Errorf("Regex matched: rejection is an elaboration failure: %s", err)
	}
	return nil
}

// JSON helpers over decoded IR.

func obj(v any) map[string]any { return v.(map[string]any) }
func arr(v any) []any          { return v.([]any) }
func str2(v any) string        { s, _ := v.(string); return s }

func byName(items any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, item := range arr(items) {
		out[str2(obj(item)["name"])] = obj(item)
	}
	return out
}

func functionsOf(program map[string]any) map[string]map[string]any {
	return byName(program["functions"])
}

func typesOf(program map[string]any) map[string]map[string]any { return byName(program["types"]) }

func names(items any) []string {
	var out []string
	for _, item := range arr(items) {
		out = append(out, str2(obj(item)["name"]))
	}
	return out
}

func typesOfParams(params any) []string {
	var out []string
	for _, p := range arr(params) {
		out = append(out, str2(obj(p)["type"]))
	}
	return out
}

// nodes walks let, join and cases bodies in source order.
func nodes(code any) []map[string]any {
	node := obj(code)
	out := []map[string]any{node}
	switch node["op"] {
	case "let":
		out = append(out, nodes(node["next"])...)
	case "join":
		out = append(out, nodes(obj(node["join"])["body"])...)
		out = append(out, nodes(node["next"])...)
	case "cases":
		for _, alt := range arr(node["alts"]) {
			out = append(out, nodes(obj(alt)["body"])...)
		}
	}
	return out
}

func calls(function map[string]any) []map[string]any {
	var out []map[string]any
	for _, node := range nodes(function["body"]) {
		if node["op"] == "let" && obj(node["value"])["kind"] == "call" {
			out = append(out, obj(node["value"]))
		}
	}
	return out
}

func ids(values any) []float64 {
	var out []float64
	for _, v := range arr(values) {
		out = append(out, v.(float64))
	}
	return out
}

func paramIDs(params any) []float64 {
	var out []float64
	for _, p := range arr(params) {
		out = append(out, obj(p)["id"].(float64))
	}
	return out
}

func provenanceOf(program map[string]any) map[string]map[string]any {
	return byName(program["provenance"])
}

// jsonEqual compares a decoded value with a JSON literal.
func jsonEqual(t *testing.T, got any, want string) {
	t.Helper()
	var expected any
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	normalized, _ := json.Marshal(got)
	var actual any
	json.Unmarshal(normalized, &actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %s\nwant %s", normalized, want)
	}
}

func hasString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func hasKeys(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

func subset(small, large []string) bool {
	for _, s := range small {
		if !hasString(large, s) {
			return false
		}
	}
	return true
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// symlinkOrSkip creates a symlink or skips where the platform or account
// cannot (Windows without developer mode or the symlink privilege).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func jsonUnmarshal(text string, v any) error { return json.Unmarshal([]byte(text), v) }

func regexpMatch(pattern, text string) bool { return regexp.MustCompile(pattern).MatchString(text) }
