package moduleir

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

var bitwiseOperations = []string{"land", "lor", "xor", "complement", "shiftRight"}

func bitwiseProgram(width, operation string) *Program {
	args := []int{1, 2}
	if operation == "complement" {
		args = []int{1}
	}
	p := numericProgram(&Value{Kind: "primitive", Name: width + "." + operation, Args: args}, width)
	p.Schema, p.Profile = SchemaV8, ProfileV6
	p.API = &APIMetadata{Records: map[string][]string{}}
	for i := range p.Functions[0].Params {
		p.Functions[0].Params[i].Type = width
	}
	return p
}

func TestBitwiseSignaturesAndArguments(t *testing.T) {
	for _, width := range []string{"UInt32", "UInt64"} {
		for _, operation := range bitwiseOperations {
			t.Run(width+"."+operation, func(t *testing.T) {
				p := bitwiseProgram(width, operation)
				primitive, ok := LookupPrimitive(p.Functions[0].Body.Value.Name)
				arity := 2
				if operation == "complement" {
					arity = 1
				}
				if !ok || primitive.Result != width || primitive.Semantics == "" || len(primitive.Params) != arity {
					t.Fatal(primitive)
				}
				for _, param := range primitive.Params {
					if param != width {
						t.Fatal(primitive)
					}
				}
				primitive.Params[0] = "mutated"
				fresh, _ := LookupPrimitive(primitive.Name)
				if fresh.Params[0] != width {
					t.Fatal("mutable registry")
				}
				if err := Validate(p); err != nil {
					t.Fatal(err)
				}
				for n := 0; n <= 3; n++ {
					if n == arity {
						continue
					}
					p := bitwiseProgram(width, operation)
					p.Functions[0].Body.Value.Args = []int{1, 2, 1}[:n]
					if err := Validate(p); err == nil {
						t.Fatalf("accepted arity %d", n)
					}
				}
				for _, change := range []func(*Program){
					func(p *Program) { p.Functions[0].Body.Value.Args = nil },
					func(p *Program) { p.Functions[0].Body.Value.Args[0] = 99 },
					func(p *Program) {
						p.Functions[0].Body.Bind.Type = "UInt32"
						if width == "UInt32" {
							p.Functions[0].Body.Bind.Type = "UInt64"
						}
					},
					func(p *Program) {
						p.Functions[0].Params[0].Type = "UInt32"
						if width == "UInt32" {
							p.Functions[0].Params[0].Type = "UInt64"
						}
					},
					func(p *Program) { p.Functions[0].Body.Value.Name += "._boxed" },
				} {
					p := bitwiseProgram(width, operation)
					change(p)
					if err := Validate(p); err == nil {
						t.Fatal("accepted invalid primitive")
					}
				}
			})
		}
	}
	for _, name := range []string{"UInt16.land", "UInt32.and", "UInt64.not", "UInt32.shift_right", "UInt64.xor.injected", "UInt32.LAND"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("unknown name admitted", name)
		}
	}
}

func TestBitwiseLegacySchemaGate(t *testing.T) {
	pairs := [][2]string{{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV3}, {SchemaV5, ProfileV3}, {SchemaV6, ProfileV4}, {SchemaV7, ProfileV5}}
	for _, pair := range pairs {
		for _, width := range []string{"UInt32", "UInt64"} {
			for _, op := range bitwiseOperations {
				p := bitwiseProgram(width, op)
				p.Schema, p.Profile = pair[0], pair[1]
				if !HasTypedAPI(p.Schema) {
					p.API = nil
				}
				if HasMultipleRoots(p.Schema) {
					p.Roots = []string{p.Root, p.Root + "2"}
					p.Root = ""
					f := p.Functions[0]
					f.Name = p.Roots[1]
					p.Functions = append(p.Functions, f)
					provenance := p.Provenance[0]
					provenance.Name = f.Name
					p.Provenance = append(p.Provenance, provenance)
				}
				if err := Validate(p); err == nil {
					t.Fatal("legacy schema accepted bitwise", pair, width, op)
				}
			}
		}
	}
}

func TestBitwiseSchemaRootAndAPIContracts(t *testing.T) {
	single := bitwiseProgram("UInt32", "complement")
	multi := multiRootProgram()
	multi.Schema, multi.Profile = SchemaV9, ProfileV7
	for _, p := range []*Program{single, multi} {
		if !HasUInt64(p.Schema) || !HasTypedAPI(p.Schema) {
			t.Fatal("missing inherited capability")
		}
		raw, _ := json.Marshal(p)
		decoded, err := Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(RootNames(p), RootNames(decoded)) {
			t.Fatal("root selection changed")
		}
		forbidden := `,"roots":[]`
		if HasMultipleRoots(p.Schema) {
			forbidden = `,"root":""`
		}
		malformed := append(append([]byte{}, raw[:len(raw)-1]...), []byte(forbidden+"}")...)
		if _, err := Decode(bytes.NewReader(malformed)); err == nil {
			t.Fatal("accepted irrelevant root field")
		}
		api := p.API
		p.API = nil
		if err := Validate(p); err == nil {
			t.Fatal("accepted missing API metadata")
		}
		p.API = api
		profile := p.Profile
		for _, bad := range []string{ProfileV4, ProfileV5, ProfileV6, ProfileV7} {
			if bad == profile {
				continue
			}
			p.Profile = bad
			if err := Validate(p); err == nil {
				t.Fatal("accepted schema/profile cross-pair", p.Schema, bad)
			}
		}
		p.Profile = profile
	}
	for _, roots := range [][]string{nil, {}, {"Example.first"}, {"Example.second", "Example.first"}, {"Example.first", "Example.first"}} {
		multi.Roots = roots
		if err := Validate(multi); err == nil {
			t.Fatal("accepted invalid v9 cardinality/order", roots)
		}
	}
}
