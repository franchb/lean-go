package moduleir

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

const uint16Profile = "UInt16"

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
			if err := check(p); err != nil {
				t.Fatal(err)
			}
			f.Params[tc.arity-1].Type = "UInt32"
			if err := check(p); err == nil {
				t.Fatal("wrong operand type")
			}
			f.Params[tc.arity-1].Type = tc.operand
			f.Body.Value.Args = args[:tc.arity-1]
			if err := check(p); err == nil {
				t.Fatal("missing operand")
			}
			f.Body.Value.Args = args
			f.Body.Bind.Type = "UInt32"
			if err := check(p); err == nil {
				t.Fatal("wrong bind type")
			}
		})
	}
	for _, op := range []string{"and", "xor._boxed", "neg", "toNat", "toUInt16", "toUInt32._boxed", "toUSize", "shiftLeft._boxed", "shift_right", "log2", "decNe"} {
		name := "UInt16." + op
		if _, ok := LookupPrimitive(name); ok {
			t.Fatalf("unexpected primitive %s", name)
		}
	}
	for _, name := range []string{"Int16.toUInt16", "USize.toUInt16", "Nat.toUInt16", "UInt8.toUInt16._boxed", "Fake.UInt16.lor", "Fake.UInt16.xor", "instXorOpUInt16"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatalf("unexpected primitive %s", name)
		}
	}
}

func TestUInt16LiteralBoundary(t *testing.T) {
	for _, tc := range []struct {
		text string
		ok   bool
	}{
		{"0", true}, {"255", true}, {"256", true}, {"32768", true}, {"65535", true},
		{"65536", false}, {"100000", false}, {"-1", false}, {"+1", false}, {"01", false},
		{"", false}, {" 1", false}, {"1e2", false}, {"0xffff", false}, {"١", false},
	} {
		p := scalarProgram(&Value{Kind: "uint16", Name: tc.text, Args: []int{}}, "UInt16", uint16Profile)
		p.Features = []string{FeatureUInt16}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(bytes.NewReader(raw)); (err == nil) != tc.ok {
			t.Fatalf("literal %q: %v", tc.text, err)
		}
	}
}

func TestUInt16NameCannotMasqueradeAsADT(t *testing.T) {
	p := numericProgram(&Value{Kind: "uint32", Name: "0", Args: []int{}}, "UInt32")
	f := &p.Functions[0]
	f.Body, f.Params, f.Result = &Code{Op: "return", Var: 1}, []Param{{ID: 1, Type: "UInt16"}}, "UInt16"
	if err := check(p); err != nil {
		t.Fatal("scalar signature", err)
	}
	p.Types = []TypeDecl{{Name: "UInt16", Constructors: []Constructor{{Name: "UInt16.mk", Fields: []string{}}}}}
	if err := check(p); err == nil {
		t.Fatal("UInt16 ADT accepted")
	}
}

func TestUInt16ToUInt32Capability(t *testing.T) {
	const name = "UInt16.toUInt32"
	primitive, ok := LookupPrimitive(name)
	if !ok || !slices.Equal(primitive.Params, []string{"UInt16"}) || primitive.Result != "UInt32" || primitive.Semantics != "zero extension: a" {
		t.Fatal(primitive)
	}
	value := &Value{Kind: "primitive", Name: name, Args: []int{1}}
	p := scalarProgram(value, "UInt32", "UInt16")
	p.Functions[0].Params = p.Functions[0].Params[:1]
	if err := check(p); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]int{nil, {}, {1, 1}, {99}} {
		value.Args = args
		if err := check(p); err == nil {
			t.Fatal("accepted arguments", args)
		}
	}
	value.Args = []int{1}
	for _, other := range []string{"UInt8", "UInt32", "UInt64"} {
		p.Functions[0].Params[0].Type = other
		if err := check(p); err == nil {
			t.Fatal("accepted operand", other)
		}
	}
	p.Functions[0].Params[0].Type = "UInt16"
	p.Functions[0].Body.Bind.Type = "UInt16"
	if err := check(p); err == nil {
		t.Fatal("accepted UInt16 result")
	}
}

func TestUInt16XorCapability(t *testing.T) {
	value := &Value{Kind: "primitive", Name: "UInt16.xor", Args: []int{1, 2}}
	p := scalarProgram(value, "UInt16", "UInt16")
	if err := check(p); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]int{nil, {}, {1}, {1, 2, 1}, {1, 99}} {
		value.Args = args
		if err := check(p); err == nil {
			t.Fatal("accepted arguments", args)
		}
	}
	value.Args = []int{1, 2}
	f := &p.Functions[0]
	for _, other := range []string{"UInt8", "UInt32", "UInt64"} {
		for i := range f.Params {
			f.Params[i].Type = other
			if err := check(p); err == nil {
				t.Fatal("accepted operand", i, other)
			}
			f.Params[i].Type = "UInt16"
		}
		f.Body.Bind.Type = other
		if err := check(p); err == nil {
			t.Fatal("accepted result", other)
		}
		f.Body.Bind.Type = "UInt16"
	}
}
