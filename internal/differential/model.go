// Package differential generates small Lean source programs from a seed,
// computes their expected results with an independent model, compares them
// with native Lean and the generated Go, and shrinks and saves failures.
//
// The model in this file deliberately depends on no extraction IR, compiler
// output or Go emitter: it must not import internal/modulegen,
// internal/nativegen, internal/moduleir or code shared with them. Numbers are
// arbitrary-precision integers (math/big) reduced explicitly modulo 2^width,
// so the expected values do not come from Go's fixed-width operators either.
// Serialized programs are the reproducible input of a campaign.
package differential

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"sort"
	"strings"
)

// ProgramSchema identifies a serialized program.
const ProgramSchema = "lean-go-differential/v1"

// Model limits; replayed programs are checked against them before use.
const (
	MaxNodes = 2048
	MaxDepth = 24
)

// MaxLiteral is the largest literal payload, 2^65-1: literals may exceed
// the width and wrap.
var MaxLiteral = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 65), big.NewInt(1))

// Widths are the supported unsigned widths.
var Widths = []int{8, 16, 32, 64}

var arity = map[string]int{
	"a": 0, "b": 0, "lit": 0, "add": 2, "sub": 2, "mul": 2, "div": 2, "mod": 2,
	"land": 2, "lor": 2, "xor": 2, "complement": 1, "shr": 2,
	"shl": 2, "if": 3, "helper": 2, "wrap": 1, "record": 2,
	"update": 3, "eq": 2, "lt": 2, "le": 2,
}

var predicates = []string{"eq", "le", "lt"} // sorted

func isPredicate(op string) bool { return op == "eq" || op == "lt" || op == "le" }

// Expr is one immutable source expression. Value is the literal payload of
// "lit" and must be nil or zero for every other operation.
type Expr struct {
	Op    string
	Args  []Expr
	Value *big.Int
}

// Program is a generated source program: two scalar inputs a and b, one
// scalar result, and a non-recursive helper of the same shape.
type Program struct {
	Seed   uint64
	Width  int
	Expr   Expr
	Helper Expr
}

// Pair is one input (a, b).
type Pair [2]uint64

// A, B, Lit and Op build expressions.
func A() Expr                         { return Expr{Op: "a"} }
func B() Expr                         { return Expr{Op: "b"} }
func Lit(v *big.Int) Expr             { return Expr{Op: "lit", Value: new(big.Int).Set(v)} }
func LitU(v uint64) Expr              { return Lit(new(big.Int).SetUint64(v)) }
func Op(op string, args ...Expr) Expr { return Expr{Op: op, Args: args} }

func (e Expr) value() *big.Int {
	if e.Value == nil {
		return new(big.Int)
	}
	return e.Value
}

// Key is a canonical text of the expression, for equality and sets.
func (e Expr) Key() string {
	var b strings.Builder
	e.key(&b)
	return b.String()
}

func (e Expr) key(b *strings.Builder) {
	b.WriteString(e.Op)
	if e.Op == "lit" || (e.Value != nil && e.Value.Sign() != 0) {
		b.WriteString(":")
		b.WriteString(e.value().String())
	}
	if len(e.Args) > 0 {
		b.WriteString("(")
		for i, arg := range e.Args {
			if i > 0 {
				b.WriteString(",")
			}
			arg.key(b)
		}
		b.WriteString(")")
	}
}

// Key is a canonical text of the program, for equality and sets.
func (p Program) Key() string {
	return fmt.Sprintf("%d/%d/%s/%s", p.Seed, p.Width, p.Expr.Key(), p.Helper.Key())
}

// Equal reports structural equality.
func (p Program) Equal(q Program) bool { return p.Key() == q.Key() }

func checkWidth(width int) error {
	for _, w := range Widths {
		if w == width {
			return nil
		}
	}
	return errors.New("width must be 8, 16, 32 or 64")
}

