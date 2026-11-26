package moduleir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// declare sets the exact feature list, as the producer does. A program that is
// invalid for any other reason keeps its list and fails for that reason.
func declare(p *Program) *Program {
	if features, err := UsedFeatures(p); err == nil {
		p.Features = features
	}
	return p
}

// check validates a test program with its used features declared.
func check(p *Program) error { return Validate(declare(p)) }

func validProgram() *Program {
	return declare(&Program{Schema: Schema, Profile: Profile, Stage: Stage, LeanVersion: LeanVersion, LeanCommit: LeanCommit, Module: "Example", Roots: []string{"Example.root"}, Inputs: map[string]string{"Example.lean": strings.Repeat("0", 64)}, Types: []TypeDecl{{Name: "Example.Tree", Constructors: []Constructor{{Name: "Example.Tree.leaf", Tag: 0, Fields: []string{"UInt32"}}, {Name: "Example.Tree.branch", Tag: 1, Fields: []string{"Example.Tree", "Example.Tree"}}}}}, Claims: []Claim{}, Rewrites: []Rewrite{}, Provenance: []Provenance{{Name: "Example.root", Dependencies: []string{"Example.Tree"}, Rewrites: []Rewrite{}}}, Functions: []Function{{Name: "Example.root", Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "return", Var: 1}}}, API: &APIMetadata{Records: map[string][]string{}}})
}
func TestGeneralProgram(t *testing.T) {
	p := validProgram()
	if err := check(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if _, err := Decode(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	p.Module = "Different"
	p.Roots = []string{"Different.id"}
	p.Functions[0].Name = p.Roots[0]
	p.Provenance[0].Name = p.Roots[0]
	if err := check(p); err != nil {
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
			if err := check(p); err == nil {
				t.Fatal("accepted invalid program")
			}
		})
	}
}
func TestStrictDecode(t *testing.T) {
	raw, _ := json.Marshal(validProgram())
	for name, bad := range map[string][]byte{
		"duplicate": bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"lean-go-ir/v24","schema":`), 1),
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
	if err := check(makeCases()); err != nil {
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
			if err := check(p); err == nil {
				t.Fatal("accepted malformed control")
			}
		})
	}
}
func TestCallClosureAndRecursion(t *testing.T) {
	p := validProgram()
	p.Functions[0].Body = &Code{Op: "let", Bind: &Param{ID: 2, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: p.Roots[0], Args: []int{1}}, Next: &Code{Op: "return", Var: 2}}
	if err := check(p); err != nil {
		t.Fatal("self recursion", err)
	}
	p.Functions[0].Body.Value.Args = []int{}
	if err := check(p); err == nil {
		t.Fatal("accepted unsaturated call")
	}
	p.Functions[0].Body.Value.Args = []int{1}
	p.Functions = append(p.Functions, Function{Name: "Example.second", Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "let", Bind: &Param{ID: 2, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: p.Roots[0], Args: []int{1}}, Next: &Code{Op: "return", Var: 2}}})
	p.Provenance = append(p.Provenance, Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []Rewrite{}})
	if err := check(p); err == nil {
		t.Fatal("accepted unreachable function")
	}
	p.Functions[0].Body.Value.Name = "Example.second"
	if err := check(p); err == nil {
		t.Fatal("accepted mutual recursion")
	}
	p.Functions[1].Body = &Code{Op: "return", Var: 1}
	if err := check(p); err != nil {
		t.Fatal("acyclic calls", err)
	}
	p.Functions[0].Body.Value.Name = "Missing"
	if err := check(p); err == nil {
		t.Fatal("accepted unresolved call")
	}
}
func TestProvenanceAndStructuralBounds(t *testing.T) {
	trusted := Rewrite{Source: "Bool.not", Target: "Bool.Internal.not", Theorem: "Bool.not_eq_internalNot", Axioms: []string{"Quot.sound"}}
	p := validProgram()
	p.Rewrites = []Rewrite{trusted}
	p.Provenance[0].Rewrites = []Rewrite{trusted}
	if err := check(p); err != nil {
		t.Fatal(err)
	}
	p.Rewrites = []Rewrite{}
	if err := check(p); err == nil {
		t.Fatal("accepted incomplete union")
	}
	p = validProgram()
	p.Rewrites = []Rewrite{trusted}
	if err := check(p); err == nil {
		t.Fatal("accepted extra union")
	}
	p = validProgram()
	p.Provenance[0].Dependencies = nil
	if err := check(p); err == nil {
		t.Fatal("accepted null constructed array")
	}
	p = validProgram()
	p.Provenance[0].Dependencies = make([]string, MaxNodes)
	for i := range p.Provenance[0].Dependencies {
		p.Provenance[0].Dependencies[i] = strings.Repeat("a", 1) + string(rune(0x1000+i))
	}
	if err := check(p); err == nil || !strings.Contains(err.Error(), "node limit") {
		t.Fatal("aggregate metadata bound", err)
	}
	p = validProgram()
	body := p.Functions[0].Body
	for i := 0; i < MaxCodeDepth; i++ {
		body = &Code{Op: "let", Bind: &Param{ID: 2 + i, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: p.Roots[0], Args: []int{1}}, Next: body}
	}
	p.Functions[0].Body = body
	if err := check(p); err == nil || !strings.Contains(err.Error(), "depth limit") {
		t.Fatal("code depth bound", err)
	}
}
func FuzzDecode(f *testing.F) {
	valid, _ := json.Marshal(validProgram())
	f.Add(valid)
	literal, _ := json.Marshal(numericProgram(&Value{Kind: "uint32", Name: "4294967295", Args: []int{}}, "UInt32"))
	f.Add(literal)
	for _, name := range []string{"UInt32.add", "UInt32.sub", "UInt32.mul", "UInt32.shiftLeft", "UInt32.decEq", "UInt32.decLt", "UInt32.decLe"} {
		primitive, _ := LookupPrimitive(name)
		encoded, _ := json.Marshal(numericProgram(&Value{Kind: "primitive", Name: name, Args: []int{1, 2}}, primitive.Result))
		f.Add(encoded)
	}
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

func TestCompilerIdentityPairs(t *testing.T) {
	const historicalVersion = "4.36.0-rc2"
	const historicalCommit = "b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a"
	for _, tc := range []struct {
		version, commit string
		valid           bool
	}{
		{LeanVersion, LeanCommit, true}, {historicalVersion, historicalCommit, true},
		{LeanVersion, historicalCommit, false}, {historicalVersion, LeanCommit, false},
		{"4.36.0-rc1", historicalCommit, false}, {LeanVersion, LeanCommit[:8], false},
	} {
		p := validProgram()
		p.LeanVersion, p.LeanCommit = tc.version, tc.commit
		if err := check(p); (err == nil) != tc.valid {
			t.Errorf("Validate with %s at %s: got %v, want valid=%t", tc.version, tc.commit, err, tc.valid)
		}
	}
}

func TestSchemaIdentity(t *testing.T) {
	stale := []string{"lean-go-ir/v1", "lean-go-ir/v25", "lean-go-ir/v24x", ""}
	for n := 2; n < 24; n++ {
		stale = append(stale, fmt.Sprintf("lean-go-ir/v%d", n))
	}
	for _, schema := range stale {
		p := validProgram()
		p.Schema = schema
		err := Validate(p)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", schema)) || !strings.Contains(err.Error(), "regenerate") {
			t.Fatalf("schema %q: %v", schema, err)
		}
		raw, _ := json.Marshal(p)
		if _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "regenerate") {
			t.Fatalf("decoded schema %q: %v", schema, err)
		}
	}
	// A stale document is named even when its shape differs from the current one.
	old := fmt.Sprintf("lean-go-ir/v%d", 7)
	stalePayload := []byte(`{"schema":"` + old + `","root":"Example.root"}`)
	if _, err := Decode(bytes.NewReader(stalePayload)); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", old)) {
		t.Fatal("stale document", err)
	}
	for _, profile := range []string{fmt.Sprintf("pure-module/v%d", 21), "pure-module/v23", "unknown"} {
		p := validProgram()
		p.Profile = profile
		if err := Validate(p); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", profile)) {
			t.Fatalf("profile %q: %v", profile, err)
		}
	}
}

func TestDeclaredFeatures(t *testing.T) {
	p := numericProgram(&Value{Kind: "primitive", Name: "UInt32.decEq", Args: []int{1, 2}}, "Bool")
	if !reflect.DeepEqual(p.Features, []string{FeatureUInt32}) {
		t.Fatal("features", p.Features)
	}
	for name, features := range map[string][]string{
		"missing":   {},
		"null":      nil,
		"extra":     {FeatureBitwise, FeatureUInt32},
		"unknown":   {FeatureUInt32, "uint128"},
		"duplicate": {FeatureUInt32, FeatureUInt32},
	} {
		p.Features = features
		if err := Validate(p); err == nil {
			t.Fatal("accepted declared features", name)
		}
	}
	unsorted := joinProgram()
	unsorted.Features = []string{FeatureUInt32, FeatureJoin}
	if err := Validate(unsorted); err == nil {
		t.Fatal("accepted unsorted features")
	}
	raw, _ := json.Marshal(validProgram())
	for name, bad := range map[string][]byte{
		"absent": bytes.Replace(raw, []byte(`"features":["uint32"],`), nil, 1),
		"null":   bytes.Replace(raw, []byte(`"features":["uint32"]`), []byte(`"features":null`), 1),
		"number": bytes.Replace(raw, []byte(`"features":["uint32"]`), []byte(`"features":[1]`), 1),
		"extra":  bytes.Replace(raw, []byte(`"features":["uint32"]`), []byte(`"features":["join","uint32"]`), 1),
	} {
		if bytes.Equal(bad, raw) {
			t.Fatal("ineffective mutation", name)
		}
		if _, err := Decode(bytes.NewReader(bad)); err == nil {
			t.Fatal("decoded features", name)
		}
	}
}

func TestFeatureFamilies(t *testing.T) {
	wide := uint64Program(&Value{Kind: "uint64", Name: "1", Args: []int{}}, "UInt64")
	shift := uint64Program(&Value{Kind: "primitive", Name: "UInt64.shiftLeft", Args: []int{1, 2}}, "UInt64")
	widen := conversionProgram("UInt32.toUInt64")
	half := scalarProgram(&Value{Kind: "primitive", Name: "UInt8.toUInt16", Args: []int{1}}, "UInt16", "UInt8")
	half.Functions[0].Params = half.Functions[0].Params[:1]
	for _, tc := range []struct {
		p    *Program
		want []string
	}{
		{validProgram(), []string{FeatureUInt32}},
		{joinProgram(), []string{FeatureJoin, FeatureUInt32}},
		{wide, []string{FeatureUInt64}},
		{shift, []string{FeatureBitwise, FeatureUInt64}},
		{widen, []string{FeatureUInt32, FeatureUInt64, FeatureWidthConversion}},
		{half, []string{FeatureUInt16, FeatureUInt8, FeatureWidthConversion}},
		{byteFoldProgram(false), []string{FeatureByteFold, FeatureUInt32, FeatureUInt8}},
		{byteFoldProgram(true), []string{FeatureByteFold, FeatureRecordFold, FeatureUInt32, FeatureUInt8}},
	} {
		got, err := UsedFeatures(tc.p)
		if err != nil || !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(tc.p.Features, tc.want) {
			t.Fatalf("features %v, declared %v, want %v: %v", got, tc.p.Features, tc.want, err)
		}
		if err := Validate(tc.p); err != nil {
			t.Fatal(err)
		}
	}
}

// byteFoldProgram folds a byte list into a UInt32 or into a two-word record.
func byteFoldProgram(record bool) *Program {
	p := validProgram()
	state := "UInt32"
	p.Types = []TypeDecl{{Name: ByteListType, Constructors: []Constructor{{Name: "List.nil", Fields: []string{}}, {Name: "List.cons", Tag: 1, Fields: []string{"UInt8", ByteListType}}}}}
	if record {
		state = "Example.State"
		p.Types = append([]TypeDecl{{Name: state, Constructors: []Constructor{{Name: "Example.State.mk", Fields: []string{"UInt32", "UInt32"}}}}}, p.Types...)
		p.API.Records[state] = []string{"a", "b"}
	}
	p.Functions[0] = Function{Name: p.Roots[0], Params: []Param{{ID: 1, Type: state}, {ID: 2, Type: ByteListType}}, Result: state, Body: &Code{Op: "cases", Var: 2, Type: ByteListType, Result: state, Alts: []Alt{
		{Constructor: "List.nil", Params: []Param{}, Body: &Code{Op: "return", Var: 1}},
		{Constructor: "List.cons", Params: []Param{{ID: 3, Type: "UInt8"}, {ID: 4, Type: ByteListType}}, Body: &Code{Op: "let", Bind: &Param{ID: 5, Type: state}, Value: &Value{Kind: "call", Name: p.Roots[0], Args: []int{1, 4}}, Next: &Code{Op: "return", Var: 5}}},
	}}}
	return declare(p)
}
