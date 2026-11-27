package modulegen

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

// randomProgram builds a valid, non-recursive module IR program from seed:
// enumerations, records and sums over scalars, Bool and earlier types;
// functions that call earlier functions, branch on Bool and ADT values, bind
// literals, primitives and constructors, and use joins and jumps. Every
// root is statically bounded, so these programs exercise the fast path, its
// conversions and the bound analysis on shapes the tracked programs lack.
func randomProgram(seed int64) *moduleir.Program {
	g := &programGen{r: rand.New(rand.NewSource(seed))}
	return g.program()
}

type programGen struct {
	r         *rand.Rand
	types     []moduleir.TypeDecl
	functions []moduleir.Function
	next      int // next binder id within the current function
	fn        int // index of the function being generated
	nodes     int // IR nodes of the current function
}

var genScalars = []string{"UInt8", "UInt16", "UInt32", "UInt64", "Bool"}

// genPrimitives lists admitted primitives by operand types and result.
var genPrimitives = func() []moduleir.Primitive {
	var out []moduleir.Primitive
	for _, width := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
		for _, op := range []string{"add", "sub", "mul", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight", "decEq", "decLt", "decLe",
			"toUInt8", "toUInt16", "toUInt32", "toUInt64"} {
			if p, ok := moduleir.LookupPrimitive(width + "." + op); ok {
				out = append(out, p)
			}
		}
	}
	return out
}()

func (g *programGen) program() *moduleir.Program {
	p := &moduleir.Program{Schema: moduleir.Schema, Profile: moduleir.Profile, Stage: moduleir.Stage, LeanVersion: moduleir.LeanVersion, LeanCommit: moduleir.LeanCommit,
		Module: "Gen", Inputs: map[string]string{"Gen.lean": strings.Repeat("0", 64)}, Claims: []moduleir.Claim{}, Rewrites: []moduleir.Rewrite{},
		API: &moduleir.APIMetadata{Records: map[string][]string{}}, Types: []moduleir.TypeDecl{}}
	g.types = []moduleir.TypeDecl{{Name: "Bool", Constructors: []moduleir.Constructor{{Name: "Bool.false", Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}}}
	for i := range 2 + g.r.Intn(5) {
		g.types = append(g.types, g.typeDecl(fmt.Sprintf("Gen.T%02d", i)))
	}
	functions := 2 + g.r.Intn(6)
	for i := range functions {
		g.fn = i
		g.functions = append(g.functions, g.function(fmt.Sprintf("Gen.f%02d", i)))
	}
	// Roots: the last function and a random selection of the others.
	roots := map[string]bool{g.functions[functions-1].Name: true}
	for _, f := range g.functions {
		if g.r.Intn(3) == 0 {
			roots[f.Name] = true
		}
	}
	reached := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if reached[name] {
			return
		}
		reached[name] = true
		for _, f := range g.functions {
			if f.Name == name {
				walkGenCode(f.Body, func(c *moduleir.Code) {
					if c.Op == "let" && c.Value.Kind == "call" {
						visit(c.Value.Name)
					}
				})
			}
		}
	}
	for root := range roots {
		p.Roots = append(p.Roots, root)
		visit(root)
	}
	sort.Strings(p.Roots)
	for _, f := range g.functions {
		if !reached[f.Name] {
			continue
		}
		p.Functions = append(p.Functions, f)
		deps := map[string]bool{}
		walkGenCode(f.Body, func(c *moduleir.Code) {
			if c.Op == "let" && c.Value.Kind == "call" {
				deps[c.Value.Name] = true
			}
		})
		var names []string
		for name := range deps {
			names = append(names, name)
		}
		sort.Strings(names)
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: f.Name, Dependencies: append([]string{}, names...), Rewrites: []moduleir.Rewrite{}})
	}
	// Keep only types the functions reach, as validation requires.
	used := map[string]bool{}
	var use func(string)
	use = func(typ string) {
		if used[typ] {
			return
		}
		used[typ] = true
		for _, t := range g.types {
			if t.Name == typ {
				for _, c := range t.Constructors {
					for _, field := range c.Fields {
						use(field)
					}
				}
			}
		}
	}
	for _, f := range p.Functions {
		use(f.Result)
		for _, param := range f.Params {
			use(param.Type)
		}
		walkGenCode(f.Body, func(c *moduleir.Code) {
			switch c.Op {
			case "let":
				use(c.Bind.Type)
			case "cases":
				use(c.Type)
			case "join":
				for _, param := range c.Join.Params {
					use(param.Type)
				}
			}
		})
	}
	for _, t := range g.types {
		if used[t.Name] {
			p.Types = append(p.Types, t)
			if len(t.Constructors) == 1 && len(t.Constructors[0].Fields) > 0 && g.r.Intn(2) == 0 {
				var fields []string
				for i := range t.Constructors[0].Fields {
					fields = append(fields, fmt.Sprintf("field%d", i))
				}
				p.API.Records[t.Name] = fields
			}
		}
	}
	return declare(p)
}

