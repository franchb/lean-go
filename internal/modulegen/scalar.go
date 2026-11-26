package modulegen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

// scalarRoots selects typed entrypoints whose entire call tree runs on native
// scalar values. IR validation excludes mutual recursion and bounds function
// count and code size. Self recursion and joins retain the explicit stack.
func (e *apiEmitter) scalarRoots() map[string]bool {
	eligible := make(map[string]bool)
	var visit func(string) bool
	visit = func(name string) bool {
		if yes, seen := eligible[name]; seen {
			return yes
		}
		eligible[name] = false
		f := e.g.functions[name]
		if _, ok := e.scalars[f.decl.Result]; !ok {
			return false
		}
		for _, p := range f.decl.Params {
			if _, ok := e.scalars[p.Type]; !ok {
				return false
			}
		}
		for _, c := range f.nodes {
			switch c.Op {
			case "return":
			case "cases":
				if _, ok := e.scalars["Bool"]; !ok || c.Type != "Bool" {
					return false
				}
			case "let":
				if _, ok := e.scalars[c.Bind.Type]; !ok {
					return false
				}
				if c.Value.Kind == "call" && !visit(c.Value.Name) {
					return false
				}
			default:
				return false
			}
		}
		eligible[name] = true
		return true
	}
	roots := make(map[string]bool)
	for _, name := range moduleir.RootNames(e.g.program) {
		if step := e.names.ByteFolds[name]; step != "" && visit(step) {
			roots[step] = true
		}
		if visit(name) {
			roots[name] = true
		}
	}
	return roots
}

func (e *apiEmitter) emitEntrypoints(w *bytes.Buffer) {
	roots := e.scalarRoots()
	if len(roots) != 0 {
		w.WriteString(`// scalarWork charges the same IR steps and logical scalar allocations as execute.
func (b *budget) scalarWork(steps, allocations uint64) bool {
	if steps > b.limits.MaxSteps-b.steps || allocations > b.limits.MaxAllocations-b.allocations { return false }
	b.steps += steps
	b.allocations += allocations
	return true
}
`)
	}
	emitted := make(map[string]bool)
	for _, root := range moduleir.RootNames(e.g.program) {
		if step := e.names.ByteFolds[root]; step != "" {
			callee := e.g.functions[step]
			if roots[step] {
				e.emitScalarFunction(w, callee, emitted)
			}
			e.byteFoldEntry(w, e.g.functions[root], callee, roots[step])
		} else if roots[root] {
			e.emitScalarFunction(w, e.g.functions[root], emitted)
			e.scalarEntry(w, root)
		} else {
			e.run(w, root)
		}
	}
}

func (e *apiEmitter) emitScalarFunction(w *bytes.Buffer, f *function, emitted map[string]bool) {
	if emitted[f.decl.Name] {
		return
	}
	emitted[f.decl.Name] = true
	used := make(map[int]bool)
	for i := len(f.nodes) - 1; i >= 0; i-- {
		c := f.nodes[i]
		switch c.Op {
		case "return", "cases":
			used[c.Var] = true
		case "let":
			if !used[c.Bind.ID] && c.Value.Kind != "call" {
				continue
			}
			for _, id := range c.Value.Args {
				used[id] = true
			}
			if c.Value.Kind == "call" {
				e.emitScalarFunction(w, e.g.functions[c.Value.Name], emitted)
			}
		}
	}
	fmt.Fprintf(w, "func scalarFunc%d(b *budget, depth int", f.id)
	for _, p := range f.decl.Params {
		fmt.Fprintf(w, ", v%d %s", p.ID, e.scalars[p.Type].GoType)
	}
	fmt.Fprintf(w, ")(%s,error){\n", e.scalars[f.decl.Result].GoType)
	zero := e.zero(f.decl.Result)
	fmt.Fprintf(w, "if err:=b.enter(%d,depth); err!=nil {return %s,err}\n", len(f.slots), zero)
	e.emitScalarBlock(w, f.decl.Body, used, zero)
	w.WriteString("}\n")
}

