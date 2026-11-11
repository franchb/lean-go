package moduleir

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func validProgram() *Program {
	return &Program{Schema: Schema, Profile: Profile, Stage: Stage, LeanVersion: LeanVersion, LeanCommit: LeanCommit, Module: "Example", Root: "Example.root", Inputs: map[string]string{"Example.lean": strings.Repeat("0", 64)}, Types: []TypeDecl{{Name: "Example.Tree", Constructors: []Constructor{{Name: "Example.Tree.leaf", Tag: 0, Fields: []string{"UInt32"}}, {Name: "Example.Tree.branch", Tag: 1, Fields: []string{"Example.Tree", "Example.Tree"}}}}}, Claims: []Claim{}, Rewrites: []Rewrite{}, Provenance: []Provenance{{Name: "Example.root", Dependencies: []string{"Example.Tree"}, Rewrites: []Rewrite{}}}, Functions: []Function{{Name: "Example.root", Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "return", Var: 1}}}}
}
func TestGeneralProgram(t *testing.T) {
	p := validProgram()
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if _, err := Decode(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	p.Module = "Different"
	p.Root = "Different.id"
	p.Functions[0].Name = p.Root
	p.Provenance[0].Name = p.Root
	if err := Validate(p); err != nil {
		t.Fatal("unexpected source/name restriction", err)
	}
}
func TestRejectInvalidPrograms(t *testing.T) {
	tests := map[string]func(*Program){
		"metadata":       func(p *Program) { p.Profile = "unknown" },
		"unknown type":   func(p *Program) { p.Functions[0].Params[0].Type = "Missing" },
		"invalid return": func(p *Program) { p.Functions[0].Body.Var = 2 },
		"duplicate type": func(p *Program) { p.Types = append(p.Types, p.Types[0]) },
		"duplicate ctor": func(p *Program) { p.Types[0].Constructors[1].Name = p.Types[0].Constructors[0].Name },
		"wrong tag":      func(p *Program) { p.Types[0].Constructors[0].Tag = 1 },
		"unreachable type": func(p *Program) {
			p.Types = append(p.Types, TypeDecl{Name: "Unused", Constructors: []Constructor{{Name: "Unused.mk", Fields: []string{}}}})
		},
		"abs input":          func(p *Program) { p.Inputs["/private/file"] = strings.Repeat("0", 64) },
		"missing provenance": func(p *Program) { p.Provenance = []Provenance{} },
		"claims":             func(p *Program) { p.Claims = []Claim{{Name: "proof", Statement: "claimed", Axioms: []string{}}} },
		"unsafe rewrite": func(p *Program) {
			p.Rewrites = []Rewrite{{Source: "a", Target: "b", Theorem: "c", Axioms: []string{"sorryAx"}}}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			p := validProgram()
			change(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid program")
			}
		})
	}
}
func TestStrictDecode(t *testing.T) {
	raw, _ := json.Marshal(validProgram())
	for name, bad := range map[string][]byte{
		"duplicate": bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"lean-go-ir/v2","schema":`), 1),
		"null":      bytes.Replace(raw, []byte(`"tag":0`), []byte(`"tag":null`), 1),
		"unknown":   bytes.Replace(raw, []byte(`"tag":0`), []byte(`"tag":0,"extra":1`), 1),
		"trailing":  append(append([]byte{}, raw...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted invalid JSON")
			}
		})
	}
}

func TestUnicodeEscapes(t *testing.T) {
	raw, _ := json.Marshal(validProgram())
	for _, escaped := range []string{`\ud800`, `\udfff`, `\ud800x`, `\ud800\u0041`, `\ud800\ud800`} {
		bad := bytes.Replace(raw, []byte(`"module":"Example"`), []byte(`"module":"`+escaped+`"`), 1)
		if _, err := Decode(bytes.NewReader(bad)); err == nil {
			t.Fatalf("accepted invalid scalar escape %s", escaped)
		}
	}
	for _, escaped := range []string{`\ud83d\ude00`, `\ufffd`, `\\ud800`, `\"quoted\"`} {
		good := bytes.Replace(raw, []byte(`"module":"Example"`), []byte(`"module":"`+escaped+`"`), 1)
		if _, err := Decode(bytes.NewReader(good)); err != nil {
			t.Fatalf("rejected valid escape %s: %v", escaped, err)
		}
	}
}

