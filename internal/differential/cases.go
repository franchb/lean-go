package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
)

// Channels are the compared executions, in signature order: native Lean,
// the metered Go package through its raw (Value) API and its typed API, and
// the native-mode Go package generated from the same IR.
var Channels = []string{"native", "raw", "typed", "native_go"}

// CaseSchema identifies a saved case artifact.
const CaseSchema = "lean-go-differential-case/v1"

// MaxCaseBytes bounds a case artifact read for replay.
const MaxCaseBytes = 2_000_000

// Observation is one comparison of every channel with the model.
type Observation struct {
	Expected   []uint64            `json:"expected"`
	Outputs    map[string][]uint64 `json:"outputs"`
	Mismatches map[string][]int    `json:"mismatches"`
}

// Signature lists the channels that disagree with the model, in channel order.
func (o Observation) Signature() []string {
	out := []string{}
	for _, name := range Channels {
		if len(o.Mismatches[name]) > 0 {
			out = append(out, name)
		}
	}
	return out
}

func sameSignature(a, b []string) bool { return reflect.DeepEqual(a, b) }

// Compare checks the backend outputs and compares them with the model.
// Protocol problems are errors, never a semantic pass.
func Compare(p Program, pairs []Pair, outputs map[string][]uint64) (Observation, error) {
	if err := ValidateInputs(p.Width, pairs); err != nil {
		return Observation{}, err
	}
	if outputs == nil || len(outputs) != len(Channels) {
		return Observation{}, fmt.Errorf("backend must report exactly the %v results", Channels)
	}
	expected := make([]uint64, len(pairs))
	for i, pair := range pairs {
		value, err := Evaluate(p, pair[0], pair[1])
		if err != nil {
			return Observation{}, err
		}
		expected[i] = value
	}
	o := Observation{Expected: expected, Outputs: map[string][]uint64{}, Mismatches: map[string][]int{}}
	for _, name := range Channels {
		values, ok := outputs[name]
		if !ok {
			return Observation{}, fmt.Errorf("backend must report exactly the %v results", Channels)
		}
		if len(values) != len(pairs) {
			return Observation{}, fmt.Errorf("%s: result count does not match inputs", name)
		}
		indexes := []int{}
		for i, value := range values {
			if checkInput(p.Width, value, name) != nil {
				return Observation{}, fmt.Errorf("%s: invalid unsigned result", name)
			}
			if value != expected[i] {
				indexes = append(indexes, i)
			}
		}
		o.Outputs[name] = append([]uint64(nil), values...)
		o.Mismatches[name] = indexes
	}
	return o, nil
}

