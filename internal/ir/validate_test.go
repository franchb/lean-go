package ir

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixture() *Program {
	p := &Program{Schema: Schema, Profile: Profile, Stage: Stage, LeanVersion: LeanVersion, LeanCommit: LeanCommit, ModelSHA256: ModelSHA256, Root: Root, Inputs: map[string]string{}}
	for _, key := range inputKeys {
		p.Inputs[key] = strings.Repeat("a", 64)
	}
	p.Inputs[inputKeys[0]] = ModelSHA256
	p.Types = []TypeDecl{
		{"bool", []Constructor{{"Bool.false", 0, []string{}}, {"Bool.true", 1, []string{}}}},
		{"tri", []Constructor{{"LeanGo.Feasibility.Tri.f", 0, []string{}}, {"LeanGo.Feasibility.Tri.u", 1, []string{}}, {"LeanGo.Feasibility.Tri.t", 2, []string{}}}},
		{"expr", []Constructor{{"LeanGo.Feasibility.Expr.lit", 0, []string{"tri"}}, {"LeanGo.Feasibility.Expr.var", 1, []string{"u32"}}, {"LeanGo.Feasibility.Expr.neg", 2, []string{"expr"}}, {"LeanGo.Feasibility.Expr.conj", 3, []string{"expr", "expr"}}, {"LeanGo.Feasibility.Expr.disj", 4, []string{"expr", "expr"}}}},
	}
	p.Claims = []Claim{{"LeanGo.Feasibility.normalize_preserves_eval", "forall e : Expr, forall rho : UInt32 -> Tri, eval rho (normalize e) = eval rho e", []string{"propext"}}, {"LeanGo.Feasibility.normalize_is_normal", "forall e : Expr, IsNormalForm (normalize e)", []string{"propext"}}}
	p.Rewrites = []Rewrite{{"Bool.not", "Bool.Internal.not", "Bool.not_eq_internalNot", []string{"Quot.sound"}}}
	ret := func(id int) *Code { return &Code{Op: "return", Var: id} }
	p.Functions = []Function{
		{"LeanGo.Feasibility.Tri.neg", []Param{{1, "tri"}}, "tri", ret(1)},
		{Root, []Param{{1, "expr"}}, "expr", &Code{Op: "let", Bind: &Param{2, "bool"}, Value: &Value{"constructor", "Bool.false", []int{}}, Next: &Code{Op: "let", Bind: &Param{3, "expr"}, Value: &Value{"call", "LeanGo.Feasibility.normalizeAux", []int{2, 1}}, Next: ret(3)}}},
		{"LeanGo.Feasibility.normalizeAux", []Param{{1, "bool"}, {2, "expr"}}, "expr", &Code{Op: "let", Bind: &Param{3, "tri"}, Value: &Value{"constructor", "LeanGo.Feasibility.Tri.f", []int{}}, Next: &Code{Op: "let", Bind: &Param{4, "tri"}, Value: &Value{"call", "LeanGo.Feasibility.Tri.neg", []int{3}}, Next: ret(2)}}},
	}
	return p
}
func TestValidProgram(t *testing.T) {
	p := fixture()
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Decode(bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
}
func TestSemanticRejection(t *testing.T) {
	cases := map[string]func(*Program){
		"metadata":               func(p *Program) { p.Stage = "base" },
		"input key":              func(p *Program) { p.Inputs["other"] = ModelSHA256 },
		"hash":                   func(p *Program) { p.Inputs[inputKeys[1]] = strings.Repeat("A", 64) },
		"claims":                 func(p *Program) { p.Claims[0].Statement = "True" },
		"duplicate claims":       func(p *Program) { p.Claims[1] = p.Claims[0] },
		"rewrite":                func(p *Program) { p.Rewrites[0].Axioms = []string{"propext"} },
		"constructor":            func(p *Program) { p.Types[0].Constructors[0].Tag = 2 },
		"duplicate constructors": func(p *Program) { p.Types[0].Constructors[1] = p.Types[0].Constructors[0] },
		"duplicate binder":       func(p *Program) { p.Functions[1].Body.Bind.ID = 1 },
		"self reference":         func(p *Program) { p.Functions[1].Body.Value = &Value{"call", "LeanGo.Feasibility.Tri.neg", []int{2}} },
		"wrong return":           func(p *Program) { p.Functions[1].Body.Next.Next.Var = 2 },
		"wrong arity":            func(p *Program) { p.Functions[1].Body.Next.Value.Args = []int{1} },
		"wrong argument type":    func(p *Program) { p.Functions[1].Body.Next.Value.Args = []int{1, 2} },
		"unresolved call":        func(p *Program) { p.Functions[1].Body.Next.Value.Name = "Bool.Internal.not" },
		"extra code":             func(p *Program) { p.Functions = append(p.Functions, p.Functions[0]) },
		"unreachable function":   func(p *Program) { p.Functions[2].Body = &Code{Op: "return", Var: 2} },
		"recursion cycle": func(p *Program) {
			p.Functions[2].Body = &Code{Op: "let", Bind: &Param{3, "expr"}, Value: &Value{"call", Root, []int{2}}, Next: &Code{Op: "return", Var: 3}}
		},
		"inactive code": func(p *Program) { p.Functions[0].Body.Type = "tri" },
		"cycle":         func(p *Program) { p.Functions[1].Body.Next = p.Functions[1].Body },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := fixture()
			mutate(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid program")
			}
		})
	}
}
func TestStrictDecode(t *testing.T) {
	b, _ := json.Marshal(fixture())
	valid := string(b)
	cases := map[string]string{
		"duplicate":        strings.Replace(valid, `"schema":`, `"schema":"lean-go-ir/v1","schema":`, 1),
		"unknown":          strings.Replace(valid, `"schema":`, `"extra":null,"schema":`, 1),
		"missing":          strings.Replace(valid, `"stage":"pure-mono",`, "", 1),
		"trailing":         valid + ` {}`,
		"inactive empty":   strings.Replace(valid, `"op":"return","var":1`, `"op":"return","var":1,"alts":[]`, 1),
		"inactive null":    strings.Replace(valid, `"op":"return","var":1`, `"op":"return","var":1,"bind":null`, 1),
		"null list":        strings.Replace(valid, `"args":[]`, `"args":null`, 1),
		"wrong field type": strings.Replace(valid, `"id":1`, `"id":"1"`, 1),
		"utf8":             valid + string([]byte{255}),
		"oversize":         strings.Repeat(" ", MaxBytes+1),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(doc)); err == nil {
				t.Fatal("accepted malformed document")
			}
		})
	}
}

