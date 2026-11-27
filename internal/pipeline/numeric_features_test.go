package pipeline

import (
	"encoding/json"
	"testing"
)

// Ports of the feature tests of the retired scripts/test_bitwise.py, test_uint8.py and
// test_width_conversions.py, which need no Lean:
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

// scripts/test_bitwise.py BitwiseFeatureTests.test_exact_names_and_all_operations
func TestBitwiseFeatureNeedsExactNamesOfAllOperations(t *testing.T) {
	var body map[string]any
	for _, width := range []string{"8", "16", "32", "64"} {
		for _, op := range []string{"land", "lor", "xor", "complement", "shiftRight", "shiftLeft"} {
			body = map[string]any{"op": "let", "value": map[string]any{"kind": "primitive", "name": "UInt" + width + "." + op, "args": []any{}},
				"next": map[string]any{"op": "return", "var": 1}}
			program, roots := rawFeatureProgram(body, false)
			assertContains(t, "UInt"+width+"."+op, rawFeatures(t, program, roots), "bitwise")
		}
	}
	for _, name := range []string{"UInt32.land._boxed", "UInt16.and", "Int16.complement", "UInt32.add", "UInt32.div", "UInt32.toUInt64"} {
		obj(body["value"])["name"] = name
		program, roots := rawFeatureProgram(body, false)
		if hasString(rawFeatures(t, program, roots), "bitwise") {
			t.Fatalf("%s declared bitwise", name)
		}
	}
}

// scripts/test_uint8.py ByteFeatureTests.test_executable_byte_types_literals_and_primitives_declare_features
func TestExecutableByteTypesLiteralsAndPrimitivesDeclareFeatures(t *testing.T) {
	casts := []string{"UInt8.toUInt32", "UInt32.toUInt8"}
	var leaves []string
	for _, op := range byteOps {
		leaves = append(leaves, "UInt8."+op)
	}
	leaves = append(leaves, casts...)
	bitwise := map[string]bool{"land": true, "lor": true, "xor": true, "complement": true, "shiftRight": true, "shiftLeft": true}
	for _, multi := range []bool{false, true} {
		for _, name := range leaves {
			family := ""
			if hasString(casts, name) {
				family = "width_conversion"
			} else if bitwise[name[len("UInt8."):]] {
				family = "bitwise"
			}
			leaf := map[string]any{"op": "let", "bind": map[string]any{"type": "UInt32"}, "value": map[string]any{"kind": "primitive", "name": name},
				"next": map[string]any{"op": "return"}}
			for _, body := range []any{leaf, map[string]any{"op": "join", "join": map[string]any{"body": leaf}, "next": map[string]any{"op": "jump"}},
				map[string]any{"op": "cases", "alts": []any{map[string]any{"body": leaf}}}} {
				program, roots := rawFeatureProgram(body, multi)
				features := rawFeatures(t, program, roots)
				assertContains(t, name, features, "uint32")
				// Python: (family in features) == (family is not None).
				if family != "" && !hasString(features, family) {
					t.Fatalf("%s: %v lacks %s", name, features, family)
				}
			}
		}
		for _, position := range []string{"param", "result", "field", "bind", "join", "alt", "literal"} {
			program, roots := rawFeatureProgram(map[string]any{"op": "return"}, multi)
			f := obj(program["functions"].([]any)[0])
			switch position {
			case "param":
				f["params"] = []any{map[string]any{"type": "UInt8"}}
			case "result":
				f["result"] = "UInt8"
			case "field":
				program["types"] = []any{map[string]any{"name": "Packet", "constructors": []any{map[string]any{"fields": []any{"UInt8"}}}}}
			case "bind":
				f["body"] = map[string]any{"op": "let", "bind": map[string]any{"type": "UInt8"}, "value": map[string]any{"kind": "call", "name": "helper"}, "next": map[string]any{"op": "return"}}
			case "join":
				f["body"] = map[string]any{"op": "join", "join": map[string]any{"params": []any{map[string]any{"type": "UInt8"}}, "body": map[string]any{"op": "return"}}, "next": map[string]any{"op": "jump"}}
			case "alt":
				f["body"] = map[string]any{"op": "cases", "alts": []any{map[string]any{"params": []any{map[string]any{"type": "UInt8"}}, "body": map[string]any{"op": "return"}}}}
			case "literal":
				f["body"] = map[string]any{"op": "let", "value": map[string]any{"kind": "uint8", "name": "255"}, "next": map[string]any{"op": "return"}}
			}
			assertContains(t, position, rawFeatures(t, program, roots), "uint8")
		}
		program, roots := rawFeatureProgram(map[string]any{"op": "return"}, multi)
		program["provenance"] = []any{map[string]any{"name": "UInt8.add", "dependencies": []any{"UInt8", "UInt32.toUInt8"}}}
		assertList(t, "provenance only", rawFeatures(t, program, roots))
		for _, name := range []string{"UInt8.add._boxed", "UInt8.toUInt8", "USize.toUInt8", "UInt16.add", "Fake.UInt8.add"} {
			program, roots := rawFeatureProgram(map[string]any{"op": "let", "value": map[string]any{"kind": "primitive", "name": name}, "next": map[string]any{"op": "return"}}, multi)
			assertList(t, name, rawFeatures(t, program, roots))
		}
	}
}

