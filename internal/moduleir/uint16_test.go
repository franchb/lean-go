package moduleir

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

var uint16Profile = [3]string{SchemaV14, ProfileV12, "UInt16"}

func TestUInt16PrimitiveBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, operand, result string
		arity                 int
	}{
		{"UInt8.toUInt16", "UInt8", "UInt16", 1},
		{"UInt16.toUInt8", "UInt16", "UInt8", 1},
		{"UInt16.lor", "UInt16", "UInt16", 2},
		{"UInt16.shiftLeft", "UInt16", "UInt16", 2},
		{"UInt16.shiftRight", "UInt16", "UInt16", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []int{1, 2}[:tc.arity]
			want := []string{tc.operand, tc.operand}[:tc.arity]
			primitive, ok := LookupPrimitive(tc.name)
			if !ok || primitive.Name != tc.name || !slices.Equal(primitive.Params, want) || primitive.Result != tc.result || primitive.Semantics == "" {
				t.Fatal("signature", primitive)
			}
			p := scalarProgram(&Value{Kind: "primitive", Name: tc.name, Args: args}, tc.result, uint16Profile)
			f := &p.Functions[0]
			f.Params = f.Params[:tc.arity]
			for i := range f.Params {
				f.Params[i].Type = tc.operand
			}
			if err := Validate(p); err != nil {
				t.Fatal(err)
			}
			f.Params[tc.arity-1].Type = "UInt32"
			if err := Validate(p); err == nil {
				t.Fatal("wrong operand type")
			}
			f.Params[tc.arity-1].Type = tc.operand
			f.Body.Value.Args = args[:tc.arity-1]
			if err := Validate(p); err == nil {
				t.Fatal("missing operand")
			}
			f.Body.Value.Args = args
			f.Body.Bind.Type = "UInt32"
			if err := Validate(p); err == nil {
				t.Fatal("wrong bind type")
			}
		})
	}
	for _, op := range []string{"add", "sub", "mul", "div", "mod", "land", "xor", "complement", "decEq", "decLt", "decLe", "toNat", "toUInt32._boxed", "toUInt64", "shiftLeft._boxed", "shift_right"} {
		name := "UInt16." + op
		if _, ok := LookupPrimitive(name); ok {
			t.Fatalf("unexpected primitive %s", name)
		}
	}
	for _, name := range []string{"UInt32.toUInt16", "UInt64.toUInt16", "Nat.toUInt16", "UInt8.toUInt16._boxed", "Fake.UInt16.lor"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatalf("unexpected primitive %s", name)
		}
	}
}

func TestUInt16LiteralAndSchemaBoundary(t *testing.T) {
	for _, pair := range [][2]string{{SchemaV14, ProfileV12}, {SchemaV15, ProfileV13}} {
		for _, tc := range []struct {
			text string
			ok   bool
		}{
			{"0", true}, {"255", true}, {"256", true}, {"32768", true}, {"65535", true},
			{"65536", false}, {"100000", false}, {"-1", false}, {"+1", false}, {"01", false},
			{"", false}, {" 1", false}, {"1e2", false}, {"0xffff", false}, {"١", false},
		} {
			p := scalarProgram(&Value{Kind: "uint16", Name: tc.text, Args: []int{}}, "UInt16", uint16Profile)
			p.Schema, p.Profile = pair[0], pair[1]
			conversionRoots(p)
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(bytes.NewReader(raw)); (err == nil) != tc.ok {
				t.Fatalf("%s literal %q: %v", pair[0], tc.text, err)
			}
			if tc.ok {
				p.Profile = ProfileV11
				if err := Validate(p); err == nil {
					t.Fatal("wrong profile", pair[0])
				}
			}
		}
	}
}