func checkExpr(e Expr, boolean, allowHelper bool, budget *int, depth int) error {
	if depth > MaxDepth {
		return errors.New("expression depth limit exceeded")
	}
	*budget--
	if *budget < 0 {
		return errors.New("expression node limit exceeded")
	}
	n, ok := arity[e.Op]
	if !ok {
		return errors.New("unknown expression operation")
	}
	if len(e.Args) != n {
		return errors.New("expression arity mismatch")
	}
	v := e.value()
	if v.Sign() < 0 || v.Cmp(MaxLiteral) > 0 {
		return fmt.Errorf("literal must be an integer in 0..%s", MaxLiteral)
	}
	if e.Op != "lit" && v.Sign() != 0 {
		return errors.New("nonliteral expression has a payload")
	}
	if isPredicate(e.Op) != boolean {
		return errors.New("expression type mismatch")
	}
	if e.Op == "helper" && !allowHelper {
		return errors.New("recursive helper is not supported")
	}
	for i, arg := range e.Args {
		if err := checkExpr(arg, e.Op == "if" && i == 0, allowHelper, budget, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// Validate rejects malformed or excessive programs before evaluation or rendering.
func Validate(p Program) error {
	if err := checkWidth(p.Width); err != nil {
		return err
	}
	budget := MaxNodes
	if err := checkExpr(p.Expr, false, true, &budget, 0); err != nil {
		return err
	}
	return checkExpr(p.Helper, false, false, &budget, 0)
}

var (
	zero = big.NewInt(0)
	one  = big.NewInt(1)
	two  = big.NewInt(2)
)

func pow2(n int) *big.Int { return new(big.Int).Exp(two, big.NewInt(int64(n)), nil) }

// digit is (x / 2^i) mod 2, by division rather than bit operations.
func digit(x *big.Int, i int) *big.Int {
	q := new(big.Int).Quo(x, pow2(i))
	return q.Mod(q, two)
}

func truth(b bool) *big.Int {
	if b {
		return big.NewInt(1)
	}
	return big.NewInt(0)
}

func eval(e Expr, a, b, modulus *big.Int, width int, helper Expr) *big.Int {
	switch e.Op {
	case "a":
		return a
	case "b":
		return b
	case "lit":
		return new(big.Int).Mod(e.value(), modulus)
	case "if":
		if eval(e.Args[0], a, b, modulus, width, helper).Sign() != 0 {
			return eval(e.Args[1], a, b, modulus, width, helper)
		}
		return eval(e.Args[2], a, b, modulus, width, helper)
	}
	values := make([]*big.Int, len(e.Args))
	for i, arg := range e.Args {
		values[i] = eval(arg, a, b, modulus, width, helper)
	}
	r := new(big.Int)
	switch e.Op {
	case "helper":
		return eval(helper, values[0], values[1], modulus, width, helper)
	case "wrap":
		return values[0]
	case "update":
		return r.Mod(r.Add(values[2], values[1]), modulus)
	case "complement":
		return r.Sub(r.Sub(modulus, one), values[0])
	}
	left, right := values[0], values[1]
	switch e.Op {
	case "land", "lor", "xor":
		// Per-bit arithmetic is independent of both Lean and the Go operators.
		for i := 0; i < width; i++ {
			x, y := digit(left, i), digit(right, i)
			var bit *big.Int
			switch e.Op {
			case "land":
				bit = new(big.Int).Mul(x, y)
			case "lor":
				bit = new(big.Int).Add(x, y)
				if bit.Cmp(one) > 0 {
					bit = big.NewInt(1)
				}
			default:
				bit = new(big.Int).Add(x, y)
				bit.Mod(bit, two)
			}
			r.Add(r, bit.Mul(bit, pow2(i)))
		}
		return r
	case "shr":
		shift := new(big.Int).Mod(right, big.NewInt(int64(width)))
		return r.Quo(left, pow2(int(shift.Int64())))
	case "add", "record":
		return r.Mod(r.Add(left, right), modulus)
	case "sub":
		return r.Mod(r.Sub(left, right), modulus)
	case "mul":
		return r.Mod(r.Mul(left, right), modulus)
	case "div":
		// Lean defines x / 0 = 0 and x % 0 = x; both results are below the modulus.
		if right.Sign() == 0 {
			return r
		}
		return r.Quo(left, right)
	case "mod":
		if right.Sign() == 0 {
			return left
		}
		return r.Rem(left, right)
	case "shl":
		shift := new(big.Int).Mod(right, big.NewInt(int64(width)))
		return r.Mod(r.Mul(left, pow2(int(shift.Int64()))), modulus)
	case "eq":
		return truth(left.Cmp(right) == 0)
	case "lt":
		return truth(left.Cmp(right) < 0)
	case "le":
		return truth(left.Cmp(right) <= 0)
	}
	panic("unknown expression operation " + e.Op)
}

// Evaluate computes the source semantics with arbitrary precision and
// explicit reduction. Inputs must already be below 2^width.
func Evaluate(p Program, a, b uint64) (uint64, error) {
	if err := Validate(p); err != nil {
		return 0, err
	}
	if err := checkInput(p.Width, a, "input a"); err != nil {
		return 0, err
	}
	if err := checkInput(p.Width, b, "input b"); err != nil {
		return 0, err
	}
	modulus := pow2(p.Width)
	r := eval(p.Expr, new(big.Int).SetUint64(a), new(big.Int).SetUint64(b), modulus, p.Width, p.Helper)
	if r.Sign() < 0 || r.Cmp(modulus) >= 0 {
		return 0, errors.New("model result outside its width")
	}
	return r.Uint64(), nil
}

func checkInput(width int, value uint64, label string) error {
	if width < 64 && value>>width != 0 {
		return fmt.Errorf("%s must be an integer in 0..%d", label, uint64(1)<<width-1)
	}
	return nil
}

// generator wraps a deterministic PCG stream. math/rand/v2's PCG is a
// specified algorithm, so a seed names the same program on every platform
// and Go release.
type generator struct{ r *rand.Rand }

const (
	programStream = 0x6c65616e2d676f31 // "lean-go1"
	inputStream   = 0x494e505554       // "INPUT"
)

func (g generator) below(n int) int { return g.r.IntN(n) }

// bits returns a uniformly random value of width bits.
func (g generator) bits(width int) uint64 {
	v := g.r.Uint64()
	if width < 64 {
		v >>= 64 - width
	}
	return v
}

func (g generator) leaf(width int) Expr {
	boundary := []*big.Int{
		big.NewInt(0), big.NewInt(1), big.NewInt(2), big.NewInt(int64(width - 1)), big.NewInt(int64(width)), big.NewInt(int64(width + 1)),
		new(big.Int).Sub(pow2(width), one), pow2(width), new(big.Int).Add(pow2(width), one),
	}
	choice := g.below(4)
	if choice < 2 {
		if choice == 0 {
			return A()
		}
		return B()
	}
	if choice == 2 {
		return Lit(boundary[g.below(len(boundary))])
	}
	return LitU(g.bits(width))
}

var generatedOps = []string{"add", "sub", "mul", "div", "mod", "shl", "land", "lor", "xor", "complement",
	"shr", "if", "wrap", "record", "update"}

func (g generator) expr(width, depth int) Expr {
	if depth == 0 || g.below(5) == 0 {
		return g.leaf(width)
	}
	op := generatedOps[g.below(len(generatedOps))]
	if op == "if" {
		condition := Op(predicates[g.below(len(predicates))], g.expr(width, depth-1), g.leaf(width))
		return Op("if", condition, g.expr(width, depth-1), g.expr(width, depth-1))
	}
	args := make([]Expr, arity[op])
	for i := range args {
		args[i] = g.expr(width, depth-1)
	}
	return Op(op, args...)
}

// Generate returns a reproducible bounded composition with records and direct calls.
func Generate(seed uint64, width, depth int) (Program, error) {
	if err := checkWidth(width); err != nil {
		return Program{}, err
	}
	if depth < 0 || depth > 4 {
		return Program{}, errors.New("generation depth must be an integer in 0..4")
	}
	g := generator{rand.New(rand.NewPCG(seed, programStream))}
	helper := Op("add", Op("sub", A(), B()), g.expr(width, min(depth, 2)))
	branch := Op("if", Op(predicates[g.below(len(predicates))], A(), B()), g.expr(width, depth), g.expr(width, depth))
	call := Op("helper", branch, g.expr(width, depth))
	recordOp := []string{"record", "update"}[g.below(2)]
	recordArgs := []Expr{Op("wrap", g.expr(width, depth))}
	for i := 1; i < arity[recordOp]; i++ {
		recordArgs = append(recordArgs, g.expr(width, depth))
	}
	operands := []Expr{call, Op(recordOp, recordArgs...)}
	g.r.Shuffle(len(operands), func(i, j int) { operands[i], operands[j] = operands[j], operands[i] })
	top := []string{"add", "sub", "mul", "shl"}[g.below(4)]
	p := Program{Seed: seed, Width: width, Expr: Op(top, operands...), Helper: helper}
	if err := Validate(p); err != nil {
		return Program{}, err
	}
	return p, nil
}

// Inputs returns exactly count distinct pairs: boundary pairs, then seeded
// random pairs. Count is the total, not the number added after boundaries.
// The prefix covers extrema, unsigned high bits, width-masked shifts, zero
// divisors and UInt64 values above 2^53.
func Inputs(seed uint64, width, count int) ([]Pair, error) {
	if err := checkWidth(width); err != nil {
		return nil, err
	}
	if count < 1 || count > MaxInputs {
		return nil, fmt.Errorf("input count must be in 1..%d", MaxInputs)
	}
	w := uint64(width)
	maximum := uint64(1)<<(w-1)<<1 - 1 // 2^width-1 without overflowing at 64
	high := uint64(1) << (w - 1)
	boundaries := []Pair{{0, 0}, {maximum, 1}, {0, 1}, {maximum, maximum},
		{1, w}, {high, 1}, {1, w - 1}, {1, w + 1},
		{1, 0}, {0, maximum}, {high - 1, high}, {high, high - 1},
		{maximum, 0}, {high, 0}}
	if width == 64 {
		boundaries = append(boundaries, Pair{1 << 53, 1<<53 + 1}, Pair{1<<53 + 1, 1 << 53})
	}
	result := append([]Pair(nil), boundaries[:min(count, len(boundaries))]...)
	seen := map[Pair]bool{}
	for _, pair := range result {
		seen[pair] = true
	}
	g := generator{rand.New(rand.NewPCG(seed^inputStream, inputStream))}
	for len(result) < count {
		pair := Pair{g.bits(width), g.bits(width)}
		if !seen[pair] {
			seen[pair] = true
			result = append(result, pair)
		}
	}
	return result, nil
}

var operators = map[string]string{"add": "+", "sub": "-", "mul": "*", "div": "/", "mod": "%",
	"land": "&&&", "lor": "|||", "xor": "^^^", "shl": "<<<", "shr": ">>>",
	"eq": "==", "lt": "<", "le": "<="}

func render(e Expr, scalar string) string {
	switch e.Op {
	case "a", "b":
		return e.Op
	case "lit":
		return fmt.Sprintf("(%s : %s)", e.value(), scalar)
	}
	args := make([]string, len(e.Args))
	for i, arg := range e.Args {
		args[i] = render(arg, scalar)
	}
	if e.Op == "complement" {
		return "(~~~" + args[0] + ")"
	}
	if operator, ok := operators[e.Op]; ok {
		body := "(" + args[0] + " " + operator + " " + args[1] + ")"
		if e.Op == "lt" || e.Op == "le" {
			return "(decide " + body + ")"
		}
		return body
	}
	switch e.Op {
	case "if":
		return "(if " + args[0] + " then " + args[1] + " else " + args[2] + ")"
	case "helper":
		return "(helper " + args[0] + " " + args[1] + ")"
	case "wrap":
		return "(unbox (Box.mk " + args[0] + "))"
	}
	state := "State.mk (Box.mk " + args[0] + ") " + args[1]
	if e.Op == "record" {
		return "(readState (" + state + "))"
	}
	return "(let state := " + state + "; " +
		"let changed := { state with inner := Box.mk " + args[2] + " }; " +
		"readState changed)"
}

// RenderLean renders one ordinary Lean module with a scalar root Generated.run.
func RenderLean(p Program) (string, error) {
	if err := Validate(p); err != nil {
		return "", err
	}
	scalar := fmt.Sprintf("UInt%d", p.Width)
	return "namespace Generated\n\n" +
		"structure Box where\n  value : " + scalar + "\n\n" +
		"structure State where\n  inner : Box\n  other : " + scalar + "\n\n" +
		"@[noinline] def unbox (box : Box) : " + scalar + " := box.value\n\n" +
		"@[noinline] def readState (state : State) : " + scalar + " :=\n" +
		"  state.inner.value + state.other\n\n" +
		"@[noinline] def helper (a b : " + scalar + ") : " + scalar + " :=\n" +
		"  " + render(p.Helper, scalar) + "\n\n" +
		"def run (a b : " + scalar + ") : " + scalar + " :=\n" +
		"  " + render(p.Expr, scalar) + "\n\n" +
		"end Generated\n", nil
}

// Complexity is the well-founded lexicographic shrinking metric: node count,
// then the sum of literal payloads.
type Complexity struct {
	Nodes    int
	Literals *big.Int
}

// Less orders complexities lexicographically.
func (c Complexity) Less(d Complexity) bool {
	if c.Nodes != d.Nodes {
		return c.Nodes < d.Nodes
	}
	return c.Literals.Cmp(d.Literals) < 0
}

func measure(e Expr) (int, *big.Int) {
	nodes, literals := 1, new(big.Int).Set(e.value())
	for _, arg := range e.Args {
		n, l := measure(arg)
		nodes += n
		literals.Add(literals, l)
	}
	return nodes, literals
}

// ProgramComplexity measures a valid program.
func ProgramComplexity(p Program) (Complexity, error) {
	if err := Validate(p); err != nil {
		return Complexity{}, err
	}
	n1, l1 := measure(p.Expr)
	n2, l2 := measure(p.Helper)
	return Complexity{n1 + n2, l1.Add(l1, l2)}, nil
}

func smallerExprs(e Expr) []Expr {
	var out []Expr
	if e.Op == "lit" {
		v := e.value()
		set := map[string]*big.Int{}
		for _, c := range []*big.Int{big.NewInt(0), big.NewInt(1), new(big.Int).Quo(v, two)} {
			if c.Cmp(v) < 0 {
				set[c.String()] = c
			}
		}
		values := make([]*big.Int, 0, len(set))
		for _, c := range set {
			values = append(values, c)
		}
		sort.Slice(values, func(i, j int) bool { return values[i].Cmp(values[j]) < 0 })
		for _, c := range values {
			out = append(out, Lit(c))
		}
	} else if !isPredicate(e.Op) && len(e.Args) > 0 {
		out = append(out, LitU(0))
		for _, arg := range e.Args {
			if !isPredicate(arg.Op) {
				out = append(out, arg)
			}
		}
	}
	for i, arg := range e.Args {
		for _, smaller := range smallerExprs(arg) {
			args := append([]Expr(nil), e.Args...)
			args[i] = smaller
			out = append(out, Expr{Op: e.Op, Args: args, Value: e.Value})
		}
	}
	return out
}

// ShrinkCandidates returns deterministic, type-valid candidates strictly
// below the program's complexity, without duplicates.
func ShrinkCandidates(p Program) ([]Program, error) {
	baseline, err := ProgramComplexity(p)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Program
	for field := 0; field < 2; field++ {
		source := p.Expr
		if field == 1 {
			source = p.Helper
		}
		for _, e := range smallerExprs(source) {
			candidate := p
			if field == 0 {
				candidate.Expr = e
			} else {
				candidate.Helper = e
			}
			key := candidate.Key()
			if seen[key] {
				continue
			}
			c, err := ProgramComplexity(candidate)
			if err != nil || !c.Less(baseline) {
				continue
			}
			seen[key] = true
			out = append(out, candidate)
		}
	}
	return out, nil
}
