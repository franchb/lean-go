package main

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	normalizer "example.com/proved-normalizer/normalizer"
)

// The model below is independent of the generated package: plain Go trees,
// the strong-Kleene truth tables and a direct negation-pushing normalizer.
type kind int

const (
	lit kind = iota
	variable
	neg
	conj
	disj
)

// Truth values are ordered false < unknown < true.
const (
	falseTri byte = iota
	unknownTri
	trueTri
)

type tree struct {
	kind        kind
	value       byte
	id          uint32
	left, right *tree
}

var ids = [2]uint32{0, ^uint32(0)}

func negTri(a byte) byte { return trueTri - a }

func evalModel(e *tree, rho map[uint32]byte) byte {
	switch e.kind {
	case lit:
		return e.value
	case variable:
		return rho[e.id]
	case neg:
		return negTri(evalModel(e.left, rho))
	case conj:
		return min(evalModel(e.left, rho), evalModel(e.right, rho))
	default:
		return max(evalModel(e.left, rho), evalModel(e.right, rho))
	}
}

func normalModel(e *tree) bool {
	switch e.kind {
	case lit, variable:
		return true
	case neg:
		return e.left.kind == variable
	default:
		return normalModel(e.left) && normalModel(e.right)
	}
}

// normalizeModel returns the expected normal form of e, or of its negation.
func normalizeModel(e *tree, negated bool) *tree {
	switch e.kind {
	case lit:
		if negated {
			return &tree{kind: lit, value: negTri(e.value)}
		}
		return e
	case variable:
		if negated {
			return &tree{kind: neg, left: e}
		}
		return e
	case neg:
		return normalizeModel(e.left, !negated)
	default:
		result := e.kind
		if negated {
			result = conj + disj - e.kind
		}
		return &tree{kind: result, left: normalizeModel(e.left, negated), right: normalizeModel(e.right, negated)}
	}
}

func sameTree(a, b *tree) bool {
	if a.kind != b.kind || a.value != b.value || a.id != b.id {
		return false
	}
	if a.left != nil && !sameTree(a.left, b.left) {
		return false
	}
	return a.right == nil || sameTree(a.right, b.right)
}