func TestTypedScopeAndControl(t *testing.T) {
	makeCases := func() *Program {
		p := validProgram()
		p.Functions[0].Body = &Code{Op: "cases", Var: 1, Type: "Example.Tree", Result: "Example.Tree", Alts: []Alt{
			{Constructor: "Example.Tree.leaf", Params: []Param{{ID: 2, Type: "UInt32"}}, Body: &Code{Op: "return", Var: 1}},
			{Constructor: "Example.Tree.branch", Params: []Param{{ID: 3, Type: "Example.Tree"}, {ID: 4, Type: "Example.Tree"}}, Body: &Code{Op: "return", Var: 3}},
		}}
		return p
	}
	if err := Validate(makeCases()); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Program){
		"nonexhaustive":    func(p *Program) { p.Functions[0].Body.Alts = p.Functions[0].Body.Alts[:1] },
		"duplicate alt":    func(p *Program) { p.Functions[0].Body.Alts[1] = p.Functions[0].Body.Alts[0] },
		"branch leak":      func(p *Program) { p.Functions[0].Body.Alts[0].Body.Var = 3 },
		"duplicate binder": func(p *Program) { p.Functions[0].Body.Alts[1].Params[0].ID = 1 },
		"mistyped field":   func(p *Program) { p.Functions[0].Body.Alts[0].Params[0].Type = "Example.Tree" },
		"mistyped return":  func(p *Program) { p.Functions[0].Body.Alts[0].Body.Var = 2 },
		"irrelevant field": func(p *Program) { p.Functions[0].Body.Alts[0].Body.Type = "UInt32" },
		"cycle":            func(p *Program) { p.Functions[0].Body.Alts[0].Body = p.Functions[0].Body },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			p := makeCases()
			change(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted malformed control")
			}
		})
	}
}
func TestCallClosureAndRecursion(t *testing.T) {
	p := validProgram()
	p.Functions[0].Body = &Code{Op: "let", Bind: &Param{ID: 2, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: p.Root, Args: []int{1}}, Next: &Code{Op: "return", Var: 2}}
	if err := Validate(p); err != nil {
		t.Fatal("self recursion", err)
	}
	p.Functions[0].Body.Value.Args = []int{}
	if err := Validate(p); err == nil {
		t.Fatal("accepted unsaturated call")
	}
	p.Functions[0].Body.Value.Args = []int{1}
	p.Functions = append(p.Functions, Function{Name: "Example.second", Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "let", Bind: &Param{ID: 2, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: p.Root, Args: []int{1}}, Next: &Code{Op: "return", Var: 2}}})
	p.Provenance = append(p.Provenance, Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []Rewrite{}})
	if err := Validate(p); err == nil {
		t.Fatal("accepted unreachable function")
	}
	p.Functions[0].Body.Value.Name = "Example.second"
	if err := Validate(p); err == nil {
		t.Fatal("accepted mutual recursion")
	}
	p.Functions[1].Body = &Code{Op: "return", Var: 1}
	if err := Validate(p); err != nil {
		t.Fatal("acyclic calls", err)
	}
	p.Functions[0].Body.Value.Name = "Missing"
	if err := Validate(p); err == nil {
		t.Fatal("accepted unresolved call")
	}
}
func TestProvenanceAndStructuralBounds(t *testing.T) {
	trusted := Rewrite{Source: "Bool.not", Target: "Bool.Internal.not", Theorem: "Bool.not_eq_internalNot", Axioms: []string{"Quot.sound"}}
	p := validProgram()
	p.Rewrites = []Rewrite{trusted}
	p.Provenance[0].Rewrites = []Rewrite{trusted}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	p.Rewrites = []Rewrite{}
	if err := Validate(p); err == nil {
		t.Fatal("accepted incomplete union")
	}
	p = validProgram()
	p.Rewrites = []Rewrite{trusted}
	if err := Validate(p); err == nil {
		t.Fatal("accepted extra union")
	}
	p = validProgram()
	p.Provenance[0].Dependencies = nil
	if err := Validate(p); err == nil {
		t.Fatal("accepted null constructed array")
	}
	p = validProgram()
	p.Provenance[0].Dependencies = make([]string, MaxNodes)
	for i := range p.Provenance[0].Dependencies {
		p.Provenance[0].Dependencies[i] = strings.Repeat("a", 1) + string(rune(0x1000+i))
	}
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "node limit") {
		t.Fatal("aggregate metadata bound", err)
	}
	p = validProgram()
	body := p.Functions[0].Body
	for i := 0; i < MaxCodeDepth; i++ {
		body = &Code{Op: "let", Bind: &Param{ID: 2 + i, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: p.Root, Args: []int{1}}, Next: body}
	}
	p.Functions[0].Body = body
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "depth limit") {
		t.Fatal("code depth bound", err)
	}
}
func FuzzDecode(f *testing.F) {
	valid, _ := json.Marshal(validProgram())
	f.Add(valid)
	f.Add([]byte(`{"schema":null}`))
	f.Add([]byte(`[]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Decode(bytes.NewReader(data))
		if err == nil {
			if err := Validate(p); err != nil {
				t.Fatal("decoder returned invalid program", err)
			}
		}
	})
}
