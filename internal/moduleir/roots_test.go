package moduleir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func multiRootProgram() *Program {
	p := validProgram()
	p.Schema, p.Profile = SchemaV7, ProfileV5
	p.Root, p.Roots = "", []string{"Example.first", "Example.second"}
	p.API = &APIMetadata{Records: map[string][]string{}}
	p.Functions = nil
	p.Provenance = nil
	for _, name := range p.Roots {
		p.Functions = append(p.Functions, Function{Name: name, Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "return", Var: 1}})
		p.Provenance = append(p.Provenance, Provenance{Name: name, Dependencies: []string{}, Rewrites: []Rewrite{}})
	}
	return p
}

func rootCall(name string) *Code {
	return &Code{Op: "let", Bind: &Param{ID: 2, Type: "Example.Tree"}, Value: &Value{Kind: "call", Name: name, Args: []int{1}}, Next: &Code{Op: "return", Var: 2}}
}

func TestMultiRootUnionClosure(t *testing.T) {
	p := multiRootProgram()
	if err := Validate(p); err != nil {
		t.Fatal("disjoint roots with shared nominal type", err)
	}
	helper := Function{Name: "Example.helper", Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "return", Var: 1}}
	p.Functions = append(p.Functions[:1], helper, p.Functions[1])
	p.Provenance = append(p.Provenance[:1], Provenance{Name: helper.Name, Dependencies: []string{}, Rewrites: []Rewrite{}}, p.Provenance[1])
	p.Functions[0].Body = rootCall(helper.Name)
	p.Functions[2].Body = rootCall(helper.Name)
	if err := Validate(p); err != nil {
		t.Fatal("shared helper in union", err)
	}
	p.Functions[0].Body = &Code{Op: "return", Var: 1}
	if err := Validate(p); err != nil {
		t.Fatal("helper reachable only from secondary root", err)
	}
	p.Functions[1].Body = rootCall("Example.second")
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "mutual recursion") {
		t.Fatal("secondary-root mutual recursion was not rejected", err)
	}
	p.Functions[1].Body = rootCall(helper.Name)
	if err := Validate(p); err != nil {
		t.Fatal("self recursion must remain supported", err)
	}
	p.Functions[2].Body = &Code{Op: "return", Var: 1}
	if err := Validate(p); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatal("accepted helper unreachable from every selected root", err)
	}
}

func TestRejectInvalidRootSets(t *testing.T) {
	changes := map[string]func(*Program){
		"no roots":        func(p *Program) { p.Roots = nil },
		"empty roots":     func(p *Program) { p.Roots = []string{} },
		"one root":        func(p *Program) { p.Roots = p.Roots[:1] },
		"duplicate":       func(p *Program) { p.Roots[1] = p.Roots[0] },
		"unordered":       func(p *Program) { p.Roots[0], p.Roots[1] = p.Roots[1], p.Roots[0] },
		"missing root":    func(p *Program) { p.Roots[1] = "Example.unknown" },
		"invalid root":    func(p *Program) { p.Roots[0] = "bad\nname" },
		"overlong root":   func(p *Program) { p.Roots[1] = strings.Repeat("x", MaxNameBytes+1) },
		"legacy root":     func(p *Program) { p.Root = p.Roots[0] },
		"old profile":     func(p *Program) { p.Profile = ProfileV4 },
		"old schema":      func(p *Program) { p.Schema = SchemaV6 },
		"missing api":     func(p *Program) { p.API = nil },
		"invalid second":  func(p *Program) { p.Functions[1].Result = "UInt64" },
		"unbound second":  func(p *Program) { p.Functions[1].Body.Var = 99 },
		"second evidence": func(p *Program) { p.Provenance = p.Provenance[:1] },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			p := multiRootProgram()
			change(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid multi-root program")
			}
		})
	}
}

func TestMultiRootCountBound(t *testing.T) {
	p := multiRootProgram()
	p.Roots, p.Functions, p.Provenance = nil, nil, nil
	for i := 0; i <= MaxRoots; i++ {
		name := fmt.Sprintf("Example.root%02d", i)
		p.Roots = append(p.Roots, name)
		p.Functions = append(p.Functions, Function{Name: name, Params: []Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &Code{Op: "return", Var: 1}})
		p.Provenance = append(p.Provenance, Provenance{Name: name, Dependencies: []string{}, Rewrites: []Rewrite{}})
		if i == MaxRoots-1 {
			if err := Validate(p); err != nil {
				t.Fatal("rejected root limit", err)
			}
		}
	}
	if err := Validate(p); err == nil {
		t.Fatal("accepted too many roots")
	}
}

