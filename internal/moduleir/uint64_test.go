package moduleir

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func scalarProgram(value *Value, result string, param string) *Program {
	p := numericProgram(value, result)
	for i := range p.Functions[0].Params {
		p.Functions[0].Params[i].Type = param
	}
	return declare(p)
}

func uint64Program(value *Value, result string) *Program {
	return scalarProgram(value, result, "UInt64")
}

func TestUInt64Literals(t *testing.T) {
	for _, literal := range []string{"0", "4294967296", "9223372036854775808", "18446744073709551615"} {
		p := uint64Program(&Value{Kind: "uint64", Name: literal, Args: []int{}}, "UInt64")
		if err := check(p); err != nil {
			t.Fatal(literal, err)
		}
		raw, _ := json.Marshal(p)
		decoded, err := Decode(bytes.NewReader(raw))
		if err != nil || decoded.Functions[0].Body.Value.Name != literal {
			t.Fatal("literal lost precision", literal, err)
		}
	}
	for _, literal := range []string{"", "00", "01", "+1", "-0", "-1", " 1", "1 ", "1.0", "1e1", "0x1", "١", "18446744073709551616", "999999999999999999999"} {
		if err := check(uint64Program(&Value{Kind: "uint64", Name: literal, Args: []int{}}, "UInt64")); err == nil {
			t.Fatal("accepted invalid UInt64", literal)
		}
	}
}

func TestUInt64Registry(t *testing.T) {
	for _, op := range []string{"add", "sub", "mul", "shiftLeft", "decEq", "decLt", "decLe"} {
		name, result := "UInt64."+op, "UInt64"
		if strings.HasPrefix(op, "dec") {
			result = "Bool"
		}
		primitive, ok := LookupPrimitive(name)
		if !ok || primitive.Name != name || primitive.Result != result || strings.Join(primitive.Params, ",") != "UInt64,UInt64" || primitive.Semantics == "" {
			t.Fatal("wrong signature", primitive)
		}
		primitive.Params[0] = "UInt32"
		fresh, _ := LookupPrimitive(name)
		if fresh.Params[0] != "UInt64" {
			t.Fatal("mutable registry")
		}
		if err := check(uint64Program(&Value{Kind: "primitive", Name: name, Args: []int{1, 2}}, result)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"UInt64.div._boxed", "UInt64.add._boxed", "UInt64.and", "UInt64.ADD"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("unknown primitive", name)
		}
	}
}

func TestUInt64StrictTypesAndProfiles(t *testing.T) {
	changes := map[string]func(*Program){
		"mixed arguments": func(p *Program) { p.Functions[0].Params[0].Type = "UInt32" },
		"mixed bind":      func(p *Program) { p.Functions[0].Body.Bind.Type = "UInt32" },
		"mixed return":    func(p *Program) { p.Functions[0].Result = "UInt32" },
		"missing api":     func(p *Program) { p.API = nil },
		"null records":    func(p *Program) { p.API.Records = nil },
		"scalar declaration": func(p *Program) {
			p.Types = []TypeDecl{{Name: "UInt64", Constructors: []Constructor{{Name: "UInt64.mk", Fields: []string{}}}}}
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			p := uint64Program(&Value{Kind: "primitive", Name: "UInt64.add", Args: []int{1, 2}}, "UInt64")
			change(p)
			if err := check(p); err == nil {
				t.Fatal("accepted invalid UInt64 program")
			}
		})
	}
}

func TestUInt64StrictDecode(t *testing.T) {
	p := uint64Program(&Value{Kind: "uint64", Name: "18446744073709551615", Args: []int{}}, "UInt64")
	raw, _ := json.Marshal(p)
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"name":"18446744073709551615"`), []byte(`"name":18446744073709551615`), 1),
		bytes.Replace(raw, []byte(`"name":"18446744073709551615"`), []byte(`"name":"18446744073709551615","name":"0"`), 1),
		bytes.Replace(raw, []byte(`"args":[]`), []byte(`"args":null`), 1),
		bytes.Replace(raw, []byte(`,"api":{"records":{}}`), nil, 1),
	} {
		if _, err := Decode(bytes.NewReader(bad)); err == nil {
			t.Fatal("accepted invalid wide JSON", string(bad))
		}
	}
}

func TestUInt64MixedFieldCallAndJoinTypes(t *testing.T) {
	p := uint64Program(&Value{Kind: "primitive", Name: "UInt64.add", Args: []int{1, 2}}, "UInt64")
	p.Functions[0].Params[1].Type = "UInt32"
	p.Functions[0].Body.Value = &Value{Kind: "call", Name: p.Roots[0], Args: []int{1, 2}}
	if err := check(p); err != nil {
		t.Fatal("valid mixed-width function call", err)
	}
	p.Functions[0].Body.Value.Args = []int{2, 1}
	if err := check(p); err == nil {
		t.Fatal("mixed-width call accepted")
	}
	p.Functions[0].Body = &Code{Op: "join", Join: &Join{ID: 3, Params: []Param{{ID: 4, Type: "UInt64"}}, Body: &Code{Op: "return", Var: 4}}, Next: &Code{Op: "jump", Target: 3, Args: []int{1}}}
	if err := check(p); err != nil {
		t.Fatal("wide join", err)
	}
	p.Functions[0].Body.Next.Args = []int{2}
	if err := check(p); err == nil {
		t.Fatal("mixed-width jump accepted")
	}
	p.Types = []TypeDecl{{Name: "Example.Mixed", Constructors: []Constructor{{Name: "Example.Mixed.mk", Fields: []string{"UInt64", "UInt32"}}}}}
	p.API.Records = map[string][]string{"Example.Mixed": {"wide", "small"}}
	p.Functions[0].Result = "Example.Mixed"
	p.Functions[0].Body = &Code{Op: "let", Bind: &Param{ID: 3, Type: "Example.Mixed"}, Value: &Value{Kind: "constructor", Name: "Example.Mixed.mk", Args: []int{1, 2}}, Next: &Code{Op: "return", Var: 3}}
	if err := check(p); err != nil {
		t.Fatal("mixed-width record", err)
	}
	p.Types[0].Constructors[0].Fields = []string{"UInt32", "UInt64"}
	if err := check(p); err == nil {
		t.Fatal("mixed-width constructor fields accepted")
	}
}
