package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
)

// The result protocol and the process handling of the backend. The timeout
// test of the process group is in internal/devexec; the native smoke test is
// in lean_test.go.

func TestExactUint64Values(t *testing.T) {
	values := []uint64{0, 9007199254740993, 18446744073709551615}
	text, _ := json.Marshal(values)
	goOut := fmt.Sprintf(`{"raw":%s,"typed":%s,"raw_state_machine":%s,"typed_state_machine":%s,"native_go":%s}`, text, text, text, text, text)
	got, err := DecodeResults(string(text), goOut, 3, 64)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]uint64{"native": values, "raw": values, "typed": values, "raw_state_machine": values, "typed_state_machine": values, "native_go": values}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestRejectsWrongCountTypeWidthAndShape(t *testing.T) {
	var backendErr *BackendError
	for _, values := range []string{`[]`, `[true]`, `[1.0]`, `[-1]`, `[4294967296]`, `["1"]`, `null`} {
		if _, err := DecodeResults(values, `{"raw":[1],"typed":[1],"raw_state_machine":[1],"typed_state_machine":[1],"native_go":[1]}`, 1, 32); !errors.As(err, &backendErr) {
			t.Errorf("native %s: %v", values, err)
		}
	}
	const rest = `"raw_state_machine":[1],"typed_state_machine":[1]`
	if _, err := DecodeResults(`[1]`, `{"raw":[1],"typed":[1],`+rest+`,"native_go":[1]}`, 1, 32); err != nil {
		t.Fatalf("valid output rejected: %v", err)
	}
	for _, raw := range []string{`{}`, `{"raw":[1]}`, `{"raw":[1],"typed":[1],"native_go":[1]}`,
		`{"raw":[1],"typed":[1],"raw_state_machine":[1],"native_go":[1]}`,
		`{"raw":[1],"typed":[1],` + rest + `,"native_go":[1],"extra":[]}`,
		`{"raw":[true],"typed":[1],` + rest + `,"native_go":[1]}`, `{"raw":[1],"typed":[1.0],` + rest + `,"native_go":[1]}`,
		`{"raw":[1],"typed":[1],` + rest + `,"native_go":[1e0]}`,
		`{"raw":[1],"typed":[1],"raw_state_machine":[2],"typed_state_machine":["1"],"native_go":[1]}`,
		`{"raw":[1],"typed":[1],"raw_state_machine":[4294967296],"typed_state_machine":[1],"native_go":[1]}`, `noise`} {
		if _, err := DecodeResults(`[1]`, raw, 1, 32); !errors.As(err, &backendErr) {
			t.Errorf("go %s: %v", raw, err)
		}
	}
}

type processCall struct {
	command []string
	dir     string
	env     []string
	stdin   string
	timeout time.Duration
}

// testBackend is a backend whose processes are answered by responses.
func testBackend(t *testing.T, responses ...any) (*Backend, *[]processCall) {
	t.Helper()
	dir := t.TempDir()
	b, err := NewBackend(dir, filepath.Join(dir, "attempts"), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b.LastWork = dir
	calls := &[]processCall{}
	b.process = func(command []string, cwd string, env []string, stdin string, timeout time.Duration) (devexec.Result, error) {
		*calls = append(*calls, processCall{command, cwd, env, stdin, timeout})
		if len(responses) == 0 {
			t.Fatalf("unexpected command %v", command)
		}
		response := responses[0]
		responses = responses[1:]
		switch r := response.(type) {
		case string:
			return devexec.Result{Stdout: r}, nil
		case devexec.Result:
			return r, nil
		case error:
			return devexec.Result{Stdout: "started", Stderr: "slow", Code: -1}, r
		}
		panic("bad response")
	}
	return b, calls
}

func readRecords(t *testing.T, dir string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(readFile(t, filepath.Join(dir, "commands.jsonl"))), "\n") {
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		out = append(out, record)
	}
	return out
}

