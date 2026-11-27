package pipeline

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/franchb/lean-go/producer"
)

// Ungated tests of the validation runner: options, the report split, and the
// skip-and-rerun logic against a simulated Lean process.

func TestValidationOptionsDefaultsAndLimits(t *testing.T) {
	if o, err := (*ValidationOptions)(nil).Normalized(); o != nil || err != nil {
		t.Fatal("nil options must mean off")
	}
	if o, err := (&ValidationOptions{Mode: ValidateOff}).Normalized(); o != nil || err != nil {
		t.Fatal("off must mean off")
	}
	if _, err := (&ValidationOptions{Mode: ValidateOff, Report: "r.json"}).Normalized(); err == nil {
		t.Fatal("a report path without validation was accepted")
	}
	o, err := (&ValidationOptions{Mode: ValidateRequire}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if o.Heartbeats != DefaultValidateHeartbeats || o.TimeoutMs != DefaultValidateTimeoutMs ||
		o.MemoryMB != DefaultValidateMemoryMB || o.ProcessTimeout != DefaultValidateProcessTimeout {
		t.Fatalf("defaults %+v", o)
	}
	if DefaultValidateMemoryMB > 8192 {
		t.Fatal("the default memory cap must stay well below a shared machine's memory")
	}
	for _, bad := range []ValidationOptions{
		{Mode: "strict"}, {Mode: ValidateReport, MemoryMB: 10}, {Mode: ValidateReport, Heartbeats: -1},
		{Mode: ValidateReport, TimeoutMs: -5}, {Mode: ValidateReport, ProcessTimeout: time.Millisecond},
	} {
		if _, err := bad.Normalized(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	custom, err := (&ValidationOptions{Mode: ValidateReport, MemoryMB: 512, Heartbeats: 7, TimeoutMs: 9, ProcessTimeout: time.Minute}).Normalized()
	if err != nil || custom.MemoryMB != 512 || custom.Heartbeats != 7 || custom.TimeoutMs != 9 || custom.ProcessTimeout != time.Minute {
		t.Fatalf("overrides %+v %v", custom, err)
	}
}

func TestDefaultReportPath(t *testing.T) {
	for in, want := range map[string]string{
		filepath.Join("a", "x.ir.json"): filepath.Join("a", "x.validation.json"),
		"x.json":                        "x.json.validation.json",
		".ir.json":                      ".ir.json.validation.json",
	} {
		if got := DefaultReportPath(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func TestValidationHarness(t *testing.T) {
	o := ValidationOptions{Heartbeats: 5, TimeoutMs: 6}
	got := validationHarness("M", []string{"M.a", "M.b"}, o, []string{"M.f", "M.g"})
	want := "import LeanGo.Module.Export\nimport LeanGo.Validate\nimport LeanGo.Validate.Command\nimport M\n" +
		"#lean_go_validate \"ir.json\" [M.a, M.b] \"M\" report \"validation.json\" heartbeats 5 timeout_ms 6 skip \"M.f M.g\"\n"
	if got != want {
		t.Fatalf("%q", got)
	}
	if strings.Contains(validationHarness("M", []string{"M.a"}, o, nil), "skip") || strings.Contains(got, "strict") {
		t.Fatal("unexpected harness options")
	}
}

func TestLastAttempt(t *testing.T) {
	for progress, want := range map[string]string{
		"":                                      "",
		"0 ms export value\n5 ms obligations\n": "",
		"5 ms proving M.f\n":                    "M.f",
		"5 ms proving M.f\n9 ms retrying M.f unfolding [M.g]\n":                      "M.f",
		"5 ms proving M.f\n9 ms M.f: proved in 4 ms\n":                               "",
		"5 ms proving M.f\n9 ms M.f: failed in 4 ms\n10 ms whole-program theorems\n": "",
		"5 ms proving M.f\n9 ms M.f: proved in 4 ms\n10 ms proving M.g":              "M.g",
	} {
		if got := lastAttempt(progress); got != want {
			t.Errorf("%q: %q, want %q", progress, got, want)
		}
	}
}

func TestClassifyReason(t *testing.T) {
	for text, want := range map[string]string{
		"":                               "",
		"validate_obl: stuck at ⊢ x = y": ReasonStuck,
		"heartbeat budget exhausted":     ReasonHeartbeats,
		"wall-clock budget exhausted":    ReasonObligationTime,
		"(kernel) excessive memory consumption detected": ReasonMemory,
		"no encoding for T: derive failed":               ReasonNoEncoding,
		"non-standard axioms #[sorryAx]":                 ReasonAxioms,
		"source signature of M.f does not match":         ReasonNoSpecification,
		"root M.f is a compiler-generated helper":        ReasonNoSpecification,
		"unsolved goals remain":                          ReasonUnsolved,
		"the proof contains sorry":                       ReasonProofRejected,
		"something new":                                  ReasonOther,
	} {
		if got := classifyReason(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}

func proved(name string) rawFunction {
	theorem := "LeanGo.Validated.obl_" + name
	return rawFunction{Name: name, Params: []string{"UInt32"}, Result: "UInt32", Kind: "source", SourceOwner: name,
		Specification: name, Status: "proved", TimeMs: 17, Heartbeats: 42, Theorem: &theorem,
		Statement: "Callable.Rel Eq … " + name, Axioms: []string{"Quot.sound", "propext"}}
}

func sampleRaw() *rawValidationReport {
	yes := true
	return &rawValidationReport{
		Module: "M", Roots: []string{"M.f", "M.g"}, Status: "completed", IRFileMatches: &yes,
		Program: "LeanGo.Validated.prog", SpecificationList: "LeanGo.Validated.spec",
		Encodings: []rawEncoding{{Type: "M.T", Status: "available"}},
		Functions: []rawFunction{proved("M.f"), proved("M.h"),
			{Name: "M.g", Kind: "source", Status: "not_attempted", Reason: "skipped by the caller", TimeMs: 0}},
		Programs: []rawProgram{
			{Root: "M.f", Closure: []string{"M.f", "M.h"}, Status: "proved", Theorem: "LeanGo.Validated.sound_0", Statement: "∀ n, …",
				PrefixedTheorem: "p", ExplicitTheorem: "e", ExplicitStatement: "∀ n a v, …", Axioms: []string{"propext"}, TimeMs: 99},
			{Root: "M.g", Closure: []string{"M.g"}, Status: "not_established", Missing: []string{"M.g"}},
		},
		Summary: map[string]any{"time_ms": float64(1234)},
	}
}

func sampleInputs() reportInputs {
	return reportInputs{
		module: "M", roots: []string{"M.f", "M.g"}, irSHA256: "abc", compiler: Compiler{Version: "4.36.0", Commit: "c"},
		options:  ValidationOptions{Heartbeats: 1, TimeoutMs: 2, MemoryMB: 3, ProcessTimeout: time.Second},
		library:  map[string]string{"producer/LeanGo/Validate.lean": "d"},
		killed:   map[string]killedObligation{"M.g": {"validation process exceeded its memory limit of 3 MiB while proving this obligation", ReasonMemory}},
		attempts: []ValidationAttempt{{Skip: []string{}, ExitCode: 137, WallMs: 5}, {Skip: []string{"M.g"}, WallMs: 6}},
	}
}

func TestReportSplitIsDeterministic(t *testing.T) {
	report, diagnostics := splitReport(sampleRaw(), sampleInputs())
	result, err := encodeValidation(report, diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	// Different timings and heartbeats must not change the tracked part.
	raw := sampleRaw()
	raw.Functions[0].TimeMs, raw.Functions[0].Heartbeats, raw.Programs[0].TimeMs = 99999, 1, 3
	raw.Summary["time_ms"] = float64(7)
	in := sampleInputs()
	in.attempts = []ValidationAttempt{{Skip: []string{}, WallMs: 1}}
	again, err := encodeValidation(splitReport(raw, in))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.ReportJSON, again.ReportJSON) {
		t.Fatalf("tracked reports differ:\n%s\n%s", result.ReportJSON, again.ReportJSON)
	}
	if bytes.Equal(result.DiagnosticsJSON, again.DiagnosticsJSON) {
		t.Fatal("diagnostics lost the timings")
	}
	for _, word := range []string{"time_ms", "heartbeats_used", "wall_ms", "skipped by the caller", "exceeded its memory"} {
		if bytes.Contains(result.ReportJSON, []byte(word)) {
			t.Errorf("tracked report contains %q", word)
		}
	}
	for _, word := range []string{`"time_ms": 17`, `"heartbeats_used": 42`, `"wall_ms": 5`, "exceeded its memory limit of 3 MiB"} {
		if !bytes.Contains(result.DiagnosticsJSON, []byte(word)) {
			t.Errorf("diagnostics lack %q", word)
		}
	}
	parsed, err := ParseValidationReport(result.ReportJSON)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Summary != (ReportSummary{Functions: 3, Proved: 2, Failed: 1, Roots: 2, RootsProved: 1}) {
		t.Fatalf("summary %+v", parsed.Summary)
	}
	g := parsed.Functions[2]
	if g.Status != "failed" || g.ReasonClass != ReasonMemory || g.Theorem != "" {
		t.Fatalf("killed obligation %+v", g)
	}
	if !slices.Equal(parsed.Functions[0].Axioms, []string{"Quot.sound", "propext"}) || parsed.Library["producer/LeanGo/Validate.lean"] != "d" {
		t.Fatalf("%+v", parsed)
	}
	if parsed.Complete() {
		t.Fatal("an incomplete report is complete")
	}
	unproved := strings.Join(parsed.Unproved(), "\n")
	if unproved != "M.g: failed: memory limit\nroot M.g: not established (needs M.g)" {
		t.Fatalf("%q", unproved)
	}
	if !strings.HasSuffix(string(result.ReportJSON), "}\n") || !strings.Contains(string(result.ReportJSON), "∀ n, …") {
		t.Fatal("report is not plain indented UTF-8 JSON")
	}
	// Unknown fields in a tracked report are refused.
	var object map[string]any
	if err := json.Unmarshal(result.ReportJSON, &object); err != nil {
		t.Fatal(err)
	}
	object["time_ms"] = 3
	extra, _ := json.Marshal(object)
	if _, err := ParseValidationReport(extra); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

func TestProcessFailureReport(t *testing.T) {
	in := sampleInputs()
	in.processReason, in.processClass = "validation process exceeded its memory limit of 3 MiB (exit -1): …", ReasonMemory
	report, diagnostics := splitReport(nil, in)
	if report.Status != ValidationProcessFailed || report.ReasonClass != ReasonMemory || diagnostics.Reason == "" {
		t.Fatalf("%+v", report)
	}
	if report.Complete() || report.SummaryLine() != "validation process_failed (memory limit)" {
		t.Fatal(report.SummaryLine())
	}
}

// fakeValidator simulates successive validation processes.
type fakeValidator struct {
	t        *testing.T
	steps    []func(workspace, harness string) processOutcome
	commands [][]string
	harness  []string
}

func (f *fakeValidator) install(t *testing.T) {
	oldProcess, oldRun := runValidationProcess, run
	runValidationProcess = func(command []string, dir string, env []string, timeout time.Duration) processOutcome {
		f.commands = append(f.commands, command)
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(validationHarnessPath)))
		if err != nil {
			t.Fatal(err)
		}
		f.harness = append(f.harness, string(data))
		if !slices.Contains(env, "LEAN_PATH=/fake/lean/path") {
			t.Error("LEAN_PATH from lake env is missing")
		}
		step := f.steps[0]
		f.steps = f.steps[1:]
		return step(dir, string(data))
	}
	run = func(command []string, dir string, env []string) (string, error) {
		if len(command) == 2 && command[1] == "env" {
			return "LAKE=/x/lake\nLEAN_PATH=/fake/lean/path\n", nil
		}
		return "", errors.New("unexpected command")
	}
	t.Cleanup(func() { runValidationProcess, run = oldProcess, oldRun })
}

func writeRaw(t *testing.T, workspace string, raw *rawValidationReport) {
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, validationReportName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeProgress(t *testing.T, workspace, text string) {
	if err := os.WriteFile(filepath.Join(workspace, validationReportName+".progress"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeRun(t *testing.T) validationRun {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "LeanGo", "Module"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts, err := (&ValidationOptions{Mode: ValidateReport, MemoryMB: 256}).Normalized()
	if err != nil {
		t.Fatal(err)
	}
	return validationRun{workspace: workspace, lake: "lake", lean: "lean", module: "M", roots: []string{"M.f", "M.g"},
		ir: []byte("{}\n"), compiler: Compiler{Version: "4.36.0", Commit: "c"}, options: *opts}
}

func completed(skip ...string) *rawValidationReport {
	raw := sampleRaw()
	raw.Functions[2] = proved("M.g")
	for i, f := range raw.Functions {
		if slices.Contains(skip, f.Name) {
			raw.Functions[i] = rawFunction{Name: f.Name, Kind: "source", Status: "not_attempted", Reason: "skipped by the caller"}
		}
	}
	return raw
}

func TestKilledProcessSkipsTheObligationAndReruns(t *testing.T) {
	r := fakeRun(t)
	f := &fakeValidator{t: t}
	f.steps = []func(string, string) processOutcome{
		func(ws, _ string) processOutcome {
			writeProgress(t, ws, "0 ms export value\n3 ms proving M.f\n4 ms M.f: proved in 1 ms\n5 ms proving M.g\n")
			return processOutcome{exitCode: 137, output: "Killed"}
		},
		func(ws, harness string) processOutcome {
			if !strings.Contains(harness, `skip "M.g"`) {
				t.Errorf("rerun harness %q", harness)
			}
			writeRaw(t, ws, completed("M.g"))
			return processOutcome{}
		},
	}
	f.install(t)
	result, err := runValidation(r)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.commands[0], []string{"lean", "-M", "256", "-DElab.async=false", validationHarnessPath}) {
		t.Fatalf("command %v", f.commands[0])
	}
	g := result.Report.Functions[2]
	if g.Status != "failed" || g.ReasonClass != ReasonMemory || len(result.Diagnostics.Attempts) != 2 {
		t.Fatalf("%+v %+v", g, result.Diagnostics.Attempts)
	}
	if !strings.Contains(result.Diagnostics.Functions[2].Reason, "exit 137") {
		t.Fatal(result.Diagnostics.Functions[2].Reason)
	}
}

func TestCaughtMemoryFailureIsIsolatedByARerun(t *testing.T) {
	r := fakeRun(t)
	f := &fakeValidator{t: t}
	memory := func(raw *rawValidationReport, names ...string) *rawValidationReport {
		for i, fn := range raw.Functions {
			if slices.Contains(names, fn.Name) {
				raw.Functions[i].Status, raw.Functions[i].Reason, raw.Functions[i].Theorem = "failed", "(kernel) excessive memory consumption detected", nil
			}
		}
		return raw
	}
	f.steps = []func(string, string) processOutcome{
		// M.f exhausts the cap; M.h after it fails only because the process stays near it.
		func(ws, _ string) processOutcome {
			writeRaw(t, ws, memory(completed(), "M.f", "M.h"))
			return processOutcome{}
		},
		func(ws, harness string) processOutcome {
			if !strings.Contains(harness, `skip "M.f"`) {
				t.Errorf("rerun harness %q", harness)
			}
			writeRaw(t, ws, completed("M.f"))
			return processOutcome{}
		},
	}
	f.install(t)
	result, err := runValidation(r)
	if err != nil {
		t.Fatal(err)
	}
	statuses := []string{}
	for _, fn := range result.Report.Functions {
		statuses = append(statuses, fn.Name+"="+fn.Status+"/"+fn.ReasonClass)
	}
	if strings.Join(statuses, " ") != "M.f=failed/memory_limit M.h=proved/ M.g=proved/" {
		t.Fatal(statuses)
	}
}

func TestTimeoutAndRepeatedKillsEnd(t *testing.T) {
	r := fakeRun(t)
	f := &fakeValidator{t: t}
	timeout := func(ws, _ string) processOutcome {
		writeProgress(t, ws, "5 ms proving M.f\n")
		return processOutcome{timedOut: true, exitCode: -1}
	}
	f.steps = []func(string, string) processOutcome{
		timeout,
		// still dies without reaching an obligation: no report, no new victim
		func(ws, _ string) processOutcome {
			writeProgress(t, ws, "0 ms export value\n")
			return processOutcome{exitCode: 1, output: "boom"}
		},
	}
	f.install(t)
	result, err := runValidation(r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Status != ValidationProcessFailed || result.Report.ReasonClass != ReasonProcessFailed ||
		!strings.Contains(result.Diagnostics.Reason, "boom") || len(f.commands) != 2 {
		t.Fatalf("%+v %q", result.Report, result.Diagnostics.Reason)
	}
}

func TestReportForOtherRootsIsRefused(t *testing.T) {
	r := fakeRun(t)
	f := &fakeValidator{t: t}
	f.steps = []func(string, string) processOutcome{func(ws, _ string) processOutcome {
		raw := completed()
		raw.Roots = []string{"M.f"}
		writeRaw(t, ws, raw)
		return processOutcome{}
	}}
	f.install(t)
	if _, err := runValidation(r); err == nil {
		t.Fatal("a report for other roots was accepted")
	}
}

func TestRequireFailsAfterWritingTheReport(t *testing.T) {
	report, diagnostics := splitReport(sampleRaw(), sampleInputs())
	result, err := encodeValidation(report, diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	o := &ValidationOptions{Mode: ValidateRequire, Report: filepath.Join(dir, "r.json"), Diagnostics: filepath.Join(dir, "d.json")}
	err = requireValidation(o, result)
	var pe *Error
	if !errors.As(err, &pe) || pe.diagnostic.Values["stage"] != "validation" || !strings.Contains(err.Error(), "M.g: failed: memory limit") {
		t.Fatalf("%v", err)
	}
	for _, path := range []string{o.Report, o.Diagnostics} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("report not written before failing:", err)
		}
	}
	o.Mode = ValidateReport
	if err := requireValidation(o, result); err != nil {
		t.Fatal("report mode failed:", err)
	}
}

func TestValidationIsOutsideTheExtractionInventory(t *testing.T) {
	if SourceDigest() == ValidationSourceDigest() || ProofSourceDigest() == ValidationSourceDigest() {
		t.Fatal("validation runner digest is not distinct")
	}
	for key := range ProducerInventory() {
		if strings.Contains(key, "Validate") {
			t.Fatalf("validation file %s in the extraction inventory", key)
		}
	}
	inventory := ValidationInventory()
	if len(inventory) != len(producer.ValidationFiles())+1 || inventory[ValidationToolInput] == "" ||
		inventory["producer/LeanGo/Validate/Command.lean"] == "" || inventory["producer/LeanGo/Validate/Meta/Tactic.lean"] == "" {
		t.Fatalf("%v", inventory)
	}
	for name := range producer.ModuleFiles() {
		if strings.HasPrefix(name, "LeanGo/Validate") {
			t.Fatal("validation file among the module producer files")
		}
	}
	// The cache key of a build without validation does not depend on the
	// validation files; a validating build has its own key and targets.
	if cacheKey("v", false) == cacheKey("v", true) {
		t.Fatal("validation builds share the plain cache key")
	}
	if !slices.Equal(producerTargets(false), []string{"LeanGo.Module.Export"}) ||
		!slices.Equal(producerTargets(true), []string{"LeanGo.Module.Export", "LeanGo.Validate", "LeanGo.Validate.Command"}) {
		t.Fatal("producer targets")
	}
	if _, ok := producerFiles(true)["LeanGo/Validate/Command.lean"]; !ok || len(producerFiles(false)) != len(producer.ModuleFiles()) {
		t.Fatal("producer files")
	}
}

// The runner does not take the command's word for what the program contains:
// an IR function or a requested root that a completed report leaves out is
// entered as not attempted and makes the report incomplete.
func TestCompletedReportMustCoverEveryFunctionAndRoot(t *testing.T) {
	yes := true
	allProved := func() *rawValidationReport {
		return &rawValidationReport{
			Module: "M", Roots: []string{"M.f", "M.g"}, Status: "completed", IRFileMatches: &yes,
			Functions: []rawFunction{proved("M.f"), proved("M.g"), proved("M.h")},
			Programs: []rawProgram{
				{Root: "M.f", Closure: []string{"M.f", "M.h"}, Status: "proved", Theorem: "t0"},
				{Root: "M.g", Closure: []string{"M.g"}, Status: "proved", Theorem: "t1"},
			},
		}
	}
	in := sampleInputs()
	in.killed = nil
	in.functions = []string{"M.f", "M.g", "M.h"}

	report, _ := splitReport(allProved(), in)
	if !report.Complete() || report.Summary.Functions != 3 || report.Summary.Proved != 3 || report.Summary.RootsProved != 2 {
		t.Fatalf("a full report is not complete: %v %+v", report.Unproved(), report.Summary)
	}

	// The command drops a function.
	raw := allProved()
	raw.Functions = raw.Functions[:2]
	report, diagnostics := splitReport(raw, in)
	unproved := strings.Join(report.Unproved(), "; ")
	if report.Complete() || !strings.Contains(unproved, "M.h: not_attempted: not reported") {
		t.Fatalf("a missing function went unnoticed: %q", unproved)
	}
	if report.Summary.Functions != 3 || report.Summary.Proved != 2 || report.Summary.NotAttempted != 1 ||
		len(diagnostics.Functions) != 3 || diagnostics.Functions[2].Name != "M.h" {
		t.Fatalf("summary %+v diagnostics %+v", report.Summary, diagnostics.Functions)
	}

	// The command drops a root.
	raw = allProved()
	raw.Programs = raw.Programs[:1]
	report, diagnostics = splitReport(raw, in)
	unproved = strings.Join(report.Unproved(), "; ")
	if report.Complete() || !strings.Contains(unproved, "root M.g: not established: not reported") {
		t.Fatalf("a missing root went unnoticed: %q", unproved)
	}
	if report.Summary.Roots != 2 || report.Summary.RootsProved != 1 || len(report.Programs) != 2 || len(diagnostics.Programs) != 2 {
		t.Fatalf("summary %+v programs %+v", report.Summary, report.Programs)
	}

	// A tracked report that lost a program entry is incomplete when read back.
	report, _ = splitReport(allProved(), in)
	report.Programs = report.Programs[:1]
	if report.Complete() || !strings.Contains(strings.Join(report.Unproved(), "; "), "root M.g: not reported") {
		t.Fatalf("a report without one of its roots reads as complete: %v", report.Unproved())
	}

	// A run that did not complete is reported as such and not padded.
	raw = allProved()
	raw.Status, raw.Reason, raw.Functions, raw.Programs = "load_failed", "IR not loaded: x", nil, nil
	report, _ = splitReport(raw, in)
	if report.Complete() || len(report.Functions) != 0 || len(report.Programs) != 0 {
		t.Fatalf("an incomplete run was padded or accepted: %+v", report)
	}

	if names := irFunctionNames([]byte(`{"functions":[{"name":"M.f"},{"name":"M.h"}]}`)); !slices.Equal(names, []string{"M.f", "M.h"}) {
		t.Fatalf("IR function names %v", names)
	}
	if irFunctionNames([]byte(`not json`)) != nil {
		t.Fatal("unreadable IR produced names")
	}
}