func walkGenCode(c *moduleir.Code, visit func(*moduleir.Code)) {
	visit(c)
	switch c.Op {
	case "let":
		walkGenCode(c.Next, visit)
	case "cases":
		for _, a := range c.Alts {
			walkGenCode(a.Body, visit)
		}
	case "join":
		walkGenCode(c.Join.Body, visit)
		walkGenCode(c.Next, visit)
	}
}

// fieldType picks a scalar, Bool or an earlier generated type.
func (g *programGen) fieldType() string {
	if len(g.types) > 1 && g.r.Intn(3) == 0 {
		return g.types[1+g.r.Intn(len(g.types)-1)].Name
	}
	return genScalars[g.r.Intn(len(genScalars))]
}

func (g *programGen) typeDecl(name string) moduleir.TypeDecl {
	t := moduleir.TypeDecl{Name: name}
	switch g.r.Intn(3) {
	case 0: // enumeration
		for i := range 1 + g.r.Intn(4) {
			t.Constructors = append(t.Constructors, moduleir.Constructor{Name: fmt.Sprintf("%s.e%d", name, i), Tag: i, Fields: []string{}})
		}
	case 1: // record
		c := moduleir.Constructor{Name: name + ".mk", Fields: []string{}}
		for range 1 + g.r.Intn(4) {
			c.Fields = append(c.Fields, g.fieldType())
		}
		t.Constructors = []moduleir.Constructor{c}
	default: // sum
		for i := range 2 + g.r.Intn(2) {
			c := moduleir.Constructor{Name: fmt.Sprintf("%s.c%d", name, i), Tag: i, Fields: []string{}}
			for range g.r.Intn(3) {
				c.Fields = append(c.Fields, g.fieldType())
			}
			t.Constructors = append(t.Constructors, c)
		}
	}
	return t
}

func (g *programGen) decl(typ string) moduleir.TypeDecl {
	for _, t := range g.types {
		if t.Name == typ {
			return t
		}
	}
	panic("unknown type " + typ)
}

func (g *programGen) id() int { g.next++; return g.next }

type genScope struct {
	vars  map[int]string
	joins []*moduleir.Join
}

func (s genScope) with(id int, typ string) genScope {
	vars := make(map[int]string, len(s.vars)+1)
	for k, v := range s.vars {
		vars[k] = v
	}
	vars[id] = typ
	return genScope{vars: vars, joins: s.joins}
}

