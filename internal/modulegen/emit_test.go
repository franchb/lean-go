package modulegen

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testProgram() *moduleir.Program {
	p := &moduleir.Program{Schema: moduleir.Schema, Profile: moduleir.Profile, Stage: moduleir.Stage, LeanVersion: moduleir.LeanVersion, LeanCommit: moduleir.LeanCommit, Module: "Example", Root: "Example.run", Inputs: map[string]string{"Example.lean": strings.Repeat("0", 64)}, Claims: []moduleir.Claim{}, Rewrites: []moduleir.Rewrite{}}
	p.Types = []moduleir.TypeDecl{{Name: "Example.Tree", Constructors: []moduleir.Constructor{{Name: "Example.Tree.leaf", Fields: []string{"UInt32"}}, {Name: "Example.Tree.branch", Tag: 1, Fields: []string{"Example.Tree", "Example.Tree"}}}}}
	ret := func(id int) *moduleir.Code { return &moduleir.Code{Op: "return", Var: id} }
	let := func(id int, kind, name string, args []int, next *moduleir.Code) *moduleir.Code {
		return &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: id, Type: "Example.Tree"}, Value: &moduleir.Value{Kind: kind, Name: name, Args: args}, Next: next}
	}
	p.Functions = []moduleir.Function{
		{Name: "Example.run", Params: []moduleir.Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: let(2, "call", "Example.swap", []int{1}, ret(2))},
		{Name: "Example.swap", Params: []moduleir.Param{{ID: 1, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &moduleir.Code{Op: "cases", Var: 1, Type: "Example.Tree", Result: "Example.Tree", Alts: []moduleir.Alt{
			{Constructor: "Example.Tree.leaf", Params: []moduleir.Param{{ID: 2, Type: "UInt32"}}, Body: let(3, "constructor", "Example.Tree.leaf", []int{2}, ret(3))},
			{Constructor: "Example.Tree.branch", Params: []moduleir.Param{{ID: 4, Type: "Example.Tree"}, {ID: 5, Type: "Example.Tree"}}, Body: let(6, "call", "Example.swap", []int{5}, let(7, "call", "Example.swap", []int{4}, let(8, "constructor", "Example.Tree.branch", []int{6, 7}, ret(8))))},
		}}},
	}
	p.Provenance = []moduleir.Provenance{{Name: "Example.run", Dependencies: []string{"Example.swap"}, Rewrites: []moduleir.Rewrite{}}, {Name: "Example.swap", Dependencies: []string{"Example.Tree", "Example.swap"}, Rewrites: []moduleir.Rewrite{}}}
	return p
}
func TestGenerate(t *testing.T) {
	p := testProgram()
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for n, b := range files {
		if !bytes.Equal(b, again[n]) {
			t.Fatal("not deterministic", n)
		}
	}
	var m struct {
		IR        string            `json:"ir_sha256"`
		Files     map[string]string `json:"generated_files"`
		Validator string            `json:"validator_source_sha256"`
		Generator string            `json:"generator_source_sha256"`
	}
	if err = json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if m.IR != digest(raw) || m.Validator != moduleir.SourceDigest() || m.Generator != SourceDigest() {
		t.Fatal("source identity")
	}
	for n, h := range m.Files {
		if h != digest(files[n]) {
			t.Fatal("artifact digest", n)
		}
	}
	runGenerated(t, files, boundaryTest)
	// A well-typed change in the IR must alter actual generated execution.
	p.Functions[1].Body.Alts[1].Body.Value.Args = []int{4}
	changed, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(changed["control.go"], files["control.go"]) {
		t.Fatal("IR not compiled")
	}
	runGenerated(t, changed, `package extracted
import "testing"
func TestChanged(t *testing.T){v,e:=Import(&Input{Type:"Example.Tree",Constructor:"Example.Tree.branch",Fields:[]*Input{leaf(1),leaf(2)}},DefaultLimits());if e!=nil{t.Fatal(e)};v,e=Run([]Value{v},DefaultLimits());if e!=nil{t.Fatal(e)};for _,c:=range v.Fields(){u,_:=c.Fields()[0].UInt32();if u!=1{t.Fatal("mutation not executable",u)}}}
func leaf(n uint32)*Input{return &Input{Type:"Example.Tree",Constructor:"Example.Tree.leaf",Fields:[]*Input{{Type:"UInt32",UInt32:n}}}}
`)
}
func TestScalarRoot(t *testing.T) {
	p := testProgram()
	p.Types = []moduleir.TypeDecl{}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 17, Type: "UInt32"}}, Result: "UInt32", Body: &moduleir.Code{Op: "return", Var: 17}}}
	p.Provenance = p.Provenance[:1]
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestScalar(t *testing.T){v,e:=Import(&Input{Type:"UInt32",UInt32:^uint32(0)},DefaultLimits());if e!=nil{t.Fatal(e)};v,e=Run([]Value{v},DefaultLimits());n,ok:=v.UInt32();if e!=nil||!ok||n!=^uint32(0){t.Fatal(n,e)}}`)
}
func runGenerated(t *testing.T, files map[string][]byte, source string) {
	t.Helper()
	dir := t.TempDir()
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for n, s := range map[string]string{"go.mod": "module example.test/extracted\n\ngo 1.27.4\n", "generated_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBin, "test", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "PATH="+filepath.Dir(goBin))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated Go: %v\n%s", err, out)
	}
}

const boundaryTest = `package extracted
import("errors";"testing")
func leaf(n uint32)*Input{return &Input{Type:"Example.Tree",Constructor:"Example.Tree.leaf",Fields:[]*Input{{Type:"UInt32",UInt32:n}}}}
func branch(a,b *Input)*Input{return &Input{Type:"Example.Tree",Constructor:"Example.Tree.branch",Fields:[]*Input{a,b}}}
func TestSemantics(t *testing.T){l:=DefaultLimits();in:=branch(leaf(1),leaf(2));v,e:=Import(in,l);if e!=nil{t.Fatal(e)};in.Fields[0].Fields[0].UInt32=99;v,e=Run([]Value{v},l);if e!=nil{t.Fatal(e)};fs:=v.Fields();a,_:=fs[0].Fields()[0].UInt32();b,_:=fs[1].Fields()[0].UInt32();if a!=2||b!=1{t.Fatal("order/ownership",a,b)};fs[0]=Value{};if v.Fields()[0].Type()!="Example.Tree"{t.Fatal("mutable accessor")}}
func TestMalformed(t *testing.T){l:=DefaultLimits();bad:=[]*Input{nil,{}, {Type:"Unknown"}, {Type:"UInt32",Constructor:"X"},{Type:"UInt32",Fields:[]*Input{{}}},{Type:"Example.Tree",Constructor:"Example.Tree.leaf"},{Type:"Example.Tree",Constructor:"Example.Tree.leaf",UInt32:1,Fields:[]*Input{{Type:"UInt32"}}},{Type:"Example.Tree",Constructor:"Example.Tree.leaf",Fields:[]*Input{leaf(0)}}};for i,in:=range bad{if v,e:=Import(in,l);!errors.Is(e,ErrMalformed)||v.Type()!=""{t.Fatal(i,e)}};cycle:=branch(nil,leaf(1));cycle.Fields[0]=cycle;if _,e:=Import(cycle,l);!errors.Is(e,ErrMalformed){t.Fatal("cycle",e)};for _,args:=range [][]Value{nil,{{}},{{},{}}}{if v,e:=Run(args,l);!errors.Is(e,ErrMalformed)||v.Type()!=""{t.Fatal("root boundary",e)}};v,_:=Import(&Input{Type:"UInt32"},l);if _,e:=Run([]Value{v},l);!errors.Is(e,ErrMalformed){t.Fatal("root type",e)}}
func TestSharingAndLimits(t *testing.T){l:=DefaultLimits();child:=leaf(7);in:=branch(child,child);v,e:=Import(in,l);if e!=nil{t.Fatal(e)};if v.n.size!=5||v.n.fields[0]==v.n.fields[1]{t.Fatal("occurrence copying")};tiny:=l;tiny.MaxNodes=4;if _,e:=Import(in,tiny);!errors.Is(e,ErrResource){t.Fatal("input nodes",e)};if _,e:=Run([]Value{v},tiny);!errors.Is(e,ErrResource){t.Fatal("owned nodes",e)};tiny=l;tiny.MaxDepth=2;if _,e:=Import(in,tiny);!errors.Is(e,ErrResource){t.Fatal("depth",e)}
 for field:=0;field<7;field++{tiny=l;fs:=[]*uint64{&tiny.MaxNodes,&tiny.MaxDepth,&tiny.MaxOutputNodes,&tiny.MaxAllocations,&tiny.MaxSteps,&tiny.MaxFrames,&tiny.MaxSlots};*fs[field]=0;if _,e:=Import(child,tiny);!errors.Is(e,ErrLimits){t.Fatal("zero",field,e)};tiny=l;fs=[]*uint64{&tiny.MaxNodes,&tiny.MaxDepth,&tiny.MaxOutputNodes,&tiny.MaxAllocations,&tiny.MaxSteps,&tiny.MaxFrames,&tiny.MaxSlots};*fs[field]++;if _,e:=Import(child,tiny);!errors.Is(e,ErrLimits){t.Fatal("cap",field,e)}}
 for _,set:=range []func(*Limits){func(x *Limits){x.MaxOutputNodes=4},func(x *Limits){x.MaxAllocations=1},func(x *Limits){x.MaxSteps=1},func(x *Limits){x.MaxFrames=1},func(x *Limits){x.MaxSlots=1}}{tiny=l;set(&tiny);if out,e:=Run([]Value{v},tiny);!errors.Is(e,ErrResource)||out.Type()!=""{t.Fatal("execution budget",tiny,e)}}
 dag:=leaf(1);for i:=0;i<25;i++{dag=branch(dag,dag)};if _,e:=Import(dag,l);!errors.Is(e,ErrResource){t.Fatal("DAG expansion",e)}
 deep:=leaf(1);for i:=0;i<1000;i++{deep=branch(deep,leaf(2))};v,e=Import(deep,l);if e!=nil{t.Fatal(e)};if _,e=Run([]Value{v},l);e!=nil{t.Fatal("iterative recursion",e)}
}
`

func TestNullaryRootAndEnums(t *testing.T) {
	p := testProgram()
	p.Types = []moduleir.TypeDecl{{Name: "Colour", Constructors: []moduleir.Constructor{{Name: "Colour.red", Tag: 0, Fields: []string{}}, {Name: "Colour.blue", Tag: 1, Fields: []string{}}}}}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{}, Result: "Colour", Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 1, Type: "Colour"}, Value: &moduleir.Value{Kind: "constructor", Name: "Colour.blue", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 1}}}}
	p.Provenance = p.Provenance[:1]
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestNullary(t *testing.T){v,e:=Run(nil,DefaultLimits());if e!=nil||v.Type()!="Colour"||v.Constructor()!="Colour.blue"{t.Fatal(v,e)}}`)
}
func TestMultipleParametersAndReusedOutput(t *testing.T) {
	p := testProgram()
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 7, Type: "Example.Tree"}, {ID: 999, Type: "Example.Tree"}}, Result: "Example.Tree", Body: &moduleir.Code{Op: "return", Var: 999}}}
	p.Provenance = p.Provenance[:1]
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestMultiple(t *testing.T){l:=DefaultLimits();input:=&Input{Type:"Example.Tree",Constructor:"Example.Tree.leaf",Fields:[]*Input{{Type:"UInt32",UInt32:8}}};v,e:=Import(input,l);if e!=nil{t.Fatal(e)};out,e:=Run([]Value{v,v},l);if e!=nil{t.Fatal(e)};n,_:=out.Fields()[0].UInt32();if n!=8{t.Fatal("wrong root parameter")};l.MaxNodes=3;if _,e=Run([]Value{v,v},l);!errors.Is(e,ErrResource){t.Fatal("aggregate args",e)};l=DefaultLimits();l.MaxOutputNodes=1;if out,e=Run([]Value{v,v},l);!errors.Is(e,ErrResource)||out.Type()!=""{t.Fatal("reused output bound",e)}}`)
}
