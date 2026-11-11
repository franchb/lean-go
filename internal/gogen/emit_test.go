package gogen

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/ir"
)

func testProgram() *ir.Program {
	const ns = "LeanGo.Feasibility."
	ret := func(id int) *ir.Code { return &ir.Code{Op: "return", Var: id} }
	let := func(id int, typ, kind, name string, args []int, next *ir.Code) *ir.Code {
		return &ir.Code{Op: "let", Bind: &ir.Param{ID: id, Type: typ}, Value: &ir.Value{Kind: kind, Name: name, Args: args}, Next: next}
	}
	p := &ir.Program{Schema: ir.Schema, Profile: ir.Profile, Stage: ir.Stage, LeanVersion: ir.LeanVersion, LeanCommit: ir.LeanCommit, ModelSHA256: ir.ModelSHA256, Root: ir.Root, Inputs: map[string]string{}}
	for _, name := range []string{"feasibility/LeanGo/Feasibility/Model.lean", "producer/lean-toolchain", "producer/lakefile.toml", "producer/Export.lean", "producer/LeanGo/Pilot/Export.lean", "producer/Reference.lean", "scripts/export.py"} {
		p.Inputs[name] = strings.Repeat("0", 64)
	}
	p.Inputs["feasibility/LeanGo/Feasibility/Model.lean"] = ir.ModelSHA256
	p.Types = []ir.TypeDecl{{Name: "bool", Constructors: []ir.Constructor{{Name: "Bool.false", Tag: 0, Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}}, {Name: "tri", Constructors: []ir.Constructor{{Name: ns + "Tri.f", Tag: 0, Fields: []string{}}, {Name: ns + "Tri.u", Tag: 1, Fields: []string{}}, {Name: ns + "Tri.t", Tag: 2, Fields: []string{}}}}, {Name: "expr", Constructors: []ir.Constructor{{Name: ns + "Expr.lit", Tag: 0, Fields: []string{"tri"}}, {Name: ns + "Expr.var", Tag: 1, Fields: []string{"u32"}}, {Name: ns + "Expr.neg", Tag: 2, Fields: []string{"expr"}}, {Name: ns + "Expr.conj", Tag: 3, Fields: []string{"expr", "expr"}}, {Name: ns + "Expr.disj", Tag: 4, Fields: []string{"expr", "expr"}}}}}
	p.Claims = []ir.Claim{{Name: ns + "normalize_preserves_eval", Statement: "forall e : Expr, forall rho : UInt32 -> Tri, eval rho (normalize e) = eval rho e", Axioms: []string{"propext"}}, {Name: ns + "normalize_is_normal", Statement: "forall e : Expr, IsNormalForm (normalize e)", Axioms: []string{"propext"}}}
	p.Rewrites = []ir.Rewrite{{Source: "Bool.not", Target: "Bool.Internal.not", Theorem: "Bool.not_eq_internalNot", Axioms: []string{"Quot.sound"}}}
	neg := ir.Function{Name: ns + "Tri.neg", Params: []ir.Param{{ID: 1, Type: "tri"}}, Result: "tri", Body: &ir.Code{Op: "cases", Var: 1, Type: "tri", Result: "tri", Alts: []ir.Alt{{Constructor: ns + "Tri.f", Body: let(2, "tri", "constructor", ns+"Tri.t", nil, ret(2))}, {Constructor: ns + "Tri.u", Body: let(3, "tri", "constructor", ns+"Tri.u", nil, ret(3))}, {Constructor: ns + "Tri.t", Body: let(4, "tri", "constructor", ns+"Tri.f", nil, ret(4))}}}}
	root := ir.Function{Name: ir.Root, Params: []ir.Param{{ID: 1, Type: "expr"}}, Result: "expr", Body: let(2, "bool", "constructor", "Bool.false", nil, let(3, "expr", "call", ns+"normalizeAux", []int{2, 1}, ret(3)))}
	aux := ir.Function{Name: ns + "normalizeAux", Params: []ir.Param{{ID: 1, Type: "bool"}, {ID: 2, Type: "expr"}}, Result: "expr", Body: &ir.Code{Op: "cases", Var: 2, Type: "expr", Result: "expr", Alts: []ir.Alt{
		{Constructor: ns + "Expr.lit", Params: []ir.Param{{ID: 3, Type: "tri"}}, Body: let(4, "tri", "call", ns+"Tri.neg", []int{3}, let(5, "expr", "constructor", ns+"Expr.lit", []int{4}, ret(5)))},
		{Constructor: ns + "Expr.var", Params: []ir.Param{{ID: 6, Type: "u32"}}, Body: let(7, "expr", "constructor", ns+"Expr.var", []int{6}, ret(7))},
		{Constructor: ns + "Expr.neg", Params: []ir.Param{{ID: 8, Type: "expr"}}, Body: let(9, "expr", "call", ns+"normalizeAux", []int{1, 8}, ret(9))},
		{Constructor: ns + "Expr.conj", Params: []ir.Param{{ID: 10, Type: "expr"}, {ID: 11, Type: "expr"}}, Body: let(12, "expr", "constructor", ns+"Expr.conj", []int{10, 11}, ret(12))},
		{Constructor: ns + "Expr.disj", Params: []ir.Param{{ID: 13, Type: "expr"}, {ID: 14, Type: "expr"}}, Body: let(15, "expr", "constructor", ns+"Expr.disj", []int{13, 14}, ret(15))},
	}}}
	p.Functions = []ir.Function{neg, root, aux}
	return p
}
func TestGenerateDeterministicAndManifest(t *testing.T) {
	p := testProgram()
	first, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range first {
		if !bytes.Equal(data, second[name]) {
			t.Fatalf("nondeterministic %s", name)
		}
	}
	var m struct {
		IR              string            `json:"ir_sha256"`
		GeneratorSource string            `json:"generator_source_sha256"`
		ValidatorSource string            `json:"validator_source_sha256"`
		Files           map[string]string `json:"generated_files"`
	}
	if err = json.Unmarshal(first["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(p)
	if m.IR != digest(canonical) || m.GeneratorSource != SourceDigest() || m.ValidatorSource != ir.SourceDigest() {
		t.Fatal("wrong evidence identity")
	}
	for name, hash := range m.Files {
		if digest(first[name]) != hash {
			t.Fatal("wrong generated digest")
		}
	}
	if _, ok := m.Files["manifest.json"]; ok {
		t.Fatal("manifest hashes itself")
	}
	p.ModelSHA256 = "bad"
	if files, err := Generate(p); err == nil || files != nil {
		t.Fatal("invalid IR emitted output")
	}
}
func TestGeneratedBoundaryAndIRMutation(t *testing.T) {
	p := testProgram()
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, boundaryTest)
	// A valid constructor remapping must change executable behavior, not metadata alone.
	p.Functions[0].Body.Alts[0].Body.Value.Name = "LeanGo.Feasibility.Tri.f"
	changed, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(files["control.go"], changed["control.go"]) {
		t.Fatal("IR mutation did not change code")
	}
	runGenerated(t, changed, `package normalizer
import "testing"
func TestMutation(t *testing.T){e,err:=Import(&Input{Kind:Literal},DefaultLimits());if err!=nil{t.Fatal(err)};n,err:=Normalize(e,DefaultLimits());v,ok:=n.Literal();if err!=nil||!ok||v!=False{t.Fatal("changed IR not executed",v,err)}}`)
}
func runGenerated(t *testing.T, files map[string][]byte, tests string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"go.mod": "module example.test/normalizer\n\ngo 1.27.4\n", "boundary_test.go": tests} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated test: %v\n%s", err, out)
	}
}

