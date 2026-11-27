package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Ported from scripts/test_export_module.py, the module half of
// scripts/test_generate.py and the unit half of scripts/test_multiroot.py.

// fakeLean stands in for Lean and Lake. It records every command.
type fakeLean struct {
	t        *testing.T
	prefix   string
	commands [][]string
	program  map[string]any // written as ir.json by the harness run
	build    func(command []string, workspace string)
	fail     error
}

func installFakeLean(t *testing.T) *fakeLean {
	t.Helper()
	f := &fakeLean{t: t, prefix: filepath.Join(t.TempDir(), "stdlib")}
	f.program = map[string]any{"module": "Example", "roots": []string{"identity"}, "types": []any{}, "functions": []any{},
		"claims": []any{}, "rewrites": []any{}, "provenance": []any{}, "api": map[string]any{"records": map[string]any{}}}
	oldRun, oldLook := run, lookPath
	run = f.run
	lookPath = func(name string) (string, bool) { return name, true }
	t.Cleanup(func() { run, lookPath = oldRun, oldLook })
	return f
}

func (f *fakeLean) run(command []string, workspace string, env []string) (string, error) {
	f.commands = append(f.commands, command)
	if f.fail != nil {
		return "", f.fail
	}
	for _, pair := range env {
		if strings.HasPrefix(pair, "LEAN_PATH=") || strings.HasPrefix(pair, "LAKE_HOME=") {
			f.t.Errorf("child environment keeps %s", pair)
		}
	}
	last := command[len(command)-1]
	switch {
	case last == "--version":
		os.WriteFile(filepath.Join(workspace, "lake-manifest.json"), []byte(`{"packages":[]}`), 0o644)
		c, _ := SelectedCompiler()
		return fmt.Sprintf("Lean (version %s, x86_64, commit %s, Release)", c.Version, c.Commit), nil
	case last == "--print-prefix":
		return f.prefix, nil
	case slices.Contains(command, "build"):
		if f.build != nil {
			f.build(command, workspace)
		}
		return "", nil
	default:
		data, _ := json.Marshal(f.program)
		os.WriteFile(filepath.Join(workspace, "ir.json"), data, 0o644)
		return "", nil
	}
}

type exportFixture struct {
	t      *testing.T
	dir    string
	source string
	output string
}

func newExportFixture(t *testing.T) *exportFixture {
	dir := t.TempDir()
	f := &exportFixture{t, dir, filepath.Join(dir, "source"), filepath.Join(dir, "ir.json")}
	writeText(t, filepath.Join(f.source, "Example.lean"), "def identity (x : UInt32) := x\n")
	return f
}

func (f *exportFixture) generate(roots ...string) error {
	_, err := Generate(GenerateOptions{SourceDir: f.source, Module: "Example", Roots: roots,
		Out: filepath.Join(f.dir, "package"), IROutput: f.output})
	return err
}

