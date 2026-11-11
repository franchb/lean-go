package moduleir

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func numericProgram(value *Value, result string) *Program {
	p := validProgram()
	p.Schema, p.Profile = "lean-go-ir/v3", "pure-module/v2"
	p.Types = []TypeDecl{}
	if result == "Bool" {
		p.Types = []TypeDecl{{Name: "Bool", Constructors: []Constructor{{Name: "Bool.false", Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}}}
	}
	p.Functions = []Function{{Name: p.Root, Params: []Param{{ID: 1, Type: "UInt32"}, {ID: 2, Type: "UInt32"}}, Result: result,
		Body: &Code{Op: "let", Bind: &Param{ID: 3, Type: result}, Value: value, Next: &Code{Op: "return", Var: 3}}}}
	return p
}

func TestNumericValues(t *testing.T) {
	for _, literal := range []string{"0", "1", "4294967295"} {
		p := numericProgram(&Value{Kind: "uint32", Name: literal, Args: []int{}}, "UInt32")
		if err := Validate(p); err != nil {
			t.Fatal(literal, err)
		}
		raw, _ := json.Marshal(p)
		if _, err := Decode(bytes.NewReader(raw)); err != nil {
			t.Fatal(literal, err)
		}
	}
	for _, name := range []string{"add", "sub", "mul", "shiftLeft", "decEq", "decLt", "decLe"} {
		result := "UInt32"
		if strings.HasPrefix(name, "dec") {
			result = "Bool"
		}
		if err := Validate(numericProgram(&Value{Kind: "primitive", Name: "UInt32." + name, Args: []int{1, 2}}, result)); err != nil {
			t.Fatal(name, err)
		}
	}
}

func TestPrimitiveRegistry(t *testing.T) {
	for _, name := range []string{"UInt32.add", "UInt32.sub", "UInt32.mul", "UInt32.shiftLeft", "UInt32.decEq", "UInt32.decLt", "UInt32.decLe"} {
		primitive, ok := LookupPrimitive(name)
		result := "UInt32"
		if strings.Contains(name, ".dec") {
			result = "Bool"
		}
		if !ok || primitive.Name != name || primitive.Result != result || strings.Join(primitive.Params, ",") != "UInt32,UInt32" || primitive.Semantics == "" {
			t.Fatal("wrong pinned signature", primitive, ok)
		}
		primitive.Params[0] = "UInt64"
		again, _ := LookupPrimitive(name)
		if again.Params[0] != "UInt32" {
			t.Fatal("mutable registry signature", name)
		}
	}
	for _, name := range []string{"", "UInt32.div", "UInt64.add", "UInt32.and", "UInt32.add._boxed"} {
		if primitive, ok := LookupPrimitive(name); ok {
			t.Fatal("unknown primitive admitted", primitive)
		}
	}
}

func TestRejectNumericValues(t *testing.T) {
	for _, literal := range []string{"", "00", "01", "+1", "-0", "-1", " 1", "1 ", "1.0", "1e1", "0x1", "١", "4294967296", strings.Repeat("9", 512)} {
		t.Run("literal/"+literal, func(t *testing.T) {
			if err := Validate(numericProgram(&Value{Kind: "uint32", Name: literal, Args: []int{}}, "UInt32")); err == nil {
				t.Fatal("accepted noncanonical or out-of-range literal")
			}
		})
	}
	changes := map[string]func(*Program){
		"old profile":      func(p *Program) { p.Schema, p.Profile = Schema, Profile },
		"old schema":       func(p *Program) { p.Schema = Schema },
		"old profile pair": func(p *Program) { p.Profile = Profile },
		"unknown leaf":     func(p *Program) { p.Functions[0].Body.Value.Name = "UInt32.div" },
		"source injection": func(p *Program) { p.Functions[0].Body.Value.Name = "UInt32.add);panic(0)//" },
		"unknown kind":     func(p *Program) { p.Functions[0].Body.Value.Kind = "extern" },
		"unsaturated":      func(p *Program) { p.Functions[0].Body.Value.Args = []int{1} },
		"extra argument":   func(p *Program) { p.Functions[0].Body.Value.Args = []int{1, 2, 1} },
		"nil arguments":    func(p *Program) { p.Functions[0].Body.Value.Args = nil },
		"unknown variable": func(p *Program) { p.Functions[0].Body.Value.Args[0] = 99 },
		"invalid variable": func(p *Program) { p.Functions[0].Body.Value.Args[0] = -1 },
		"wrong result":     func(p *Program) { p.Functions[0].Body.Value.Name = "UInt32.add" },
		"wrong argument":   func(p *Program) { p.Functions[0].Params[0].Type = "Bool" },
		"missing Bool":     func(p *Program) { p.Types = []TypeDecl{} },
		"renamed false":    func(p *Program) { p.Types[0].Constructors[0].Name = "Bool.no" },
		"reversed Bool": func(p *Program) {
			p.Types[0].Constructors[0].Name, p.Types[0].Constructors[1].Name = "Bool.true", "Bool.false"
		},
		"Bool field": func(p *Program) { p.Types[0].Constructors[1].Fields = []string{"UInt32"} },
		"Bool extra": func(p *Program) {
			p.Types[0].Constructors = append(p.Types[0].Constructors, Constructor{Name: "Bool.maybe", Tag: 2, Fields: []string{}})
		},
		"literal arguments": func(p *Program) { p.Functions[0].Body.Value = &Value{Kind: "uint32", Name: "1", Args: []int{1}} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			p := numericProgram(&Value{Kind: "primitive", Name: "UInt32.decEq", Args: []int{1, 2}}, "Bool")
			change(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid numeric program")
			}
		})
	}
	for _, args := range [][]int{nil, {1}} {
		if err := Validate(numericProgram(&Value{Kind: "uint32", Name: "1", Args: args}, "UInt32")); err == nil {
			t.Fatal("accepted literal with absent or nonempty args")
		}
	}
	p := numericProgram(&Value{Kind: "uint32", Name: "1", Args: []int{}}, "UInt32")
	p.Schema, p.Profile = Schema, Profile
	if err := Validate(p); err == nil {
		t.Fatal("old profile accepted a numeric literal")
	}
}

func TestBoolProfileCompatibility(t *testing.T) {
	p := validProgram()
	p.Types = []TypeDecl{{Name: "Bool", Constructors: []Constructor{{Name: "Legacy.flag", Fields: []string{}}}}}
	p.Functions[0].Params[0].Type, p.Functions[0].Result = "Bool", "Bool"
	if err := Validate(p); err != nil {
		t.Fatal("changed old profile Bool admission", err)
	}
	p.Schema, p.Profile = "lean-go-ir/v3", "pure-module/v2"
	if err := Validate(p); err == nil {
		t.Fatal("new profile accepted counterfeit Bool without a comparison")
	}
}

func TestStrictNumericDecode(t *testing.T) {
	raw, _ := json.Marshal(numericProgram(&Value{Kind: "uint32", Name: "1", Args: []int{}}, "UInt32"))
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"name":"1"`), []byte(`"name":1`), 1),
		bytes.Replace(raw, []byte(`"name":"1"`), []byte(`"name":"1","name":"2"`), 1),
		bytes.Replace(raw, []byte(`"args":[]`), []byte(`"args":null`), 1),
		bytes.Replace(raw, []byte(`"args":[]`), []byte(`"args":[],"uint32":1`), 1),
	} {
		if _, err := Decode(bytes.NewReader(bad)); err == nil {
			t.Fatal("accepted malformed literal JSON", string(bad))
		}
	}
}
