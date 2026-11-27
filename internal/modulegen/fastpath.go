package modulegen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
	"github.com/franchb/lean-go/internal/nativegen"
)

// The fast path runs a statically bounded root as plain Go when the caller's
// limits cover its static worst case (bound.go) in all seven dimensions:
//
//	func RunR(args, l Limits) (T, error) {
//		if fastCoversN(l) { // valid limits covering the bound
//			if every argument converts { return natR(converted args), nil }
//		}
//		... the state machine, unchanged ...
//	}
//
// Why this is observably identical: when the limits are valid and cover the
// bound, no check of the state machine can fail with ErrResource on
// well-formed arguments, and on well-formed arguments of a well-typed program
// it cannot fail with ErrMalformed either, so it returns the root's value.
// The plain Go code is the native emitter's output for the same IR, which
// computes that value, and the result is converted back into fresh nodes
// with the same types, tags, scalars, sizes and depths. An argument is
// converted only if every node of it is well-formed for its type (non-nil,
// expected type id, a known tag with its arity, nonzero size and depth, and
// cached size and depth within the type's static bounds); otherwise, as for
// invalid or uncovering limits, the state machine runs and produces exactly
// today's result or error. Run/RunRoot take the same path after their own
// argument checks.
//
// Generated names: the native code's package-level names start with
// fastNativePrefix and this file's with "fast"; no other generated
// declaration starts with either.

const fastNativePrefix = "nat"

// generateOptions select variants of the output; Generate uses the zero value.
type generateOptions struct {
	// stateMachine leaves the fast path out (GenerateStateMachine).
	stateMachine bool
	// fastSwitch adds `var fastDisabled bool`; setting it makes every
	// fastCovers function false, so a test can run the state machine alone.
	fastSwitch bool
}

type fastPlan struct {
	native *nativegen.Embedded
	bounds map[string][dimensions]uint64 // eligible roots
	roots  []string                      // eligible roots in root order
}

// planFastPath selects the eligible roots: not byte folds (they keep their
// own entry), statically bounded, and with a bound some valid Limits covers.
func (g *generator) planFastPath() *fastPlan {
	folds := byteFoldSteps(g.program)
	analysis := newBoundAnalysis(g)
	plan := &fastPlan{bounds: map[string][dimensions]uint64{}}
	for _, root := range moduleir.RootNames(g.program) {
		if folds[root] != "" {
			continue
		}
		bound, ok := analysis.rootBound(root)
		if !ok || !withinCaps(bound) {
			continue
		}
		plan.bounds[root] = bound
		plan.roots = append(plan.roots, root)
	}
	if len(plan.roots) == 0 {
		return nil
	}
	native, err := nativegen.GenerateEmbedded(g.program, plan.roots, fastNativePrefix)
	if err != nil {
		// Only a type of the program without a finite value (on which the
		// native model insists) can fail here; keep the state machine.
		return nil
	}
	plan.native = native
	return plan
}

func (p *fastPlan) eligible(root string) bool {
	if p == nil {
		return false
	}
	_, ok := p.bounds[root]
	return ok
}

// fastConversions collects the types whose node conversions are needed.
type fastConversions struct {
	in, out map[string]bool
	inOrder []string
	outOrd  []string
}

func (e *apiEmitter) collectConversions(typ string, seen map[string]bool, order *[]string) {
	if _, scalar := e.scalars[typ]; scalar || seen[typ] {
		return
	}
	seen[typ] = true
	for _, c := range e.typeDecl(typ).Constructors {
		for _, field := range c.Fields {
			e.collectConversions(field, seen, order)
		}
	}
	*order = append(*order, typ)
}

func (e *apiEmitter) typeDecl(typ string) moduleir.TypeDecl {
	for _, t := range e.g.program.Types {
		if t.Name == typ {
			return t
		}
	}
	return moduleir.TypeDecl{}
}

