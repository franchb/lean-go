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
	for i := range p.Functions[0].Params {
		p.Functions[0].Params[i].Type = width
	}
	return declare(p)
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
				if err := check(p); err != nil {
					t.Fatal(err)
				}
				for n := 0; n <= 3; n++ {
					if n == arity {
						continue
					}
					p := bitwiseProgram(width, operation)
					p.Functions[0].Body.Value.Args = []int{1, 2, 1}[:n]
					if err := check(p); err == nil {
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
					if err := check(p); err == nil {
						t.Fatal("accepted invalid primitive")
					}
				}
			})
		}
	}
	for _, name := range []string{"UInt16.and", "UInt32.and", "UInt64.not", "UInt32.shift_right", "UInt64.xor.injected", "UInt32.LAND"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("unknown name admitted", name)
		}
	}
}

func TestBitwiseRootAndAPIContracts(t *testing.T) {
	single := bitwiseProgram("UInt32", "complement")
	multi := multiRootProgram()
	for _, p := range []*Program{single, multi} {
		raw, _ := json.Marshal(p)
		decoded, err := Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(RootNames(p), RootNames(decoded)) {
			t.Fatal("root selection changed")
		}
		malformed := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"root":""}`)...)
		if _, err := Decode(bytes.NewReader(malformed)); err == nil {
			t.Fatal("accepted irrelevant root field")
		}
		api := p.API
		p.API = nil
		if err := check(p); err == nil {
			t.Fatal("accepted missing API metadata")
		}
		p.API = api
	}
	for _, roots := range [][]string{nil, {}, {"Example.second", "Example.first"}, {"Example.first", "Example.first"}} {
		multi.Roots = roots
		if err := check(multi); err == nil {
			t.Fatal("accepted invalid root cardinality/order", roots)
		}
	}
}
