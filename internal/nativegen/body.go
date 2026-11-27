package nativegen

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

// fnEmitter writes one IR function body as straight-line Go. Pure single-use
// bindings, constants and field projections are emitted inline at their use,
// so small functions stay within Go's inlining budget. Joins become a block,
// a label and gotos, with parameters as variables of the enclosing block.
type fnEmitter struct {
	m     *model
	f     *funcInfo
	live  map[int]bool
	uses  map[int]int
	exprs map[int]expr // bindings emitted inline at their uses
	joins map[int]*moduleir.Join
	reach map[int]bool
	used  map[string]bool
}

func (m *model) emitFunction(w *bytes.Buffer, f *funcInfo, used map[string]bool) error {
	e := &fnEmitter{m: m, f: f, live: map[int]bool{}, uses: map[int]int{}, exprs: map[int]expr{}, joins: map[int]*moduleir.Join{}, reach: map[int]bool{}, used: used}
	walkCode(f.decl.Body, func(c *moduleir.Code) {
		if c.Op == "join" {
			e.joins[c.Join.ID] = c.Join
		}
	})
	e.analyze()
	if f.root && !f.depth && f.fold == nil && m.prefix == "" {
		fmt.Fprintf(w, "\n// %s is the Lean function %s.\n", f.callName, f.decl.Name)
	} else {
		fmt.Fprintf(w, "\n// %s implements the Lean function %s.\n", f.callName, f.decl.Name)
	}
	fmt.Fprintf(w, "func %s {\n", m.signature(f))
	if f.selfRec {
		fmt.Fprintf(w, "if d >= MaxDepth {\nreturn %s, ErrDepth\n}\n", m.zero(f.decl.Result))
	}
	if err := e.code(w, f.decl.Body); err != nil {
		return err
	}
	w.WriteString("}\n")
	return nil
}

// needsScrutinee reports whether a cases reads its scrutinee: always for
// several alternatives, otherwise only for live fields.
func (e *fnEmitter) needsScrutinee(c *moduleir.Code) bool {
	needed := len(c.Alts) > 1
	for _, p := range c.Alts[0].Params {
		needed = needed || e.live[p.ID]
	}
	return needed
}