type caseJSON struct {
	Schema      string       `json:"schema"`
	Program     ProgramJSON  `json:"program"`
	Inputs      []Pair       `json:"inputs"`
	Observation *Observation `json:"observation,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// SaveCase stores the full source AST and inputs; a seed alone is not enough
// for replay. The observation and error are evidence, not replay input.
func SaveCase(path string, p Program, pairs []Pair, o *Observation, failure error) error {
	if err := ValidateInputs(p.Width, pairs); err != nil {
		return err
	}
	program, err := Serialize(p)
	if err != nil {
		return err
	}
	data := caseJSON{Schema: CaseSchema, Program: program, Inputs: pairs, Observation: o}
	if failure != nil {
		data.Error = failure.Error()
	}
	return writeJSON(path, data)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// LoadCase reads a saved case: its program and exact inputs.
func LoadCase(path string) (Program, []Pair, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Program{}, nil, err
	}
	if info.Size() > MaxCaseBytes {
		return Program{}, nil, errors.New("case artifact exceeds 2 MB")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Program{}, nil, err
	}
	value, err := decodeJSON(raw)
	if err != nil {
		return Program{}, nil, err
	}
	data, ok := value.(map[string]any)
	if !ok || data["schema"] != CaseSchema {
		return Program{}, nil, errors.New("unsupported differential case schema")
	}
	for _, key := range []string{"schema", "program", "inputs"} {
		if _, ok := data[key]; !ok {
			return Program{}, nil, errors.New("unexpected differential case fields")
		}
	}
	for key := range data {
		switch key {
		case "schema", "program", "inputs", "observation", "error":
		default:
			return Program{}, nil, errors.New("unexpected differential case fields")
		}
	}
	p, err := Deserialize(data["program"])
	if err != nil {
		return Program{}, nil, err
	}
	pairs, err := ParseInputs(p.Width, data["inputs"])
	if err != nil {
		return Program{}, nil, err
	}
	return p, pairs, nil
}

// InputCandidates first isolates single pairs and halves, then decreases
// values without changing the width.
func InputCandidates(pairs []Pair) [][]Pair {
	var out [][]Pair
	if len(pairs) > 1 {
		for _, pair := range pairs {
			out = append(out, []Pair{pair})
		}
		middle := len(pairs) / 2
		out = append(out, append([]Pair(nil), pairs[:middle]...), append([]Pair(nil), pairs[middle:]...))
	}
	for i, pair := range pairs {
		for side, value := range pair {
			smaller := map[uint64]bool{0: true, 1: true, value / 2: true}
			if value > 0 {
				smaller[value&(value-1)] = true
			}
			for _, s := range sortedUints(smaller) {
				if s >= value {
					continue
				}
				changed := append([]Pair(nil), pairs...)
				changed[i][side] = s
				out = append(out, changed)
			}
		}
	}
	return out
}

func sortedUints(set map[uint64]bool) []uint64 {
	out := make([]uint64, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Reduction is the best counterexample found within a shrink budget.
type Reduction struct {
	Program        Program
	Inputs         []Pair
	Observation    Observation
	Attempts       int
	RejectedErrors int
}

// CheckFunc compiles and runs a candidate and compares it with the model.
type CheckFunc func(Program, []Pair) (Observation, error)

// MaxShrinkAttempts bounds a reduction.
const MaxShrinkAttempts = 1000

// ReduceCase greedily reduces a failure while preserving exactly the
// original set of failing channels. A build, execution or protocol error
// never replaces a semantic mismatch; it counts as a rejected candidate.
// The result is a smaller counterexample, not a globally minimal one.
func ReduceCase(p Program, pairs []Pair, initial Observation, check CheckFunc, attempts int) (Reduction, error) {
	if attempts < 0 || attempts > MaxShrinkAttempts {
		return Reduction{}, fmt.Errorf("shrink attempts must be in 0..%d", MaxShrinkAttempts)
	}
	if err := ValidateInputs(p.Width, pairs); err != nil {
		return Reduction{}, err
	}
	signature := initial.Signature()
	if len(signature) == 0 {
		return Reduction{}, errors.New("cannot shrink a passing case")
	}
	tried, rejected := 0, 0
	current := initial
	type candidate struct {
		program Program
		inputs  []Pair
	}
	for tried < attempts {
		var candidates []candidate
		// Input reduction comes before source reduction; both strictly decrease.
		for _, inputs := range InputCandidates(pairs) {
			candidates = append(candidates, candidate{p, inputs})
		}
		programs, err := ShrinkCandidates(p)
		if err != nil {
			return Reduction{}, err
		}
		for _, program := range programs {
			candidates = append(candidates, candidate{program, pairs})
		}
		accepted := false
		for _, c := range candidates {
			if tried >= attempts {
				break
			}
			tried++
			observation, err := check(c.program, c.inputs)
			if err != nil {
				rejected++
				continue
			}
			if sameSignature(observation.Signature(), signature) {
				p, pairs, current = c.program, c.inputs, observation
				accepted = true
				break
			}
		}
		if !accepted {
			break
		}
	}
	return Reduction{Program: p, Inputs: pairs, Observation: current, Attempts: tried, RejectedErrors: rejected}, nil
}