func exprCases() *Code {
	alts := []Alt{}
	id := 5
	for _, ctor := range fixture().Types[2].Constructors {
		params := []Param{}
		for _, typ := range ctor.Fields {
			params = append(params, Param{id, typ})
			id++
		}
		alts = append(alts, Alt{ctor.Name, params, &Code{Op: "return", Var: 2}})
	}
	return &Code{Op: "cases", Var: 2, Type: "expr", Result: "expr", Alts: alts}
}
func TestCasesAndLexicalScope(t *testing.T) {
	base := func() *Program { p := fixture(); p.Functions[2].Body.Next.Next = exprCases(); return p }
	if err := Validate(base()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Program){
		"missing alternative":           func(p *Program) { p.Functions[2].Body.Next.Next.Alts = p.Functions[2].Body.Next.Next.Alts[:4] },
		"duplicate alternative":         func(p *Program) { p.Functions[2].Body.Next.Next.Alts[1] = p.Functions[2].Body.Next.Next.Alts[0] },
		"field type":                    func(p *Program) { p.Functions[2].Body.Next.Next.Alts[0].Params[0].Type = "expr" },
		"field arity":                   func(p *Program) { p.Functions[2].Body.Next.Next.Alts[0].Params = nil },
		"duplicate across alternatives": func(p *Program) { p.Functions[2].Body.Next.Next.Alts[1].Params[0].ID = 5 },
		"wrong discriminant":            func(p *Program) { p.Functions[2].Body.Next.Next.Type = "tri" },
		"wrong branch result":           func(p *Program) { p.Functions[2].Body.Next.Next.Alts[0].Body.Var = 5 },
		"wrong cases result":            func(p *Program) { p.Functions[2].Body.Next.Next.Result = "tri" },
		"sibling scope leakage": func(p *Program) {
			p.Functions[2].Body.Next.Next.Alts[1].Body = &Code{Op: "let", Bind: &Param{20, "tri"}, Value: &Value{"call", negName, []int{5}}, Next: &Code{Op: "return", Var: 2}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := base()
			mutate(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid alternative")
			}
		})
	}
}
func TestCodeAndNodeBounds(t *testing.T) {
	chain := func(depth int) *Program {
		p := fixture()
		c := &Code{Op: "return", Var: 1}
		for i := depth; i > 1; i-- {
			c = &Code{Op: "let", Bind: &Param{i, "tri"}, Value: &Value{"constructor", "LeanGo.Feasibility.Tri.f", []int{}}, Next: c}
		}
		p.Functions[0].Body = c
		return p
	}
	if err := Validate(chain(MaxCodeDepth)); err != nil {
		t.Fatal(err)
	}
	if err := Validate(chain(MaxCodeDepth + 1)); err == nil {
		t.Fatal("accepted excessive code depth")
	}
	b, _ := json.Marshal(chain(MaxCodeDepth + 1))
	if _, err := Decode(bytes.NewReader(b)); err == nil {
		t.Fatal("decoded excessive code depth")
	}
	p := fixture()
	var tree func(int) *Code
	tree = func(n int) *Code {
		if n == 0 {
			return &Code{Op: "return", Var: 2}
		}
		return &Code{Op: "cases", Var: 1, Type: "bool", Result: "expr", Alts: []Alt{{"Bool.false", []Param{}, tree(n - 1)}, {"Bool.true", []Param{}, tree(n - 1)}}}
	}
	p.Functions[2].Body.Next.Next = tree(13)
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "node limit") {
		t.Fatalf("expected node limit, got %v", err)
	}
	b, _ = json.Marshal(p)
	if _, err := Decode(bytes.NewReader(b)); err == nil {
		t.Fatal("decoded excessive nodes")
	}
	if _, err := Decode(strings.NewReader(strings.Repeat("[", 2050) + strings.Repeat("]", 2050))); err == nil {
		t.Fatal("accepted excessive JSON nesting")
	}
}
func TestAllowedSelfRecursion(t *testing.T) {
	p := fixture()
	p.Functions[2].Body.Next.Next = &Code{Op: "let", Bind: &Param{5, "expr"}, Value: &Value{"call", auxName, []int{1, 2}}, Next: &Code{Op: "return", Var: 5}}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	p.Functions[0].Body = &Code{Op: "let", Bind: &Param{2, "tri"}, Value: &Value{"call", negName, []int{1}}, Next: &Code{Op: "return", Var: 2}}
	if err := Validate(p); err == nil {
		t.Fatal("accepted forbidden self recursion")
	}
}
func TestSourceDigest(t *testing.T) {
	digest := SourceDigest()
	if len(digest) != 64 || digest != SourceDigest() {
		t.Fatalf("unstable source identity %q", digest)
	}
}