func TestASCIINamesOnly(t *testing.T) {
	if err := ValidateName("Example.value'"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../Example", "A; import X", "A\nB", "A..B", "A/B", `A"`, "", "Ä", "A.1"} {
		if ValidateName(name) == nil {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestSnapshotIgnoresStaleBuildProducts(t *testing.T) {
	f := newExportFixture(t)
	writeText(t, filepath.Join(f.source, ".lake", "Stale.lean"), "def stale := 0")
	writeText(t, filepath.Join(f.source, "lakefile.lean"), "unsafe script never run")
	writeText(t, filepath.Join(f.source, "Example.olean"), "stale compiled code")
	snapshot, err := SourceSnapshot(f.source)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0].Module != "Example" || snapshot[0].Relative != "Example.lean" {
		t.Fatalf("snapshot %+v", snapshot)
	}
}

func expectError(t *testing.T, err error, pattern string) {
	t.Helper()
	if err == nil || !regexp.MustCompile(pattern).MatchString(err.Error()) {
		t.Fatalf("error %v, want %q", err, pattern)
	}
}

func TestSnapshotRejectsReservedSymlinkedAndAmbiguousModules(t *testing.T) {
	f := newExportFixture(t)
	for _, name := range []string{"Lean.lean", "Std.lean", "Init.lean", "LeanGo.lean", "Bad-Name.lean"} {
		path := filepath.Join(f.source, name)
		writeText(t, path, "")
		if _, err := SourceSnapshot(f.source); err == nil {
			t.Errorf("%s accepted", name)
		}
		os.Remove(path)
	}
	link := filepath.Join(f.source, "Link.lean")
	symlinkOrSkip(t, filepath.Join(f.source, "Example.lean"), link)
	_, err := SourceSnapshot(f.source)
	expectError(t, err, "regular")
	os.Remove(link)
	link = filepath.Join(f.source, "linked")
	symlinkOrSkip(t, f.source, link)
	_, err = SourceSnapshot(f.source)
	expectError(t, err, "symlink")
	os.Remove(link)
	writeText(t, filepath.Join(f.source, "A", "B.lean"), "")
	writeText(t, filepath.Join(f.source, "A.B.lean"), "")
	_, err = SourceSnapshot(f.source)
	expectError(t, err, "ambiguous")
}

func TestSnapshotResourceLimits(t *testing.T) {
	f := newExportFixture(t)
	defer func(files, size int) { maxFiles, maxSourceBytes = files, size }(maxFiles, maxSourceBytes)
	maxFiles = 0
	_, err := SourceSnapshot(f.source)
	expectError(t, err, "limit")
	maxFiles, maxSourceBytes = MaxFiles, 1
	_, err = SourceSnapshot(f.source)
	expectError(t, err, "limit")
}

func TestInstrumentationPreservesValidHeaders(t *testing.T) {
	for _, c := range []struct {
		source  string
		imports []string
		modern  bool
	}{
		{"/- license /- nested -/ -/\nimport Std\n-- explanation\ndef f := 1\n", []string{"Std"}, false},
		{"module\nprelude\npublic meta import Lean\npublic import Std\npublic def f := 1\n", []string{"Lean", "Std"}, true},
		{"-- license\ndef f := 1\n", nil, false},
	} {
		mapping, imports, err := instrumentSource([]byte(c.source))
		if err != nil || !slices.Equal(imports, c.imports) {
			t.Fatalf("%q: imports %v, err %v", c.source, imports, err)
		}
		text := string(mapping.Instrumented)
		expected := "import LeanGo.Module.Audit"
		if c.modern {
			expected = "public meta import LeanGo.Module.Audit"
		}
		at := strings.Index(text, expected)
		if at < 0 || at > strings.Index(text, "def f") {
			t.Fatalf("%q: audit import missing or late:\n%s", c.source, text)
		}
		if len(c.imports) > 0 && strings.Index(text, "import "+c.imports[len(c.imports)-1]) > at {
			t.Fatalf("%q: audit import precedes the user imports", c.source)
		}
		if c.modern && !strings.HasPrefix(text, "module\nprelude\n") {
			t.Fatalf("%q: modern header moved", c.source)
		}
	}
	_, _, err := instrumentSource([]byte("/- broken"))
	expectError(t, err, "unterminated")
}

func TestHeaderScanStopsAtFirstSourceCommand(t *testing.T) {
	body := "def run := \"body\"\n/- body parsing belongs to Lean"
	mapping, imports, err := instrumentSource([]byte(body))
	if err != nil || len(imports) != 0 || !strings.HasSuffix(string(mapping.Instrumented), body) {
		t.Fatalf("%v %v %q", imports, err, mapping.Instrumented)
	}
}

func TestFreshnessTracksOriginalSourceAndTrustedProducer(t *testing.T) {
	inventory := ProducerInventory()
	for _, name := range []string{"Records", "Reduced", "Local", "LocalSource", "LocalObserve", "Primitives", "Export", "Audit", "Diagnostics"} {
		if _, ok := inventory["producer/LeanGo/Module/"+name+".lean"]; !ok {
			t.Errorf("producer file %s missing from the inventory", name)
		}
	}
	f := newExportFixture(t)
	sources, err := SourceSnapshot(f.source)
	if err != nil {
		t.Fatal(err)
	}
	program := NewObject()
	inputs := NewObject()
	for _, key := range sortedKeys(InputInventory(sources)) {
		inputs.Set(key, InputInventory(sources)[key])
		if filepath.IsAbs(key) {
			t.Fatalf("absolute inventory key %s", key)
		}
	}
	program.Set("inputs", inputs)
	if _, ok := inputs.Values["source/Example.lean"]; !ok {
		t.Fatal("source digest missing")
	}
	if err := VerifyInputs(f.source, program); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.source, "Example.lean")
	old := readText(t, path)
	writeText(t, path, old+"changed")
	expectError(t, VerifyInputs(f.source, program), "changed")
	writeText(t, path, old)
	writeText(t, filepath.Join(f.source, "Added.lean"), "")
	expectError(t, VerifyInputs(f.source, program), "changed")
	inputs.Set(ToolInput, strings.Repeat("0", 64))
	os.Remove(filepath.Join(f.source, "Added.lean"))
	expectError(t, VerifyInputs(f.source, program), "changed")
}

func TestFreshIsolatedBuildAndMetadata(t *testing.T) {
	fake := installFakeLean(t)
	fake.build = func(command []string, workspace string) {
		if !slices.Equal(command[1:], []string{"build", "LeanGo.Module.Export", "Example"}) {
			t.Errorf("build command %v", command)
		}
		text := readText(t, filepath.Join(workspace, "Example.lean"))
		if !strings.Contains(text, "import LeanGo.Module.Audit") || !strings.Contains(text, "set_option compiler.postponeCompile false") {
			t.Errorf("instrumentation missing:\n%s", text)
		}
		if _, err := os.Stat(filepath.Join(workspace, "LeanGo", "Module", "Export.lean")); err != nil {
			t.Error("producer sources were not staged")
		}
	}
	f := newExportFixture(t)
	t.Setenv("LEAN_PATH", "/poison")
	t.Setenv("LAKE_HOME", "/poison")
	result, err := ExportModule(ExportOptions{SourceDir: f.source, Module: "Example", Roots: []string{"identity"}})
	if err != nil {
		t.Fatal(err)
	}
	var program map[string]any
	json.Unmarshal(result.IR, &program)
	if program["schema"] != "lean-go-ir/v24" || program["profile"] != "pure-module/v22" || len(arr(program["features"])) != 0 {
		t.Fatalf("identity %v %v %v", program["schema"], program["profile"], program["features"])
	}
	jsonEqual(t, program["api"], `{"records":{}}`)
	jsonEqual(t, program["roots"], `["identity"]`)
	if _, ok := program["root"]; ok {
		t.Fatal("legacy root field present")
	}
	inputs := obj(program["inputs"])
	if inputs["source/Example.lean"] != Digest([]byte("def identity (x : UInt32) := x\n")) {
		t.Fatal("source digest")
	}
	if _, ok := inputs["producer/LeanGo/Module/Primitives.lean"]; !ok {
		t.Fatal("producer digest missing")
	}
	// The harness is the last command and imports the module.
	last := fake.commands[len(fake.commands)-1]
	if !slices.Equal(last[1:], []string{"env", "lean", "LeanGo/Module/Harness.lean"}) {
		t.Fatalf("harness command %v", last)
	}
}

func TestMissingImportFailsBeforeBuildAndPreservesOutput(t *testing.T) {
	fake := installFakeLean(t)
	f := newExportFixture(t)
	writeText(t, filepath.Join(f.source, "Example.lean"), "import Missing\ndef identity := 1\n")
	writeText(t, f.output, "original")
	expectError(t, f.generate("identity"), "missing local import")
	if len(fake.commands) != 2 {
		t.Fatalf("commands before the import check: %v", fake.commands)
	}
	if readText(t, f.output) != "original" {
		t.Fatal("output changed")
	}
}

func TestFailedProcessAndSourceConflictPreserveOutputs(t *testing.T) {
	fake := installFakeLean(t)
	fake.fail = exportError("failed")
	f := newExportFixture(t)
	writeText(t, f.output, "original")
	expectError(t, f.generate("identity"), "failed")
	if readText(t, f.output) != "original" {
		t.Fatal("output changed")
	}
	fake.commands = nil
	f.output = filepath.Join(f.source, "Example.lean")
	expectError(t, f.generate("identity"), "conflicts")
	if len(fake.commands) != 0 {
		t.Fatal("processes ran before the output conflict check")
	}
}

// scripts/test_generate.py (module paths).

func TestOverlappingOutputPathsAreRejected(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	for _, c := range [][2]string{{pkg, filepath.Join(pkg, "control.go")}, {pkg, pkg}, {filepath.Join(pkg, "nested"), pkg},
		{pkg, filepath.Join(pkg, "unused", "..", "control.go")}} {
		expectError(t, validateOutputPaths(c[0], c[1]), "overlap")
	}
	if err := validateOutputPaths(pkg, filepath.Join(root, "program.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCollisionFailsBeforeExternalProcesses(t *testing.T) {
	fake := installFakeLean(t)
	f := newExportFixture(t)
	pkg := filepath.Join(f.dir, "package")
	writeText(t, filepath.Join(pkg, "control.go"), "original generated source")
	writeText(t, filepath.Join(pkg, "manifest.json"), "original manifest")
	_, err := Generate(GenerateOptions{SourceDir: f.source, Module: "Example", Roots: []string{"identity"}, Out: pkg, IROutput: filepath.Join(pkg, "control.go")})
	expectError(t, err, "overlap")
	if len(fake.commands) != 0 || readText(t, filepath.Join(pkg, "control.go")) != "original generated source" {
		t.Fatal("collision ran processes or changed output")
	}
}

func TestPartialModuleArgumentsAndExplicitOutputs(t *testing.T) {
	fake := installFakeLean(t)
	for _, opts := range []GenerateOptions{{Module: "Example"}, {Roots: []string{"Example.run"}}, {SourceDir: "/tmp/unused", Module: "Example"}} {
		_, err := Generate(opts)
		expectError(t, err, "together")
	}
	_, err := Generate(GenerateOptions{SourceDir: "/tmp/unused", Module: "Example", Roots: []string{"Example.run"}})
	expectError(t, err, "explicit")
	if len(fake.commands) != 0 {
		t.Fatal("processes ran")
	}
}

func TestModuleOutputSourceConflictsFailBeforeProcesses(t *testing.T) {
	fake := installFakeLean(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	lean := filepath.Join(source, "Example.lean")
	writeText(t, lean, "def identity (x : UInt32) := x")
	for _, c := range [][2]string{{filepath.Join(root, "package"), lean}, {source, filepath.Join(root, "ir.json")},
		{filepath.Join(root, "package"), filepath.Join(source, "lakefile.toml")}} {
		_, err := Generate(GenerateOptions{SourceDir: source, Module: "Example", Roots: []string{"identity"}, Out: c[0], IROutput: c[1]})
		expectError(t, err, "overlap|conflicts")
	}
	if len(fake.commands) != 0 || readText(t, lean) != "def identity (x : UInt32) := x" {
		t.Fatal("conflict ran processes or changed source")
	}
}

func TestStaleIRSchemasAreRejectedByTheEmitter(t *testing.T) {
	for version := 1; version < 24; version++ {
		_, err := Emit([]byte(fmt.Sprintf(`{"schema":"lean-go-ir/v%d"}`, version)), EmitOptions{})
		expectError(t, err, "unsupported IR schema.*regenerate")
	}
}

func TestOtherGoToolchainWarnsUnlessStrict(t *testing.T) {
	fake := installFakeLean(t)
	defer func(old func() string) { goVersion = old }(goVersion)
	goVersion = func() string { return "go1.99.0" }
	if !strings.Contains(GoWarning(), "go1.99.0") || !strings.Contains(GoWarning(), RequiredGo) {
		t.Fatalf("warning %q must name both versions", GoWarning())
	}
	f := newExportFixture(t)
	_, err := Generate(GenerateOptions{SourceDir: f.source, Module: "Example", Roots: []string{"identity"},
		Out: filepath.Join(f.dir, "package"), IROutput: f.output, StrictGo: true})
	var pe *Error
	if !errors.As(err, &pe) || pe.Diagnostic().Values["code"] != "toolchain_mismatch" || len(fake.commands) != 0 {
		t.Fatalf("strict mode: %v after %v", err, fake.commands)
	}
	// Without --strict-go generation proceeds to the producer (here the fake
	// producer's empty program then fails validation in the emitter).
	err = f.generate("identity")
	if len(fake.commands) == 0 || err == nil || !strings.Contains(err.Error(), "lean-go:") {
		t.Fatalf("non-strict mode did not continue: %v", err)
	}
	goVersion = func() string { return RequiredGo }
	if GoWarning() != "" {
		t.Fatal("the pinned toolchain warns")
	}
}

// scripts/test_multiroot.py (RootSelectionTests).

func TestRootSelectionIsCanonical(t *testing.T) {
	roots, err := SelectRoots([]string{"Example.run"})
	if err != nil || !slices.Equal(roots, []string{"Example.run"}) {
		t.Fatal(roots, err)
	}
	roots, _ = SelectRoots([]string{"Example.z", "Example.a"})
	if !slices.Equal(roots, []string{"Example.a", "Example.z"}) {
		t.Fatal(roots)
	}
	var many []string
	for i := 0; i < 64; i++ {
		many = append(many, fmt.Sprintf("Example.root%d", i))
	}
	roots, err = SelectRoots(many)
	if err != nil || !slices.IsSorted(roots) || len(roots) != 64 {
		t.Fatal(err)
	}
}

func TestMalformedAndDuplicateRootsFailBeforeProcesses(t *testing.T) {
	fake := installFakeLean(t)
	var tooMany []string
	for i := 0; i < 65; i++ {
		tooMany = append(tooMany, fmt.Sprintf("A.root%d", i))
	}
	for _, selection := range [][]string{nil, {}, {"A", "A"}, {"A", "A; import Lean"}, {"A", "A.λ"}, tooMany} {
		if _, err := ExportModule(ExportOptions{SourceDir: "/unused", Module: "Example", Roots: selection}); err == nil {
			t.Errorf("%v accepted", selection)
		}
	}
	if len(fake.commands) != 0 {
		t.Fatal("processes ran")
	}
}

func TestProducerIdentityAndExactSelectionFields(t *testing.T) {
	compiler, _ := SelectedCompiler()
	common := `"module":"Example","types":[],"functions":[],"claims":[],"rewrites":[],"provenance":[],"api":{"records":{}}`
	for _, roots := range [][]string{{"Example.a"}, {"Example.a", "Example.b"}} {
		raw, _ := json.Marshal(roots)
		program, err := completeProgram([]byte(`{`+common+`,"roots":`+string(raw)+`}`), "Example", roots, map[string]string{}, compiler)
		if err != nil {
			t.Fatal(err)
		}
		if program.Values["schema"] != "lean-go-ir/v24" || program.Values["profile"] != "pure-module/v22" {
			t.Fatal("identity")
		}
		if _, ok := program.Values["root"]; ok {
			t.Fatal("root field")
		}
	}
	roots := []string{"Example.a", "Example.b"}
	for _, changed := range []string{`"roots":["Example.b","Example.a"]`, `"roots":["Example.a"]`,
		`"roots":["Example.a","Example.b"],"root":"Example.a"`, `"root":"Example.a"`} {
		if _, err := completeProgram([]byte(`{`+common+`,`+changed+`}`), "Example", roots, map[string]string{}, compiler); err == nil {
			t.Errorf("%s accepted", changed)
		}
	}
	other := strings.Replace(common, `"module":"Example"`, `"module":"Other"`, 1)
	if _, err := completeProgram([]byte(`{`+other+`,"roots":["Example.a","Example.b"]}`), "Example", roots, map[string]string{}, compiler); err == nil {
		t.Error("other module accepted")
	}
}

func TestHarnessReceivesCanonicalRoots(t *testing.T) {
	fake := installFakeLean(t)
	fake.program["roots"] = []string{"Example.a", "Example.z"}
	f := newExportFixture(t)
	var harness string
	opts := ExportOptions{SourceDir: f.source, Module: "Example", Roots: []string{"Example.z", "Example.a"}}
	opts.harness = func(module string, roots []string) string {
		harness = exportHarness(module, roots)
		return harness
	}
	if _, err := ExportModule(opts); err != nil {
		t.Fatal(err)
	}
	if harness != "import LeanGo.Module.Export\nimport Example\n#lean_go_export_roots \"ir.json\" [Example.a, Example.z] \"Example\"\n" {
		t.Fatalf("harness %q", harness)
	}
	if exportHarness("Example", []string{"Example.a"}) != "import LeanGo.Module.Export\nimport Example\n#lean_go_export \"ir.json\" Example.a \"Example\"\n" {
		t.Fatal("single-root harness")
	}
}

// scripts/test_native_mode.py: the mode reaches the emitter.

func TestEmitModes(t *testing.T) {
	data, err := os.ReadFile("../../testdata/trees.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	native, err := Emit(data, EmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	metered, err := Emit(data, EmitOptions{Mode: "metered"})
	if err != nil {
		t.Fatal(err)
	}
	if string(metered["api.go"]) == string(native[firstKey(native)]) && len(metered) == len(native) {
		t.Fatal("metered mode produced the native package")
	}
	explicit, _ := Emit(data, EmitOptions{Mode: "native"})
	if string(explicit["functions.go"]) != string(native["functions.go"]) || !strings.Contains(string(native["manifest.json"]), `"mode": "native"`) {
		t.Fatal("native is not the default")
	}
	if _, err := Emit(data, EmitOptions{Mode: "fast"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, err := Emit(data, EmitOptions{Mode: "metered", NativeMaxDepth: 5}); err == nil {
		t.Fatal("max depth without native mode accepted")
	}
	if _, err := Emit(data, EmitOptions{NativeMaxDepth: 5}); err != nil {
		t.Fatalf("max depth with the default native mode: %v", err)
	}
}

func firstKey(m map[string][]byte) string {
	keys := keysOf(m)
	slices.Sort(keys)
	return keys[0]
}
