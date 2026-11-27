package differential

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
)

type exprJSON struct {
	Op    string     `json:"op"`
	Args  []exprJSON `json:"args"`
	Value *big.Int   `json:"value"`
}

// ProgramJSON is the serialized form of a program.
type ProgramJSON struct {
	Schema string   `json:"schema"`
	Seed   uint64   `json:"seed"`
	Width  int      `json:"width"`
	Expr   exprJSON `json:"expr"`
	Helper exprJSON `json:"helper"`
}

func exprData(e Expr) exprJSON {
	args := make([]exprJSON, len(e.Args))
	for i, arg := range e.Args {
		args[i] = exprData(arg)
	}
	return exprJSON{Op: e.Op, Args: args, Value: new(big.Int).Set(e.value())}
}

// Serialize validates and converts a program to its JSON form.
func Serialize(p Program) (ProgramJSON, error) {
	if err := Validate(p); err != nil {
		return ProgramJSON{}, err
	}
	return ProgramJSON{Schema: ProgramSchema, Seed: p.Seed, Width: p.Width, Expr: exprData(p.Expr), Helper: exprData(p.Helper)}, nil
}

// decodeJSON decodes data keeping every number exact (json.Number).
func decodeJSON(data []byte) (any, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if d.More() {
		return nil, errors.New("invalid JSON: extra data after the document")
	}
	return value, nil
}

var integerText = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// natural accepts only a JSON integer literal (no sign, fraction or exponent),
// as Python's type(n) is int; booleans and strings are not integers.
func natural(v any) (*big.Int, bool) {
	n, ok := v.(json.Number)
	if !ok || !integerText.MatchString(string(n)) {
		return nil, false
	}
	value, ok := new(big.Int).SetString(string(n), 10)
	return value, ok
}

func exactKeys(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

func parseExpr(data any, budget *int, depth int) (Expr, error) {
	if depth > MaxDepth {
		return Expr{}, errors.New("expression depth limit exceeded")
	}
	*budget--
	if *budget < 0 {
		return Expr{}, errors.New("expression node limit exceeded")
	}
	m, ok := data.(map[string]any)
	if !ok || !exactKeys(m, "op", "args", "value") {
		return Expr{}, errors.New("invalid expression fields")
	}
	op, ok := m["op"].(string)
	n, known := arity[op]
	if !ok || !known {
		return Expr{}, errors.New("unknown expression operation")
	}
	args, ok := m["args"].([]any)
	if !ok || len(args) != n {
		return Expr{}, errors.New("expression arity mismatch")
	}
	value, ok := natural(m["value"])
	if !ok || value.Cmp(MaxLiteral) > 0 {
		return Expr{}, fmt.Errorf("literal must be an integer in 0..%s", MaxLiteral)
	}
	e := Expr{Op: op, Value: value}
	for _, arg := range args {
		parsed, err := parseExpr(arg, budget, depth+1)
		if err != nil {
			return Expr{}, err
		}
		e.Args = append(e.Args, parsed)
	}
	return e, nil
}

// Deserialize validates decoded JSON (numbers as json.Number) as a program.
// Limits apply while parsing, before construction.
func Deserialize(data any) (Program, error) {
	m, ok := data.(map[string]any)
	if !ok || !exactKeys(m, "schema", "seed", "width", "expr", "helper") {
		return Program{}, errors.New("invalid program fields")
	}
	if m["schema"] != ProgramSchema {
		return Program{}, errors.New("unsupported program schema")
	}
	seed, ok := natural(m["seed"])
	if !ok || !seed.IsUint64() {
		return Program{}, errors.New("seed must be an integer in 0..18446744073709551615")
	}
	width, ok := natural(m["width"])
	if !ok || !width.IsInt64() || checkWidth(int(width.Int64())) != nil {
		return Program{}, errors.New("width must be 8, 16, 32 or 64")
	}
	budget := MaxNodes
	expr, err := parseExpr(m["expr"], &budget, 0)
	if err != nil {
		return Program{}, err
	}
	helper, err := parseExpr(m["helper"], &budget, 0)
	if err != nil {
		return Program{}, err
	}
	p := Program{Seed: seed.Uint64(), Width: int(width.Int64()), Expr: expr, Helper: helper}
	if err := Validate(p); err != nil {
		return Program{}, err
	}
	return p, nil
}

// DeserializeBytes decodes and validates a serialized program.
func DeserializeBytes(data []byte) (Program, error) {
	value, err := decodeJSON(data)
	if err != nil {
		return Program{}, err
	}
	return Deserialize(value)
}

// roundTrip serializes and re-reads a program, as a replay would.
func roundTrip(p Program) (Program, error) {
	s, err := Serialize(p)
	if err != nil {
		return Program{}, err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return Program{}, err
	}
	return DeserializeBytes(data)
}

// MaxInputs bounds the input pairs of one case.
const MaxInputs = 4096

// ValidateInputs checks the count and width of typed input pairs.
func ValidateInputs(width int, pairs []Pair) error {
	if len(pairs) < 1 || len(pairs) > MaxInputs {
		return fmt.Errorf("inputs must contain 1..%d pairs", MaxInputs)
	}
	for _, pair := range pairs {
		for _, n := range pair {
			if checkInput(width, n, "input") != nil {
				return errors.New("input is outside its unsigned width")
			}
		}
	}
	return nil
}

// ParseInputs validates decoded JSON as input pairs of the given width:
// a list of 1..4096 two-element lists of exact unsigned integers.
func ParseInputs(width int, data any) ([]Pair, error) {
	values, ok := data.([]any)
	if !ok || len(values) < 1 || len(values) > MaxInputs {
		return nil, fmt.Errorf("inputs must contain 1..%d pairs", MaxInputs)
	}
	limit := new(big.Int).Lsh(big.NewInt(1), uint(width))
	pairs := make([]Pair, len(values))
	for i, value := range values {
		pair, ok := value.([]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("each input must be a pair")
		}
		for j, item := range pair {
			n, ok := natural(item)
			if !ok || n.Cmp(limit) >= 0 {
				return nil, errors.New("input is outside its unsigned width")
			}
			pairs[i][j] = n.Uint64()
		}
	}
	return pairs, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
