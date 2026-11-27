package modulegen

import (
	"github.com/franchb/lean-go/internal/moduleir"
)

// Static resource bounds for the metered fast path.
//
// A root is statically bounded when its reachable call graph has no
// recursion and every type it uses is non-recursive. For such a root, bound
// computes, per dimension of Limits, an upper bound on what the state
// machine (control.go, runtime.go) can consume on any well-formed arguments.
// The analysis follows the state machine's own accounting:
//
//   - MaxSteps: execute calls b.step once per loop iteration, and every
//     iteration executes exactly one IR node of the top frame (let, cases,
//     join, jump or return). A call is the let node itself; the callee's
//     nodes are its own iterations. So the steps of a run are the IR nodes it
//     executes, and the bound is the longest path through the body counted in
//     nodes, a call adding its callee's bound and a jump the join body's.
//   - MaxAllocations: b.make runs once for every executed let of a literal,
//     primitive or constructor (dead or not), never for calls, cases, joins,
//     jumps or returns. Bound: the largest number of such lets on a path.
//   - MaxSlots: b.enter charges the callee's slot count for the root frame
//     and for every call, and frames never give slots back. Bound: the root's
//     slot count plus, along the path, the bounds of the callees.
//   - MaxFrames: the root frame is entered with no frame below it, and a call
//     at stack length d needs d < MaxFrames, so MaxFrames must be at least
//     the deepest stack: 1 for the root plus the deepest call chain.
//   - MaxNodes: only the argument check of Run/RunRoot reads it (the sum of
//     the arguments' cached sizes). Every well-formed value of a
//     non-recursive type T has at most maxSize(T) nodes.
//   - MaxDepth: checked against each argument's depth, each node b.make
//     builds and the result. Bound: the largest maxDepth of the argument
//     types, the types of the values the run builds and the result type.
//   - MaxOutputNodes: checked against each node b.make builds and the
//     result. A built node's size is one plus its fields' cached sizes, so it
//     is at most maxSize of its type. Bound: the largest maxSize of the built
//     types and the result type.
//
// Each dimension takes the maximum over all paths independently, so the
// bound dominates every single run. Counts saturate at saturated, far above
// every cap of DefaultLimits, so a saturated bound is never covered.

// The seven dimensions, in the field order of Limits.
const (
	dimNodes = iota
	dimDepth
	dimOutputNodes
	dimAllocations
	dimSteps
	dimFrames
	dimSlots
	dimensions
)

// limitCaps is DefaultLimits of every generated package, in Limits order.
// checkLimits rejects larger limits, so a bound above a cap is never covered.
var limitCaps = [dimensions]uint64{65536, 4096, 131072, 65536, 4194304, 8192, 1048576}

var limitNames = [dimensions]string{"MaxNodes", "MaxDepth", "MaxOutputNodes", "MaxAllocations", "MaxSteps", "MaxFrames", "MaxSlots"}

const saturated = uint64(1) << 62

func satAdd(a, b uint64) uint64 {
	if a >= saturated || b >= saturated-a {
		return saturated
	}
	return a + b
}

// shape bounds every value of a type: its node count and depth.
type shape struct {
	min, max, depth uint64
	recursive       bool
}

// cost bounds the path-dependent dimensions of one function or code tree.
type cost struct{ steps, allocations, slots, frames uint64 }

func (c cost) plus(d cost) cost {
	return cost{satAdd(c.steps, d.steps), satAdd(c.allocations, d.allocations), satAdd(c.slots, d.slots), max(c.frames, d.frames)}
}

func (c cost) join(d cost) cost {
	return cost{max(c.steps, d.steps), max(c.allocations, d.allocations), max(c.slots, d.slots), max(c.frames, d.frames)}
}

type boundAnalysis struct {
	g      *generator
	shapes map[string]shape
	active map[string]bool
	costs  map[string]cost
	codes  map[*moduleir.Code]cost
}

func newBoundAnalysis(g *generator) *boundAnalysis {
	return &boundAnalysis{g: g, shapes: map[string]shape{}, active: map[string]bool{}, costs: map[string]cost{}, codes: map[*moduleir.Code]cost{}}
}

// shape returns the size and depth bounds of typ; recursive is set when typ
// lies on, or reaches, a cycle of the field graph.
func (a *boundAnalysis) shape(typ string) shape {
	if _, scalar := scalarFeatureTypes[typ]; scalar {
		return shape{1, 1, 1, false}
	}
	if s, ok := a.shapes[typ]; ok {
		return s
	}
	if a.active[typ] {
		return shape{recursive: true}
	}
	a.active[typ] = true
	defer delete(a.active, typ)
	result := shape{min: saturated}
	for _, typeDecl := range a.g.program.Types {
		if typeDecl.Name != typ {
			continue
		}
		for _, c := range typeDecl.Constructors {
			size, least, depth := uint64(1), uint64(1), uint64(1)
			for _, field := range c.Fields {
				s := a.shape(field)
				result.recursive = result.recursive || s.recursive
				size, least = satAdd(size, s.max), satAdd(least, s.min)
				depth = max(depth, satAdd(s.depth, 1))
			}
			result.max, result.min, result.depth = max(result.max, size), min(result.min, least), max(result.depth, depth)
		}
	}
	a.shapes[typ] = result
	return result
}