// emitFast writes the native code, the conversions, the covers functions and
// fastRun for the raw entry points.
func (e *apiEmitter) emitFast(w *bytes.Buffer) {
	plan := e.g.fast
	if plan == nil {
		return
	}
	w.WriteString("\n// Fast path: native Go for statically bounded roots, used only when the\n// limits cover the root's static worst case. See internal/modulegen/fastpath.go.\n")
	w.Write(plan.native.Source)
	conv := fastConversions{in: map[string]bool{}, out: map[string]bool{}}
	for _, root := range plan.roots {
		decl := e.g.functions[root].decl
		for _, p := range decl.Params {
			e.collectConversions(p.Type, conv.in, &conv.inOrder)
		}
		e.collectConversions(decl.Result, conv.out, &conv.outOrd)
	}
	analysis := newBoundAnalysis(e.g)
	for _, typ := range conv.inOrder {
		e.emitFastIn(w, typ, analysis.shape(typ))
	}
	if len(conv.outOrd) > 0 {
		w.WriteString(`
// fastArena hands out the nodes and field slots of one converted result,
// allocated up front: in one block when the result type has a constant
// size, otherwise in two. Nodes are immutable once built, as everywhere else.
type fastArena struct { nodes []node; fields []*node }
func (a *fastArena) next() *node { n := &a.nodes[0]; a.nodes = a.nodes[1:]; return n }
func (a *fastArena) take(k int) []*node { f := a.fields[:k:k]; a.fields = a.fields[k:]; return f }
// fastMeasure sets the cached size and depth exactly as measure computes them.
func fastMeasure(n *node) { n.size, n.depth = 1, 1; for _, c := range n.fields { n.size += c.size; if c.depth >= n.depth { n.depth = c.depth + 1 } } }
`)
		scalarsUsed := map[string]bool{}
		for _, typ := range conv.outOrd {
			for _, c := range e.typeDecl(typ).Constructors {
				for _, field := range c.Fields {
					if _, scalar := e.scalars[field]; scalar {
						scalarsUsed[field] = true
					}
				}
			}
		}
		for _, scalar := range []string{"Bool", "UInt8", "UInt16", "UInt32", "UInt64"} {
			if !scalarsUsed[scalar] {
				continue
			}
			if scalar == "Bool" {
				fmt.Fprintf(w, "func (a *fastArena) putBool(v bool) *node { n := a.next(); n.typ, n.size, n.depth = %d, 1, 1; if v { n.tag = 1 }; return n }\n", e.g.types["Bool"])
			} else {
				fmt.Fprintf(w, "func (a *fastArena) put%s(v %s) *node { n := a.next(); n.typ, n.%s, n.size, n.depth = %d, v, 1, 1; return n }\n", scalar, e.scalars[scalar].GoType, scalarField(scalar), e.g.types[scalar])
			}
		}
		for _, typ := range conv.outOrd {
			e.emitFastOut(w, typ, analysis.shape(typ))
		}
	}
	for _, root := range plan.roots {
		f := e.g.functions[root]
		b := plan.bounds[root]
		// Every bound is at least 1, so the conditions also imply that
		// checkLimits accepts l: each limit positive and within its cap.
		fmt.Fprintf(w, "\n// fastCovers%d reports whether l is valid and covers the static worst case of\n// %s in every dimension.\nfunc fastCovers%d(l Limits) bool {\n", f.id, strings.ReplaceAll(root, "\n", " "), f.id)
		if e.g.options.fastSwitch {
			w.WriteString("if fastDisabled { return false }\n")
		}
		var parts []string
		for i, name := range limitNames {
			parts = append(parts, fmt.Sprintf("%d <= l.%s && l.%s <= %d", b[i], name, name, limitCaps[i]))
		}
		fmt.Fprintf(w, "return %s\n}\n", strings.Join(parts, " && "))
	}
	if e.g.options.fastSwitch {
		w.WriteString("\n// fastDisabled turns the fast path off (test builds only).\nvar fastDisabled bool\n")
	}
	e.emitFastRun(w)
}

func (e *apiEmitter) nativeGo(typ string) string { return e.g.fast.native.Types[typ].Go }

func (e *apiEmitter) nativeZero(typ string) string {
	if scalar, ok := e.scalars[typ]; ok {
		return scalar.Zero
	}
	return e.nativeGo(typ) + "{}"
}

// fastRead returns a statement converting the node expression src of type
// typ into the native variable dst, or running failure.
func (e *apiEmitter) fastRead(typ, src, dst, failure string) string {
	if scalar, ok := e.scalars[typ]; ok {
		return fmt.Sprintf("%s, ok := %s(%s); if !ok { %s }\n", dst, scalar.Read, src, failure)
	}
	return fmt.Sprintf("%s, ok := fastIn%d(%s); if !ok { %s }\n", dst, e.g.types[typ], src, failure)
}

