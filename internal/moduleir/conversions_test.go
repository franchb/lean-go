package moduleir

import (
	"reflect"
	"testing"
)

func conversionProgram(name string) *Program {
	operand, result := "UInt32", "UInt64"
	if name == "UInt64.toUInt32" {
		operand, result = result, operand
	}
	p := numericProgram(&Value{Kind: "primitive", Name: name, Args: []int{1}}, result)
	p.Functions[0].Params = []Param{{ID: 1, Type: operand}}
	return declare(p)
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
		if err := check(p); err != nil {
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
			if err := check(p); err == nil {
				t.Fatal("accepted invalid conversion", name)
			}
		}
	}
	for _, name := range []string{"UInt32.toUInt32", "UInt64.toUInt64", "UInt16.toUInt16", "UInt32.toUInt128", "UInt64.toNat", "UInt64.toUSize", "UInt32.toUInt64._boxed", "Fake.UInt32.toUInt64", "UInt32.toUint64", "UInt64.toUInt32.injected"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("admitted unknown conversion", name)
		}
		if err := check(conversionProgram(name)); err == nil {
			t.Fatal("validated unknown conversion", name)
		}
	}
}
