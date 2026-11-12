package modulegen

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func multiRootProgram() *moduleir.Program {
	p := typedProgram()
	p.Schema, p.Profile, p.Root = moduleir.SchemaV7, moduleir.ProfileV5, ""
	p.Roots = []string{"Example.bump", "Example.create", "Example.read", "Example.small", "Example.zero"}
	p.Types = []moduleir.TypeDecl{{Name: "Example.Counter", Constructors: []moduleir.Constructor{{Name: "Example.Counter.mk", Fields: []string{"UInt64"}}}}}
	p.API.Records = map[string][]string{"Example.Counter": {"value"}}
	ret := func(id int) *moduleir.Code { return &moduleir.Code{Op: "return", Var: id} }
	bind := func(id int, typ, kind, name string, args []int, next *moduleir.Code) *moduleir.Code {
		return &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: id, Type: typ}, Value: &moduleir.Value{Kind: kind, Name: name, Args: args}, Next: next}
	}
	p.Functions = []moduleir.Function{
		{Name: "Example.bump", Params: []moduleir.Param{{ID: 4, Type: "Example.Counter"}, {ID: 9, Type: "UInt64"}}, Result: "Example.Counter", Body: bind(11, "UInt64", "call", "Example.helper", []int{4},
			bind(18, "UInt64", "primitive", "UInt64.add", []int{11, 9},
				bind(20, "Example.Counter", "constructor", "Example.Counter.mk", []int{18}, ret(20))))},
		{Name: "Example.create", Params: []moduleir.Param{{ID: 42, Type: "UInt64"}}, Result: "Example.Counter", Body: bind(43, "Example.Counter", "constructor", "Example.Counter.mk", []int{42}, ret(43))},
		{Name: "Example.helper", Params: []moduleir.Param{{ID: 7, Type: "Example.Counter"}}, Result: "UInt64", Body: &moduleir.Code{Op: "cases", Var: 7, Type: "Example.Counter", Result: "UInt64", Alts: []moduleir.Alt{{Constructor: "Example.Counter.mk", Params: []moduleir.Param{{ID: 99, Type: "UInt64"}}, Body: ret(99)}}}},
		{Name: "Example.read", Params: []moduleir.Param{{ID: 5, Type: "Example.Counter"}}, Result: "UInt64", Body: bind(8, "UInt64", "call", "Example.helper", []int{5}, ret(8))},
		{Name: "Example.small", Params: []moduleir.Param{{ID: 123, Type: "UInt32"}}, Result: "UInt32", Body: ret(123)},
		{Name: "Example.zero", Params: []moduleir.Param{}, Result: "UInt64", Body: bind(31, "UInt64", "uint64", "0", []int{}, ret(31))},
	}
	p.Provenance = nil
	for _, f := range p.Functions {
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: f.Name, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	return p
}

func TestMultiRootSharedValuesAndDispatch(t *testing.T) {
	files, err := Generate(multiRootProgram())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"runtime.go", "api.go"} {
		if bytes.Contains(files[source], []byte("func Run(")) || bytes.Contains(files[source], []byte("func RunTyped(")) {
			t.Fatal("multi-root package has an implicit default entry", source)
		}
	}
	runGenerated(t, files, multiRootExecution)
}