func TestForcesPureGoAndLogsCommands(t *testing.T) {
	b, calls := testBackend(t, devexec.Result{Stdout: "[1]", Stderr: "warning"})
	out, err := b.run("example", []string{"program"}, b.LastWork, "input", true)
	if err != nil || out != "[1]" {
		t.Fatalf("%q %v", out, err)
	}
	call := (*calls)[0]
	if call.timeout != 2*time.Second || call.stdin != "input" {
		t.Errorf("call %+v", call)
	}
	for key, want := range map[string]string{"CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOPROXY": "off",
		"PATH": filepath.Join(b.LastWork, "no-producer-tools")} {
		if got := lookup(call.env, key); got != want {
			t.Errorf("%s=%q, want %q", key, got, want)
		}
	}
	record := readRecords(t, b.LastWork)[0]
	if !reflect.DeepEqual(record["command"], []any{"program"}) || record["stdout"] != "[1]" || record["stderr"] != "warning" {
		t.Errorf("record %v", record)
	}
}

func TestNonzeroExitIsNotASuccessfulSample(t *testing.T) {
	b, _ := testBackend(t, devexec.Result{Stdout: "context", Stderr: "unsupported", Code: 1})
	_, err := b.run("extract", []string{"compiler"}, b.LastWork, "", false)
	var backendErr *BackendError
	if !errors.As(err, &backendErr) || backendErr.Stage != "extract" || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("got %v", err)
	}
}

func TestTimeoutPreservesPartialDiagnostics(t *testing.T) {
	b, _ := testBackend(t, &devexec.TimeoutError{Command: []string{"compiler"}, Timeout: 2 * time.Second})
	_, err := b.run("native-build", []string{"compiler"}, b.LastWork, "", false)
	var backendErr *BackendError
	if !errors.As(err, &backendErr) || backendErr.Stage != "native-build" {
		t.Fatalf("got %v", err)
	}
	record := readRecords(t, b.LastWork)[0]
	if record["stdout"] != "started" || record["stderr"] != "slow" || record["error"] == nil {
		t.Errorf("record %v", record)
	}
}

func TestMissingToolAndWrongVersionFail(t *testing.T) {
	b, _ := testBackend(t)
	b.lookPath = func(name string) (string, error) {
		if name == "go" {
			return "/tools/go", nil
		}
		return "", errors.New("missing")
	}
	if err := b.preflight(); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("got %v", err)
	}
	if b.tools != nil {
		t.Fatal("tools recorded after a failure")
	}
	compiler, _ := pipeline.SelectedCompiler()
	lean := fmt.Sprintf("Lean (version %s, x86_64, commit %s, Release)", compiler.Version, compiler.Commit)
	b, calls := testBackend(t, lean, "go version go1.26.0 linux/amd64")
	b.lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	if err := b.preflight(); err == nil || !strings.Contains(err.Error(), "expected Go") {
		t.Fatalf("got %v", err)
	}
	if b.tools != nil || len(*calls) != 2 {
		t.Fatal("tools recorded or lean-go built after a failure")
	}
	b, _ = testBackend(t, "Lean (version 4.0.0, x86_64, commit "+strings.Repeat("0", 40)+", Release)")
	b.lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	if err := b.preflight(); err == nil || b.tools != nil {
		t.Fatalf("wrong Lean accepted: %v", err)
	}
}

// NaN, infinity and booleans cannot be a time.Duration, and the command line
// accepts only integer seconds in 1..3600.
func TestTimeoutMustBePositive(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1} {
		if _, err := NewBackend(t.TempDir(), t.TempDir(), timeout); err == nil {
			t.Errorf("timeout %v accepted", timeout)
		}
	}
}

