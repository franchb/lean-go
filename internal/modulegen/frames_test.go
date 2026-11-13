package modulegen

import (
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

// Sibling calls visit the same retired frame with 8, 2, 5, 2, then 8 slots.
// The larger callee also enters a third depth while earlier results stay live.
func frameReuseProgram() *moduleir.Program {
	p := typedProgram()
	ret := func(id int) *moduleir.Code { return &moduleir.Code{Op: "return", Var: id} }
	let := func(id int, typ, kind, name string, args []int, next *moduleir.Code) *moduleir.Code {
		return &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: id, Type: typ}, Value: &moduleir.Value{Kind: kind, Name: name, Args: args}, Next: next}
	}
	leaf := func(id, value int, next *moduleir.Code) *moduleir.Code {
		return let(id, "Example.Tree", "constructor", "Example.Tree.leaf", []int{value}, next)
	}
	branch := func(id, left, right int, next *moduleir.Code) *moduleir.Code {
		return let(id, "Example.Tree", "constructor", "Example.Tree.branch", []int{left, right}, next)
	}
	call := func(id int, name string, next *moduleir.Code) *moduleir.Code {
		return let(id, "Example.Tree", "call", name, []int{1}, next)
	}
	literal := func(id int, value string, next *moduleir.Code) *moduleir.Code {
		return let(id, "UInt32", "uint32", value, []int{}, next)
	}
	parameter := []moduleir.Param{{ID: 1, Type: "UInt32"}}
	p.Functions = []moduleir.Function{
		{Name: "Example.large", Params: parameter, Result: "Example.Tree", Body: literal(2, "10", leaf(3, 2, call(4, "Example.small", literal(5, "11", leaf(6, 5, branch(7, 3, 4, branch(8, 7, 6, ret(8))))))))},
		{Name: "Example.medium", Params: parameter, Result: "Example.Tree", Body: literal(2, "20", leaf(3, 2, leaf(4, 1, branch(5, 3, 4, ret(5)))))},
		{Name: p.Root, Params: parameter, Result: "Example.Tree", Body: call(2, "Example.large", call(3, "Example.small", call(4, "Example.medium", call(5, "Example.small", call(6, "Example.large", branch(7, 2, 3, branch(8, 4, 5, branch(9, 7, 8, branch(10, 9, 6, ret(10))))))))))},
		{Name: "Example.small", Params: parameter, Result: "Example.Tree", Body: leaf(2, 1, ret(2))},
	}
	p.Provenance = []moduleir.Provenance{
		{Name: "Example.large", Dependencies: []string{"Example.Tree", "Example.small"}, Rewrites: []moduleir.Rewrite{}},
		{Name: "Example.medium", Dependencies: []string{"Example.Tree"}, Rewrites: []moduleir.Rewrite{}},
		{Name: p.Root, Dependencies: []string{"Example.Tree", "Example.large", "Example.medium", "Example.small"}, Rewrites: []moduleir.Rewrite{}},
		{Name: "Example.small", Dependencies: []string{"Example.Tree"}, Rewrites: []moduleir.Rewrite{}},
	}
	return p
}

func TestRetiredFrameSlots(t *testing.T) {
	files, err := Generate(frameReuseProgram())
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, retiredFrameTests)
}