func TestLegacySchemasRejectUInt16(t *testing.T) {
	for _, pair := range [][2]string{
		{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV3}, {SchemaV5, ProfileV3},
		{SchemaV6, ProfileV4}, {SchemaV7, ProfileV5}, {SchemaV8, ProfileV6}, {SchemaV9, ProfileV7},
		{SchemaV10, ProfileV8}, {SchemaV11, ProfileV9}, {SchemaV12, ProfileV10}, {SchemaV13, ProfileV11},
	} {
		// Isolate literal and primitive admission from any scalar in the signature.
		p := numericProgram(&Value{Kind: "uint32", Name: "0", Args: []int{}}, "UInt32")
		p.Schema, p.Profile = pair[0], pair[1]
		if HasTypedAPI(p.Schema) {
			p.API = &APIMetadata{Records: map[string][]string{}}
		}
		conversionRoots(p)
		f := &p.Functions[0]
		f.Body = &Code{Op: "return", Var: 1}
		if HasMultipleRoots(p.Schema) {
			p.Functions[1].Body = &Code{Op: "return", Var: 1}
		}
		if err := Validate(p); err != nil {
			t.Fatal("legacy identity", pair, err)
		}
		for _, val := range []Value{
			{Kind: "uint16", Name: "1", Args: []int{}},
			{Kind: "primitive", Name: "UInt8.toUInt16", Args: []int{1}},
			{Kind: "primitive", Name: "UInt16.toUInt32", Args: []int{1}},
			{Kind: "primitive", Name: "UInt16.toUInt8", Args: []int{1}},
			{Kind: "primitive", Name: "UInt16.lor", Args: []int{1, 2}},
			{Kind: "primitive", Name: "UInt16.shiftLeft", Args: []int{1, 2}},
			{Kind: "primitive", Name: "UInt16.shiftRight", Args: []int{1, 2}},
		} {
			f.Body = &Code{Op: "let", Bind: &Param{ID: 3, Type: "UInt16"}, Value: &val, Next: &Code{Op: "return", Var: 1}}
			err := Validate(p)
			if err == nil || !(strings.Contains(err.Error(), "requires pure-module") || strings.Contains(err.Error(), "require pure-module")) {
				t.Fatalf("%s did not reject %s at the capability boundary: %v", p.Schema, val.Name, err)
			}
		}
		f.Body = &Code{Op: "return", Var: 1}
		f.Params = []Param{{ID: 1, Type: "UInt16"}}
		f.Result = "UInt16"
		if err := Validate(p); err == nil {
			t.Fatal("legacy scalar signature", pair)
		}
		p.Types = []TypeDecl{{Name: "UInt16", Constructors: []Constructor{{Name: "UInt16.mk", Fields: []string{}}}}}
		if err := Validate(p); err != nil {
			t.Fatal("legacy explicit UInt16 ADT", pair, err)
		}
	}
}

func TestUInt16ToUInt32Capability(t *testing.T) {
	const name = "UInt16.toUInt32"
	primitive, ok := LookupPrimitive(name)
	if !ok || !slices.Equal(primitive.Params, []string{"UInt16"}) || primitive.Result != "UInt32" || primitive.Semantics != "zero extension: a" {
		t.Fatal(primitive)
	}
	for _, pair := range [][2]string{{SchemaV16, ProfileV14}, {SchemaV17, ProfileV15}} {
		value := &Value{Kind: "primitive", Name: name, Args: []int{1}}
		p := scalarProgram(value, "UInt32", [3]string{pair[0], pair[1], "UInt16"})
		p.Functions[0].Params = p.Functions[0].Params[:1]
		conversionRoots(p)
		if err := Validate(p); err != nil {
			t.Fatal(pair, err)
		}
		for _, args := range [][]int{nil, {}, {1, 1}, {99}} {
			value.Args = args
			if err := Validate(p); err == nil {
				t.Fatal("accepted arguments", args)
			}
		}
		value.Args = []int{1}
		for _, other := range []string{"UInt8", "UInt32", "UInt64"} {
			p.Functions[0].Params[0].Type = other
			if err := Validate(p); err == nil {
				t.Fatal("accepted operand", other)
			}
		}
		p.Functions[0].Params[0].Type = "UInt16"
		p.Functions[0].Body.Bind.Type = "UInt16"
		if err := Validate(p); err == nil {
			t.Fatal("accepted UInt16 result")
		}
		p.Functions[0].Body.Bind.Type = "UInt32"
		if HasMultipleRoots(p.Schema) {
			p.Schema, p.Profile = SchemaV15, ProfileV13
		} else {
			p.Schema, p.Profile = SchemaV14, ProfileV12
		}
		if err := Validate(p); err == nil || !strings.Contains(err.Error(), name+" requires") {
			t.Fatal("older UInt16 profile", p.Schema, err)
		}
	}
}