func (e *apiEmitter) emitFastIn(w *bytes.Buffer, typ string, s shape) {
	id := e.g.types[typ]
	zero := e.nativeZero(typ)
	failure := "return " + zero + ", false"
	fmt.Fprintf(w, "\n// fastIn%d converts a well-formed %s node; false means fall back.\nfunc fastIn%d(n *node) (%s, bool) {\n", id, strings.ReplaceAll(typ, "\n", " "), id, e.nativeGo(typ))
	fmt.Fprintf(w, "if n == nil || n.typ != %d || n.size == 0 || n.depth == 0 || n.size > %d || n.depth > %d { %s }\n", id, s.max, s.depth, failure)
	w.WriteString("switch n.tag {\n")
	for i, c := range e.typeDecl(typ).Constructors {
		fmt.Fprintf(w, "case %d:\nif len(n.fields) != %d { %s }\n", c.Tag, len(c.Fields), failure)
		args := make([]string, len(c.Fields))
		for j, field := range c.Fields {
			args[j] = fmt.Sprintf("a%d", j)
			w.WriteString(e.fastRead(field, fmt.Sprintf("n.fields[%d]", j), args[j], failure))
		}
		fmt.Fprintf(w, "return %s, true\n", e.g.fast.native.Constructor(typ, i, args))
	}
	fmt.Fprintf(w, "}\n%s\n}\n", failure)
}

// fastSize returns an expression for the node count of native value x.
func (e *apiEmitter) fastSize(typ, x string, s shape) string {
	if s.min == s.max {
		return fmt.Sprint(s.max)
	}
	return fmt.Sprintf("fastSize%d(%s)", e.g.types[typ], x)
}

// fastCase calls body for every constructor of a native value x of typ,
// inside a switch on its tag when typ has several constructors.
func (e *apiEmitter) fastCases(w *bytes.Buffer, typ, x string, body func(i int, c moduleir.Constructor, fields []string)) {
	info := e.g.fast.native.Types[typ]
	constructors := e.typeDecl(typ).Constructors
	fieldsOf := func(i int) []string {
		out := make([]string, len(constructors[i].Fields))
		for j := range out {
			out[j] = x + "." + info.Fields[i][j]
		}
		return out
	}
	if len(constructors) == 1 {
		body(0, constructors[0], fieldsOf(0))
		return
	}
	fmt.Fprintf(w, "switch %s.tag {\n", x)
	for i, c := range constructors {
		if i == len(constructors)-1 {
			w.WriteString("default:\n")
		} else {
			fmt.Fprintf(w, "case %d:\n", info.Tags[i])
		}
		body(i, c, fieldsOf(i))
	}
	w.WriteString("}\n")
}

func (e *apiEmitter) emitFastOut(w *bytes.Buffer, typ string, s shape) {
	id := e.g.types[typ]
	analysis := newBoundAnalysis(e.g)
	native := e.nativeGo(typ)
	if s.min != s.max {
		fmt.Fprintf(w, "\n// fastSize%d counts the nodes of x as a %s value.\nfunc fastSize%d(x %s) uint64 {\n", id, strings.ReplaceAll(typ, "\n", " "), id, native)
		e.fastCases(w, typ, "x", func(i int, c moduleir.Constructor, fields []string) {
			parts := []string{"1"}
			for j, field := range c.Fields {
				parts = append(parts, e.fastSize(field, fields[j], analysis.shape(field)))
			}
			fmt.Fprintf(w, "return %s\n", strings.Join(parts, " + "))
		})
		w.WriteString("}\n")
	}
	fmt.Fprintf(w, "\n// fastPut%d builds the nodes of x in a.\nfunc fastPut%d(a *fastArena, x %s) *node {\nn := a.next()\n", id, id, native)
	e.fastCases(w, typ, "x", func(i int, c moduleir.Constructor, fields []string) {
		// Arena nodes are zero: set only the other fields, one by one,
		// rather than storing a whole struct through the pointer.
		fmt.Fprintf(w, "n.typ, n.tag = %d, %d\n", id, c.Tag)
		if len(c.Fields) == 0 {
			return
		}
		fmt.Fprintf(w, "f := a.take(%d)\n", len(c.Fields))
		for j, field := range c.Fields {
			fmt.Fprintf(w, "f[%d] = %s\n", j, e.fastPut(field, fields[j]))
		}
		w.WriteString("n.fields = f\n")
	})
	w.WriteString("fastMeasure(n)\nreturn n\n}\n")
	if s.min == s.max {
		// One allocation holds every node and field slot of the result.
		fmt.Fprintf(w, "\n// fastSlab%d holds the %d nodes of a %s value.\ntype fastSlab%d struct { nodes [%d]node; fields [%d]*node }\n", id, s.max, strings.ReplaceAll(typ, "\n", " "), id, s.max, s.max-1)
		fmt.Fprintf(w, "\n// fastOut%d converts x into fresh nodes.\nfunc fastOut%d(x %s) *node {\ns := new(fastSlab%d)\na := fastArena{nodes: s.nodes[:], fields: s.fields[:]}\nreturn fastPut%d(&a, x)\n}\n", id, id, native, id, id)
		return
	}
	fmt.Fprintf(w, "\n// fastOut%d converts x into fresh nodes.\nfunc fastOut%d(x %s) *node {\ns := fastSize%d(x)\na := fastArena{nodes: make([]node, s), fields: make([]*node, s-1)}\nreturn fastPut%d(&a, x)\n}\n", id, id, native, id, id)
}