// scripts/test_width_conversions.py WidthFeatureTests.test_executable_casts_declare_feature_in_every_container
func TestExecutableCastsDeclareFeatureInEveryContainer(t *testing.T) {
	converts := func(program map[string]any, roots []string) bool {
		return hasString(rawFeatures(t, program, roots), "width_conversion")
	}
	var leaf map[string]any
	for _, name := range []string{"UInt32.toUInt64", "UInt64.toUInt32", "UInt16.toUInt32"} {
		leaf = map[string]any{"op": "let", "value": map[string]any{"kind": "primitive", "name": name, "args": []any{1}}, "next": map[string]any{"op": "return", "var": 2}}
		bodies := []any{leaf, map[string]any{"op": "join", "join": map[string]any{"body": leaf}, "next": map[string]any{"op": "jump"}},
			map[string]any{"op": "join", "join": map[string]any{"body": map[string]any{"op": "return"}}, "next": leaf},
			map[string]any{"op": "cases", "alts": []any{map[string]any{"body": leaf}}},
			map[string]any{"op": "let", "value": map[string]any{"kind": "primitive", "name": "UInt64.land"}, "next": leaf}}
		for _, multi := range []bool{false, true} {
			for i, body := range bodies {
				program, roots := rawFeatureProgram(body, multi)
				if !converts(program, roots) {
					t.Fatalf("%s in container %d does not declare width_conversion", name, i)
				}
			}
			program, roots := rawFeatureProgram(map[string]any{"op": "return"}, multi)
			program["functions"] = append(program["functions"].([]any), map[string]any{"name": "Example.helper", "body": leaf})
			if !converts(program, roots) {
				t.Fatalf("%s in a helper does not declare width_conversion", name)
			}
			for _, kind := range []string{"call", "constructor"} {
				body := copyJSON(leaf).(map[string]any)
				obj(body["value"])["kind"] = kind
				program, roots := rawFeatureProgram(body, multi)
				program["provenance"] = []any{map[string]any{"name": name, "dependencies": []any{name}}}
				if converts(program, roots) {
					t.Fatalf("%s %s node declared width_conversion", name, kind)
				}
			}
		}
	}
	for _, name := range []string{"UInt32.toUInt32", "UInt64.toUInt64", "UInt16.toUInt16", "USize.toUInt32",
		"UInt32.toUInt64._boxed", "UInt64.toUInt32.injected", "Fake.UInt32.toUInt64",
		"UInt16.toUInt32._boxed", "Fake.UInt16.toUInt32"} {
		obj(leaf["value"])["name"] = name
		program, roots := rawFeatureProgram(leaf, false)
		if converts(program, roots) {
			t.Fatalf("%s declared width_conversion", name)
		}
	}
}