var scalarFeatureTypes = map[string]bool{"UInt8": true, "UInt16": true, "UInt32": true, "UInt64": true}

// reachable returns the functions reachable from root, or false when one of
// them calls itself (IR validation already excludes mutual recursion).
func (a *boundAnalysis) reachable(root string) ([]*function, bool) {
	seen := map[string]bool{}
	var out []*function
	var visit func(string) bool
	visit = func(name string) bool {
		if seen[name] {
			return true
		}
		seen[name] = true
		f := a.g.functions[name]
		out = append(out, f)
		for _, c := range f.nodes {
			if c.Op == "let" && c.Value.Kind == "call" {
				if c.Value.Name == name || !visit(c.Value.Name) {
					return false
				}
			}
		}
		return true
	}
	return out, visit(root)
}

// function returns the cost of a call of f, its own frame included.
func (a *boundAnalysis) function(f *function) cost {
	if c, ok := a.costs[f.decl.Name]; ok {
		return c
	}
	body := a.code(f, f.decl.Body)
	c := cost{body.steps, body.allocations, satAdd(uint64(len(f.slots)), body.slots), satAdd(body.frames, 1)}
	a.costs[f.decl.Name] = c
	return c
}

// code returns the cost of running c to the return of its function. Join
// bodies cannot jump to themselves (IR validation), so the recursion through
// jumps terminates, and memoization keeps it linear in the code size.
func (a *boundAnalysis) code(f *function, c *moduleir.Code) cost {
	if known, ok := a.codes[c]; ok {
		return known
	}
	step := cost{steps: 1}
	var result cost
	switch c.Op {
	case "return":
		result = step
	case "let":
		if c.Value.Kind == "call" {
			callee := a.function(a.g.functions[c.Value.Name])
			result = step.plus(callee).plus(a.code(f, c.Next))
		} else {
			result = cost{steps: 1, allocations: 1}.plus(a.code(f, c.Next))
		}
	case "cases":
		var alts cost
		for _, alt := range c.Alts {
			alts = alts.join(a.code(f, alt.Body))
		}
		result = step.plus(alts)
	case "join":
		result = step.plus(a.code(f, c.Next))
	case "jump":
		result = step.plus(a.code(f, f.joins[c.Target].Body))
	}
	a.codes[c] = result
	return result
}

// rootBound returns the static bound of root in Limits order, or false when
// root is not statically bounded (recursion or a recursive type).
func (a *boundAnalysis) rootBound(root string) ([dimensions]uint64, bool) {
	var bound [dimensions]uint64
	functions, ok := a.reachable(root)
	if !ok {
		return bound, false
	}
	used := func(typ string) bool { return !a.shape(typ).recursive }
	built := map[string]bool{}
	for _, f := range functions {
		if !used(f.decl.Result) {
			return bound, false
		}
		for _, p := range f.decl.Params {
			if !used(p.Type) {
				return bound, false
			}
		}
		for _, c := range f.nodes {
			switch c.Op {
			case "let":
				if !used(c.Bind.Type) {
					return bound, false
				}
				if c.Value.Kind != "call" {
					built[c.Bind.Type] = true
				}
			case "cases":
				if !used(c.Type) {
					return bound, false
				}
				for _, alt := range c.Alts {
					for _, p := range alt.Params {
						if !used(p.Type) {
							return bound, false
						}
					}
				}
			case "join":
				for _, p := range c.Join.Params {
					if !used(p.Type) {
						return bound, false
					}
				}
			}
		}
	}
	decl := a.g.functions[root].decl
	result := a.shape(decl.Result)
	bound[dimDepth], bound[dimOutputNodes] = result.depth, result.max
	for _, p := range decl.Params {
		s := a.shape(p.Type)
		bound[dimNodes] = satAdd(bound[dimNodes], s.max)
		bound[dimDepth] = max(bound[dimDepth], s.depth)
	}
	for typ := range built {
		s := a.shape(typ)
		bound[dimDepth] = max(bound[dimDepth], s.depth)
		bound[dimOutputNodes] = max(bound[dimOutputNodes], s.max)
	}
	c := a.function(a.g.functions[root])
	bound[dimAllocations], bound[dimSteps], bound[dimFrames], bound[dimSlots] = c.allocations, c.steps, c.frames, c.slots
	// MaxNodes and the other limits must be at least 1 (checkLimits).
	for i := range bound {
		bound[i] = max(bound[i], 1)
	}
	return bound, true
}

// withinCaps reports whether some valid Limits can cover bound.
func withinCaps(bound [dimensions]uint64) bool {
	for i, b := range bound {
		if b > limitCaps[i] {
			return false
		}
	}
	return true
}
