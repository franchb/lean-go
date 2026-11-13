package moduleir

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func conversionProgram(name string) *Program {
	operand, result := "UInt32", "UInt64"
	if name == "UInt64.toUInt32" {
		operand, result = result, operand
	}
	p := numericProgram(&Value{Kind: "primitive", Name: name, Args: []int{1}}, result)
	p.Schema, p.Profile = SchemaV10, ProfileV8
	p.API = &APIMetadata{Records: map[string][]string{}}
	p.Functions[0].Params = []Param{{ID: 1, Type: operand}}
	return p
}

func conversionRoots(p *Program) {
	if !HasMultipleRoots(p.Schema) {
		return
	}
	p.Roots = []string{p.Root, p.Root + "2"}
	p.Root = ""
	f := p.Functions[0]
	f.Name = p.Roots[1]
	p.Functions = append(p.Functions, f)
	provenance := p.Provenance[0]
	provenance.Name = f.Name
	p.Provenance = append(p.Provenance, provenance)
}

func TestWidthConversionCatalogAndArguments(t *testing.T) {
	for _, name := range []string{"UInt32.toUInt64", "UInt64.toUInt32"} {
		p := conversionProgram(name)
		primitive, ok := LookupPrimitive(name)
		if !ok || primitive.Name != name || primitive.Result != p.Functions[0].Result || !reflect.DeepEqual(primitive.Params, []string{p.Functions[0].Params[0].Type}) || primitive.Semantics == "" {
			t.Fatal(primitive)
		}
		primitive.Params[0] = "mutated"
		fresh, _ := LookupPrimitive(name)
		if fresh.Params[0] != p.Functions[0].Params[0].Type {
			t.Fatal("mutable catalog")
		}
		if err := Validate(p); err != nil {
			t.Fatal(err)
		}
		for _, change := range []func(*Program){
			func(p *Program) { p.Functions[0].Body.Value.Args = nil },
			func(p *Program) { p.Functions[0].Body.Value.Args = []int{} },
			func(p *Program) { p.Functions[0].Body.Value.Args = []int{1, 1} },
			func(p *Program) { p.Functions[0].Body.Value.Args = []int{99} },
			func(p *Program) { p.Functions[0].Params[0].Type = p.Functions[0].Result },
			func(p *Program) { p.Functions[0].Body.Bind.Type = p.Functions[0].Params[0].Type },
		} {
			p := conversionProgram(name)
			change(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid conversion", name)
			}
		}
	}
	for _, name := range []string{"UInt32.toUInt32", "UInt64.toUInt64", "UInt16.toUInt64", "UInt32.toUInt16", "UInt64.toNat", "UInt32.toUInt64._boxed", "Fake.UInt32.toUInt64", "UInt32.toUint64", "UInt64.toUInt32.injected"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("admitted unknown conversion", name)
		}
		if err := Validate(conversionProgram(name)); err == nil {
			t.Fatal("validated unknown conversion", name)
		}
	}
}

func TestWidthConversionLegacyRejection(t *testing.T) {
	pairs := [][2]string{{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV3}, {SchemaV5, ProfileV3}, {SchemaV6, ProfileV4}, {SchemaV7, ProfileV5}, {SchemaV8, ProfileV6}, {SchemaV9, ProfileV7}}
	for _, pair := range pairs {
		for _, name := range []string{"UInt32.toUInt64", "UInt64.toUInt32"} {
			p := conversionProgram(name)
			p.Schema, p.Profile = pair[0], pair[1]
			if !HasTypedAPI(p.Schema) {
				p.API = nil
			}
			// An old schema may model UInt64 as an ADT; it still cannot admit the cast.
			if !HasUInt64(p.Schema) {
				p.Types = []TypeDecl{{Name: "UInt64", Constructors: []Constructor{{Name: "UInt64.mk", Fields: []string{"UInt32"}}}}}
			}
			conversionRoots(p)
			if err := Validate(p); err == nil {
				t.Fatal("legacy conversion accepted", pair, name)
			}
		}
	}
}

func TestWidthConversionSchemaContracts(t *testing.T) {
	for _, pair := range [][2]string{{SchemaV10, ProfileV8}, {SchemaV11, ProfileV9}} {
		p := conversionProgram("UInt32.toUInt64")
		p.Schema, p.Profile = pair[0], pair[1]
		conversionRoots(p)
		if !HasUInt64(p.Schema) || !HasTypedAPI(p.Schema) || HasMultipleRoots(p.Schema) != (p.Schema == SchemaV11) {
			t.Fatal("capabilities", pair)
		}
		raw, _ := json.Marshal(p)
		if _, err := Decode(bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		forbidden := `,"roots":[]`
		if HasMultipleRoots(p.Schema) {
			forbidden = `,"root":""`
		}
		malformed := append(append([]byte{}, raw[:len(raw)-1]...), []byte(forbidden+"}")...)
		if _, err := Decode(bytes.NewReader(malformed)); err == nil {
			t.Fatal("irrelevant root field")
		}
		api := p.API
		p.API = nil
		if err := Validate(p); err == nil {
			t.Fatal("missing API")
		}
		p.API = api
		for _, bad := range []string{Profile, ProfileV2, ProfileV3, ProfileV4, ProfileV5, ProfileV6, ProfileV7, ProfileV8, ProfileV9} {
			if bad == pair[1] {
				continue
			}
			p.Profile = bad
			if err := Validate(p); err == nil {
				t.Fatal("cross-pair", pair[0], bad)
			}
		}
		p.Profile = pair[1]
		if HasMultipleRoots(p.Schema) {
			for _, roots := range [][]string{nil, {}, {p.Roots[0]}, {p.Roots[1], p.Roots[0]}, {p.Roots[0], p.Roots[0]}} {
				copy := *p
				copy.Roots = roots
				if err := Validate(&copy); err == nil {
					t.Fatal("invalid roots", roots)
				}
			}
		}
	}
	for _, schema := range []string{"lean-go-ir/v12", "lean-go-ir/v100", "lean-go-ir/v11x"} {
		if HasUInt64(schema) || HasTypedAPI(schema) || HasMultipleRoots(schema) {
			t.Fatal("unknown capability", schema)
		}
	}
}