const multiRootExecution = `package extracted
import("errors";"testing")
func TestSharedValues(t *testing.T){
 l:=DefaultLimits();start,e:=RunCreate(^uint64(0),l);if e!=nil{t.Fatal(e)};next,e:=RunBump(start,2,l);if e!=nil{t.Fatal(e)};value,e:=RunRead(next,l);if e!=nil||value!=1{t.Fatal(value,e)}
 old,e:=RunRead(start,l);if e!=nil||old!=^uint64(0){t.Fatal("input mutated",old,e)};field,ok:=next.Value();if !ok||field!=1{t.Fatal("shared getter",field)}
 amount,e:=Import(&Input{Type:"UInt64",UInt64:2},l);if e!=nil{t.Fatal(e)};raw,e:=RunRoot("Example.bump",[]Value{start.v,amount},l);if e!=nil{t.Fatal(e)};read,e:=RunRoot("Example.read",[]Value{raw},l);n,ok:=read.UInt64();if e!=nil||!ok||n!=1{t.Fatal("raw dispatch",n,e)}
 if small,e:=RunSmall(^uint32(0),l);e!=nil||small!=^uint32(0){t.Fatal("narrow root",small,e)};if zero,e:=RunZero(l);e!=nil||zero!=0{t.Fatal("nullary",zero,e)}
 for _,name:=range []string{"","Example.helper","Example.missing"}{if out,e:=RunRoot(name,[]Value{start.v},l);!errors.Is(e,ErrMalformed)||out!=(Value{}){t.Fatal("hidden root",name,e)}}
 for _,args:=range [][]Value{nil,{start.v},{amount,amount},{start.v,{}},{start.v,amount,amount}}{if out,e:=RunRoot("Example.bump",args,l);!errors.Is(e,ErrMalformed)||out!=(Value{}){t.Fatal("signature",e)}}
 if out,e:=RunRead(Counter{},l);!errors.Is(e,ErrMalformed)||out!=0{t.Fatal("invalid typed value",e)}
}
func TestBudgetsPerRootCall(t *testing.T){
 l:=DefaultLimits();value,e:=RunCreate(7,l);if e!=nil{t.Fatal(e)};delta,e:=Import(&Input{Type:"UInt64",UInt64:2},l);if e!=nil{t.Fatal(e)}
 changes:=[]func(*Limits){func(l *Limits){l.MaxNodes=2},func(l *Limits){l.MaxDepth=1},func(l *Limits){l.MaxOutputNodes=1},func(l *Limits){l.MaxAllocations=1},func(l *Limits){l.MaxSteps=1},func(l *Limits){l.MaxFrames=1},func(l *Limits){l.MaxSlots=1}}
 for _,change:=range changes{tiny:=l;change(&tiny);out,e:=RunBump(value,2,tiny);raw,re:=RunRoot("Example.bump",[]Value{value.v,delta},tiny);if !errors.Is(e,ErrResource)||!errors.Is(re,ErrResource)||out!=(Counter{})||raw!=(Value{}){t.Fatal("resource parity",tiny,e,re)}}
 for i:=0;i<7;i++{bad:=l;fields:=[]*uint64{&bad.MaxNodes,&bad.MaxDepth,&bad.MaxOutputNodes,&bad.MaxAllocations,&bad.MaxSteps,&bad.MaxFrames,&bad.MaxSlots};*fields[i]=0;if out,e:=RunSmall(1,bad);!errors.Is(e,ErrLimits)||out!=0{t.Fatal("limits",i,e)}}
 l.MaxAllocations=2;for i:=0;i<3;i++{value,e=RunBump(value,2,l);if e!=nil{t.Fatal("budget leaked between roots",e)};n,e:=RunRead(value,l);if e!=nil||n!=uint64(9+2*i){t.Fatal(n,e)}}
}
`

func TestMultiRootManifestAndDeterminism(t *testing.T) {
	p := multiRootProgram()
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema     string               `json:"schema"`
		API        goAPI                `json:"go_api"`
		Functions  map[string]int       `json:"function_ids"`
		Primitives []moduleir.Primitive `json:"primitives"`
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Example.bump": "RunBump", "Example.create": "RunCreate", "Example.read": "RunRead", "Example.small": "RunSmall", "Example.zero": "RunZero"}
	if manifest.Schema != "lean-go-manifest/v7" || manifest.API.Entrypoint != "" || !reflect.DeepEqual(manifest.API.Entrypoints, want) || len(manifest.Functions) != 6 || len(manifest.Primitives) != 1 {
		t.Fatalf("incomplete manifest: %+v", manifest)
	}
	if bytes.Contains(files["manifest.json"], []byte(`"entrypoint":`)) || bytes.Contains(files["manifest.json"], []byte(`"root":`)) {
		t.Fatal("multi-root manifest has a scalar root selection")
	}
	again, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range files {
		if !bytes.Equal(source, again[name]) {
			t.Fatal("nondeterministic output", name)
		}
	}
}

