package pipeline

import (
	"encoding/json"
	"testing"
)

// Ports of BitwiseFeatureTests in scripts/test_bitwise.py that need no Lean:
// the producer declares a feature family only for executable primitive
// leaves, and malformed producer bodies are protocol errors.

// rawFeatureProgram is BitwiseFeatureTests.raw: a producer result with one function.
func rawFeatureProgram(body any, multi bool) (map[string]any, []string) {
	roots := []string{"Example.run"}
	if multi {
		roots = append(roots, "Example.second")
	}
	return map[string]any{"module": "Example", "functions": []any{map[string]any{"name": "Example.run", "body": body}},
		"types": []any{}, "claims": []any{}, "rewrites": []any{}, "provenance": []any{}, "api": map[string]any{"records": map[string]any{}},
		"roots": roots}, roots
}

// completeRaw is BitwiseFeatureTests.complete: the export's completion step.
func completeRaw(t *testing.T, program map[string]any, roots []string) (*Object, error) {
	t.Helper()
	raw, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	return completeProgram(raw, "Example", roots, map[string]string{}, DefaultCompiler)
}

func rawFeatures(t *testing.T, program map[string]any, roots []string) []string {
	t.Helper()
	result, err := completeRaw(t, program, roots)
	if err != nil {
		t.Fatal(err)
	}
	if result.Values["schema"] != Schema || result.Values["profile"] != Profile || Schema != "lean-go-ir/v24" || Profile != "pure-module/v22" {
		t.Fatalf("schema %v profile %v", result.Values["schema"], result.Values["profile"])
	}
	return typesAny(result.Values["features"])
}

// copyJSON deep-copies a JSON-shaped value.
func copyJSON(v any) any {
	data, _ := json.Marshal(v)
	var out any
	json.Unmarshal(data, &out)
	return out
}

func TestOnlyEmittedPrimitiveNodesDeclareBitwise(t *testing.T) {
	leaf := map[string]any{"op": "let", "value": map[string]any{"kind": "primitive", "name": "UInt64.complement", "args": []any{1}},
		"next": map[string]any{"op": "return", "var": 2}}
	bodies := []any{leaf, map[string]any{"op": "join", "join": map[string]any{"body": leaf}, "next": map[string]any{"op": "jump"}},
		map[string]any{"op": "cases", "alts": []any{map[string]any{"body": leaf}}}}
	for _, multi := range []bool{false, true} {
		for _, body := range bodies {
			program, roots := rawFeatureProgram(body, multi)
			assertContains(t, "executable leaf", rawFeatures(t, program, roots), "bitwise")
		}
		// Source evidence and call names do not authorize an executable leaf.
		for _, kind := range []string{"call", "constructor"} {
			body := copyJSON(leaf).(map[string]any)
			obj(body["value"])["kind"] = kind
			program, roots := rawFeatureProgram(body, multi)
			program["provenance"] = []any{map[string]any{"name": "UInt64.complement"}}
			if hasString(rawFeatures(t, program, roots), "bitwise") {
				t.Fatalf("%s node declared bitwise", kind)
			}
		}
		program, roots := rawFeatureProgram(map[string]any{"op": "return", "var": 1}, multi)
		program["functions"] = append(program["functions"].([]any), map[string]any{"name": "Example.helper", "body": leaf})
		assertContains(t, "helper leaf", rawFeatures(t, program, roots), "bitwise")
	}
}

func TestUInt16XorUsesOnlyExecutableLeaves(t *testing.T) {
	for _, multi := range []bool{false, true} {
		value := map[string]any{"kind": "primitive", "name": "UInt16.xor", "args": []any{1, 2}}
		leaf := map[string]any{"op": "let", "value": value, "next": map[string]any{"op": "return"}}
		program, roots := rawFeatureProgram(map[string]any{"op": "return"}, multi)
		program["functions"] = append(program["functions"].([]any), map[string]any{"name": "Example.helper", "body": map[string]any{
			"op": "join", "join": map[string]any{"body": map[string]any{"op": "return"}}, "next": map[string]any{"op": "cases", "alts": []any{map[string]any{"body": leaf}}}}})
		assertList(t, "features", rawFeatures(t, program, roots), "bitwise", "join")
		program["provenance"] = []any{map[string]any{"name": "instXorOpUInt16", "dependencies": []any{"UInt16.xor"}}}
		for _, c := range [][2]string{{"call", "UInt16.xor"}, {"constructor", "UInt16.xor"}, {"primitive", "UInt16.xor._boxed"},
			{"primitive", "Fake.UInt16.xor"}, {"primitive", "instXorOpUInt16"}} {
			value["kind"], value["name"] = c[0], c[1]
			assertList(t, c[0]+" "+c[1], rawFeatures(t, program, roots), "join")
		}
	}
}

// The freshness half of test_module_freshness_and_malformed_error is
// TestFreshnessTracksOriginalSourceAndTrustedProducer (Generate always calls
// VerifyInputs for module IR); this is the malformed-body half.
func TestMalformedProducerBodiesAreExportErrors(t *testing.T) {
	for _, body := range []any{nil, map[string]any{}, map[string]any{"op": "let"}, map[string]any{"op": "join", "join": []any{}},
		map[string]any{"op": "cases", "alts": nil}} {
		program, roots := rawFeatureProgram(body, false)
		if _, err := completeRaw(t, program, roots); err == nil {
			t.Fatalf("malformed body %v accepted", body)
		} else if _, ok := err.(*Error); !ok {
			t.Fatalf("malformed body %v: %T is not an export error", body, err)
		}
	}
}