func TestMultiRootStrictDecode(t *testing.T) {
	p := multiRootProgram()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(bytes.NewReader(raw))
	if err != nil || !reflect.DeepEqual(RootNames(decoded), p.Roots) {
		t.Fatal("valid multi-root wire", err)
	}
	if bytes.Contains(raw, []byte(`"root":`)) {
		t.Fatal("v7 emitted legacy root")
	}
	roots := `"roots":["Example.first","Example.second"]`
	for name, bad := range map[string][]byte{
		"absent":        bytes.Replace(raw, []byte(roots+","), nil, 1),
		"null":          bytes.Replace(raw, []byte(roots), []byte(`"roots":null`), 1),
		"scalar":        bytes.Replace(raw, []byte(roots), []byte(`"roots":"Example.first"`), 1),
		"number":        bytes.Replace(raw, []byte(roots), []byte(`"roots":[1,"Example.second"]`), 1),
		"mixed root":    bytes.Replace(raw, []byte(roots), []byte(roots+`,"root":""`), 1),
		"duplicate key": bytes.Replace(raw, []byte(roots), []byte(roots+`,"roots":[]`), 1),
		"unknown":       bytes.Replace(raw, []byte(roots), []byte(roots+`,"entrypoints":[]`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(raw, bad) {
				t.Fatal("test did not change the wire")
			}
			if _, err := Decode(bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted malformed multi-root wire")
			}
		})
	}
}

func TestLegacyRootShapeCompatibility(t *testing.T) {
	for _, pair := range [][2]string{{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV3}, {SchemaV5, ProfileV3}, {SchemaV6, ProfileV4}} {
		t.Run(pair[0], func(t *testing.T) {
			p := validProgram()
			p.Schema, p.Profile = pair[0], pair[1]
			if HasTypedAPI(p.Schema) {
				p.API = &APIMetadata{Records: map[string][]string{}}
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(bytes.NewReader(raw)); err != nil {
				t.Fatal("legacy wire rejected", err)
			}
			if !reflect.DeepEqual(RootNames(p), []string{p.Root}) || bytes.Contains(raw, []byte(`"roots":`)) {
				t.Fatal("legacy root encoding changed")
			}
			for _, value := range []string{`null`, `[]`, `["Example.root"]`} {
				bad := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"roots":`+value+`}`)...)
				if _, err := Decode(bytes.NewReader(bad)); err == nil {
					t.Fatal("legacy wire accepted roots", value)
				}
			}
			p.Roots = []string{}
			if err := Validate(p); err == nil {
				t.Fatal("legacy constructed program accepted nonnil roots")
			}
		})
	}
	if HasTypedAPI("unknown") || HasUInt64("unknown") || HasUInt64(SchemaV5) || !HasUInt64(SchemaV7) {
		t.Fatal("schema capability helpers admitted the wrong family")
	}
}

func TestMultiRootWideAndJoinCapabilities(t *testing.T) {
	p := multiRootProgram()
	p.Functions[1] = Function{Name: p.Roots[1], Params: []Param{{ID: 1, Type: "UInt64"}}, Result: "UInt64", Body: &Code{
		Op: "join", Join: &Join{ID: 2, Params: []Param{{ID: 3, Type: "UInt64"}}, Body: &Code{Op: "return", Var: 3}}, Next: &Code{Op: "jump", Target: 2, Args: []int{1}}}}
	if err := Validate(p); err != nil {
		t.Fatal("v7 lost wide join capability", err)
	}
	p.Functions[1].Body = &Code{Op: "let", Bind: &Param{ID: 2, Type: "UInt64"}, Value: &Value{Kind: "primitive", Name: "UInt64.add", Args: []int{1, 1}}, Next: &Code{Op: "return", Var: 2}}
	if err := Validate(p); err != nil {
		t.Fatal("v7 lost wide primitive capability", err)
	}
}
