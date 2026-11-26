package modulegen

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/franchb/lean-go/internal/moduleir"
)

const byteListType = "List UInt8"

// Recognize the complete recurrence in validated IR. Names of roots and steps
// are user-defined. All operands, branches and the returned state must agree.
func byteFoldSteps(p *moduleir.Program) map[string]string {
	if !moduleir.HasByteFolds(p.Schema) {
		return nil
	}
	for _, typ := range p.Types {
		if typ.Name != byteListType || len(typ.Constructors) != 2 {
			continue
		}
		nilCtor, cons := typ.Constructors[0], typ.Constructors[1]
		if nilCtor.Name != "List.nil" || len(nilCtor.Fields) != 0 || cons.Name != "List.cons" || !slices.Equal(cons.Fields, []string{"UInt8", byteListType}) {
			return nil
		}
		roots := moduleir.RootNames(p)
		steps := make(map[string]string)
		scalars := apiScalars(p)
		for _, f := range p.Functions {
			if _, ok := scalars[f.Result]; ok && slices.Contains(roots, f.Name) {
				if step := byteFoldStep(f); step != "" {
					steps[f.Name] = step
				}
			}
		}
		return steps
	}
	return nil
}

func byteFoldStep(f moduleir.Function) string {
	if len(f.Params) != 2 || f.Params[0].Type != f.Result || f.Params[1].Type != byteListType {
		return ""
	}
	body := f.Body
	if body.Op != "cases" || body.Type != byteListType || body.Var != f.Params[1].ID || len(body.Alts) != 2 {
		return ""
	}
	empty, cons := body.Alts[0], body.Alts[1]
	if empty.Constructor != "List.nil" || empty.Body.Op != "return" || empty.Body.Var != f.Params[0].ID || cons.Constructor != "List.cons" || len(cons.Params) != 2 {
		return ""
	}
	call := cons.Body
	if call.Op != "let" || call.Value.Kind != "call" || call.Bind.Type != f.Result || !slices.Equal(call.Value.Args, []int{f.Params[0].ID, cons.Params[0].ID}) {
		return ""
	}
	recur := call.Next
	if recur.Op != "let" || recur.Value.Kind != "call" || recur.Value.Name != f.Name || !slices.Equal(recur.Value.Args, []int{call.Bind.ID, cons.Params[1].ID}) || recur.Next.Op != "return" || recur.Next.Var != recur.Bind.ID {
		return ""
	}
	return call.Value.Name
}

func (e *apiEmitter) byteFoldEntry(w *bytes.Buffer, f, step *function, native bool) {
	entry := e.names.Entrypoint
	if moduleir.HasMultipleRoots(e.g.program.Schema) {
		entry = e.names.Entrypoints[f.decl.Name]
	}
	scalar := e.scalars[f.decl.Result]
	fmt.Fprintf(w, "// %s borrows data for this call. Limits follow the equivalent Lean list and call tree.\n", entry)
	fmt.Fprintf(w, "func %s(state %s, data []byte, l Limits)(%s,error){\n", entry, scalar.GoType, scalar.GoType)
	fmt.Fprintf(w, "if err:=checkLimits(l);err!=nil{return %s,err}\n", scalar.Zero)
	// A nil node, one accumulator, and one cons and byte per iteration.
	fmt.Fprintf(w, "if l.MaxNodes<2 || uint64(len(data))>(l.MaxNodes-2)/2 || uint64(len(data))>=l.MaxDepth{return %s,ErrResource}\n", scalar.Zero)
	if native {
		// Charge the case, two calls and deferred return for each cons, and
		// the final nil case and return. Only the Go stack is flattened.
		fmt.Fprintf(w, "b:=budget{limits:l};if err:=b.enter(%d,0);err!=nil{return %s,err}\n", len(f.slots), scalar.Zero)
		fmt.Fprintf(w, "for i, value:=range data { if !b.scalarWork(4,0){return %s,ErrResource}; next,err:=scalarFunc%d(&b,i+1,state,value);if err!=nil{return %s,err};state=next;if err=b.enter(%d,i+1);err!=nil{return %s,err} }\n", scalar.Zero, step.id, scalar.Zero, len(f.slots), scalar.Zero)
		fmt.Fprintf(w, "if !b.scalarWork(2,0){return %s,ErrResource};return state,nil\n}\n", scalar.Zero)
		return
	}
	// For a non-native step, materialize the bounded input and use the raw
	// executor. The same typed slice API works for steps with joins/recursion.
	listID := e.g.types[byteListType]
	fmt.Fprintf(w, "tail:=&node{typ:%d,size:1,depth:1};for i:=len(data)-1;i>=0;i--{tail=&node{typ:%d,tag:1,size:tail.size+2,depth:tail.depth+1,fields:[]*node{apiUInt8Node(data[i]),tail}}}\n", listID, listID)
	call := "Run("
	if moduleir.HasMultipleRoots(e.g.program.Schema) {
		call = fmt.Sprintf("RunRoot(%q,", f.decl.Name)
	}
	fmt.Fprintf(w, "out,err:=%s[]Value{{n:%s(state)},{n:tail}},l);if err!=nil{return %s,err};value,ok:=%s(out.n);if !ok{return %s,ErrMalformed};return value,nil\n}\n", call, scalar.Node, scalar.Zero, scalar.Read, scalar.Zero)
}
