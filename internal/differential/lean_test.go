package differential

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Lean-gated tests: set LEAN_GO_DIFFERENTIAL=1 with the pinned Lean, Lake
// and Go on PATH.

func requireDifferential(t *testing.T) {
	t.Helper()
	if os.Getenv("LEAN_GO_DIFFERENTIAL") != "1" {
		t.Skip("set LEAN_GO_DIFFERENTIAL=1 for compiled generated-program tests")
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

// Ported from test_differential_backend.py NativeSmokeTests.
func TestNativeAndGoMatchIndependentModel(t *testing.T) {
	requireDifferential(t)
	b, err := NewBackend(checkoutRoot(t), t.TempDir(), 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, width := range []int{32, 64} {
		p, err := Generate(0, width, 3)
		if err != nil {
			t.Fatal(err)
		}
		maximum, w := maxOf(width), uint64(width)
		inputs := []Pair{{0, 0}, {0, maximum}, {maximum, w + 1}, {maximum - 1, w}}
		expected := make([]uint64, len(inputs))
		for i, pair := range inputs {
			expected[i] = mustEval(t, p, pair[0], pair[1])
		}
		got, err := b.Check(p, inputs)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string][]uint64{}
		for _, name := range Channels {
			want[name] = expected
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("width %d: got %v, want %v", width, got, want)
		}
		// Coverage only: the independent semantic oracle above never reads IR.
		var ir struct {
			Types, Functions []struct{ Name string }
		}
		data, err := os.ReadFile(filepath.Join(b.LastWork, "program.ir.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &ir); err != nil {
			t.Fatal(err)
		}
		types := map[string]bool{}
		for _, item := range ir.Types {
			types[item.Name] = true
		}
		if !types["Generated.Box"] || !types["Generated.State"] {
			t.Fatalf("width %d: types %v", width, ir.Types)
		}
		for _, item := range ir.Functions {
			seen[item.Name] = true
		}
	}
	for _, name := range []string{"Generated.helper", "Generated.readState", "Generated.unbox"} {
		if !seen[name] {
			t.Errorf("function %s was not extracted", name)
		}
	}
}

// Ported from test_differential.py CompiledFaultInjectionTests.
func TestCompiledMismatchIsReducedAndReplayedWithCleanBackend(t *testing.T) {
	requireDifferential(t)
	directory := t.TempDir()
	faulty, err := NewBackend(checkoutRoot(t), filepath.Join(directory, "faulty"), 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	faulty.ConsumerSource = func(width int) string {
		source := ConsumerSource(width)
		if strings.Count(source, "result.Raw[i] = uint64(value)") != 1 {
			t.Fatal("fault anchor not found exactly once")
		}
		return strings.Replace(source, "result.Raw[i] = uint64(value)", "result.Raw[i] = uint64(value + 1)", 1)
	}
	p, err := Generate(0, 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	pairs := []Pair{{0, 0}, {maxOf(32), 65}}
	check := func(candidate Program, values []Pair) (Observation, error) {
		outputs, err := faulty.Check(candidate, values)
		if err != nil {
			return Observation{}, err
		}
		return Compare(candidate, values, outputs)
	}
	original, err := check(p, pairs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Signature(), []string{"raw"}) {
		t.Fatalf("original signature %v", original.Signature())
	}
	reduced, err := ReduceCase(p, pairs, original, check, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reduced.Observation.Signature(), []string{"raw"}) || reduced.Attempts != 2 || reduced.RejectedErrors != 0 ||
		!reflect.DeepEqual(reduced.Inputs, []Pair{{0, 0}}) {
		t.Fatalf("reduction %+v", reduced)
	}
	if !mustComplexity(t, reduced.Program).Less(mustComplexity(t, p)) {
		t.Fatal("program was not reduced")
	}
	artifact := filepath.Join(directory, "minimized.json")
	if err := SaveCase(artifact, reduced.Program, reduced.Inputs, &reduced.Observation, nil); err != nil {
		t.Fatal(err)
	}
	replayProgram, replayInputs, err := LoadCase(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !replayProgram.Equal(reduced.Program) || !reflect.DeepEqual(replayInputs, reduced.Inputs) {
		t.Fatal("artifact does not replay the reduction")
	}
	clean, err := NewBackend(checkoutRoot(t), filepath.Join(directory, "clean"), 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := clean.Check(replayProgram, replayInputs)
	if err != nil {
		t.Fatal(err)
	}
	replayed := mustCompare(t, replayProgram, replayInputs, outputs)
	if len(replayed.Signature()) != 0 {
		t.Fatalf("clean replay mismatches: %v", replayed.Signature())
	}
	for _, name := range Channels {
		if !reflect.DeepEqual(replayed.Outputs[name], replayed.Expected) {
			t.Fatalf("%s: %v, want %v", name, replayed.Outputs[name], replayed.Expected)
		}
	}
}
