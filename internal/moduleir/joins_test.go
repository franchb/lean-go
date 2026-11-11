package moduleir

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func joinDocument(t *testing.T, body string) []byte {
	t.Helper()
	p := numericProgram(&Value{Kind: "uint32", Name: "1", Args: []int{}}, "UInt32")
	p.Schema, p.Profile = "lean-go-ir/v4", "pure-module/v3"
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["functions"].([]any)[0].(map[string]any)["body"] = json.RawMessage(body)
	raw, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func joinProgram() *Program {
	p := numericProgram(&Value{Kind: "uint32", Name: "1", Args: []int{}}, "UInt32")
	p.Schema, p.Profile = SchemaV4, ProfileV3
	p.Functions[0].Body = &Code{Op: "join", Join: &Join{ID: 3, Params: []Param{{ID: 4, Type: "UInt32"}},
		Body: &Code{Op: "let", Bind: &Param{ID: 5, Type: "UInt32"}, Value: &Value{Kind: "primitive", Name: "UInt32.add", Args: []int{4, 1}}, Next: &Code{Op: "return", Var: 5}}},
		Next: &Code{Op: "jump", Target: 3, Args: []int{2}}}
	return p
}

func TestLexicalJoins(t *testing.T) {
	for name, change := range map[string]func(*Program){
		"capture and numeric body": func(*Program) {},
		"unused body":              func(p *Program) { p.Functions[0].Body.Next = &Code{Op: "return", Var: 1} },
		"nested earlier target": func(p *Program) {
			p.Functions[0].Body.Next = &Code{Op: "join", Join: &Join{ID: 6, Params: []Param{{ID: 7, Type: "UInt32"}}, Body: &Code{Op: "jump", Target: 3, Args: []int{7}}}, Next: &Code{Op: "jump", Target: 6, Args: []int{2}}}
		},
		"nested parameter capture": func(p *Program) {
			p.Functions[0].Body.Join.Body = &Code{Op: "join", Join: &Join{ID: 6, Params: []Param{}, Body: &Code{Op: "return", Var: 4}}, Next: &Code{Op: "jump", Target: 6, Args: []int{}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := joinProgram()
			change(p)
			if err := Validate(p); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRejectMalformedJoins(t *testing.T) {
	tests := map[string]func(*Code){
		"missing declaration":          func(c *Code) { c.Join = nil },
		"missing body":                 func(c *Code) { c.Join.Body = nil },
		"missing continuation":         func(c *Code) { c.Next = nil },
		"missing params":               func(c *Code) { c.Join.Params = nil },
		"zero join":                    func(c *Code) { c.Join.ID = 0 },
		"negative join":                func(c *Code) { c.Join.ID = -1 },
		"join collides with variable":  func(c *Code) { c.Join.ID = 1 },
		"parameter collides with join": func(c *Code) { c.Join.Params[0].ID = 3 },
		"let collides with join":       func(c *Code) { c.Join.Body.Bind.ID = 3 },
		"invalid parameter type":       func(c *Code) { c.Join.Params[0].Type = "Unknown" },
		"duplicate parameter":          func(c *Code) { c.Join.Params = append(c.Join.Params, c.Join.Params[0]) },
		"parameter leaks to next":      func(c *Code) { c.Next.Args[0] = 4 },
		"body binding leaks to next":   func(c *Code) { c.Next.Args[0] = 5 },
		"join used as variable":        func(c *Code) { c.Next.Args[0] = 3 },
		"variable used as join":        func(c *Code) { c.Next.Target = 1 },
		"unknown target":               func(c *Code) { c.Next.Target = 99 },
		"negative target":              func(c *Code) { c.Next.Target = -1 },
		"nil arguments":                func(c *Code) { c.Next.Args = nil },
		"missing argument":             func(c *Code) { c.Next.Args = []int{} },
		"extra argument":               func(c *Code) { c.Next.Args = []int{1, 2} },
		"unknown argument":             func(c *Code) { c.Next.Args[0] = 99 },
		"zero argument":                func(c *Code) { c.Next.Args[0] = 0 },
		"self jump":                    func(c *Code) { c.Join.Body = &Code{Op: "jump", Target: 3, Args: []int{4}} },
		"hidden self jump": func(c *Code) {
			c.Join.Body = &Code{Op: "join", Join: &Join{ID: 6, Params: []Param{}, Body: &Code{Op: "jump", Target: 3, Args: []int{4}}}, Next: &Code{Op: "jump", Target: 6, Args: []int{}}}
		},
		"forward jump": func(c *Code) {
			c.Join.Body = &Code{Op: "jump", Target: 6, Args: []int{}}
			c.Next = &Code{Op: "join", Join: &Join{ID: 6, Params: []Param{}, Body: &Code{Op: "return", Var: 1}}, Next: &Code{Op: "jump", Target: 3, Args: []int{2}}}
		},
		"body join escapes": func(c *Code) {
			c.Join.Body = &Code{Op: "join", Join: &Join{ID: 6, Params: []Param{}, Body: &Code{Op: "return", Var: 4}}, Next: &Code{Op: "jump", Target: 6, Args: []int{}}}
			c.Next = &Code{Op: "jump", Target: 6, Args: []int{}}
		},
		"duplicate join": func(c *Code) {
			c.Next = &Code{Op: "join", Join: &Join{ID: 3, Params: []Param{}, Body: &Code{Op: "return", Var: 1}}, Next: &Code{Op: "return", Var: 1}}
		},
		"continuation capture": func(c *Code) {
			c.Join.Body = &Code{Op: "return", Var: 6}
			c.Next = &Code{Op: "let", Bind: &Param{ID: 6, Type: "UInt32"}, Value: &Value{Kind: "uint32", Name: "0", Args: []int{}}, Next: c.Next}
		},
		"invalid unused body": func(c *Code) { c.Join.Body = &Code{Op: "return", Var: 99}; c.Next = &Code{Op: "return", Var: 1} },
		"cycle through body":  func(c *Code) { c.Join.Body = c },
		"cycle through next":  func(c *Code) { c.Next = c },
		"shared code":         func(c *Code) { c.Join.Body = &Code{Op: "return", Var: 1}; c.Next = c.Join.Body },
		"join carries args":   func(c *Code) { c.Args = []int{} },
		"join carries target": func(c *Code) { c.Target = 3 },
		"join carries value":  func(c *Code) { c.Value = &Value{} },
		"jump carries next":   func(c *Code) { c.Next.Next = &Code{Op: "return", Var: 1} },
		"jump carries join":   func(c *Code) { c.Next.Join = c.Join },
		"jump carries var":    func(c *Code) { c.Next.Var = 1 },
		"let carries target":  func(c *Code) { c.Join.Body.Target = 3 },
		"return carries args": func(c *Code) { c.Join.Body.Next.Args = []int{} },
		"return carries join": func(c *Code) { c.Join.Body.Next.Join = c.Join },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			p := joinProgram()
			mutate(p.Functions[0].Body)
			if err := Validate(p); err == nil {
				t.Fatal("accepted malformed join")
			}
		})
	}
}

func TestJoinScopeAcrossCasesAndFunctions(t *testing.T) {
	p := joinProgram()
	p.Types = []TypeDecl{{Name: "Flag", Constructors: []Constructor{{Name: "Flag.no", Fields: []string{}}, {Name: "Flag.yes", Tag: 1, Fields: []string{}}}}}
	p.Functions[0].Params[0].Type = "Flag"
	c := p.Functions[0].Body
	c.Join.Body = &Code{Op: "return", Var: 4}
	c.Next.Args[0] = 1
	if err := Validate(p); err == nil {
		t.Fatal("accepted mistyped jump argument")
	}
	c.Next.Args[0] = 2
	c.Join.Body = &Code{Op: "return", Var: 1}
	if err := Validate(p); err == nil {
		t.Fatal("accepted wrong join result")
	}
	c.Join.Body = &Code{Op: "return", Var: 4}
	p.Functions[0].Body = &Code{Op: "cases", Var: 1, Type: "Flag", Result: "UInt32", Alts: []Alt{
		{Constructor: "Flag.no", Params: []Param{}, Body: c},
		{Constructor: "Flag.yes", Params: []Param{}, Body: &Code{Op: "jump", Target: 3, Args: []int{2}}},
	}}
	if err := Validate(p); err == nil {
		t.Fatal("accepted sibling branch target")
	}
	p.Functions[0].Body.Alts[1].Body = &Code{Op: "return", Var: 2}
	if err := Validate(p); err != nil {
		t.Fatal("valid branch-local join", err)
	}
	p.Functions[0].Body.Target = 3
	if err := Validate(p); err == nil {
		t.Fatal("accepted irrelevant cases target")
	}
	p = joinProgram()
	p.Functions = append(p.Functions, Function{Name: "Example.second", Params: []Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32", Body: &Code{Op: "jump", Target: 3, Args: []int{1}}})
	p.Provenance = append(p.Provenance, Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []Rewrite{}})
	p.Functions[0].Body.Join.Body.Value = &Value{Kind: "call", Name: "Example.second", Args: []int{4}}
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "jump") {
		t.Fatal("accepted cross-function jump", err)
	}
}

func TestJoinClosureAndProfiles(t *testing.T) {
	p := joinProgram()
	// The only mention of this type is an unused join parameter.
	p.Types = []TypeDecl{{Name: "Unused", Constructors: []Constructor{{Name: "Unused.mk", Fields: []string{}}}}}
	p.Functions[0].Body.Join.Params[0].Type = "Unused"
	p.Functions[0].Body.Join.Body.Value = &Value{Kind: "call", Name: "Example.second", Args: []int{1}}
	p.Functions[0].Body.Next = &Code{Op: "return", Var: 1}
	p.Functions = append(p.Functions, Function{Name: "Example.second", Params: []Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32", Body: &Code{Op: "return", Var: 1}})
	p.Provenance = append(p.Provenance, Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []Rewrite{}})
	if err := Validate(p); err != nil {
		t.Fatal("unused join type/call closure omitted", err)
	}
	for _, pair := range [][2]string{{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV2}, {SchemaV3, ProfileV3}} {
		p := joinProgram()
		p.Schema, p.Profile = pair[0], pair[1]
		if err := Validate(p); err == nil {
			t.Fatal("accepted incompatible join profile", pair)
		}
	}
	p = numericProgram(&Value{Kind: "primitive", Name: "UInt32.decLt", Args: []int{1, 2}}, "Bool")
	p.Schema, p.Profile = SchemaV4, ProfileV3
	if err := Validate(p); err != nil {
		t.Fatal("v4 lost numeric support", err)
	}
	p.Types[0].Constructors[0].Name = "Bool.fake"
	if err := Validate(p); err == nil {
		t.Fatal("v4 lost canonical Bool requirement")
	}
}

func TestStrictJoinDecode(t *testing.T) {
	valid := joinDocument(t, `{"op":"join","join":{"id":3,"params":[],"body":{"op":"return","var":1}},"next":{"op":"jump","target":3,"args":[]}}`)
	for _, pair := range [][2]string{
		{`"id":3`, `"id":3,"id":3`}, {`"id":3`, `"id":"3"`}, {`"id":3`, `"id":3.5`},
		{`"params":[],`, ``}, {`"params":[]`, `"params":null`}, {`"params":[]`, `"params":{}`},
		{`"args":[]`, `"args":[],"extra":0`}, {`,"args":[]`, ``}, {`"args":[]`, `"args":null`},
		{`"args":[]`, `"args":{}`}, {`"args":[]`, `"args":["1"]`}, {`"args":[]`, `"args":[1.5]`},
		{`"target":3`, `"target":3,"target":3`}, {`"target":3`, `"target":null`}, {`"target":3`, `"target":"3"`},
		{`"body":{"op":"return","var":1}`, `"body":null`}, {`"join":{"id":3`, `"join":{"extra":0,"id":3`},
		{`"lean-go-ir/v4"`, `"lean-go-ir/v3"`},
	} {
		bad := bytes.Replace(valid, []byte(pair[0]), []byte(pair[1]), 1)
		if bytes.Equal(valid, bad) {
			t.Fatalf("ineffective mutation %q", pair[0])
		}
		if _, err := Decode(bytes.NewReader(bad)); err == nil {
			t.Fatalf("accepted malformed join JSON %s", bad)
		}
	}
}

func TestJoinArityAndDepthLimits(t *testing.T) {
	p := joinProgram()
	c := p.Functions[0].Body
	c.Join.Body = &Code{Op: "return", Var: 1}
	c.Join.Params, c.Next.Args = []Param{}, []int{}
	for i := 0; i < MaxArity; i++ {
		c.Join.Params = append(c.Join.Params, Param{ID: 10 + i, Type: "UInt32"})
		c.Next.Args = append(c.Next.Args, 1)
	}
	if err := Validate(p); err != nil {
		t.Fatal("exact join arity", err)
	}
	c.Join.Params = append(c.Join.Params, Param{ID: 100, Type: "UInt32"})
	c.Next.Args = append(c.Next.Args, 1)
	if err := Validate(p); err == nil {
		t.Fatal("accepted oversized join signature")
	}
	for _, branch := range []string{"body", "next"} {
		p := joinProgram()
		body := &Code{Op: "return", Var: 1}
		for i := 0; i < MaxCodeDepth-1; i++ {
			c := &Code{Op: "join", Join: &Join{ID: 3 + i, Params: []Param{}, Body: &Code{Op: "return", Var: 1}}, Next: &Code{Op: "return", Var: 1}}
			if branch == "body" {
				c.Join.Body = body
			} else {
				c.Next = body
			}
			body = c
		}
		p.Functions[0].Body = body
		if err := Validate(p); err != nil {
			t.Fatal("exact depth", branch, err)
		}
		p.Functions[0].Body = &Code{Op: "join", Join: &Join{ID: 999, Params: []Param{}, Body: body}, Next: &Code{Op: "return", Var: 1}}
		if err := Validate(p); err == nil || !strings.Contains(err.Error(), "depth") {
			t.Fatal("excessive depth", branch, err)
		}
	}
}

func TestJoinStructuralBudget(t *testing.T) {
	c := &Code{Op: "join", Join: &Join{ID: 2, Params: []Param{{ID: 3, Type: "UInt32"}}, Body: &Code{Op: "return", Var: 3}}, Next: &Code{Op: "jump", Target: 2, Args: []int{1}}}
	// Three Code objects, one Join object, and one parameter binder.
	for _, remaining := range []int{5, 4} {
		v := validator{nodes: MaxNodes - remaining, joins: true}
		err := v.code(c, Function{Result: "UInt32"}, map[int]string{1: "UInt32"}, map[int]*Join{}, map[int]bool{1: true}, map[*Code]bool{}, 1)
		if remaining == 5 {
			if err != nil || v.nodes != MaxNodes {
				t.Fatal("exact join structural budget", v.nodes, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "node limit") {
			t.Fatal("join structural budget exceeded", err)
		}
	}
}

func TestJoinLegacyDecodeCompatibility(t *testing.T) {
	for _, p := range []*Program{validProgram(), numericProgram(&Value{Kind: "uint32", Name: "7", Args: []int{}}, "UInt32")} {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(bytes.NewReader(raw))
		if err != nil || decoded.Schema != p.Schema || decoded.Profile != p.Profile {
			t.Fatal("legacy profile decode changed", p.Schema, err)
		}
	}
}

func FuzzJoinDecode(f *testing.F) {
	p := joinProgram()
	raw, _ := json.Marshal(p)
	f.Add(raw)
	p.Functions[0].Body.Join.Params = []Param{}
	p.Functions[0].Body.Join.Body = &Code{Op: "return", Var: 1}
	p.Functions[0].Body.Next.Args = []int{}
	raw, _ = json.Marshal(p)
	f.Add(raw)
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Decode(bytes.NewReader(data))
		if err == nil {
			if err := Validate(p); err != nil {
				t.Fatal("decoder returned invalid program", err)
			}
		}
	})
}

func TestJoinSchemaRoundTrip(t *testing.T) {
	for _, body := range []string{
		`{"op":"join","join":{"id":3,"params":[],"body":{"op":"return","var":1}},"next":{"op":"jump","target":3,"args":[]}}`,
		`{"op":"join","join":{"id":3,"params":[{"id":4,"type":"UInt32"}],"body":{"op":"return","var":4}},"next":{"op":"jump","target":3,"args":[2]}}`,
	} {
		p, err := Decode(bytes.NewReader(joinDocument(t, body)))
		if err != nil {
			t.Fatal("valid lexical join", err)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Decode(bytes.NewReader(raw)); err != nil {
			t.Fatal("join round trip", err, string(raw))
		}
	}
}