func build(t testing.TB, e *tree, limits normalizer.Limits) normalizer.Expr {
	t.Helper()
	var result normalizer.Expr
	var err error
	switch e.kind {
	case lit:
		var value normalizer.Tri
		switch e.value {
		case falseTri:
			value, err = normalizer.NewTriF(limits)
		case unknownTri:
			value, err = normalizer.NewTriU(limits)
		default:
			value, err = normalizer.NewTriT(limits)
		}
		if err != nil {
			t.Fatal(err)
		}
		result, err = normalizer.NewExprLit(value, limits)
	case variable:
		result, err = normalizer.NewExprVar(e.id, limits)
	case neg:
		result, err = normalizer.NewExprNeg(build(t, e.left, limits), limits)
	case conj:
		result, err = normalizer.NewExprConj(build(t, e.left, limits), build(t, e.right, limits), limits)
	default:
		result, err = normalizer.NewExprDisj(build(t, e.left, limits), build(t, e.right, limits), limits)
	}
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func read(t testing.TB, e normalizer.Expr) *tree {
	t.Helper()
	if value, ok := e.AsLit(); ok {
		switch {
		case value.IsF():
			return &tree{kind: lit, value: falseTri}
		case value.IsU():
			return &tree{kind: lit, value: unknownTri}
		case value.IsT():
			return &tree{kind: lit, value: trueTri}
		}
	}
	if id, ok := e.AsVar(); ok {
		return &tree{kind: variable, id: id}
	}
	if a, ok := e.AsNeg(); ok {
		return &tree{kind: neg, left: read(t, a)}
	}
	if a, b, ok := e.AsConj(); ok {
		return &tree{kind: conj, left: read(t, a), right: read(t, b)}
	}
	if a, b, ok := e.AsDisj(); ok {
		return &tree{kind: disj, left: read(t, a), right: read(t, b)}
	}
	t.Fatal("generated result has no valid constructor")
	return nil
}

func checkClaims(t testing.TB, input *tree) {
	t.Helper()
	limits := normalizer.DefaultLimits()
	result, err := normalizer.RunTyped(build(t, input, limits), limits)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	got := read(t, result)
	if !sameTree(got, normalizeModel(input, false)) {
		t.Fatal("generated result differs from the independent normal form")
	}
	if !normalModel(got) {
		t.Fatal("normal form: negation remains above a non-variable")
	}
	for a := falseTri; a <= trueTri; a++ {
		for b := falseTri; b <= trueTri; b++ {
			rho := map[uint32]byte{ids[0]: a, ids[1]: b}
			if evalModel(got, rho) != evalModel(input, rho) {
				t.Fatalf("evaluation changed under valuation (%d, %d)", a, b)
			}
		}
	}
}

func expressions(depth int) []*tree {
	result := []*tree{}
	for value := falseTri; value <= trueTri; value++ {
		result = append(result, &tree{kind: lit, value: value})
	}
	for _, id := range ids {
		result = append(result, &tree{kind: variable, id: id})
	}
	if depth == 0 {
		return result
	}
	smaller := expressions(depth - 1)
	for _, a := range smaller {
		result = append(result, &tree{kind: neg, left: a})
		for _, b := range smaller {
			result = append(result, &tree{kind: conj, left: a, right: b}, &tree{kind: disj, left: a, right: b})
		}
	}
	return result
}

func randomTree(random *rand.Rand, depth int) *tree {
	if depth == 0 || random.IntN(4) == 0 {
		if random.IntN(2) == 0 {
			return &tree{kind: lit, value: byte(random.IntN(3))}
		}
		return &tree{kind: variable, id: ids[random.IntN(2)]}
	}
	switch random.IntN(3) {
	case 0:
		return &tree{kind: neg, left: randomTree(random, depth-1)}
	case 1:
		return &tree{kind: conj, left: randomTree(random, depth-1), right: randomTree(random, depth-1)}
	default:
		return &tree{kind: disj, left: randomTree(random, depth-1), right: randomTree(random, depth-1)}
	}
}

func TestNormalizerClaims(t *testing.T) {
	corpus := expressions(2)
	if len(corpus) != 7265 {
		t.Fatalf("depth-two corpus has %d expressions", len(corpus))
	}
	for _, input := range corpus {
		checkClaims(t, input)
	}
	random := rand.New(rand.NewPCG(20261111, 3))
	for range 512 {
		checkClaims(t, randomTree(random, 8))
	}
}

func TestDeepNegationChain(t *testing.T) {
	input := &tree{kind: conj, left: &tree{kind: variable, id: ids[1]}, right: &tree{kind: lit, value: unknownTri}}
	for range 1001 {
		input = &tree{kind: neg, left: input}
	}
	checkClaims(t, input)
}

func TestNormalizerErrors(t *testing.T) {
	limits := normalizer.DefaultLimits()
	input := build(t, &tree{kind: neg, left: &tree{kind: variable, id: 7}}, limits)
	if _, err := normalizer.RunTyped(input, normalizer.Limits{}); !errors.Is(err, normalizer.ErrLimits) {
		t.Fatalf("invalid limits returned %v", err)
	}
	exhausted := limits
	exhausted.MaxSteps = 1
	if _, err := normalizer.RunTyped(input, exhausted); !errors.Is(err, normalizer.ErrResource) {
		t.Fatalf("exhausted budget returned %v", err)
	}
	if _, err := normalizer.RunTyped(normalizer.Expr{}, limits); err == nil {
		t.Fatal("zero expression accepted")
	}
	if _, err := normalizer.NewExprNeg(normalizer.Expr{}, limits); err == nil {
		t.Fatal("constructor accepted a zero child")
	}
	shallow := limits
	shallow.MaxDepth = 2
	deep := build(t, &tree{kind: neg, left: &tree{kind: neg, left: &tree{kind: lit, value: trueTri}}}, limits)
	if _, err := normalizer.RunTyped(deep, shallow); !errors.Is(err, normalizer.ErrResource) {
		t.Fatalf("depth limit returned %v", err)
	}
}

func TestOutput(t *testing.T) {
	var output bytes.Buffer
	if err := demonstrate(&output); err != nil {
		t.Fatal(err)
	}
	const expected = "normalize(not (x4294967295 and U)) = (not x4294967295 or U)\n" +
		"normalize(not not F) = F\n" +
		"normalize(not (F or x0)) = (T and not x0)\n"
	if output.String() != expected {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestGoOnlyEnvironment(t *testing.T) {
	if os.Getenv("LEAN_GO_DEMO_GO_ONLY") != "1" {
		t.Skip("producer tools are hidden by the repository verification gate")
	}
	for _, tool := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
		if _, err := exec.LookPath(tool); err == nil {
			t.Fatalf("producer tool available in Go-only environment: %s", tool)
		}
	}
}

func Example() {
	main()
	// Output:
	// normalize(not (x4294967295 and U)) = (not x4294967295 or U)
	// normalize(not not F) = F
	// normalize(not (F or x0)) = (T and not x0)
}