func (s genScope) of(typ string) []int {
	var ids []int
	for id, t := range s.vars {
		if t == typ {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func (g *programGen) function(name string) moduleir.Function {
	g.next, g.nodes = 0, 0
	f := moduleir.Function{Name: name, Result: g.fieldType(), Params: []moduleir.Param{}}
	scope := genScope{vars: map[int]string{}}
	for range g.r.Intn(4) {
		p := moduleir.Param{ID: g.id(), Type: g.fieldType()}
		f.Params = append(f.Params, p)
		scope = scope.with(p.ID, p.Type)
	}
	f.Body = g.code(scope, f.Result, 0)
	return f
}

// code generates a code tree that returns a value of result.
func (g *programGen) code(scope genScope, result string, depth int) *moduleir.Code {
	g.nodes++
	if depth > 12 || g.nodes > 120 {
		return g.finish(scope, result)
	}
	switch k := g.r.Intn(10); {
	case k < 5:
		bind, value := g.value(scope)
		return &moduleir.Code{Op: "let", Bind: &bind, Value: value, Next: g.code(scope.with(bind.ID, bind.Type), result, depth+1)}
	case k < 7:
		var candidates []int
		for id, typ := range scope.vars {
			if _, scalar := scalarFeatureTypes[typ]; !scalar {
				candidates = append(candidates, id)
			}
		}
		if len(candidates) == 0 {
			return g.code(scope, result, depth+1)
		}
		sort.Ints(candidates)
		v := candidates[g.r.Intn(len(candidates))]
		typ := scope.vars[v]
		c := &moduleir.Code{Op: "cases", Var: v, Type: typ, Result: result}
		for _, ctor := range g.decl(typ).Constructors {
			alt := moduleir.Alt{Constructor: ctor.Name, Params: []moduleir.Param{}}
			inner := scope
			for _, field := range ctor.Fields {
				p := moduleir.Param{ID: g.id(), Type: field}
				alt.Params = append(alt.Params, p)
				inner = inner.with(p.ID, field)
			}
			alt.Body = g.code(inner, result, depth+1)
			c.Alts = append(c.Alts, alt)
		}
		return c
	case k < 8:
		j := &moduleir.Join{ID: g.id(), Params: []moduleir.Param{}}
		inner := scope
		for range g.r.Intn(3) {
			p := moduleir.Param{ID: g.id(), Type: g.fieldType()}
			j.Params = append(j.Params, p)
			inner = inner.with(p.ID, p.Type)
		}
		// The body sees the enclosing scope and joins, but not this join.
		j.Body = g.code(inner, result, depth+1)
		next := genScope{vars: scope.vars, joins: append(append([]*moduleir.Join{}, scope.joins...), j)}
		return &moduleir.Code{Op: "join", Join: j, Next: g.code(next, result, depth+1)}
	case k < 9 && len(scope.joins) > 0:
		j := scope.joins[g.r.Intn(len(scope.joins))]
		return g.jump(scope, j, depth)
	default:
		return g.finish(scope, result)
	}
}

// materialize returns code binding a value of typ (reusing a variable when
// possible) and calls rest with its id and the extended scope.
func (g *programGen) materialize(scope genScope, typ string, rest func(genScope, int) *moduleir.Code) *moduleir.Code {
	if ids := scope.of(typ); len(ids) > 0 && g.r.Intn(4) != 0 {
		return rest(scope, ids[g.r.Intn(len(ids))])
	}
	if _, scalar := scalarFeatureTypes[typ]; scalar {
		bind := moduleir.Param{ID: g.id(), Type: typ}
		return &moduleir.Code{Op: "let", Bind: &bind, Value: g.literal(typ), Next: rest(scope.with(bind.ID, typ), bind.ID)}
	}
	ctors := g.decl(typ).Constructors
	ctor := ctors[g.r.Intn(len(ctors))]
	var args []int
	var build func(genScope, int) *moduleir.Code
	build = func(s genScope, i int) *moduleir.Code {
		if i == len(ctor.Fields) {
			bind := moduleir.Param{ID: g.id(), Type: typ}
			return &moduleir.Code{Op: "let", Bind: &bind, Value: &moduleir.Value{Kind: "constructor", Name: ctor.Name, Args: append([]int{}, args...)}, Next: rest(s.with(bind.ID, typ), bind.ID)}
		}
		return g.materialize(s, ctor.Fields[i], func(s2 genScope, id int) *moduleir.Code {
			args = append(args, id)
			return build(s2, i+1)
		})
	}
	return build(scope, 0)
}

func (g *programGen) finish(scope genScope, result string) *moduleir.Code {
	return g.materialize(scope, result, func(_ genScope, id int) *moduleir.Code { return &moduleir.Code{Op: "return", Var: id} })
}

func (g *programGen) jump(scope genScope, j *moduleir.Join, depth int) *moduleir.Code {
	var args []int
	var build func(genScope, int) *moduleir.Code
	build = func(s genScope, i int) *moduleir.Code {
		if i == len(j.Params) {
			return &moduleir.Code{Op: "jump", Target: j.ID, Args: append([]int{}, args...)}
		}
		return g.materialize(s, j.Params[i].Type, func(s2 genScope, id int) *moduleir.Code {
			args = append(args, id)
			return build(s2, i+1)
		})
	}
	return build(scope, 0)
}

func (g *programGen) literal(typ string) *moduleir.Value {
	if typ == "Bool" {
		return &moduleir.Value{Kind: "constructor", Name: []string{"Bool.false", "Bool.true"}[g.r.Intn(2)], Args: []int{}}
	}
	bits := map[string]int{"UInt8": 8, "UInt16": 16, "UInt32": 32, "UInt64": 64}[typ]
	var v uint64
	switch g.r.Intn(4) {
	case 0:
		v = uint64(g.r.Intn(4))
	case 1:
		v = ^uint64(0)
	default:
		v = g.r.Uint64()
	}
	if bits < 64 {
		v &= 1<<bits - 1
	}
	return &moduleir.Value{Kind: strings.ToLower(typ), Name: fmt.Sprint(v), Args: []int{}}
}

// value picks a binding: a literal, a primitive, a constructor or a call of
// an earlier function, with arguments from scope (materialized if needed is
// avoided: only applicable choices are made).
func (g *programGen) value(scope genScope) (moduleir.Param, *moduleir.Value) {
	for attempt := 0; attempt < 8; attempt++ {
		switch g.r.Intn(4) {
		case 0:
			p := genPrimitives[g.r.Intn(len(genPrimitives))]
			if args, ok := g.pick(scope, p.Params); ok {
				return moduleir.Param{ID: g.id(), Type: p.Result}, &moduleir.Value{Kind: "primitive", Name: p.Name, Args: args}
			}
		case 1:
			if len(g.types) > 1 {
				t := g.types[g.r.Intn(len(g.types))]
				c := t.Constructors[g.r.Intn(len(t.Constructors))]
				if args, ok := g.pick(scope, c.Fields); ok {
					return moduleir.Param{ID: g.id(), Type: t.Name}, &moduleir.Value{Kind: "constructor", Name: c.Name, Args: args}
				}
			}
		case 2:
			if g.fn > 0 {
				callee := g.functions[g.r.Intn(g.fn)]
				var params []string
				for _, p := range callee.Params {
					params = append(params, p.Type)
				}
				if args, ok := g.pick(scope, params); ok {
					return moduleir.Param{ID: g.id(), Type: callee.Result}, &moduleir.Value{Kind: "call", Name: callee.Name, Args: args}
				}
			}
		}
	}
	typ := genScalars[g.r.Intn(len(genScalars))]
	return moduleir.Param{ID: g.id(), Type: typ}, g.literal(typ)
}

func (g *programGen) pick(scope genScope, types []string) ([]int, bool) {
	args := []int{}
	for _, typ := range types {
		ids := scope.of(typ)
		if len(ids) == 0 {
			return nil, false
		}
		args = append(args, ids[g.r.Intn(len(ids))])
	}
	return args, true
}