// A block contains only pure scalar bindings followed by a call, branch or
// return. Every binding costs one logical allocation even if its value is dead.
func (e *apiEmitter) emitScalarBlock(w *bytes.Buffer, first *moduleir.Code, used map[int]bool, zero string) {
	c := first
	allocations := 0
	for c.Op == "let" && c.Value.Kind != "call" {
		allocations++
		c = c.Next
	}
	fmt.Fprintf(w, "if !b.scalarWork(%d,%d){return %s,ErrResource}\n", allocations+1, allocations, zero)
	for bind := first; bind != c; bind = bind.Next {
		if used[bind.Bind.ID] {
			fmt.Fprintf(w, "v%d := %s\n", bind.Bind.ID, e.scalarExpression(bind))
		}
	}
	switch c.Op {
	case "return":
		fmt.Fprintf(w, "return v%d,nil\n", c.Var)
	case "cases":
		positive, negative := c.Alts[0], c.Alts[1]
		if positive.Constructor != "Bool.true" {
			positive, negative = negative, positive
		}
		fmt.Fprintf(w, "if v%d {\n", c.Var)
		e.emitScalarBlock(w, positive.Body, used, zero)
		w.WriteString("}\n")
		e.emitScalarBlock(w, negative.Body, used, zero)
	case "let":
		callee := e.g.functions[c.Value.Name]
		var call strings.Builder
		fmt.Fprintf(&call, "scalarFunc%d(b,depth+1", callee.id)
		for _, id := range c.Value.Args {
			fmt.Fprintf(&call, ",v%d", id)
		}
		call.WriteByte(')')
		if used[c.Bind.ID] {
			fmt.Fprintf(w, "v%d,err:=%s\nif err!=nil{return %s,err}\n", c.Bind.ID, call.String(), zero)
		} else {
			fmt.Fprintf(w, "if _,err:=%s; err!=nil{return %s,err}\n", call.String(), zero)
		}
		e.emitScalarBlock(w, c.Next, used, zero)
	}
}

func (e *apiEmitter) scalarExpression(c *moduleir.Code) string {
	value := c.Value
	switch value.Kind {
	case "primitive":
		left := fmt.Sprintf("v%d", value.Args[0])
		right := ""
		if len(value.Args) == 2 {
			right = fmt.Sprintf("v%d", value.Args[1])
		}
		return primitiveExpression(value.Name, left, right)
	case "constructor":
		return fmt.Sprint(value.Name == "Bool.true")
	default:
		return fmt.Sprintf("%s(%s)", e.scalars[c.Bind.Type].GoType, value.Name)
	}
}

func (e *apiEmitter) scalarEntry(w *bytes.Buffer, name string) {
	f := e.g.functions[name]
	entry := e.names.Entrypoint
	if moduleir.HasMultipleRoots(e.g.program.Schema) {
		entry = e.names.Entrypoints[name]
	}
	fields := make([]string, len(f.decl.Params))
	var args strings.Builder
	for i, p := range f.decl.Params {
		fields[i] = p.Type
		fmt.Fprintf(&args, ",a%d", i)
	}
	zero := e.zero(f.decl.Result)
	fmt.Fprintf(w, "// %s runs the scalar root with native Go values and the public resource limits.\n", entry)
	fmt.Fprintf(w, "func %s(%sl Limits)(%s,error){\n", entry, e.parameters(fields), e.scalars[f.decl.Result].GoType)
	fmt.Fprintf(w, "if err:=checkLimits(l);err!=nil{return %s,err}\n", zero)
	if len(fields) > 0 {
		fmt.Fprintf(w, "if l.MaxNodes < %d {return %s,ErrResource}\n", len(fields), zero)
	}
	fmt.Fprintf(w, "b:=budget{limits:l}; return scalarFunc%d(&b,0%s)\n}\n", f.id, args.String())
}
