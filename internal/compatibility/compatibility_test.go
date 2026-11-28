package compatibility

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
	"github.com/franchb/lean-go/producer"
)

var (
	base = Identity{Version: Pinned.Version, Commit: Pinned.Commit}
	next = Identity{Version: "4.99.0-rc1", Commit: "1234567890abcdef1234567890abcdef12345678"}
)

func root(t *testing.T) string {
	t.Helper()
	r, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func version(value Identity) string {
	return fmt.Sprintf("Lean (version %s, x86_64-unknown-linux-gnu, commit %s, Release)\n", value.Version, value.Commit)
}

type failureOptions struct {
	code, stage, stdout string
	returncode          int
	declaration         *string
}

func failure(o failureOptions) CommandResult {
	if o.code == "" {
		o.code = "unsupported_type"
	}
	if o.stage == "" {
		o.stage = "admission"
	}
	if o.returncode == 0 {
		o.returncode = 1
	}
	var declaration any
	if o.declaration != nil {
		declaration = *o.declaration
	}
	report := map[string]any{"schema": "lean-go-diagnostics/v1", "success": false, "diagnostics": []any{
		map[string]any{"code": o.code, "stage": o.stage, "message": "fixture", "declaration": declaration,
			"compiler_form": nil, "location": nil, "hint": nil}}}
	data, _ := json.Marshal(report)
	return CommandResult{Code: o.returncode, Stdout: o.stdout, Stderr: string(data)}
}

func ptr(s string) *string { return &s }

func parse(t *testing.T, text string) any {
	t.Helper()
	v, err := pipeline.ParseJSON([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// ---- IdentityTests ----

func TestReleaseIdentitiesAndLakeMatch(t *testing.T) {
	for _, value := range []Identity{base, next} {
		got, err := ParseIdentity(version(value))
		if err != nil || !reflect.DeepEqual(got, value) {
			t.Fatalf("%v %v", got, err)
		}
		if got, err := MatchingIdentity(version(value), version(value)); err != nil || !reflect.DeepEqual(got, value) {
			t.Fatalf("%v %v", got, err)
		}
	}
	if _, err := MatchingIdentity(version(base), version(next)); err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Fatalf("got %v", err)
	}
}

func TestIncompleteOrAmbiguousIdentitiesFail(t *testing.T) {
	v := version(base)
	for _, text := range []string{"", "Lean version 4.35.0", strings.Replace(v, base.Commit, "eb6cafb", 1),
		v + v, strings.Replace(v, "Release", "Debug", 1), strings.Replace(v, base.Version, base.Version+";echo", 1)} {
		if _, err := ParseIdentity(text); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
}

func TestNoShimOrNonexecutableSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	bin := filepath.Join(t.TempDir(), "bin")
	os.Mkdir(bin, 0o755)
	for _, name := range []string{"lean", "lake"} {
		os.WriteFile(filepath.Join(bin, name), []byte("not a toolchain"), 0o644)
	}
	if _, err := CompilerBin(bin); err == nil {
		t.Fatal("nonexecutable tools accepted")
	}
	for _, name := range []string{"lean", "lake"} {
		os.Chmod(filepath.Join(bin, name), 0o755)
	}
	if _, err := CompilerBin(bin); err == nil || !strings.Contains(err.Error(), "installed Lean library") {
		t.Fatalf("got %v", err)
	}
	// A symlinked tool (an elan shim) is not an installation either.
	os.MkdirAll(filepath.Join(filepath.Dir(bin), "lib", "lean"), 0o755)
	for _, name := range []string{"Init.olean", "Lean.olean"} {
		os.WriteFile(filepath.Join(filepath.Dir(bin), "lib", "lean", name), nil, 0o644)
	}
	if got, err := CompilerBin(filepath.Dir(bin)); err != nil || got != bin {
		t.Fatalf("installation directory: %q %v", got, err)
	}
	os.Remove(filepath.Join(bin, "lake"))
	os.Symlink(filepath.Join(bin, "lean"), filepath.Join(bin, "lake"))
	if _, err := CompilerBin(bin); err == nil {
		t.Fatal("symlinked lake accepted")
	}
}

func snapshot(t *testing.T) Snapshot {
	t.Helper()
	s, err := SourceSnapshot(root(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The snapshot is the lean-go build closure: no tests, scripts or harness.
func TestSnapshotIsTheLeanGoBuildClosure(t *testing.T) {
	s := snapshot(t)
	for _, name := range []string{"go.mod", "cmd/lean-go/main.go", "internal/leanversion/identity.go", "internal/pipeline/cache_other.go",
		"producer/lakefile.toml", "producer/lean-toolchain", "producer/LeanGo/Module/Export.lean", "internal/modulegen/templates/api.go.tmpl"} {
		if _, ok := s[name]; !ok {
			t.Errorf("snapshot lacks %s", name)
		}
	}
	for name := range s {
		if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "internal/compatibility/") ||
			strings.HasPrefix(name, "cmd/lean-go-dev/") {
			t.Errorf("snapshot contains %s", name)
		}
	}
	inputs, err := SnapshotInputs(root(t), s)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cmd/lean-go-dev/main.go", "internal/compatibility/runner.go", "compatibility/corpus/manifest.json",
		"compatibility/reproducers/NumericBoundaryName.lean", "compatibility/corpus/uint16_xor/native/lakefile.toml"} {
		if _, ok := inputs[name]; !ok {
			t.Errorf("retained inputs lack %s", name)
		}
	}
}

func TestBoundedAdaptationAndSamePinIsEmpty(t *testing.T) {
	s := snapshot(t)
	unchanged := s.clone()
	for _, c := range pipeline.SupportedCompilers {
		adapted, evidence, patch, err := AdaptPins(s, Identity{Version: c.Version, Commit: c.Commit})
		if err != nil || !reflect.DeepEqual(adapted, s) || len(evidence.Files) != 0 || patch != "" {
			t.Fatalf("%v: adapted differs, %v, %q, %v", c, evidence.Files, patch, err)
		}
	}
	adapted, evidence, patch, err := AdaptPins(s, next)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, unchanged) {
		t.Fatal("checkout bytes must never be mutated")
	}
	substitutions, _ := PinSubstitutions(next)
	var paths []string
	for _, f := range evidence.Files {
		paths = append(paths, f.Path)
		if f.BeforeSHA256 != Digest(s[f.Path]) || f.AfterSHA256 != Digest(adapted[f.Path]) {
			t.Errorf("%s digests", f.Path)
		}
	}
	if !reflect.DeepEqual(paths, sortedKeys(substitutions)) || len(evidence.SemanticAdaptations) != 0 || evidence.PatchSHA256 != Digest([]byte(patch)) {
		t.Fatalf("evidence %+v", evidence)
	}
	for name, data := range s {
		if strings.HasSuffix(name, ".lean") && string(adapted[name]) != string(data) {
			t.Errorf("no Lean semantic adaptation is authorized: %s", name)
		}
	}
	identity := string(adapted["internal/leanversion/identity.go"])
	if !strings.Contains(identity, `Version = "4.99.0-rc1"`) || !strings.Contains(identity, next.Commit) {
		t.Errorf("identity table not adapted:\n%s", identity)
	}
	if string(adapted["producer/lean-toolchain"]) != "leanprover/lean4:v4.99.0-rc1\n" {
		t.Errorf("toolchain %q", adapted["producer/lean-toolchain"])
	}
	if !strings.Contains(patch, "--- a/internal/leanversion/identity.go\n+++ b/internal/leanversion/identity.go\n") ||
		!strings.Contains(patch, "+leanprover/lean4:v4.99.0-rc1\n") {
		t.Errorf("patch:\n%s", patch)
	}
}

func TestMissingDuplicateOrChangedAnchorFailsBeforeWrite(t *testing.T) {
	original := snapshot(t)
	for _, value := range [][]byte{{}, append(append([]byte{}, original["producer/lean-toolchain"]...), original["producer/lean-toolchain"]...),
		[]byte("leanprover/lean4:v4.0.0\n")} {
		changed := original.clone()
		changed["producer/lean-toolchain"] = value
		if _, _, _, err := AdaptPins(changed, next); err == nil {
			t.Errorf("anchor %q accepted", value)
		}
	}
	missing := original.clone()
	delete(missing, "internal/leanversion/identity.go")
	if _, _, _, err := AdaptPins(missing, next); err == nil {
		t.Error("missing identity table accepted")
	}
}

func TestProductionPinStaysStrict(t *testing.T) {
	if err := Pinned.CheckVersion(version(base)); err != nil {
		t.Fatal(err)
	}
	if err := Pinned.CheckVersion(version(next)); err == nil {
		t.Fatal("research compiler accepted by the production pin")
	}
	data, err := os.ReadFile(filepath.Join(root(t), "producer", "lean-toolchain"))
	if err != nil || string(data) != string(Pinned.PinFile()) {
		t.Fatalf("pin file %q %v", data, err)
	}
	if embedded, _ := producer.Read(producer.ToolchainFile); string(embedded) != string(data) {
		t.Fatal("embedded pin differs")
	}
}

// ---- ProtocolTests ----

func status(o Outcome) string { s, _ := o["status"].(string); return s }

func TestOnlyExpectedAdmissionCodeMatches(t *testing.T) {
	if s := status(ExtractionOutcome(failure(failureOptions{}), ptr("unsupported_type"), nil)); s != "expected_rejection" {
		t.Error(s)
	}
	if s := status(ExtractionOutcome(failure(failureOptions{}), ptr("source_audit"), nil)); s != "admission_mismatch" {
		t.Error(s)
	}
	if s := status(ExtractionOutcome(failure(failureOptions{}), nil, nil)); s != "admission_mismatch" {
		t.Error(s)
	}
	for _, stage := range []string{"toolchain", "build", "emission"} {
		if s := status(ExtractionOutcome(failure(failureOptions{stage: stage}), ptr("unsupported_type"), nil)); s != "extraction_failed" {
			t.Errorf("%s: %s", stage, s)
		}
	}
}

func manifestCases(t *testing.T) []Case {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root(t), "compatibility", "corpus", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := ValidateCases(parse(t, string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestPointerEqualityRejectionNamesTheIntrinsic(t *testing.T) {
	var c Case
	for _, item := range manifestCases(t) {
		if item.Name == "with_ptr_eq" {
			c = item
		}
	}
	for declaration, want := range map[string]string{"withPtrEq": "expected_rejection", "run": "admission_mismatch"} {
		rejected := failure(failureOptions{code: "source_audit", declaration: ptr(declaration)})
		if s := status(ExtractionOutcome(rejected, c.Reject, c.RejectDeclaration)); s != want {
			t.Errorf("%s: %s, want %s", declaration, s, want)
		}
	}
}

func TestCrashesNoiseAndMalformedJSONAreNeverRejections(t *testing.T) {
	valid := failure(failureOptions{}).Stderr
	results := []CommandResult{failure(failureOptions{returncode: -9}), failure(failureOptions{returncode: 2}),
		failure(failureOptions{stdout: "noise"}), failure(failureOptions{code: "invented"}),
		{Code: 1, Stderr: "Traceback"}, {Code: 1, Stderr: "{}"},
		{Code: 1, Stderr: strings.Replace(valid, `"success":false`, `"success":0`, 1)},
		{Code: 1, Stderr: strings.Replace(valid, `"success":false`, `"success":true,"success":false`, 1)},
		{Code: -1, Stdout: "before", Stderr: "timeout"}}
	for i, result := range results {
		if s := status(ExtractionOutcome(result, ptr("unsupported_type"), nil)); s != "extraction_protocol_failed" {
			t.Errorf("case %d: %s", i, s)
		}
	}
	if s := status(ExtractionOutcome(CommandResult{}, ptr("source_audit"), nil)); s != "unexpected_acceptance" {
		t.Error(s)
	}
	if s := status(ExtractionOutcome(CommandResult{}, nil, nil)); s != "extracted" {
		t.Error(s)
	}
}

// The fixture codes must stay the pipeline's admission codes: ValidReport
// accepts an admission diagnostic exactly when its code is one of them.
func TestAdmissionCodesMatchThePipeline(t *testing.T) {
	candidates := append([]string{"invented", "producer_failed", "invalid_input", "toolchain_mismatch", "unsupported"}, AdmissionCodes...)
	for _, code := range candidates {
		valid := pipeline.ValidReport([]byte(failure(failureOptions{code: code}).Stderr))
		if valid != isAdmissionCode(code) {
			t.Errorf("%s: pipeline admits %v, harness %v", code, valid, isAdmissionCode(code))
		}
	}
}

func TestCanonicalIRExcludesOnlyTopLevelIdentity(t *testing.T) {
	text := `{"inputs":{"source":"hash"},"lean_version":"4.35.0","lean_commit":"abc","functions":[],"provenance":[{"inputs":"keep"}],"claims":[],"rewrites":[]}`
	canonical, err := CanonicalIR(parse(t, text))
	if err != nil || !strings.Contains(canonical, "keep") {
		t.Fatalf("%q %v", canonical, err)
	}
	other, _ := CanonicalIR(parse(t, `{"inputs":{},"lean_version":"next","lean_commit":"def","functions":[],"provenance":[{"inputs":"keep"}],"claims":[],"rewrites":[]}`))
	if other != canonical {
		t.Error("identity fields changed the canonical IR")
	}
	for _, key := range []string{"functions", "provenance", "claims", "rewrites"} {
		o := parse(t, text).(*pipeline.Object)
		o.Set(key, []any{"different"})
		if changed, _ := CanonicalIR(o); changed == canonical {
			t.Errorf("%s is not part of the canonical IR", key)
		}
	}
	if _, err := CanonicalIR(parse(t, `{}`)); err == nil {
		t.Error("empty IR accepted")
	}
	// Sorted keys, indented by two spaces.
	if want := "{\n  \"claims\": [],\n  \"functions\": [],\n"; !strings.HasPrefix(canonical, want) {
		t.Errorf("layout %q", canonical)
	}
}

func TestShapeContractRequiresCalledSurvivor(t *testing.T) {
	expected := map[string][]string{"called_function_suffixes": {"._redArg"}, "ops": {"join"}}
	program := parse(t, `{"functions":[{"name":"A.run._redArg","body":{"op":"return"}}]}`)
	if missing, _ := IRShapes(program, expected); len(missing) != 2 {
		t.Fatalf("missing %v", missing)
	}
	program = parse(t, `{"functions":[{"name":"A.run._redArg","body":{"op":"return"}},{"name":"A.run","body":{"op":"join","value":{"kind":"call","name":"A.run._redArg"}}}]}`)
	if missing, _ := IRShapes(program, expected); len(missing) != 0 {
		t.Fatalf("missing %v", missing)
	}
}

func TestIRDriftAndCandidateFailuresAreDifferences(t *testing.T) {
	baseline := map[string]Outcome{"a": {"status": "accepted", "canonical_ir_sha256": "a"}}
	for _, c := range []struct {
		after Outcome
		kind  string
	}{{Outcome{"status": "accepted", "canonical_ir_sha256": "b"}, "canonical_ir_drift"},
		{Outcome{"status": "behavior_mismatch"}, "behavior_mismatch"}, {Outcome{"status": "not_run"}, "not_run"},
		{Outcome{"status": "expected_rejection"}, "outcome_changed"}} {
		got := CompareCases([]string{"a"}, baseline, map[string]Outcome{"a": c.after})
		if !reflect.DeepEqual(got, []Difference{{"a", c.kind}}) {
			t.Errorf("got %v, want %s", got, c.kind)
		}
	}
	if got := CompareCases([]string{"a"}, baseline, baseline); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestMalformedManifestIsPreconditionFailure(t *testing.T) {
	valid := `"name":"example","module":"Example","roots":["run"]`
	for _, mutation := range []string{`"reject":[]`, `"reject":"producer_failed"`, `"roots":"run"`, `"roots":[]`,
		`"source_versions":["../outside"]`, `"source_versions":"4.36.0"`, `"source_versions":["4.36.0","4.36.0"]`,
		`"source_versions":[]`, `"shapes":[]`, `"shapes":{"ops":"join"}`, `"shapes":{"other":[]}`, `"name":"../outside"`,
		`"name":"Example"`, `"module":"../Example"`, `"reject_declaration":"run"`, `"unexpected":1`} {
		text := "[{" + valid + "," + mutation + "}]"
		// Later duplicate keys are rejected by the strict parser; build the object by replacement instead.
		o := parse(t, "{"+valid+"}").(*pipeline.Object)
		m := parse(t, "{"+mutation+"}").(*pipeline.Object)
		for _, k := range m.Keys {
			o.Set(k, m.Values[k])
		}
		if _, err := ValidateCases([]any{o}); err == nil {
			t.Errorf("%s accepted", text)
		}
	}
	cases, err := ValidateCases(parse(t, "[{"+valid+"}]"))
	if err != nil || len(cases) != 1 || cases[0].Name != "example" || cases[0].Module != "Example" || !reflect.DeepEqual(cases[0].Roots, []string{"run"}) {
		t.Fatalf("%v %v", cases, err)
	}
	if _, err := ValidateCases(parse(t, "[]")); err == nil {
		t.Error("empty manifest accepted")
	}
	if _, err := ValidateCases(parse(t, "[{"+valid+"},{"+valid+"}]")); err == nil {
		t.Error("duplicate names accepted")
	}
	if got := manifestCases(t); len(got) != 24 {
		t.Errorf("tracked manifest has %d cases", len(got))
	}
}

// ---- RunnerTests ----

func TestReusedOverlappingAndSymlinkOutputsAreRejected(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	os.Mkdir(repo, 0o755)
	for _, path := range []string{repo, filepath.Join(repo, "output"), parent} {
		if _, err := ValidateOutput(path, repo, nil); err == nil {
			t.Errorf("%s accepted", path)
		}
	}
	install := filepath.Join(parent, "lean")
	if _, err := ValidateOutput(filepath.Join(install, "out"), repo, []string{filepath.Join(install, "bin")}); err == nil {
		t.Error("output inside a compiler installation accepted")
	}
	// A new path below a plain directory is accepted as given. On Windows
	// runners the temporary directory is spelled with an 8.3 short name
	// (RUNNER~1), so it differs from its resolved form without any link.
	fresh := filepath.Join(parent, "fresh")
	if got, err := ValidateOutput(fresh, repo, nil); err != nil || got != fresh {
		t.Fatalf("%q %v", got, err)
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := ValidateOutput(filepath.Join(alias, "new"), repo, nil); err == nil {
		t.Error("symlinked output accepted")
	}
	// A link to a directory outside the checkout is still a link.
	elsewhere := filepath.Join(parent, "elsewhere")
	if err := os.Mkdir(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOutput(filepath.Join(link, "new"), repo, nil); err == nil {
		t.Error("output below a link to an unprotected directory accepted")
	}
}

type recorded struct {
	stage string
	argv  []string
	cwd   string
	env   map[string]string
}

// scripted answers commands in order and records them.
func scripted(t *testing.T, results ...CommandResult) (*Commands, *[]recorded) {
	t.Helper()
	calls := &[]recorded{}
	c := &Commands{Timeout: time.Second}
	c.Run = func(stage string, argv []string, cwd string, env map[string]string, logs string) CommandResult {
		*calls = append(*calls, recorded{stage, argv, cwd, env})
		if len(results) == 0 {
			t.Fatalf("unexpected command %s %v", stage, argv)
		}
		r := results[0]
		results = results[1:]
		return r
	}
	return c, calls
}

func TestVersionedCallerMustCompileBeforeAdmission(t *testing.T) {
	c := Case{Name: "with_ptr_eq", Module: "Example", Roots: []string{"run"}, Reject: ptr("source_audit"), SourceVersions: []string{"4.36.0-rc2"}}
	compiled, failed := CommandResult{}, CommandResult{Code: 1, Stderr: "ill-typed caller"}
	for _, item := range []struct {
		version, variant string
		control          CommandResult
	}{{"4.36.0", "source", compiled}, {"4.36.0-rc2", "4.36.0-rc2", compiled}, {"4.36.0", "source", failed}} {
		role := filepath.Join(t.TempDir(), "run")
		commands, calls := scripted(t, item.control, failure(failureOptions{code: "source_audit"}))
		r := &Runner{Commands: commands}
		result, err := r.RunCase(c, filepath.Join(root(t), "compatibility", "corpus"), root(t), role,
			Identity{Version: item.version, Bin: "/explicit/bin"}, "lean-go", "go", map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(role, "cases", "with_ptr_eq")
		copied, _ := os.ReadFile(filepath.Join(directory, "source", "Example.lean"))
		fixture, _ := os.ReadFile(filepath.Join(root(t), "compatibility", "corpus", "with_ptr_eq", item.variant, "Example.lean"))
		if string(copied) != string(fixture) || len(copied) == 0 {
			t.Errorf("%s: wrong fixture source copied", item.version)
		}
		first := (*calls)[0]
		if first.stage != "lean-typecheck" || !reflect.DeepEqual(first.argv, []string{filepath.Join("/explicit/bin", "lean"), "Example.lean"}) ||
			first.cwd != filepath.Join(directory, "source") {
			t.Errorf("control %+v", first)
		}
		if result["fixture_source"] != item.variant {
			t.Errorf("fixture_source %v", result["fixture_source"])
		}
		if item.control.Code != 0 {
			if status(result) != "source_typecheck_failed" || len(*calls) != 1 {
				t.Errorf("%v after %d calls", result, len(*calls))
			}
		} else if status(result) != "expected_rejection" || result["source_typecheck"] != "passed" {
			t.Errorf("result %v", result)
		}
		var stored map[string]any
		data, _ := os.ReadFile(filepath.Join(directory, "result.json"))
		if json.Unmarshal(data, &stored) != nil || stored["status"] != result["status"] {
			t.Errorf("result.json %s", data)
		}
	}
}

func TestCommandTimeoutIsRecordedAndNotAdmission(t *testing.T) {
	saved := process
	defer func() { process = saved }()
	process = func([]string, string, []string, string, time.Duration) (devexec.Result, error) {
		return devexec.Result{Stdout: "before", Stderr: "timeout", Code: -1}, &devexec.TimeoutError{Command: []string{"lean"}, Timeout: time.Second}
	}
	directory := t.TempDir()
	env := map[string]string{"CGO_ENABLED": "0", "SECRET_FOR_TEST": "do-not-log"}
	result := NewCommands(time.Second).Run("extract", []string{"lean"}, directory, env, directory)
	var record map[string]any
	data, _ := os.ReadFile(filepath.Join(directory, "commands.jsonl"))
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record["stdout"] != "before" || record["error"] == nil || record["returncode"] != -1.0 {
		t.Errorf("record %v", record)
	}
	if _, ok := record["environment"].(map[string]any)["SECRET_FOR_TEST"]; ok {
		t.Error("environment dumped")
	}
	if s := status(ExtractionOutcome(result, ptr("source_audit"), nil)); s != "extraction_protocol_failed" {
		t.Errorf("timeout classified as %s", s)
	}
}

func TestProducerBuildFailureKeepsEveryCaseNotRun(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	os.MkdirAll(filepath.Join(dir, "reproducers"), 0o755)
	os.WriteFile(filepath.Join(dir, "reproducers", "NumericBoundaryName.lean"), []byte("probe"), 0o644)
	cases := []Case{{Name: "accepted"}, {Name: "negative", Reject: ptr("source_audit")}}
	commands, _ := scripted(t, CommandResult{Stdout: "probe"}, CommandResult{Code: 1, Stderr: "producer API changed"})
	r := &Runner{Commands: commands,
		observe: func(string, string, map[string]string, *Commands, string) (Identity, error) { return next, nil },
		runCase: func(*Runner, Case, string, string, string, Identity, string, string, map[string]string) (Outcome, error) {
			t.Fatal("a case ran after a producer failure")
			return nil, nil
		}}
	result, err := r.RunCompiler(filepath.Join(dir, "candidate"), filepath.Join(dir, "bin"), snapshot(t), corpus, cases, "/tool/go", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "producer_build_failed" || !reflect.DeepEqual(result.Cases, map[string]Outcome{"accepted": {"status": "not_run"}, "negative": {"status": "not_run"}}) {
		t.Fatalf("result %+v", result)
	}
	// The research adaptation is applied to the run-owned workspace only.
	adapted, _ := os.ReadFile(filepath.Join(dir, "candidate", "producer", "producer", "lean-toolchain"))
	if string(adapted) != "leanprover/lean4:v4.99.0-rc1\n" {
		t.Errorf("workspace pin %q", adapted)
	}
	if result.NumericBoundaryProbe["stdout"] != "probe" {
		t.Errorf("probe %v", result.NumericBoundaryProbe)
	}
}

func TestMalformedSuccessAndWrongBehaviorCannotPass(t *testing.T) {
	directory := t.TempDir()
	consumer := filepath.Join(directory, "consumer")
	os.Mkdir(consumer, 0o755)
	os.WriteFile(filepath.Join(directory, "program.ir.json"), []byte("{}"), 0o644)
	commands, _ := scripted(t, CommandResult{}, CommandResult{Code: 1, Stdout: "assertion failed"})
	r := &Runner{Commands: commands}
	result, err := r.CheckAccepted(Case{}, directory, directory, consumer, base, "/tool/go", map[string]string{})
	if err != nil || status(result) != "extraction_protocol_failed" {
		t.Fatalf("%v %v", result, err)
	}
	program := fmt.Sprintf(`{"lean_version":%q,"lean_commit":%q,"inputs":{},"functions":[]}`, base.Version, base.Commit)
	os.WriteFile(filepath.Join(directory, "program.ir.json"), []byte(program), 0o644)
	other := Identity{Version: base.Version, Commit: next.Commit}
	if result, _ := r.CheckAccepted(Case{}, directory, directory, consumer, other, "/tool/go", map[string]string{}); status(result) != "extraction_protocol_failed" {
		t.Errorf("IR of another compiler: %v", result)
	}
	os.WriteFile(filepath.Join(directory, "consumer_test.go.txt"), []byte("package consumer"), 0o644)
	result, err = r.CheckAccepted(Case{}, directory, directory, consumer, base, "/tool/go", map[string]string{})
	if err != nil || status(result) != "behavior_mismatch" || result["behavior"] != "failed" {
		t.Fatalf("%v %v", result, err)
	}
}

func TestNativeOracleFailuresCannotPass(t *testing.T) {
	directory := t.TempDir()
	os.Mkdir(filepath.Join(directory, "source"), 0o755)
	fixture := filepath.Join(root(t), "compatibility", "corpus", "uint16_xor")
	ok, bad := CommandResult{}, CommandResult{Code: 1, Stderr: "failed"}
	for _, c := range []struct {
		results []CommandResult
		want    string
	}{{[]CommandResult{bad}, "native_oracle_build_failed"}, {[]CommandResult{ok, bad}, "native_oracle_failed"},
		{[]CommandResult{ok, ok}, "native_oracle_failed"}} {
		commands, _ := scripted(t, c.results...)
		r := &Runner{Commands: commands}
		got, err := r.BuildNativeOracle(fixture, directory, directory, Identity{Bin: "/tool/bin"}, map[string]string{})
		if err != nil || got != c.want {
			t.Errorf("got %q %v, want %s", got, err, c.want)
		}
	}
}

// runReplay runs a replay script with a stand-in go that records its
// arguments and exits 7.
func runReplay(t *testing.T, script, dir string) ([]string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	fake := filepath.Join(t.TempDir(), "bin")
	os.Mkdir(fake, 0o755)
	record := filepath.Join(t.TempDir(), "argv")
	os.WriteFile(filepath.Join(fake, "go"), []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > "+shellQuote(record)+"\npwd >> "+shellQuote(record)+"\nexit 7\n"), 0o755)
	os.MkdirAll(filepath.Join(dir, "source"), 0o755)
	path := filepath.Join(dir, "replay.sh")
	os.WriteFile(path, []byte(script), 0o755)
	command := exec.Command(sh, path, "/new/artifacts")
	command.Env = []string{"PATH=" + fake + ":/bin:/usr/bin"}
	err = command.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(record)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), code
}

func assertReplayArguments(t *testing.T, script, dir, timeout, caseName string) {
	t.Helper()
	argv, code := runReplay(t, script, dir)
	want := []string{"run", "-buildvcs=false", "./cmd/lean-go-dev", "compatibility", "--baseline-bin", "/baseline/bin",
		"--timeout", timeout, "--candidate-bin", "/candidate/bin's"}
	if caseName != "" {
		want = append(want, "--case", caseName)
	}
	resolved, _ := filepath.EvalSymlinks(filepath.Join(dir, "source"))
	want = append(want, "--artifacts", "/new/artifacts", resolved)
	if !reflect.DeepEqual(argv, want) || code != 7 {
		t.Fatalf("argv %q exit %d, want %q", argv, code, want)
	}
}

func TestExecutableReplayPreservesNondefaultTimeout(t *testing.T) {
	for _, caseName := range []string{"", "numeric"} {
		script := ReplayScript("/baseline/bin", "/candidate/bin's", caseName, 12750*time.Millisecond)
		assertReplayArguments(t, script, t.TempDir(), "12.75", caseName)
	}
}

func TestDifferenceBundleIsSourceOnlyAndReplaysOneCase(t *testing.T) {
	directory := t.TempDir()
	os.MkdirAll(filepath.Join(directory, "source"), 0o755)
	os.WriteFile(filepath.Join(directory, "source", "input.txt"), []byte("snapshot"), 0o644)
	WriteJSON(filepath.Join(directory, "inputs.json"), map[string]string{"input.txt": "hash"})
	differences := []Difference{{"example", "canonical_ir_drift"}}
	WriteJSON(filepath.Join(directory, "report.json"), map[string]any{"differences": differences})
	for role, content := range map[string]string{"baseline": "{\"body\": 1}\n", "candidate": "{\"body\": 2}\n"} {
		caseDir := filepath.Join(directory, role, "cases", "example")
		os.MkdirAll(filepath.Join(caseDir, ".lake", "build"), 0o755)
		os.WriteFile(filepath.Join(caseDir, "program.canonical.json"), []byte(content), 0o644)
		os.WriteFile(filepath.Join(caseDir, "consumer.test"), []byte("binary"), 0o644)
		os.WriteFile(filepath.Join(caseDir, ".lake", "build", "cache"), []byte("cache"), 0o644)
	}
	if err := DifferenceBundles(directory, differences, "/baseline/bin", "/candidate/bin's", 18500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(filepath.Join(directory, "differences", "example", "replay.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	names := map[string]*zip.File{}
	for _, f := range archive.File {
		names[f.Name] = f
		if strings.HasSuffix(f.Name, "consumer.test") || strings.Contains(f.Name, ".lake") {
			t.Errorf("bundle contains %s", f.Name)
		}
	}
	for _, want := range []string{"source/input.txt", "ir.diff", "difference.json", "replay.sh", "report.json", "inputs.json",
		"baseline/cases/example/program.canonical.json", "candidate/cases/example/program.canonical.json"} {
		if names[want] == nil {
			t.Errorf("bundle lacks %s", want)
		}
	}
	read := func(name string) string {
		r, _ := names[name].Open()
		defer r.Close()
		data, _ := io.ReadAll(r)
		return string(data)
	}
	if diff := read("ir.diff"); !strings.Contains(diff, "-{\"body\": 1}\n+{\"body\": 2}\n") ||
		!strings.HasPrefix(diff, "--- baseline/program.canonical.json\n+++ candidate/program.canonical.json\n@@ -1 +1 @@\n") {
		t.Errorf("ir.diff %q", diff)
	}
	replay := read("replay.sh")
	if !strings.Contains(replay, "'--case' 'example'") {
		t.Errorf("replay.sh %s", replay)
	}
	assertReplayArguments(t, replay, t.TempDir(), "18.5", "example")
}

func TestObservedPrefixMustMatchSelectedInstallation(t *testing.T) {
	commands, _ := scripted(t, CommandResult{Stdout: version(base)}, CommandResult{Stdout: version(base)}, CommandResult{Stdout: "/different/install\n"})
	_, err := ObserveCompiler("/selected/bin", "/work", map[string]string{}, commands, "/logs")
	if err == nil || !strings.Contains(err.Error(), "print-prefix") {
		t.Fatalf("got %v", err)
	}
	commands, _ = scripted(t, CommandResult{Stdout: version(base)}, CommandResult{Stdout: version(next)})
	if _, err := ObserveCompiler("/selected/bin", "/work", map[string]string{}, commands, "/logs"); err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Fatalf("got %v", err)
	}
}

func TestUnifiedDiffMatchesDifflib(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"
	after := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\n"
	want := "--- x\n+++ y\n@@ -1,5 +1,5 @@\n a\n-b\n+B\n c\n d\n e\n@@ -11,3 +11,4 @@\n k\n l\n m\n+n\n"
	if got := UnifiedDiff(before, after, "x", "y"); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if UnifiedDiff(before, before, "x", "y") != "" {
		t.Fatal("equal texts differ")
	}
	if got := UnifiedDiff("", "a\n", "x", "y"); got != "--- x\n+++ y\n@@ -0,0 +1 @@\n+a\n" {
		t.Fatalf("got %q", got)
	}
}

// The demo check covers every proof demo, and the native clamp package next
// to the metered one.
func TestDemosAreEveryTrackedProofDemo(t *testing.T) {
	var names []string
	for _, demo := range Demos() {
		names = append(names, demo.Name)
		for _, spec := range demo.Packages {
			if spec.Contract == "" || spec.Evidence == "" {
				t.Errorf("%s has no proof contract", spec.Package)
			}
		}
	}
	want := []string{"clamp", "flags", "words", "bytes", "header", "wire16", "crc16", "adler32", "normalizer", "clock"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("demos %v", names)
	}
	if clamp := Demos()[0]; len(clamp.Packages) != 2 || clamp.Packages[1].Mode != "native" {
		t.Fatalf("clamp packages %+v", clamp.Packages)
	}
}