func TestMultiRootNameCollisions(t *testing.T) {
	p := multiRootProgram()
	p.Types = []moduleir.TypeDecl{{Name: "Example.RunSame", Constructors: []moduleir.Constructor{{Name: "Example.RunSame.mk", Fields: []string{"UInt64"}}}}}
	p.API.Records = map[string][]string{"Example.RunSame": {"value"}}
	p.Roots = []string{"A.same", "B.same", "C.root", "D.typed"}
	p.Functions = nil
	p.Provenance = nil
	for _, name := range p.Roots {
		p.Functions = append(p.Functions, moduleir.Function{Name: name, Params: []moduleir.Param{{ID: 3, Type: "Example.RunSame"}}, Result: "Example.RunSame", Body: &moduleir.Code{Op: "return", Var: 3}})
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: name, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A.same": "RunSame2", "B.same": "RunSame3", "C.root": "RunRoot2", "D.typed": "RunTyped2"}
	if got := newGoAPI(p).Entrypoints; !reflect.DeepEqual(got, want) {
		t.Fatalf("names: %v", got)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestNames(t *testing.T){l:=DefaultLimits();v,e:=NewRunSame(7,l);if e!=nil{t.Fatal(e)};for _,call:=range []func(RunSame,Limits)(RunSame,error){RunSame2,RunSame3,RunRoot2,RunTyped2}{v,e=call(v,l);n,ok:=v.Value();if e!=nil||!ok||n!=7{t.Fatal(n,e)}}}`)
}

func TestMultiRootRecursiveSharedClosure(t *testing.T) {
	p := typedProgram()
	p.Schema, p.Profile, p.Root = moduleir.SchemaV7, moduleir.ProfileV5, ""
	p.Roots = []string{"Example.run", "Example.swap"}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestRecursion(t *testing.T){l:=DefaultLimits();a,_:=NewTreeLeaf(1,l);b,_:=NewTreeLeaf(2,l);tree,_:=NewTreeBranch(a,b,l);first,e:=RunRun(tree,l);if e!=nil{t.Fatal(e)};second,e:=RunSwap(first,l);if e!=nil{t.Fatal(e)};left,right,ok:=second.AsBranch();x,xok:=left.AsLeaf();y,yok:=right.AsLeaf();if !ok||!xok||!yok||x!=1||y!=2{t.Fatal(x,y)}}`)
}

func TestSingleRootKeepsPreviouslyAvailableNames(t *testing.T) {
	p := typedProgram()
	p.Schema, p.Profile = moduleir.SchemaV6, moduleir.ProfileV4
	p.Types = []moduleir.TypeDecl{{Name: "Example.RunRoot", Constructors: []moduleir.Constructor{{Name: "Example.RunRoot.mk", Fields: []string{"UInt64"}}}}}
	p.API.Records = map[string][]string{"Example.RunRoot": {"value"}}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "Example.RunRoot"}}, Result: "Example.RunRoot", Body: &moduleir.Code{Op: "return", Var: 1}}}
	p.Provenance = p.Provenance[:1]
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if api := newGoAPI(p); api.Types["Example.RunRoot"] != "RunRoot" || api.Entrypoint != "RunTyped" || len(api.Entrypoints) != 0 {
		t.Fatalf("legacy names changed: %+v", api)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestLegacyName(t *testing.T){l:=DefaultLimits();v,e:=NewRunRoot(9,l);if e!=nil{t.Fatal(e)};var input RunRoot=v;out,e:=RunTyped(input,l);n,ok:=out.Value();if e!=nil||!ok||n!=9{t.Fatal(n,e)}}`)
}