func TestLeanReferenceHasExactInputLiterals(t *testing.T) {
	source := ReferenceSource(64, []Pair{{maxOf(64), maxOf(64)}})
	for _, want := range []string{"UInt64 × UInt64", "18446744073709551615", "Generated.run pair.1 pair.2"} {
		if !strings.Contains(source, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestCheckBuildsNativeAndIsolatedRawTypedAndNativeConsumer(t *testing.T) {
	b, calls := testBackend(t, "", "[3]", "", "", "", `{"raw":[3],"typed":[3],"raw_state_machine":[3],"typed_state_machine":[3],"native_go":[3]}`)
	b.tools = map[string]string{"go": "/tools/go", "lean": "/tools/lean", "lake": "/tools/lake", "lean-go": "/tools/lean-go"}
	var emitted [][2]string
	b.StateMachine = func(ir, out string) error {
		emitted = append(emitted, [2]string{ir, out})
		return nil
	}
	p := Program{Width: 32, Expr: Op("add", A(), B()), Helper: A()}
	result, err := b.Check(p, []Pair{{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]uint64{"native": {3}, "raw": {3}, "typed": {3}, "raw_state_machine": {3}, "typed_state_machine": {3}, "native_go": {3}}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("got %v", result)
	}
	var stages []string
	for _, record := range readRecords(t, b.LastWork) {
		stages = append(stages, record["stage"].(string))
	}
	if !reflect.DeepEqual(stages, []string{"native-build", "native-run", "extract", "native-emit", "state-machine-emit", "go-build", "go-run"}) {
		t.Fatalf("stages %v", stages)
	}
	if !reflect.DeepEqual((*calls)[0].command, []string{"/tools/lake", "build", "reference"}) {
		t.Errorf("native build %v", (*calls)[0].command)
	}
	extract := (*calls)[2].command
	if extract[0] != "/tools/lean-go" || extract[1] != "generate" || !strings.Contains(strings.Join(extract, " "), "--mode metered") {
		t.Errorf("extract %v", extract)
	}
	if emit := (*calls)[3].command; !strings.Contains(strings.Join(emit, " "), "emit --in") || emit[len(emit)-1] != "native" {
		t.Errorf("native emit %v", emit)
	}
	last := (*calls)[len(*calls)-1]
	if lookup(last.env, "PATH") != filepath.Join(b.LastWork, "consumer", "no-producer-tools") {
		t.Errorf("go-run PATH %q", lookup(last.env, "PATH"))
	}
	var stdin any
	if err := json.Unmarshal([]byte(last.stdin), &stdin); err != nil || !reflect.DeepEqual(stdin, []any{[]any{1.0, 2.0}}) {
		t.Errorf("stdin %q", last.stdin)
	}
	entries, _ := os.ReadDir(filepath.Join(b.LastWork, "source"))
	if len(entries) != 1 || entries[0].Name() != "Generated.lean" {
		t.Errorf("source entries %v", entries)
	}
	if readFile(t, filepath.Join(b.LastWork, "source", "Generated.lean")) != readFile(t, filepath.Join(b.LastWork, "native", "Generated.lean")) {
		t.Error("native and extracted sources differ")
	}
	consumer := readFile(t, filepath.Join(b.LastWork, "consumer", "main.go"))
	// The state-machine package is generated in process from the IR lean-go wrote.
	if !reflect.DeepEqual(emitted, [][2]string{{filepath.Join(b.LastWork, "program.ir.json"), filepath.Join(b.LastWork, "consumer", "statemachine")}}) {
		t.Errorf("state machine emitted %v", emitted)
	}
	for _, want := range []string{"x.RunTyped", "x.Run(args", "s.RunTyped(pair[0], pair[1], stateLimits)", "s.Run(stateArgs, stateLimits)",
		`s "differentialconsumer/statemachine"`, "n.Run(pair[0], pair[1])"} {
		if !strings.Contains(consumer, want) {
			t.Errorf("consumer lacks %q", want)
		}
	}
}

func TestRejectsInvalidInputsBeforeProcesses(t *testing.T) {
	b, calls := testBackend(t)
	p := Program{Width: 32, Expr: Op("add", A(), B()), Helper: A()}
	for _, inputs := range [][]Pair{{}, {{1 << 32, 0}}, make([]Pair, 4097)} {
		if _, err := b.Check(p, inputs); err == nil {
			t.Errorf("%d inputs accepted", len(inputs))
		}
	}
	// An invalid program is rejected before rendering or any process.
	if _, err := b.Check(Program{Width: 32, Expr: Op("unknown"), Helper: A()}, []Pair{{1, 2}}); err == nil {
		t.Error("invalid program accepted")
	}
	if len(*calls) != 0 {
		t.Fatalf("processes ran: %v", *calls)
	}
}
