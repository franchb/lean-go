package differential

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from the retired scripts/test_differential.py (CaseTests, ReductionTests,
// CampaignTests). The compiled fault-injection test is in lean_test.go.

func addition(width int) Program {
	return Program{Seed: 91, Width: width, Expr: Op("add", A(), B()), Helper: A()}
}

// results returns the model's values on every channel, plus one (wrapped)
// on the broken channels.
func results(p Program, pairs []Pair, broken ...string) map[string][]uint64 {
	out := map[string][]uint64{}
	for _, name := range Channels {
		values := make([]uint64, len(pairs))
		for i, pair := range pairs {
			v, err := Evaluate(p, pair[0], pair[1])
			if err != nil {
				panic(err)
			}
			for _, b := range broken {
				if b == name {
					v = (v + 1) & maxOf(p.Width)
				}
			}
			values[i] = v
		}
		out[name] = values
	}
	return out
}

func mustCompare(t *testing.T, p Program, pairs []Pair, outputs map[string][]uint64) Observation {
	t.Helper()
	o, err := Compare(p, pairs, outputs)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestCompareReportsExactChannelsAndInputIndexes(t *testing.T) {
	p := addition(64)
	pairs := []Pair{{maxOf(64), 1}, {1, 1}}
	output := map[string][]uint64{"native": {0, 2}, "raw": {0, 3}, "typed": {1, 2}, "raw_state_machine": {0, 2}, "typed_state_machine": {0, 2}, "native_go": {0, 2}}
	o := mustCompare(t, p, pairs, output)
	if !reflect.DeepEqual(o.Expected, []uint64{0, 2}) {
		t.Errorf("expected %v", o.Expected)
	}
	if !reflect.DeepEqual(o.Signature(), []string{"raw", "typed"}) {
		t.Errorf("signature %v", o.Signature())
	}
	want := map[string][]int{"native": {}, "raw": {1}, "typed": {0}, "raw_state_machine": {}, "typed_state_machine": {}, "native_go": {}}
	if !reflect.DeepEqual(o.Mismatches, want) {
		t.Errorf("mismatches %v", o.Mismatches)
	}
	output["native_go"] = []uint64{1, 3}
	if o := mustCompare(t, p, pairs, output); !reflect.DeepEqual(o.Signature(), []string{"raw", "typed", "native_go"}) {
		t.Errorf("signature %v", o.Signature())
	}
	output["raw"], output["typed"], output["native_go"] = []uint64{0, 2}, []uint64{0, 2}, []uint64{0, 2}
	output["typed_state_machine"] = []uint64{0, 3}
	if o := mustCompare(t, p, pairs, output); !reflect.DeepEqual(o.Signature(), []string{"typed_state_machine"}) ||
		!reflect.DeepEqual(o.Mismatches["typed_state_machine"], []int{1}) {
		t.Errorf("state-machine signature %v %v", o.Signature(), o.Mismatches)
	}
}

func TestInvalidInputsFailBeforeEvaluation(t *testing.T) {
	// Typed pairs: empty, out of width, too many.
	many := make([]Pair, 4097)
	for _, pairs := range [][]Pair{{}, {{0, 1 << 32}}, many} {
		if err := ValidateInputs(32, pairs); err == nil {
			t.Errorf("%d pairs accepted", len(pairs))
		}
		if _, err := Compare(addition(32), pairs, results(addition(32), []Pair{{0, 0}})); err == nil {
			t.Error("compare accepted invalid inputs")
		}
	}
	// Untyped JSON as replay reads it: arity, booleans, signs, floats, strings.
	for _, text := range []string{`[]`, `[[1]]`, `[[1,2,3]]`, `[[true,0]]`, `[[0,-1]]`, `[[0,4294967296]]`,
		`[[1.0,2]]`, `[[1e0,2]]`, `[["1",2]]`, `"1,2"`, `null`, `{"a":[1,2]}`} {
		value, err := decodeJSON([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseInputs(32, value); err == nil {
			t.Errorf("%s accepted", text)
		}
	}
	value, _ := decodeJSON([]byte("[" + strings.Repeat("[0,0],", 4096) + "[0,0]]"))
	if _, err := ParseInputs(32, value); err == nil {
		t.Error("4097 pairs accepted")
	}
	if pairs, err := ParseInputs(64, mustDecode(t, `[[18446744073709551615,9007199254740993]]`)); err != nil ||
		pairs[0] != (Pair{maxOf(64), 1<<53 + 1}) {
		t.Errorf("exact wide inputs: %v %v", pairs, err)
	}
}

func mustDecode(t *testing.T, text string) any {
	t.Helper()
	v, err := decodeJSON([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestBackendProtocolCannotBeASemanticPass(t *testing.T) {
	good := func() map[string][]uint64 {
		return map[string][]uint64{"native": {3}, "raw": {3}, "typed": {3}, "raw_state_machine": {3}, "typed_state_machine": {3}, "native_go": {3}}
	}
	missing := good()
	delete(missing, "native")
	extra := good()
	extra["extra"] = []uint64{}
	empty := good()
	empty["raw"] = []uint64{}
	wide := good()
	wide["raw"] = []uint64{1 << 32}
	for i, output := range []map[string][]uint64{nil, missing, extra, empty, wide} {
		if _, err := Compare(addition(32), []Pair{{1, 2}}, output); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	// Values Go's []uint64 cannot hold are rejected where results are decoded.
	for _, raw := range []string{`[true]`, `[-1]`, `[3.0]`, `"3"`, `{"0":3}`} {
		goOut := `{"raw":` + raw + `,"typed":[3],"raw_state_machine":[3],"typed_state_machine":[3],"native_go":[3]}`
		if _, err := DecodeResults("[3]", goOut, 1, 32); err == nil {
			t.Errorf("raw %s accepted", raw)
		}
	}
}

func TestArtifactRoundtripUsesFullProgramAndExactWideInputs(t *testing.T) {
	p := addition(64)
	p.Expr = Op("mul", A(), B())
	pairs := []Pair{{maxOf(64), 1<<53 + 1}}
	o := mustCompare(t, p, pairs, results(p, pairs, "typed"))
	artifact := filepath.Join(t.TempDir(), "case.json")
	if err := SaveCase(artifact, p, pairs, &o, nil); err != nil {
		t.Fatal(err)
	}
	stored := map[string]any{}
	data, _ := os.ReadFile(artifact)
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored["program"], serializedMap(t, p)) {
		t.Errorf("stored program %v", stored["program"])
	}
	mismatches := stored["observation"].(map[string]any)["mismatches"].(map[string]any)
	if !reflect.DeepEqual(mismatches["typed"], []any{json.Number("0")}) {
		t.Errorf("typed mismatches %v", mismatches["typed"])
	}
	if !strings.Contains(string(data), "18446744073709551615") || !strings.Contains(string(data), "9007199254740993") {
		t.Error("wide inputs are not exact")
	}
	stored["observation"] = map[string]any{"expected": "untrusted historical evidence"}
	data, _ = json.Marshal(stored)
	if err := os.WriteFile(artifact, data, 0o644); err != nil {
		t.Fatal(err)
	}
	actual, actualPairs, err := LoadCase(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !actual.Equal(p) || !reflect.DeepEqual(actualPairs, pairs) {
		t.Fatalf("got %v %v", actual.Key(), actualPairs)
	}
	if mustRender(t, actual) != mustRender(t, p) {
		t.Fatal("rendered source changed")
	}
}

func TestInvalidArtifactSchemaFieldsAndInputsAreRejected(t *testing.T) {
	program := serializedMap(t, addition(32))
	valid := map[string]any{"schema": CaseSchema, "program": program, "inputs": []any{[]any{1, 2}}}
	without := with(valid, "schema", CaseSchema)
	delete(without, "inputs")
	malformed := []any{[]any{}, map[string]any{}, with(valid, "schema", "future"), with(valid, "extra", 1),
		with(valid, "program", map[string]any{}), with(valid, "inputs", []any{[]any{false, 0}}), without}
	artifact := filepath.Join(t.TempDir(), "case.json")
	for i, data := range malformed {
		text, _ := json.Marshal(data)
		if err := os.WriteFile(artifact, text, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadCase(artifact); err == nil {
			t.Errorf("malformed case %d accepted", i)
		}
	}
	text, _ := json.Marshal(valid)
	os.WriteFile(artifact, text, 0o644)
	if _, _, err := LoadCase(artifact); err != nil {
		t.Fatalf("valid case rejected: %v", err)
	}
	os.WriteFile(artifact, []byte(strings.Repeat(" ", 2_000_001)), 0o644)
	if _, _, err := LoadCase(artifact); err == nil || !strings.Contains(err.Error(), "2 MB") {
		t.Fatalf("oversized artifact: %v", err)
	}
}

func checkWith(t *testing.T, broken func(Program, []Pair) []string) CheckFunc {
	return func(p Program, pairs []Pair) (Observation, error) {
		return Compare(p, pairs, results(p, pairs, broken(p, pairs)...))
	}
}

func TestReductionKeepsExactFailingChannels(t *testing.T) {
	p, pairs := addition(32), []Pair{{9, 8}, {7, 6}}
	initial := mustCompare(t, p, pairs, results(p, pairs, "raw"))
	check := checkWith(t, func(_ Program, values []Pair) []string {
		if reflect.DeepEqual(values, []Pair{pairs[0]}) {
			return []string{"raw", "typed"}
		}
		return []string{"raw"}
	})
	reduced, err := ReduceCase(p, pairs, initial, check, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reduced.Inputs, []Pair{pairs[1]}) || !reflect.DeepEqual(reduced.Observation.Signature(), []string{"raw"}) || reduced.Attempts != 2 {
		t.Fatalf("got %+v", reduced)
	}
}

func TestCompileRejectionIsNotACounterexample(t *testing.T) {
	p, pairs := addition(32), []Pair{{9, 8}, {7, 6}}
	initial := mustCompare(t, p, pairs, results(p, pairs, "raw"))
	check := func(candidate Program, values []Pair) (Observation, error) {
		if reflect.DeepEqual(values, []Pair{pairs[0]}) {
			return Observation{}, &BackendError{"extract", "unsupported candidate"}
		}
		return Compare(candidate, values, results(candidate, values, "raw"))
	}
	reduced, err := ReduceCase(p, pairs, initial, check, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reduced.Inputs, []Pair{pairs[1]}) || reduced.RejectedErrors != 1 || !reflect.DeepEqual(reduced.Observation.Signature(), []string{"raw"}) {
		t.Fatalf("got %+v", reduced)
	}
}

func TestProtocolErrorsAndBudgetLeaveOriginalFailure(t *testing.T) {
	p, pairs := addition(32), []Pair{{9, 8}, {7, 6}}
	initial := mustCompare(t, p, pairs, results(p, pairs, "typed"))
	calls := 0
	check := func(Program, []Pair) (Observation, error) {
		calls++
		return Observation{}, errors.New("invalid backend output")
	}
	reduced, err := ReduceCase(p, pairs, initial, check, 3)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || reduced.Attempts != 3 || reduced.RejectedErrors != 3 {
		t.Fatalf("calls %d, %+v", calls, reduced)
	}
	if !reduced.Program.Equal(p) || !reflect.DeepEqual(reduced.Inputs, pairs) || !reflect.DeepEqual(reduced.Observation, initial) {
		t.Fatal("original failure was replaced")
	}
}

func TestZeroBudgetDoesNotExecuteBackend(t *testing.T) {
	p, pairs := addition(32), []Pair{{9, 8}}
	initial := mustCompare(t, p, pairs, results(p, pairs, "raw"))
	check := func(Program, []Pair) (Observation, error) { t.Fatal("backend called"); return Observation{}, nil }
	reduced, err := ReduceCase(p, pairs, initial, check, 0)
	if err != nil || reduced.Attempts != 0 || !reflect.DeepEqual(reduced.Inputs, pairs) {
		t.Fatalf("got %+v %v", reduced, err)
	}
}

// Python also rejected True and 1.0 as budgets; Go's int parameter cannot
// hold them, and the command-line parser rejects them (cmd/lean-go-dev).
func TestPassingCaseAndInvalidBudgetsAreNotShrunk(t *testing.T) {
	p, pairs := addition(32), []Pair{{0, 0}}
	check := func(Program, []Pair) (Observation, error) { t.Fatal("backend called"); return Observation{}, nil }
	passed := mustCompare(t, p, pairs, results(p, pairs))
	if _, err := ReduceCase(p, pairs, passed, check, 20); err == nil || !strings.Contains(err.Error(), "passing") {
		t.Fatalf("passing case: %v", err)
	}
	failed := mustCompare(t, p, pairs, results(p, pairs, "raw"))
	for _, budget := range []int{-1, 1001} {
		if _, err := ReduceCase(p, pairs, failed, check, budget); err == nil {
			t.Errorf("budget %d accepted", budget)
		}
	}
}

func TestInjectedSemanticBugShrinksInputsAndProgram(t *testing.T) {
	p := addition(32)
	p.Expr = Op("add", addition(32).Expr, lit(42))
	pairs := []Pair{{255, 31}, {7, 8}}
	check := checkWith(t, func(Program, []Pair) []string { return []string{"raw"} })
	initial, _ := check(p, pairs)
	reduced, err := ReduceCase(p, pairs, initial, check, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reduced.Inputs, []Pair{{0, 0}}) {
		t.Errorf("inputs %v", reduced.Inputs)
	}
	if !mustComplexity(t, reduced.Program).Less(mustComplexity(t, p)) {
		t.Error("program was not reduced")
	}
	if !reflect.DeepEqual(reduced.Observation.Signature(), []string{"raw"}) || reduced.RejectedErrors != 0 {
		t.Errorf("got %+v", reduced)
	}
}

// fakeBackend records calls and returns check's answer.
type fakeBackend struct {
	calls []programCase
	check func(Program, []Pair) (map[string][]uint64, error)
}

func (f *fakeBackend) Check(p Program, pairs []Pair) (map[string][]uint64, error) {
	f.calls = append(f.calls, programCase{p, pairs})
	return f.check(p, pairs)
}

func runCampaign(t *testing.T, options Options, check func(Program, []Pair) (map[string][]uint64, error), hooks ...func(*Campaign)) (int, *fakeBackend, string) {
	t.Helper()
	backend := &fakeBackend{check: check}
	var stdout bytes.Buffer
	c := NewCampaign(options, t.TempDir(), &stdout)
	c.newBackend = func(string, string, time.Duration) (Checker, error) { return backend, nil }
	for _, hook := range hooks {
		hook(c)
	}
	status, err := c.Run()
	if err != nil {
		t.Fatal(err)
	}
	return status, backend, stdout.String()
}

func campaignReport(t *testing.T, dir string) (string, Report) {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(dir, "campaign-*", "report.json"))
	if len(paths) != 1 {
		t.Fatalf("%d reports", len(paths))
	}
	var report Report
	data, _ := os.ReadFile(paths[0])
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(paths[0]), report
}

func passing(p Program, pairs []Pair) (map[string][]uint64, error) { return results(p, pairs), nil }

func options(artifacts string, mutate func(*Options)) Options {
	o := DefaultOptions()
	o.Artifacts = artifacts
	mutate(&o)
	return o
}

func TestPassingCampaignRecordsExactSourceAndEvidence(t *testing.T) {
	dir := t.TempDir()
	status, backend, _ := runCampaign(t, options(dir, func(o *Options) { o.Seeds, o.Widths, o.Cases = []uint64{4}, []int{64}, 16 }), passing)
	campaign, report := campaignReport(t, dir)
	if status != 0 || report.PlannedPrograms != 1 || report.CompletedPrograms != 1 || report.Results[0].Status != "passed" || report.Schema != CampaignSchema {
		t.Fatalf("status %d report %+v", status, report)
	}
	caseDir := filepath.Join(campaign, "case-0000")
	p, pairs, err := LoadCase(filepath.Join(caseDir, "case.json"))
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(caseDir, "Generated.lean")) != mustRender(t, p) || len(pairs) != 16 || len(backend.calls) != 1 {
		t.Fatal("case evidence differs")
	}
	if _, err := os.Stat(filepath.Join(caseDir, "failure.json")); err == nil {
		t.Fatal("passing case wrote failure.json")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestMismatchStopsCampaignAndPersistsOriginalAndReduction(t *testing.T) {
	mismatch := func(p Program, pairs []Pair) (map[string][]uint64, error) { return results(p, pairs, "raw"), nil }
	dir := t.TempDir()
	status, _, _ := runCampaign(t, options(dir, func(o *Options) {
		o.Seeds, o.Widths, o.Cases, o.Shrink, o.ShrinkAttempts = []uint64{4, 5}, []int{32}, 3, true, 6
	}), mismatch)
	campaign, report := campaignReport(t, dir)
	if status != 1 || report.PlannedPrograms != 2 || report.CompletedPrograms != 1 {
		t.Fatalf("status %d report %+v", status, report)
	}
	result := report.Results[0]
	if result.Status != "mismatch" || !reflect.DeepEqual(result.Channels, []string{"raw"}) {
		t.Fatalf("result %+v", result)
	}
	caseDir := filepath.Join(campaign, "case-0000")
	original, originalInputs, err := LoadCase(filepath.Join(caseDir, "failure.json"))
	if err != nil {
		t.Fatal(err)
	}
	minimized, minimizedInputs, err := LoadCase(filepath.Join(caseDir, "minimized.json"))
	if err != nil {
		t.Fatal(err)
	}
	stored, storedInputs, _ := LoadCase(filepath.Join(caseDir, "case.json"))
	if !stored.Equal(original) || !reflect.DeepEqual(storedInputs, originalInputs) {
		t.Fatal("case.json differs from failure.json")
	}
	if readFile(t, filepath.Join(caseDir, "Generated.lean")) != mustRender(t, original) ||
		readFile(t, filepath.Join(caseDir, "Minimized.lean")) != mustRender(t, minimized) {
		t.Fatal("rendered sources differ")
	}
	if len(minimizedInputs) > len(originalInputs) {
		t.Fatal("inputs grew")
	}
	outputs, _ := mismatch(minimized, minimizedInputs)
	if o := mustCompare(t, minimized, minimizedInputs, outputs); !reflect.DeepEqual(o.Signature(), []string{"raw"}) {
		t.Fatalf("minimized signature %v", o.Signature())
	}
}

func TestExecutionErrorIsSavedAndNeverShrunk(t *testing.T) {
	failing := func(Program, []Pair) (map[string][]uint64, error) {
		return nil, &BackendError{"native-build", "compile failed"}
	}
	dir := t.TempDir()
	status, backend, _ := runCampaign(t, options(dir, func(o *Options) { o.Seeds, o.Widths, o.Shrink = []uint64{0}, []int{32}, true }), failing)
	campaign, report := campaignReport(t, dir)
	if status != 1 || report.Results[0].Status != "error" {
		t.Fatalf("status %d report %+v", status, report)
	}
	failure := map[string]any{}
	json.Unmarshal([]byte(readFile(t, filepath.Join(campaign, "case-0000", "failure.json"))), &failure)
	if !strings.Contains(failure["error"].(string), "native-build") {
		t.Errorf("error %v", failure["error"])
	}
	if _, ok := failure["observation"]; ok {
		t.Error("an error has an observation")
	}
	if _, err := os.Stat(filepath.Join(campaign, "case-0000", "minimized.json")); err == nil {
		t.Error("an error was shrunk")
	}
	if len(backend.calls) != 1 {
		t.Errorf("%d backend calls", len(backend.calls))
	}
}

func TestReplayDoesNotCallGeneratorOrReplaceStoredInputs(t *testing.T) {
	p := addition(64)
	p.Expr = Op("sub", A(), B())
	pairs := []Pair{{maxOf(64), 1<<53 + 3}}
	root := t.TempDir()
	artifact := filepath.Join(root, "saved.json")
	if err := SaveCase(artifact, p, pairs, nil, nil); err != nil {
		t.Fatal(err)
	}
	status, backend, _ := runCampaign(t, options(filepath.Join(root, "replay"), func(o *Options) { o.Replay = artifact }), passing,
		func(c *Campaign) {
			c.generate = func(uint64, int, int) (Program, error) { t.Fatal("generated"); return Program{}, nil }
			c.inputs = func(uint64, int, int) ([]Pair, error) { t.Fatal("inputs"); return nil, nil }
		})
	if status != 0 || len(backend.calls) != 1 || !backend.calls[0].program.Equal(p) || !reflect.DeepEqual(backend.calls[0].inputs, pairs) {
		t.Fatalf("status %d calls %v", status, backend.calls)
	}
}

func TestReplayRejectsGenerationSelectionArguments(t *testing.T) {
	for _, mutate := range []func(*Options){
		func(o *Options) { o.Seeds = []uint64{1} },
		func(o *Options) { o.Widths = []int{32} },
	} {
		o := options(t.TempDir(), func(o *Options) { o.Replay = "unused.json" })
		mutate(&o)
		c := NewCampaign(o, t.TempDir(), &bytes.Buffer{})
		if _, err := c.selected(); err == nil || !strings.Contains(err.Error(), "combined") {
			t.Fatalf("got %v", err)
		}
	}
}
