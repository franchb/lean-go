package modulegen

import (
	"bytes"
	"fmt"

	"github.com/franchb/lean-go/internal/moduleir"
)

func (g *generator) emitRootMetadata(w *bytes.Buffer) {
	if moduleir.HasMultipleRoots(g.program.Schema) {
		g.emitRootSignatures(w)
		return
	}
	root := g.functions[g.program.Root].decl
	w.WriteString("func rootParameters()[]int{return []int{")
	for _, param := range root.Params {
		fmt.Fprintf(w, "%d,", g.types[param.Type])
	}
	w.WriteString("}}\n")
	fmt.Fprintf(w, "func rootResult()int{return %d}\n", g.types[root.Result])
}

// Root selection is closed over the explicitly exported names, even when the
// shared runtime closure contains additional callable helpers.
func (g *generator) emitRootSignatures(w *bytes.Buffer) {
	w.WriteString("func rootSignature(root string)(int,int,[]int,bool){switch root{\n")
	for _, name := range moduleir.RootNames(g.program) {
		root := g.functions[name]
		fmt.Fprintf(w, "case %q:return %d,%d,[]int{", name, root.id, g.types[root.decl.Result])
		for _, param := range root.decl.Params {
			fmt.Fprintf(w, "%d,", g.types[param.Type])
		}
		w.WriteString("},true\n")
	}
	w.WriteString("};return 0,0,nil,false}\n")
}

func (g *generator) emitInitialFrame(w *bytes.Buffer) {
	if !moduleir.HasMultipleRoots(g.program.Schema) {
		root := g.functions[g.program.Root]
		fmt.Fprintf(w, "func execute(b *budget,args []*node)(*node,error){if err:=b.enter(%d,0);err!=nil{return nil,err};initial:=frame{function:%d,destination:-1,slots:make([]*node,%d)}\n", len(root.slots), root.id, len(root.slots))
		g.emitRootArguments(w, root)
		return
	}
	w.WriteString("func execute(b *budget,entry int,args []*node)(*node,error){var initial frame;switch entry{\n")
	for _, name := range moduleir.RootNames(g.program) {
		root := g.functions[name]
		fmt.Fprintf(w, "case %d:if err:=b.enter(%d,0);err!=nil{return nil,err};initial=frame{function:%d,destination:-1,slots:make([]*node,%d)}\n", root.id, len(root.slots), root.id, len(root.slots))
		g.emitRootArguments(w, root)
	}
	w.WriteString("default:return nil,ErrMalformed}\n")
}

func (g *generator) emitRootArguments(w *bytes.Buffer, root *function) {
	for i, param := range root.decl.Params {
		fmt.Fprintf(w, "initial.slots[%d]=args[%d]\n", root.slots[param.ID], i)
	}
}
