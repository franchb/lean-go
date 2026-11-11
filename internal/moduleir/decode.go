package moduleir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Decode reads and validates one bounded strict JSON document. Producer metadata
// is traceability, not authentication of these bytes or their proof claims.
func Decode(r io.Reader) (*Program, error) {
	if r == nil {
		return nil, fmt.Errorf("IR: nil reader")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("IR read: %w", err)
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("IR exceeds %d bytes", MaxBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("IR is not UTF-8")
	}
	if err := checkUnicodeEscapes(data); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	nodes := 0
	tree, err := readJSON(d, 0, &nodes)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("IR has trailing JSON or invalid suffix")
	}
	if err = checkShape(tree, "program"); err != nil {
		return nil, err
	}
	var p Program
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("IR field type: %w", err)
	}
	if err = Validate(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// readJSON rejects duplicate decoded keys, including escaped spellings. Its
// parser bound accommodates 256 code levels through cases/alternatives/arrays.
func readJSON(d *json.Decoder, depth int, nodes *int) (any, error) {
	if depth > 2048 {
		return nil, fmt.Errorf("JSON nesting limit exceeded")
	}
	token, err := d.Token()
	if err != nil {
		return nil, fmt.Errorf("IR JSON: %w", err)
	}
	// No admitted payload is nullable. Reject null before encoding/json can
	// silently turn it into a scalar zero value (notably constructor tag 0).
	if token == nil {
		return nil, fmt.Errorf("IR JSON null is not an admitted value")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		*nodes++
		if *nodes > MaxNodes {
			return nil, fmt.Errorf("IR node limit exceeded")
		}
		obj := map[string]any{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("invalid object key")
			}
			if _, exists := obj[key]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", key)
			}
			value, err := readJSON(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			obj[key] = value
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for d.More() {
			value, err := readJSON(d, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			arr = append(arr, value)
		}
		if _, err := d.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter")
	}
}

var objectFields = map[string][]string{
	"program":    {"schema", "profile", "stage", "lean_version", "lean_commit", "module", "root", "inputs", "types", "claims", "rewrites", "provenance", "functions"},
	"provenance": {"name", "dependencies", "rewrites"},
	"param":      {"id", "type"}, "constructor": {"name", "tag", "fields"}, "type": {"name", "constructors"},
	"claim": {"name", "statement", "axioms"}, "rewrite": {"source", "target", "theorem", "axioms"},
	"value": {"kind", "name", "args"}, "alt": {"constructor", "params", "body"}, "function": {"name", "params", "result", "body"},
	"let": {"op", "bind", "value", "next"}, "cases": {"op", "var", "type", "result", "alts"}, "return": {"op", "var"},
	"join": {"op", "join", "next"}, "join_decl": {"id", "params", "body"}, "jump": {"op", "target", "args"},
}

func checkShape(value any, kind string) error {
	obj, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", kind)
	}
	if kind == "code" {
		op, ok := obj["op"].(string)
		if !ok {
			return fmt.Errorf("code requires string op")
		}
		switch op {
		case "let", "cases", "return", "join", "jump":
			kind = op
		default:
			return fmt.Errorf("unsupported code op %q", op)
		}
	}
	fields := objectFields[kind]
	if kind == "program" && (obj["schema"] == SchemaV5 || obj["schema"] == SchemaV6) {
		fields = append(append([]string{}, fields...), "api")
	}
	if len(obj) != len(fields) {
		return fmt.Errorf("%s has missing or irrelevant fields", kind)
	}
	for _, field := range fields {
		if _, ok := obj[field]; !ok {
			return fmt.Errorf("%s missing %s", kind, field)
		}
	}
	numberField := ""
	switch kind {
	case "param", "join_decl":
		numberField = "id"
	case "jump":
		numberField = "target"
	case "constructor":
		numberField = "tag"
	case "cases", "return":
		numberField = "var"
	}
	if numberField != "" {
		if _, ok := obj[numberField].(json.Number); !ok {
			return fmt.Errorf("%s.%s must be a non-null number", kind, numberField)
		}
	}
	check := func(field, child string) error { return checkShape(obj[field], child) }
	list := func(field, child string) error { return checkArray(obj[field], child) }
	switch kind {
	case "program":
		if api, exists := obj["api"]; exists {
			if err := checkAPIShape(api); err != nil {
				return err
			}
		}
		if _, ok := obj["inputs"].(map[string]any); !ok {
			return fmt.Errorf("inputs must be an object")
		}
		for _, pair := range [][2]string{{"types", "type"}, {"claims", "claim"}, {"rewrites", "rewrite"}, {"provenance", "provenance"}, {"functions", "function"}} {
			if err := list(pair[0], pair[1]); err != nil {
				return err
			}
		}
	case "provenance":
		if err := list("dependencies", ""); err != nil {
			return err
		}
		return list("rewrites", "rewrite")
	case "type":
		return list("constructors", "constructor")
	case "constructor":
		return list("fields", "")
	case "claim", "rewrite":
		return list("axioms", "")
	case "value", "jump":
		return list("args", "")
	case "alt", "function", "join_decl":
		if err := list("params", "param"); err != nil {
			return err
		}
		return check("body", "code")
	case "let":
		for _, pair := range [][2]string{{"bind", "param"}, {"value", "value"}, {"next", "code"}} {
			if err := check(pair[0], pair[1]); err != nil {
				return err
			}
		}
	case "cases":
		return list("alts", "alt")
	case "join":
		if err := check("join", "join_decl"); err != nil {
			return err
		}
		return check("next", "code")
	}
	return nil
}
func checkArray(value any, kind string) error {
	arr, ok := value.([]any)
	if !ok {
		return fmt.Errorf("expected non-null array")
	}
	if kind != "" {
		for _, item := range arr {
			if err := checkShape(item, kind); err != nil {
				return err
			}
		}
	}
	return nil
}