// analyze finds reachable joins and live variables, then counts reads. Every
// let is pure, so a binding nobody reads is dropped; jumps to unreachable
// joins never happen.
func (e *fnEmitter) analyze() {
	var reads func(c *moduleir.Code, read func(int))
	reads = func(c *moduleir.Code, read func(int)) {
		switch c.Op {
		case "let":
			if e.live[c.Bind.ID] {
				for _, id := range c.Value.Args {
					read(id)
				}
			}
			reads(c.Next, read)
		case "return":
			read(c.Var)
		case "cases":
			if e.needsScrutinee(c) {
				// Projections and switches read a structured scrutinee
				// repeatedly; count two reads so it is bound to a variable.
				// A Bool condition is read once and may stay inline.
				read(c.Var)
				if e.m.types[c.Type].kind != kindBool {
					read(c.Var)
				}
			}
			for _, a := range c.Alts {
				reads(a.Body, read)
			}
		case "join":
			if e.reach[c.Join.ID] {
				reads(c.Join.Body, read)
			}
			reads(c.Next, read)
		case "jump":
			e.reach[c.Target] = true
			for i, id := range c.Args {
				if e.live[e.joins[c.Target].Params[i].ID] {
					read(id)
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		reached := len(e.reach)
		reads(e.f.decl.Body, func(id int) {
			if !e.live[id] {
				e.live[id] = true
				changed = true
			}
		})
		changed = changed || len(e.reach) != reached
	}
	reads(e.f.decl.Body, func(id int) { e.uses[id]++ })
}

func (e *fnEmitter) ref(id int) expr {
	if x, ok := e.exprs[id]; ok {
		return x
	}
	return expr{text: fmt.Sprintf("v%d", id), atomic: true}
}

func (e *fnEmitter) values(ids []int) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = e.ref(id).text
	}
	return out
}

func (e *fnEmitter) code(w *bytes.Buffer, c *moduleir.Code) error {
	m := e.m
	switch c.Op {
	case "return":
		if e.f.depth {
			fmt.Fprintf(w, "return %s, nil\n", e.ref(c.Var).text)
		} else {
			fmt.Fprintf(w, "return %s\n", e.ref(c.Var).text)
		}
		return nil
	case "let":
		if e.live[c.Bind.ID] {
			if err := e.let(w, c); err != nil {
				return err
			}
		}
		return e.code(w, c.Next)
	case "cases":
		return e.cases(w, c)
	case "jump":
		j := e.joins[c.Target]
		var dst, src []string
		for i, id := range c.Args {
			if e.live[j.Params[i].ID] {
				dst = append(dst, fmt.Sprintf("v%d", j.Params[i].ID))
				src = append(src, e.ref(id).text)
			}
		}
		if len(dst) > 0 {
			fmt.Fprintf(w, "%s = %s\n", strings.Join(dst, ", "), strings.Join(src, ", "))
		}
		fmt.Fprintf(w, "goto j%d\n", c.Target)
		return nil
	case "join":
		j := c.Join
		if !e.reach[j.ID] {
			return e.code(w, c.Next)
		}
		// Parameters live in the enclosing block; the continuation runs in
		// its own block, so every goto leaves scopes and never enters one.
		for _, p := range j.Params {
			if e.live[p.ID] {
				fmt.Fprintf(w, "var v%d %s\n", p.ID, m.goType(p.Type))
			}
		}
		w.WriteString("{\n")
		if err := e.code(w, c.Next); err != nil {
			return err
		}
		fmt.Fprintf(w, "}\nj%d:\n", j.ID)
		return e.code(w, j.Body)
	}
	return fmt.Errorf("unsupported code operation %q", c.Op)
}

// bindExpr either inlines x at the binding's single use or binds a variable.
func (e *fnEmitter) bindExpr(w *bytes.Buffer, id int, x expr) {
	if x.constant || e.uses[id] == 1 {
		e.exprs[id] = x
		return
	}
	fmt.Fprintf(w, "v%d := %s\n", id, x.text)
}

func (e *fnEmitter) let(w *bytes.Buffer, c *moduleir.Code) error {
	m := e.m
	id := c.Bind.ID
	switch c.Value.Kind {
	case "uint8", "uint16", "uint32", "uint64":
		value, err := strconv.ParseUint(c.Value.Name, 10, scalarWidths[c.Bind.Type])
		if err != nil {
			return err
		}
		e.exprs[id] = literal(c.Bind.Type, value)
	case "primitive":
		if primitive, _ := LookupPrimitive(c.Value.Name); primitive.Result == "Bool" && m.types["Bool"].kind != kindBool {
			return fmt.Errorf("primitive %s requires the canonical Lean Bool", c.Value.Name)
		}
		args := make([]expr, len(c.Value.Args))
		for i, arg := range c.Value.Args {
			args[i] = e.ref(arg)
		}
		e.used[c.Value.Name] = true
		x, ok, err := primitiveExpr(c.Value.Name, args)
		if err != nil {
			return err
		}
		if ok {
			e.bindExpr(w, id, x)
			return nil
		}
		statements, err := primitiveStatements(fmt.Sprintf("v%d", id), c.Value.Name, args)
		if err != nil {
			return err
		}
		w.WriteString(statements)
	case "constructor":
		ref := m.ctors[c.Value.Name]
		info := m.types[ref.typ]
		if info.kind == kindBool {
			e.exprs[id] = literal("Bool", uint64(ref.index))
		} else {
			e.bindExpr(w, id, expr{text: m.constructorExpr(info, ref.index, e.values(c.Value.Args))})
		}
	case "call":
		callee := m.funcs[c.Value.Name]
		args := strings.Join(e.values(c.Value.Args), ", ")
		if callee.depth {
			fmt.Fprintf(w, "v%d, err := %s(d+1, %s)\nif err != nil {\nreturn %s, err\n}\n", id, callee.callName, args, m.zero(e.f.decl.Result))
		} else {
			e.bindExpr(w, id, expr{text: fmt.Sprintf("%s(%s)", callee.callName, args), atomic: true})
		}
	default:
		return fmt.Errorf("unsupported value kind %q", c.Value.Kind)
	}
	return nil
}

// bind makes live constructor fields available as projections of source.
func (e *fnEmitter) bind(a moduleir.Alt, source string, fields []string) {
	for i, p := range a.Params {
		if e.live[p.ID] {
			e.exprs[p.ID] = expr{text: source + "." + fields[i], atomic: true}
		}
	}
}

func (e *fnEmitter) cases(w *bytes.Buffer, c *moduleir.Code) error {
	info := e.m.types[c.Type]
	scrutinee := e.ref(c.Var)
	switch info.kind {
	case kindBool:
		var yes, no *moduleir.Code
		for _, a := range c.Alts {
			if a.Constructor == "Bool.true" {
				yes = a.Body
			} else {
				no = a.Body
			}
		}
		fmt.Fprintf(w, "if %s {\n", scrutinee.text)
		if err := e.code(w, yes); err != nil {
			return err
		}
		w.WriteString("}\n")
		return e.code(w, no)
	case kindRecord:
		e.bind(c.Alts[0], scrutinee.operand(), info.fields[0])
		return e.code(w, c.Alts[0].Body)
	}
	source := scrutinee.operand()
	if info.kind == kindBoxed {
		if !e.needsScrutinee(c) {
			return e.code(w, c.Alts[0].Body)
		}
		source = fmt.Sprintf("n%d", c.Var)
		fmt.Fprintf(w, "%s := %s.node()\n", source, scrutinee.operand())
	}
	if len(c.Alts) == 1 {
		e.bind(c.Alts[0], source, info.fields[e.m.ctors[c.Alts[0].Constructor].index])
		return e.code(w, c.Alts[0].Body)
	}
	fmt.Fprintf(w, "switch %s.tag {\n", source)
	for k, a := range c.Alts {
		i := e.m.ctors[a.Constructor].index
		if k == len(c.Alts)-1 {
			// The tag is unexported and always names a constructor, so the
			// last alternative covers exactly the remaining tag.
			fmt.Fprintf(w, "default: // %s\n", a.Constructor)
		} else {
			fmt.Fprintf(w, "case %d: // %s\n", info.tags[i], a.Constructor)
		}
		e.bind(a, source, info.fields[i])
		if err := e.code(w, a.Body); err != nil {
			return err
		}
	}
	w.WriteString("}\n")
	return nil
}
