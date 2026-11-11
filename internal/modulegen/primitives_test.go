package modulegen

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

var numericOperations = []string{"UInt32.add", "UInt32.sub", "UInt32.mul", "UInt32.shiftLeft", "UInt32.decEq", "UInt32.decLt", "UInt32.decLe"}

func numericProgram() *moduleir.Program {
	p := testProgram()
	p.Schema, p.Profile = "lean-go-ir/v3", "pure-module/v2"
	p.Types = []moduleir.TypeDecl{
		{Name: "Bool", Constructors: []moduleir.Constructor{{Name: "Bool.false", Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}},
		{Name: "Example.Numbers", Constructors: []moduleir.Constructor{{Name: "Example.Numbers.mk", Fields: []string{"UInt32", "UInt32", "UInt32", "UInt32", "Bool", "Bool", "Bool", "UInt32", "UInt32"}}}},
	}
	body := &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 12, Type: "Example.Numbers"}, Value: &moduleir.Value{Kind: "constructor", Name: "Example.Numbers.mk", Args: []int{3, 4, 5, 6, 7, 8, 9, 10, 11}}, Next: &moduleir.Code{Op: "return", Var: 12}}
	for i, literal := range []string{"4294967295", "0"} {
		body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 11 - i, Type: "UInt32"}, Value: &moduleir.Value{Kind: "uint32", Name: literal, Args: []int{}}, Next: body}
	}
	for i := len(numericOperations) - 1; i >= 0; i-- {
		result := "UInt32"
		if i >= 4 {
			result = "Bool"
		}
		body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: i + 3, Type: result}, Value: &moduleir.Value{Kind: "primitive", Name: numericOperations[i], Args: []int{1, 2}}, Next: body}
	}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "UInt32"}, {ID: 2, Type: "UInt32"}}, Result: "Example.Numbers", Body: body}}
	p.Provenance = p.Provenance[:1]
	return p
}

func TestNumericGeneratedExecution(t *testing.T) {
	files, err := Generate(numericProgram())
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, numericConsumerTests)
}

func TestNumericManifest(t *testing.T) {
	p := numericProgram()
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if !bytes.Equal(content, again[name]) {
			t.Fatal("nondeterministic numeric artifact", name)
		}
	}
	var manifest struct {
		Schema     string `json:"schema"`
		Primitives []struct {
			Name, Result, Semantics string
			Params                  []string
		} `json:"primitives"`
	}
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	want := []string{"UInt32.add", "UInt32.decEq", "UInt32.decLe", "UInt32.decLt", "UInt32.mul", "UInt32.shiftLeft", "UInt32.sub"}
	if manifest.Schema != "lean-go-manifest/v3" || len(manifest.Primitives) != len(want) {
		t.Fatal("wrong numeric manifest schema/catalog", manifest)
	}
	for i, primitive := range manifest.Primitives {
		result := "UInt32"
		if strings.Contains(want[i], ".dec") {
			result = "Bool"
		}
		if primitive.Name != want[i] || strings.Join(primitive.Params, ",") != "UInt32,UInt32" || primitive.Result != result || primitive.Semantics == "" {
			t.Fatal("wrong primitive evidence", primitive)
		}
	}
	legacy, err := Generate(testProgram())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacy["manifest.json"], []byte(`"primitives"`)) {
		t.Fatal("old profile manifest gained primitives")
	}
	// Repeated uses do not duplicate evidence, and unused registry members are absent.
	p = numericProgram()
	for code := p.Functions[0].Body; code.Op == "let"; code = code.Next {
		if code.Value.Kind == "primitive" && !strings.Contains(code.Value.Name, ".dec") {
			code.Value.Name = "UInt32.add"
		}
	}
	files, err = Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil || len(manifest.Primitives) != 4 {
		t.Fatal("manifest does not contain exact used primitive subset", manifest, err)
	}
}

func TestNumericBoolCasesAndScalarRoots(t *testing.T) {
	p := numericProgram()
	p.Types = p.Types[:1]
	p.Functions[0].Result = "UInt32"
	p.Functions[0].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 3, Type: "Bool"}, Value: &moduleir.Value{Kind: "primitive", Name: "UInt32.decLt", Args: []int{1, 2}}, Next: &moduleir.Code{Op: "cases", Var: 3, Type: "Bool", Result: "UInt32", Alts: []moduleir.Alt{
		{Constructor: "Bool.false", Params: []moduleir.Param{}, Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 4, Type: "UInt32"}, Value: &moduleir.Value{Kind: "uint32", Name: "4294967295", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 4}}},
		{Constructor: "Bool.true", Params: []moduleir.Param{}, Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 5, Type: "UInt32"}, Value: &moduleir.Value{Kind: "uint32", Name: "0", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 5}}},
	}}}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestBoolCase(t *testing.T){for _,a:=range []uint32{0,2,^uint32(0)}{x,_:=Import(&Input{Type:"UInt32",UInt32:a},DefaultLimits());y,_:=Import(&Input{Type:"UInt32",UInt32:1},DefaultLimits());out,e:=Run([]Value{x,y},DefaultLimits());n,ok:=out.UInt32();want:=^uint32(0);if a==0{want=0};if e!=nil||!ok||n!=want{t.Fatal(a,n,e)}}}`)
	// A nullary literal root needs no declared ADTs and still allocates one node.
	p.Types = []moduleir.TypeDecl{}
	p.Functions[0].Params = []moduleir.Param{}
	p.Functions[0].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 1, Type: "UInt32"}, Value: &moduleir.Value{Kind: "uint32", Name: "4294967295", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 1}}
	files, err = Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestLiteralRoot(t *testing.T){l:=DefaultLimits();l.MaxAllocations=1;l.MaxOutputNodes=1;out,e:=Run(nil,l);n,ok:=out.UInt32();if e!=nil||!ok||n!=^uint32(0){t.Fatal(n,e)}}`)
	if !bytes.Contains(files["manifest.json"], []byte(`"primitives": []`)) {
		t.Fatal("numeric manifest omitted empty primitive catalog")
	}
}