func TestExportedFixture(t *testing.T) {
	file, err := os.Open("../../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := Decode(file); err != nil {
		t.Fatal(err)
	}
}
func TestExactByteBoundary(t *testing.T) {
	data, err := json.Marshal(fixture())
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, bytes.Repeat([]byte(" "), MaxBytes-len(data))...)
	if _, err := Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(append(data, ' '))); err == nil {
		t.Fatal("accepted byte limit overflow")
	}
}

func TestRejectNullConstructorTags(t *testing.T) {
	data, err := os.ReadFile("../../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	tested := 0
	for _, typ := range doc["types"].([]any) {
		for _, item := range typ.(map[string]any)["constructors"].([]any) {
			ctor := item.(map[string]any)
			if ctor["tag"] != float64(0) {
				continue
			}
			name := ctor["name"].(string)
			t.Run(name, func(t *testing.T) {
				ctor["tag"] = nil
				defer func() { ctor["tag"] = float64(0) }()
				bad, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = Decode(bytes.NewReader(bad)); err == nil {
					t.Fatal("accepted null as constructor tag zero")
				}
			})
			tested++
		}
	}
	if tested != 3 {
		t.Fatalf("expected all three zero tags; tested %d", tested)
	}
}

func TestRejectNullScalarLeaves(t *testing.T) {
	data, err := os.ReadFile("../../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	checked := 0
	reject := func() {
		t.Helper()
		bad, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Decode(bytes.NewReader(bad)); err == nil {
			t.Fatal("accepted null scalar leaf")
		}
		checked++
	}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				switch item.(type) {
				case map[string]any, []any:
					visit(item)
				default:
					v[key] = nil
					reject()
					v[key] = item
				}
			}
		case []any:
			for i, item := range v {
				switch item.(type) {
				case map[string]any, []any:
					visit(item)
				default:
					v[i] = nil
					reject()
					v[i] = item
				}
			}
		}
	}
	visit(doc)
	if checked < 100 {
		t.Fatalf("expected a complete scalar audit, checked %d", checked)
	}
}