const boundaryTest = `package normalizer
import("errors";"testing")
func TestBoundary(t *testing.T){
 l:=DefaultLimits();bad:=[]*Input{nil,{}, {Kind:Literal,Value:Tri(3)}, {Kind:Literal,ID:1},{Kind:Variable,Value:True},{Kind:Variable,Left:&Input{}},{Kind:Not},{Kind:Not,Left:&Input{Kind:Literal},Right:&Input{}},{Kind:And,Left:&Input{Kind:Literal}},{Kind:Or,Left:&Input{Kind:Literal},Right:&Input{Kind:Literal},ID:1}}
 for i,in:=range bad {e,err:=Import(in,l);if !errors.Is(err,ErrMalformed)||e.Kind()!=Invalid{t.Fatal("malformed",i,err)}}
 cycle:=&Input{Kind:Not};cycle.Left=cycle;if _,err:=Import(cycle,l);!errors.Is(err,ErrMalformed){t.Fatal("cycle",err)}
 source:=&Input{Kind:Variable,ID:^uint32(0)};e,err:=Import(source,l);if err!=nil{t.Fatal(err)};source.ID=1;id,ok:=e.Variable();if !ok||id!=^uint32(0){t.Fatal("ownership")};n,err:=Normalize(e,l);id,ok=n.Variable();if err!=nil||!ok||id!=^uint32(0){t.Fatal("u32",err)}
 shared:=&Input{Kind:And,Left:source,Right:source};e,err=Import(shared,l);if err!=nil{t.Fatal(err)};a,b:=e.Children();if a.n==b.n||e.n.size!=3||e.n.depth!=2{t.Fatal("sharing not occurrence copied")}
 tiny:=l;tiny.MaxNodes=2;if _,err=Import(shared,tiny);!errors.Is(err,ErrResource){t.Fatal("input count",err)};tiny=l;tiny.MaxDepth=1;if _,err=Import(shared,tiny);!errors.Is(err,ErrResource){t.Fatal("depth",err)}
 if _,err=Normalize(Expr{},l);!errors.Is(err,ErrMalformed){t.Fatal("zero",err)};tiny=l;tiny.MaxNodes=2;if _,err=Normalize(e,tiny);!errors.Is(err,ErrResource){t.Fatal("owned count",err)}
 for field:=0;field<6;field++{tiny=l;fields:=[]*uint64{&tiny.MaxNodes,&tiny.MaxDepth,&tiny.MaxOutputNodes,&tiny.MaxAllocations,&tiny.MaxSteps,&tiny.MaxFrames};*fields[field]=0;if _,err=Import(source,tiny);!errors.Is(err,ErrLimits){t.Fatal("zero policy",field,err)};tiny=l;fields=[]*uint64{&tiny.MaxNodes,&tiny.MaxDepth,&tiny.MaxOutputNodes,&tiny.MaxAllocations,&tiny.MaxSteps,&tiny.MaxFrames};*fields[field]++;if _,err=Import(source,tiny);!errors.Is(err,ErrLimits){t.Fatal("above policy",field,err)}}
 tiny=l;tiny.MaxOutputNodes=2;if out,err:=Normalize(e,tiny);!errors.Is(err,ErrResource)||out.Kind()!=Invalid{t.Fatal("reused output count",err)}
 lit,err:=Import(&Input{Kind:Literal},l);if err!=nil{t.Fatal(err)};n,err=Normalize(lit,l);v,ok:=n.Literal();if err!=nil||!ok||v!=True{t.Fatal("compiled tri branch",v,err)}
 tiny=l;tiny.MaxSteps=1;if out,err:=Normalize(lit,tiny);!errors.Is(err,ErrResource)||out.Kind()!=Invalid{t.Fatal("steps",err)};tiny=l;tiny.MaxFrames=1;if _,err=Normalize(lit,tiny);!errors.Is(err,ErrResource){t.Fatal("frames",err)}
 nested,err:=Import(&Input{Kind:Not,Left:&Input{Kind:Not,Left:&Input{Kind:Literal}}},l);if err!=nil{t.Fatal(err)};if _,err=Normalize(nested,l);err!=nil{t.Fatal("continuations",err)}
 bgt:=&budget{limits:l};bgt.limits.MaxAllocations=1;if _,err=bgt.make(Literal,False,0,nil,nil);err!=nil{t.Fatal(err)};if _,err=bgt.make(Literal,False,0,nil,nil);!errors.Is(err,ErrResource){t.Fatal("allocations",err)}
 bgt=&budget{limits:l};bgt.limits.MaxDepth=1;if _,err=bgt.make(Not,False,0,lit.n,nil);!errors.Is(err,ErrResource){t.Fatal("output depth",err)}
}`
