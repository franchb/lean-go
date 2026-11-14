package moduleir

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var byteProfile = [3]string{SchemaV12, ProfileV10, "UInt8"}

func TestUInt8CatalogAndLiteralBoundary(t *testing.T) {
	for _, op := range []string{"add", "sub", "mul", "decEq", "decLt", "decLe", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight"} {
		name, result, args := "UInt8."+op, "UInt8", []int{1, 2}
		params := []string{"UInt8", "UInt8"}
		if strings.HasPrefix(op, "dec") {
			result = "Bool"
		}
		if op == "complement" {
			args = args[:1]
			params = params[:1]
		}
		leaf, ok := LookupPrimitive(name)
		if !ok || leaf.Result != result || !reflect.DeepEqual(leaf.Params, params) || leaf.Semantics == "" {
			t.Fatal(name, leaf)
		}
		leaf.Params[0] = "mutated"
		fresh, _ := LookupPrimitive(name)
		if fresh.Params[0] != "UInt8" {
			t.Fatal("mutable catalog")
		}
		if err := Validate(scalarProgram(&Value{Kind: "primitive", Name: name, Args: args}, result, byteProfile)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, literal := range []string{"0", "1", "127", "128", "255"} {
		p := scalarProgram(&Value{Kind: "uint8", Name: literal, Args: []int{}}, "UInt8", byteProfile)
		if err := Validate(p); err != nil {
			t.Fatal(literal, err)
		}
		raw, _ := json.Marshal(p)
		if _, err := Decode(bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, literal := range []string{"", "00", "01", "+1", "-0", "-1", " 1", "1 ", "1.0", "1e1", "0x1", "٢", "256", "9999"} {
		if err := Validate(scalarProgram(&Value{Kind: "uint8", Name: literal, Args: []int{}}, "UInt8", byteProfile)); err == nil {
			t.Fatal("invalid literal", literal)
		}
	}
	for _, name := range []string{"UInt8.div", "UInt8.mod", "UInt8.toUInt64", "UInt64.toUInt8", "UInt16.toUInt8", "UInt8.toUInt16", "UInt8.toNat", "UInt8.add._boxed", "Fake.UInt8.add"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("unexpected primitive", name)
		}
	}
}

func TestUInt8CastsAndExactTypes(t *testing.T) {
	for _, cast := range [][3]string{{"UInt8.toUInt32", "UInt8", "UInt32"}, {"UInt32.toUInt8", "UInt32", "UInt8"}} {
		p := scalarProgram(&Value{Kind: "primitive", Name: cast[0], Args: []int{1}}, cast[2], byteProfile)
		p.Functions[0].Params = []Param{{ID: 1, Type: cast[1]}}
		primitive, ok := LookupPrimitive(cast[0])
		if !ok || !reflect.DeepEqual(primitive.Params, []string{cast[1]}) || primitive.Result != cast[2] {
			t.Fatal(primitive)
		}
		if err := Validate(p); err != nil {
			t.Fatal(err)
		}
		p.Functions[0].Params[0].Type = cast[2]
		if err := Validate(p); err == nil {
			t.Fatal("mixed cast argument")
		}
	}
	changes := []func(*Program){
		func(p *Program) { p.Functions[0].Params[0].Type = "UInt32" },
		func(p *Program) { p.Functions[0].Body.Bind.Type = "UInt32" },
		func(p *Program) { p.Functions[0].Body.Value.Args = []int{1} },
		func(p *Program) { p.Profile = "pure-module/v9" },
		func(p *Program) {
			p.Types = []TypeDecl{{Name: "UInt8", Constructors: []Constructor{{Name: "UInt8.mk", Fields: []string{}}}}}
		},
	}
	for i, change := range changes {
		p := scalarProgram(&Value{Kind: "primitive", Name: "UInt8.add", Args: []int{1, 2}}, "UInt8", byteProfile)
		change(p)
		if err := Validate(p); err == nil {
			t.Fatal("invalid byte program", i)
		}
	}
}

func TestLegacySchemasRejectAllUInt8Payloads(t *testing.T) {
	pairs := [][2]string{{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV3}, {SchemaV5, ProfileV3}, {SchemaV6, ProfileV4}, {SchemaV7, ProfileV5}, {SchemaV8, ProfileV6}, {SchemaV9, ProfileV7}, {SchemaV10, ProfileV8}, {SchemaV11, ProfileV9}}
	for _, pair := range pairs {
		for _, kind := range []string{"signature", "literal", "primitive", "cast"} {
			p := scalarProgram(&Value{Kind: "uint8", Name: "1", Args: []int{}}, "UInt8", byteProfile)
			switch kind {
			case "signature":
				p.Functions[0].Body = &Code{Op: "return", Var: 1}
			case "primitive":
				p = scalarProgram(&Value{Kind: "primitive", Name: "UInt8.decEq", Args: []int{1, 2}}, "Bool", byteProfile)
			case "cast":
				p = scalarProgram(&Value{Kind: "primitive", Name: "UInt32.toUInt8", Args: []int{1}}, "UInt8", byteProfile)
				p.Functions[0].Params = []Param{{ID: 1, Type: "UInt32"}}
			}
			if err := Validate(p); err != nil {
				t.Fatal("byte positive control", kind, err)
			}
			p.Schema, p.Profile = pair[0], pair[1]
			if !HasTypedAPI(p.Schema) {
				p.API = nil
			}
			if HasMultipleRoots(p.Schema) {
				p.Roots = []string{p.Root, "Example.second"}
				p.Root = ""
				p.Functions = append(p.Functions, Function{Name: "Example.second", Params: []Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32", Body: &Code{Op: "return", Var: 1}})
				p.Provenance = append(p.Provenance, Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []Rewrite{}})
			}
			if err := Validate(p); err == nil {
				t.Fatal("legacy byte admission", pair, kind)
			}
			raw, _ := json.Marshal(p)
			if _, err := Decode(bytes.NewReader(raw)); err == nil {
				t.Fatal("legacy JSON byte admission", pair, kind)
			}
		}
	}
}

func TestUInt8NameCannotMasqueradeAsLegacyADT(t *testing.T) {
	p := numericProgram(&Value{Kind: "uint32", Name: "1", Args: []int{}}, "UInt32")
	p.Schema, p.Profile = SchemaV11, ProfileV9
	p.Roots = []string{p.Root, "Example.second"}
	p.Root = ""
	p.API = &APIMetadata{Records: map[string][]string{}}
	p.Types = []TypeDecl{{Name: "Byte", Constructors: []Constructor{{Name: "Byte.mk", Fields: []string{"UInt32"}}}}}
	p.Functions[0].Params = []Param{{ID: 1, Type: "Byte"}}
	p.Functions[0].Result = "Byte"
	p.Functions[0].Body = &Code{Op: "return", Var: 1}
	p.Functions = append(p.Functions, Function{Name: "Example.second", Params: []Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32", Body: &Code{Op: "return", Var: 1}})
	p.Provenance = append(p.Provenance, Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []Rewrite{}})
	if err := Validate(p); err != nil {
		t.Fatal("ordinary ADT positive control", err)
	}
	p.Types[0].Name = "UInt8"
	p.Types[0].Constructors[0].Name = "UInt8.mk"
	p.Functions[0].Params[0].Type = "UInt8"
	p.Functions[0].Result = "UInt8"
	if err := Validate(p); err == nil {
		t.Fatal("legacy UInt8 ADT accepted")
	}
}