func TestNumericPrimitiveMutation(t *testing.T) {
	p := numericProgram()
	original, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Functions[0].Body.Value.Name = "UInt32.sub"
	mutated, err := Generate(p)
	if err != nil {
		t.Fatal("well-typed mutation failed generation", err)
	}
	if bytes.Equal(original["control.go"], mutated["control.go"]) {
		t.Fatal("primitive mutation did not affect generated execution")
	}
	out, err := runGeneratedOutput(t, mutated, numericConsumerTests)
	if err == nil || !bytes.Contains(out, []byte("numeric mismatch")) || bytes.Contains(out, []byte("[build failed]")) {
		t.Fatalf("mutation did not fail semantic assertion: %v\n%s", err, out)
	}
}

const numericConsumerTests = `package extracted
import("errors";"math/rand";"testing")
func scalar(t *testing.T,n uint32)Value{t.Helper();in:=&Input{Type:"UInt32",UInt32:n};v,e:=Import(in,DefaultLimits());if e!=nil{t.Fatal(e)};in.UInt32=42;return v}
func checkPair(t *testing.T,a,b uint32){t.Helper();left,right:=scalar(t,a),scalar(t,b);values:=[]Value{left,right};for repetition:=0;repetition<2;repetition++{out,e:=Run(values,DefaultLimits());if e!=nil{t.Fatal(e)};fs:=out.Fields();if out.Type()!="Example.Numbers"||out.Constructor()!="Example.Numbers.mk"||len(fs)!=9{t.Fatal("wrong output shape",out)}
 want:=[]uint32{uint32(uint64(a)+uint64(b)),uint32(uint64(a)+(1<<32)-uint64(b)),uint32(uint64(a)*uint64(b)),uint32(uint64(a)*(uint64(1)<<(uint64(b)%32)))};for i,n:=range want{got,ok:=fs[i].UInt32();if !ok||got!=n{t.Fatalf("numeric mismatch a=%d b=%d operation=%d got=%d want=%d",a,b,i,got,n)}}
 for i,want:=range []bool{a==b,a<b,a<=b}{name:="Bool.false";if want{name="Bool.true"};if fs[i+4].Type()!="Bool"||fs[i+4].Constructor()!=name||len(fs[i+4].Fields())!=0{t.Fatalf("comparison mismatch %d %d %d",a,b,i)}}
 zero,_:=fs[7].UInt32();max,_:=fs[8].UInt32();if zero!=0||max!=^uint32(0){t.Fatal("literal mismatch",zero,max)};fs[0]=Value{};if out.Fields()[0].Type()!="UInt32"{t.Fatal("mutable fields accessor")}}
 l,_:=left.UInt32();r,_:=right.UInt32();if l!=a||r!=b{t.Fatal("operands mutated")}}
func TestNumericSemantics(t *testing.T){boundary:=[]uint32{0,1,2,31,32,33,65535,65536,1<<31,^uint32(0)-1,^uint32(0)};for _,a:=range boundary{for _,b:=range boundary{checkPair(t,a,b)}};random:=rand.New(rand.NewSource(73819));for i:=0;i<300;i++{checkPair(t,random.Uint32(),random.Uint32())}}
func TestNumericBudgets(t *testing.T){args:=[]Value{scalar(t,1),scalar(t,2)};l:=DefaultLimits();l.MaxAllocations=10;l.MaxSteps=11;l.MaxOutputNodes=10;if _,e:=Run(args,l);e!=nil{t.Fatal("exact budget",e)};for _,set:=range []func(*Limits){func(l *Limits){l.MaxAllocations=9},func(l *Limits){l.MaxAllocations=1},func(l *Limits){l.MaxSteps=10},func(l *Limits){l.MaxOutputNodes=9},func(l *Limits){l.MaxSlots=11}}{tiny:=l;set(&tiny);out,e:=Run(args,tiny);if !errors.Is(e,ErrResource)||out.Type()!=""{t.Fatal("numeric budget returned partial output",tiny,e)}}}
`
