package modulegen

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func joinTestProgram() *moduleir.Program {
	p := testProgram()
	p.Schema, p.Profile = moduleir.SchemaV4, moduleir.ProfileV3
	p.Types = []moduleir.TypeDecl{}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32",
		Body: &moduleir.Code{Op: "join", Join: &moduleir.Join{ID: 2, Params: []moduleir.Param{}, Body: &moduleir.Code{Op: "return", Var: 1}}, Next: &moduleir.Code{Op: "jump", Target: 2, Args: []int{}}}}}
	p.Provenance = p.Provenance[:1]
	return p
}

func TestZeroArityJoinExecutionAndCosts(t *testing.T) {
	files, err := Generate(joinTestProgram())
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestCaptureAndCosts(t *testing.T){
 l:=DefaultLimits();l.MaxSteps=3;l.MaxFrames=1;l.MaxSlots=1;l.MaxAllocations=1
 in:=&Input{Type:"UInt32",UInt32:41};v,e:=Import(in,l);if e!=nil{t.Fatal(e)};in.UInt32=99
 for i:=0;i<2;i++{out,e:=Run([]Value{v},l);n,ok:=out.UInt32();if e!=nil||!ok||n!=41{t.Fatal(n,e)}}
 b:=&budget{limits:l};out,e:=execute(b,[]*node{v.n});if e!=nil||out!=v.n||b.steps!=3||b.slots!=1||b.allocations!=0{t.Fatal("join charged extra resources",b,e)}
 l.MaxSteps=2;bad,e:=Run([]Value{v},l);if !errors.Is(e,ErrResource)||bad.Type()!=""{t.Fatal("exact step boundary",e)}
}`)
}

func TestNestedJoinsAcrossCall(t *testing.T) {
	p := joinTestProgram()
	ret := func(id int) *moduleir.Code { return &moduleir.Code{Op: "return", Var: id} }
	let := func(id int, kind, name string, args []int, next *moduleir.Code) *moduleir.Code {
		return &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: id, Type: "UInt32"}, Value: &moduleir.Value{Kind: kind, Name: name, Args: args}, Next: next}
	}
	p.Functions[0].Params = []moduleir.Param{{ID: 1, Type: "UInt32"}, {ID: 2, Type: "UInt32"}}
	p.Functions[0].Body = &moduleir.Code{Op: "join", Join: &moduleir.Join{ID: 3, Params: []moduleir.Param{{ID: 4, Type: "UInt32"}, {ID: 5, Type: "UInt32"}, {ID: 6, Type: "UInt32"}},
		Body: let(7, "call", "Example.second", []int{4, 5}, let(8, "primitive", "UInt32.add", []int{7, 1}, let(9, "primitive", "UInt32.add", []int{8, 6}, ret(9))))},
		Next: &moduleir.Code{Op: "join", Join: &moduleir.Join{ID: 10, Params: []moduleir.Param{{ID: 11, Type: "UInt32"}}, Body: &moduleir.Code{Op: "jump", Target: 3, Args: []int{11, 1, 11}}}, Next: &moduleir.Code{Op: "jump", Target: 10, Args: []int{2}}}}
	p.Functions = append(p.Functions, moduleir.Function{Name: "Example.second", Params: []moduleir.Param{{ID: 1, Type: "UInt32"}, {ID: 2, Type: "UInt32"}}, Result: "UInt32", Body: let(3, "primitive", "UInt32.sub", []int{1, 2}, ret(3))})
	p.Provenance = append(p.Provenance, moduleir.Provenance{Name: "Example.second", Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["control.go"], []byte("f.slots[2], f.slots[3], f.slots[4] =")) {
		t.Fatal("jump does not use parallel parameter assignment")
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestNested(t *testing.T){
 l:=DefaultLimits();l.MaxSteps=10;l.MaxSlots=12;l.MaxFrames=2;l.MaxAllocations=3
 for _,a:=range []uint32{0,7,^uint32(0)}{for _,b:=range []uint32{0,13,^uint32(0)}{
  x,_:=Import(&Input{Type:"UInt32",UInt32:a},l);y,_:=Import(&Input{Type:"UInt32",UInt32:b},l);args:=[]Value{x,y}
  out,e:=Run(args,l);n,ok:=out.UInt32();if e!=nil||!ok||n!=b+b{t.Fatal("nested operands/capture",a,b,n,e)}
  for _,change:=range []func(*Limits){func(l *Limits){l.MaxSteps--},func(l *Limits){l.MaxSlots--},func(l *Limits){l.MaxFrames--},func(l *Limits){l.MaxAllocations--}}{tiny:=l;change(&tiny);out,e=Run(args,tiny);if !errors.Is(e,ErrResource)||out.Type()!=""{t.Fatal("exact budget",tiny,e)}}
 }}
}`)
}

func TestJoinFunctionSelfRecursion(t *testing.T) {
	p := testProgram()
	p.Schema, p.Profile = moduleir.SchemaV4, moduleir.ProfileV3
	for i := range p.Functions {
		p.Functions[i].Body = &moduleir.Code{Op: "join", Join: &moduleir.Join{ID: 100, Params: []moduleir.Param{}, Body: p.Functions[i].Body}, Next: &moduleir.Code{Op: "jump", Target: 100, Args: []int{}}}
	}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, boundaryTest)
}

func TestJoinManifestAndCompatibility(t *testing.T) {
	p := numericProgram()
	p.Schema, p.Profile = moduleir.SchemaV4, moduleir.ProfileV3
	p.Functions[0].Body = &moduleir.Code{Op: "join", Join: &moduleir.Join{ID: 100, Params: []moduleir.Param{}, Body: p.Functions[0].Body}, Next: &moduleir.Code{Op: "jump", Target: 100, Args: []int{}}}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if !bytes.Equal(data, again[name]) {
			t.Fatal("nondeterministic join artifact", name)
		}
	}
	var manifest struct {
		Schema     string               `json:"schema"`
		Generator  string               `json:"generator"`
		Primitives []moduleir.Primitive `json:"primitives"`
		Program    moduleir.Program     `json:"program"`
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "lean-go-manifest/v4" || manifest.Generator != "lean-go-modulegen/v4" || len(manifest.Primitives) != len(numericOperations) {
		t.Fatal("wrong join manifest", manifest.Schema, manifest.Generator, manifest.Primitives)
	}
	raw, err := json.Marshal(manifest.Program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := moduleir.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatal("manifest program round trip", err)
	}
	for _, old := range []*moduleir.Program{testProgram(), numericProgram()} {
		previous, err := Generate(old)
		if err != nil {
			t.Fatal(err)
		}
		var previousManifest struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(previous["manifest.json"], &previousManifest); err != nil {
			t.Fatal(err)
		}
		want := "lean-go-manifest/v2"
		if old.Schema == moduleir.SchemaV3 {
			want = "lean-go-manifest/v3"
		}
		if previousManifest.Schema != want {
			t.Fatal("legacy manifest schema changed", previousManifest.Schema, want)
		}
		old.Schema, old.Profile = moduleir.SchemaV4, moduleir.ProfileV3
		updated, err := Generate(old)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"runtime.go", "schema.go", "control.go"} {
			if !bytes.Equal(previous[name], updated[name]) {
				t.Fatal("profile changed old Go output", name)
			}
		}
	}
}