func (e *apiEmitter) fastPut(typ, x string) string {
	if _, scalar := e.scalars[typ]; scalar {
		return fmt.Sprintf("a.put%s(%s)", typ, x)
	}
	return fmt.Sprintf("fastPut%d(a, %s)", e.g.types[typ], x)
}

// fastNode returns an expression converting native x of typ into a node.
func (e *apiEmitter) fastNode(typ, x string) string {
	if scalar, ok := e.scalars[typ]; ok {
		return scalar.Node + "(" + x + ")"
	}
	return fmt.Sprintf("fastOut%d(%s)", e.g.types[typ], x)
}

// fastCall returns the native call of root on the converted arguments x0….
func (e *apiEmitter) fastCall(root string) string {
	decl := e.g.functions[root].decl
	args := make([]string, len(decl.Params))
	for i := range args {
		args[i] = fmt.Sprintf("x%d", i)
	}
	return fmt.Sprintf("%s(%s)", e.g.fast.native.Functions[root], strings.Join(args, ", "))
}

// emitFastRun writes fastRun, which Run/RunRoot call after their argument
// checks: it returns the result node and true when the fast path applies.
func (e *apiEmitter) emitFastRun(w *bytes.Buffer) {
	plan := e.g.fast
	multi := moduleir.HasMultipleRoots(e.g.program)
	if multi {
		w.WriteString("\n// fastRun runs the root with function id entry natively when it is eligible,\n// l covers its bound and every argument converts.\nfunc fastRun(entry int, args []*node, l Limits) (*node, bool) {\nswitch entry {\n")
	} else {
		w.WriteString("\n// fastRun runs the root natively when l covers its bound and every argument converts.\nfunc fastRun(args []*node, l Limits) (*node, bool) {\n")
	}
	for _, root := range plan.roots {
		f := e.g.functions[root]
		if multi {
			fmt.Fprintf(w, "case %d:\n", f.id)
		}
		fmt.Fprintf(w, "if !fastCovers%d(l) { return nil, false }\n", f.id)
		for i, p := range f.decl.Params {
			w.WriteString(e.fastRead(p.Type, fmt.Sprintf("args[%d]", i), fmt.Sprintf("x%d", i), "return nil, false"))
		}
		fmt.Fprintf(w, "return %s, true\n", e.fastNode(f.decl.Result, e.fastCall(root)))
	}
	if multi {
		w.WriteString("}\nreturn nil, false\n")
	}
	w.WriteString("}\n")
}

// fastPrelude is the start of a typed entry point: the native call when the
// limits cover the bound and every typed argument converts.
func (e *apiEmitter) fastPrelude(root string) string {
	f := e.g.functions[root]
	var b strings.Builder
	fmt.Fprintf(&b, "if fastCovers%d(l) {\n", f.id)
	var conditions []string
	for i, p := range f.decl.Params {
		if _, scalar := e.scalars[p.Type]; scalar {
			fmt.Fprintf(&b, "x%d := a%d\n", i, i)
			continue
		}
		fmt.Fprintf(&b, "x%d, ok%d := fastIn%d(a%d.v.n)\n", i, i, e.g.types[p.Type], i)
		conditions = append(conditions, fmt.Sprintf("ok%d", i))
	}
	result := f.decl.Result
	value := e.fastCall(root)
	if _, scalar := e.scalars[result]; !scalar {
		value = fmt.Sprintf("%s{v: Value{%s}}", e.names.Types[result], e.fastNode(result, value))
	}
	if len(conditions) > 0 {
		fmt.Fprintf(&b, "if %s {\nreturn %s, nil\n}\n", strings.Join(conditions, " && "), value)
	} else {
		fmt.Fprintf(&b, "return %s, nil\n", value)
	}
	b.WriteString("}\n")
	return b.String()
}