const retiredFrameTests = `package extracted
import("errors";"reflect";"sync";"testing")

func TestRetiredFrameStorage(t *testing.T){
 old:=make([]*node,8)
 for i:=range old{old[i]=&node{scalar:uint32(i+1)}}
 retained:=old[7]
 parent:=&node{scalar:42}
 stack:=[]frame{{slots:[]*node{parent}},{function:99,pc:99,destination:99,slots:old}}
 f:=frameForCall(stack[:1],1,2,2)
 if stack[0].slots[0]!=parent{t.Fatal("active parent slots changed")}
 if f.function!=1||f.pc!=0||f.destination!=2||len(f.slots)!=2||cap(f.slots)!=8||&f.slots[0]!=&old[0]{t.Fatal("shrink must reuse and reset frame",f)}
 for i,v:=range old{if v!=nil{t.Fatal("retired tail retained a node",i)}}
 if retained.scalar!=8{t.Fatal("clearing slots mutated returned node")}
 f.slots[0]=retained
 stack[1]=f;f=frameForCall(stack[:1],3,4,8)
 if len(f.slots)!=8||&f.slots[0]!=&old[0]{t.Fatal("growth within capacity did not reuse")}
 for i,v:=range f.slots{if v!=nil{t.Fatal("reused slot not cleared",i)}}
 f.slots[7]=retained
 stack[1]=f;f=frameForCall(stack[:1],5,6,16)
 if len(f.slots)!=16||&f.slots[0]==&old[0]{t.Fatal("insufficient capacity did not allocate")}
 for i,v:=range f.slots{if v!=nil{t.Fatal("new slot not zero",i)}}
 if retained.scalar!=8{t.Fatal("growth mutated returned node")}
 stack[1]=f;f.slots[0]=retained
 fresh:=frameForCall(stack,7,8,2)
 if &fresh.slots[0]==&f.slots[0]||stack[1].slots[0]!=retained{t.Fatal("full stack reused active storage")}
}

func exactLimits()Limits{
 l:=DefaultLimits();l.MaxNodes=1;l.MaxOutputNodes=29;l.MaxDepth=7
 l.MaxSlots=39;l.MaxFrames=3;l.MaxSteps=39;l.MaxAllocations=24
 return l
}
func leaves(t *testing.T,v Value)[]uint32{
 t.Helper()
 if v.Constructor()=="Example.Tree.leaf"{n,ok:=v.Fields()[0].UInt32();if !ok{t.Fatal("not a scalar leaf")};return []uint32{n}}
 fields:=v.Fields();if v.Constructor()!="Example.Tree.branch"||len(fields)!=2{t.Fatal("invalid branch")}
 return append(leaves(t,fields[0]),leaves(t,fields[1])...)
}
// Compare public tree structure: imported scalars may use an empty field slice
// while typed scalar inputs use nil, which is not a semantic difference.
func treeValue(t *testing.T,v Value)any{
 t.Helper()
 if v.Constructor()=="Example.Tree.leaf"{n,ok:=v.Fields()[0].UInt32();if !ok{t.Fatal("not a scalar leaf")};return n}
 fs:=v.Fields();if len(fs)!=2{t.Fatal("invalid branch")}
 return [2]any{treeValue(t,fs[0]),treeValue(t,fs[1])}
}
func checkOutput(t *testing.T,v Value,n uint32){
 t.Helper()
 want:=[]uint32{10,n,11,n,20,n,n,10,n,11}
 if got:=leaves(t,v);!reflect.DeepEqual(got,want){t.Fatal("earlier results changed during slot reuse",got,want)}
}
func TestRepeatedCallsAndBudgets(t *testing.T){
 l:=exactLimits()
 input,e:=Import(&Input{Type:"UInt32",UInt32:7},l);if e!=nil{t.Fatal(e)}
 b:=&budget{limits:l};out,e:=execute(b,[]*node{input.n});if e!=nil{t.Fatal(e)}
 if b.slots!=39||b.steps!=39||b.allocations!=24{t.Fatal("logical budget changed",b)}
 saved:=Value{out};checkOutput(t,saved,7)
 typed,e:=RunTyped(7,l);raw,re:=Run([]Value{input},l)
 if e!=nil||re!=nil||!reflect.DeepEqual(treeValue(t,typed.v),treeValue(t,raw)){t.Fatal("typed/raw exact limits",e,re)}
 checkOutput(t,raw,7)
 if _,e:=RunTyped(99,l);e!=nil{t.Fatal(e)};checkOutput(t,saved,7)
 for _,reduce:=range []func(*Limits){
  func(l *Limits){l.MaxSlots--},func(l *Limits){l.MaxFrames--},
  func(l *Limits){l.MaxSteps--},func(l *Limits){l.MaxAllocations--},
  func(l *Limits){l.MaxOutputNodes--},func(l *Limits){l.MaxDepth--},
 }{
  tiny:=l;reduce(&tiny)
  typed,e:=RunTyped(7,tiny);raw,re:=Run([]Value{input},tiny)
  if !errors.Is(e,ErrResource)||!errors.Is(re,ErrResource)||typed!=(Tree{})||raw!=(Value{}){t.Fatal("exact boundary changed or leaked partial result",tiny,e,re)}
 }
 checkOutput(t,saved,7)
}
func TestConcurrentInvocations(t *testing.T){
 var workers sync.WaitGroup
 outputs:=make([]Value,8);failures:=make(chan error,8)
 for i:=range 8{workers.Go(func(){for range 16{v,e:=RunTyped(uint32(i),exactLimits());if e!=nil{failures<-e;return};outputs[i]=v.v}})}
 workers.Wait();close(failures)
 for e:=range failures{t.Error(e)}
 if t.Failed(){return}
 for i,v:=range outputs{checkOutput(t,v,uint32(i))}
}
`
