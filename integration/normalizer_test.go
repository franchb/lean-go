package integration_test

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	n "github.com/franchb/lean-go/generated/normalizer"
)

var meet = [3][3]n.Tri{{n.False, n.False, n.False}, {n.False, n.Unknown, n.Unknown}, {n.False, n.Unknown, n.True}}
var join = [3][3]n.Tri{{n.False, n.Unknown, n.True}, {n.Unknown, n.Unknown, n.True}, {n.True, n.True, n.True}}
var negate = [3]n.Tri{n.True, n.Unknown, n.False}

func leaf(v n.Tri) *n.Input                   { return &n.Input{Kind: n.Literal, Value: v} }
func variable(id uint32) *n.Input             { return &n.Input{Kind: n.Variable, ID: id} }
func unary(a *n.Input) *n.Input               { return &n.Input{Kind: n.Not, Left: a} }
func binary(k n.Kind, a, b *n.Input) *n.Input { return &n.Input{Kind: k, Left: a, Right: b} }

func evalInput(e *n.Input, a, b n.Tri) n.Tri {
	switch e.Kind {
	case n.Literal:
		return e.Value
	case n.Variable:
		if e.ID == 0 {
			return a
		}
		if e.ID == 1 {
			return b
		}
		return n.Unknown
	case n.Not:
		return negate[evalInput(e.Left, a, b)]
	case n.And:
		return meet[evalInput(e.Left, a, b)][evalInput(e.Right, a, b)]
	case n.Or:
		return join[evalInput(e.Left, a, b)][evalInput(e.Right, a, b)]
	}
	panic("invalid oracle expression")
}
func snapshot(e n.Expr) *n.Input {
	l, r := e.Children()
	switch e.Kind() {
	case n.Literal:
		v, ok := e.Literal()
		if !ok {
			panic("literal accessor")
		}
		return leaf(v)
	case n.Variable:
		v, ok := e.Variable()
		if !ok {
			panic("variable accessor")
		}
		return variable(v)
	case n.Not:
		return unary(snapshot(l))
	case n.And, n.Or:
		return binary(e.Kind(), snapshot(l), snapshot(r))
	}
	panic("invalid generated handle")
}
func normal(e *n.Input) bool {
	switch e.Kind {
	case n.Literal, n.Variable:
		return true
	case n.Not:
		return e.Left.Kind == n.Variable
	case n.And, n.Or:
		return normal(e.Left) && normal(e.Right)
	}
	return false
}
func ids(e *n.Input, out *[]uint32) {
	switch e.Kind {
	case n.Variable:
		*out = append(*out, e.ID)
	case n.Not:
		ids(e.Left, out)
	case n.And, n.Or:
		ids(e.Left, out)
		ids(e.Right, out)
	}
}
func checkExpression(t testing.TB, e *n.Input) *n.Input {
	t.Helper()
	owned, err := n.Import(e, n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	result, err := n.Normalize(owned, n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	out := snapshot(result)
	if !normal(out) {
		t.Fatal("output is not normal form")
	}
	var before, after []uint32
	ids(e, &before)
	ids(out, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("variable order changed: %v -> %v", before, after)
	}
	for a := n.False; a <= n.True; a++ {
		for b := n.False; b <= n.True; b++ {
			if x, y := evalInput(e, a, b), evalInput(out, a, b); x != y {
				t.Fatalf("semantics changed for (%v,%v): %v -> %v", a, b, x, y)
			}
		}
	}
	return out
}
func expressions(depth int) []*n.Input {
	leaves := []*n.Input{leaf(n.False), leaf(n.Unknown), leaf(n.True), variable(0), variable(1)}
	if depth == 0 {
		return leaves
	}
	prev := expressions(depth - 1)
	out := append([]*n.Input{}, leaves...)
	for _, a := range prev {
		out = append(out, unary(a))
	}
	for _, a := range prev {
		for _, b := range prev {
			out = append(out, binary(n.And, a, b), binary(n.Or, a, b))
		}
	}
	return out
}
func TestExhaustiveDepthTwo(t *testing.T) {
	corpus := expressions(2)
	if len(corpus) != 7265 {
		t.Fatalf("corpus count %d", len(corpus))
	}
	for i, e := range corpus {
		t.Run(fmt.Sprint(i), func(t *testing.T) { checkExpression(t, e) })
	}
}
func TestOrderedDeMorganAndUInt32(t *testing.T) {
	e := unary(binary(n.And, variable(^uint32(0)), binary(n.Or, variable(0), leaf(n.Unknown))))
	want := binary(n.Or, unary(variable(^uint32(0))), binary(n.And, unary(variable(0)), leaf(n.Unknown)))
	if out := checkExpression(t, e); !reflect.DeepEqual(out, want) {
		t.Fatalf("ordered result mismatch: %#v", out)
	}
	checkExpression(t, unary(unary(unary(leaf(n.Unknown)))))
}
func TestMalformedAndOwnership(t *testing.T) {
	cycle := unary(nil)
	cycle.Left = cycle
	cases := []*n.Input{nil, {}, {Kind: 99}, {Kind: n.Literal, Value: 3}, {Kind: n.Literal, Left: leaf(n.False)}, {Kind: n.Literal, ID: 1}, {Kind: n.Variable, Value: n.Unknown}, {Kind: n.Not}, {Kind: n.Not, Left: variable(0), Right: variable(1)}, {Kind: n.And, Left: variable(0)}, cycle}
	for i, e := range cases {
		if _, err := n.Import(e, n.DefaultLimits()); !errors.Is(err, n.ErrMalformed) {
			t.Fatalf("malformed %d: %v", i, err)
		}
	}
	if _, err := n.Normalize(n.Expr{}, n.DefaultLimits()); !errors.Is(err, n.ErrMalformed) {
		t.Fatalf("zero handle: %v", err)
	}
	shared := variable(1)
	input := binary(n.And, shared, shared)
	e, err := n.Import(input, n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	shared.ID = 99
	input.Left = leaf(n.False)
	input.Right = nil
	if out := snapshot(e); !reflect.DeepEqual(out, binary(n.And, variable(1), variable(1))) {
		t.Fatal("input mutation escaped ownership boundary")
	}
	limits := n.DefaultLimits()
	limits.MaxNodes = 2
	if _, err := n.Import(binary(n.And, shared, shared), limits); !errors.Is(err, n.ErrResource) {
		t.Fatalf("shared occurrence budget: %v", err)
	}
	limits.MaxNodes = 3
	if _, err := n.Import(binary(n.And, shared, shared), limits); err != nil {
		t.Fatalf("sharing mistaken for cycle: %v", err)
	}
}
func TestInputBudgets(t *testing.T) {
	for _, nodes := range []uint64{2, 3, 4} {
		l := n.DefaultLimits()
		l.MaxNodes = nodes
		_, err := n.Import(binary(n.And, variable(0), variable(1)), l)
		if (nodes < 3) != errors.Is(err, n.ErrResource) {
			t.Fatalf("nodes %d: %v", nodes, err)
		}
	}
	for _, depth := range []uint64{1, 2, 3} {
		l := n.DefaultLimits()
		l.MaxDepth = depth
		_, err := n.Import(unary(variable(0)), l)
		if (depth < 2) != errors.Is(err, n.ErrResource) {
			t.Fatalf("depth %d: %v", depth, err)
		}
	}
	l := n.DefaultLimits()
	l.MaxNodes = 0
	if _, err := n.Import(variable(0), l); !errors.Is(err, n.ErrLimits) {
		t.Fatalf("zero limit: %v", err)
	}
	l = n.DefaultLimits()
	l.MaxSteps++
	if _, err := n.Import(variable(0), l); !errors.Is(err, n.ErrLimits) {
		t.Fatalf("above cap: %v", err)
	}
	e, err := n.Import(unary(variable(0)), n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	l = n.DefaultLimits()
	l.MaxDepth = 1
	if _, err = n.Normalize(e, l); !errors.Is(err, n.ErrResource) {
		t.Fatalf("normalize input depth not rechecked: %v", err)
	}
}
func TestNormalizationBudgetThresholds(t *testing.T) {
	e, err := n.Import(unary(binary(n.And, variable(0), variable(1))), n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	dimensions := []struct {
		name string
		get  func(n.Limits) uint64
		set  func(*n.Limits, uint64)
	}{
		{"output", func(l n.Limits) uint64 { return l.MaxOutputNodes }, func(l *n.Limits, v uint64) { l.MaxOutputNodes = v }},
		{"allocations", func(l n.Limits) uint64 { return l.MaxAllocations }, func(l *n.Limits, v uint64) { l.MaxAllocations = v }},
		{"steps", func(l n.Limits) uint64 { return l.MaxSteps }, func(l *n.Limits, v uint64) { l.MaxSteps = v }},
		{"frames", func(l n.Limits) uint64 { return l.MaxFrames }, func(l *n.Limits, v uint64) { l.MaxFrames = v }},
	}
	for _, d := range dimensions {
		t.Run(d.name, func(t *testing.T) {
			lo, hi := uint64(1), d.get(n.DefaultLimits())
			for lo < hi {
				mid := lo + (hi-lo)/2
				l := n.DefaultLimits()
				d.set(&l, mid)
				_, err := n.Normalize(e, l)
				if err == nil {
					hi = mid
				} else if errors.Is(err, n.ErrResource) {
					lo = mid + 1
				} else {
					t.Fatal(err)
				}
			}
			if lo <= 1 {
				t.Fatalf("test does not exercise %s budget", d.name)
			}
			for _, v := range []uint64{lo - 1, lo, lo + 1} {
				l := n.DefaultLimits()
				d.set(&l, v)
				out, err := n.Normalize(e, l)
				if v < lo {
					if !errors.Is(err, n.ErrResource) || out.Kind() != n.Invalid {
						t.Fatalf("below threshold %d: %v", v, err)
					}
				} else if err != nil {
					t.Fatalf("threshold %d: %v", v, err)
				}
			}
		})
	}
	// One fresh binary node still produces three output occurrences when leaves are reused.
	owned, err := n.Import(binary(n.And, variable(0), variable(1)), n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, cap := range []uint64{2, 3, 4} {
		limits := n.DefaultLimits()
		limits.MaxAllocations = 1
		limits.MaxOutputNodes = cap
		_, err := n.Normalize(owned, limits)
		if (cap < 3) != errors.Is(err, n.ErrResource) {
			t.Fatalf("reused output occurrence cap %d: %v", cap, err)
		}
	}
	for _, cap := range []uint64{2, 3, 4} {
		limits := n.DefaultLimits()
		limits.MaxAllocations = cap
		_, err := n.Normalize(e, limits)
		if (cap < 3) != errors.Is(err, n.ErrResource) {
			t.Fatalf("fresh construction cap %d: %v", cap, err)
		}
	}
}
func TestDeepAndExponentialSharing(t *testing.T) {
	e := variable(0)
	for i := 1; i < 4096; i++ {
		e = unary(e)
	}
	owned, err := n.Import(e, n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	out, err := n.Normalize(owned, n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind() != n.Not {
		t.Fatal("deep odd-negation normalization")
	}
	e = unary(e)
	if _, err = n.Import(e, n.DefaultLimits()); !errors.Is(err, n.ErrResource) {
		t.Fatalf("depth overflow: %v", err)
	}
	dag := variable(0)
	for i := 0; i < 17; i++ {
		dag = binary(n.And, dag, dag)
	}
	if _, err = n.Import(dag, n.DefaultLimits()); !errors.Is(err, n.ErrResource) {
		t.Fatalf("expanded DAG budget: %v", err)
	}
}
func randomExpr(r *rand.Rand, depth int) *n.Input {
	if depth == 0 {
		if r.Intn(2) == 0 {
			return leaf(n.Tri(r.Intn(3)))
		}
		return variable(uint32(r.Intn(3)))
	}
	switch r.Intn(5) {
	case 0:
		return leaf(n.Tri(r.Intn(3)))
	case 1:
		return variable(r.Uint32())
	case 2:
		return unary(randomExpr(r, depth-1))
	case 3:
		return binary(n.And, randomExpr(r, depth-1), randomExpr(r, depth-1))
	default:
		return binary(n.Or, randomExpr(r, depth-1), randomExpr(r, depth-1))
	}
}
func TestSeededLargerExpressions(t *testing.T) {
	r := rand.New(rand.NewSource(20261111))
	for i := 0; i < 1000; i++ {
		checkExpression(t, randomExpr(r, 8))
	}
}
func FuzzNormalize(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(20261111))
	f.Add(int64(-1))
	f.Fuzz(func(t *testing.T, seed int64) { checkExpression(t, randomExpr(rand.New(rand.NewSource(seed)), 7)) })
}
func BenchmarkNormalizeDeep(b *testing.B) {
	e := variable(0)
	for i := 1; i < 4096; i++ {
		e = unary(e)
	}
	owned, err := n.Import(e, n.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := n.Normalize(owned, n.DefaultLimits()); err != nil {
			b.Fatal(err)
		}
	}
}

func wideTree(depth int) *n.Input {
	if depth == 0 {
		return variable(0)
	}
	return binary(n.And, wideTree(depth-1), wideTree(depth-1))
}
func BenchmarkNormalizeWide(b *testing.B) {
	owned, err := n.Import(wideTree(12), n.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := n.Normalize(owned, n.DefaultLimits()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkImportShared(b *testing.B) {
	input := variable(0)
	for i := 0; i < 12; i++ {
		input = binary(n.And, input, input)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := n.Import(input, n.DefaultLimits()); err != nil {
			b.Fatal(err)
		}
	}
}
func TestConcurrentOwnedNormalization(t *testing.T) {
	sourceLeaf := variable(^uint32(0))
	owned, err := n.Import(unary(binary(n.And, sourceLeaf, leaf(n.Unknown))), n.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for i := uint32(0); ; i++ {
			select {
			case <-stop:
				return
			default:
				sourceLeaf.ID = i
			}
		}
	}()
	defer func() { close(stop); <-stopped }()
	results := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		go func() {
			for i := 0; i < 100; i++ {
				out, err := n.Normalize(owned, n.DefaultLimits())
				if err != nil {
					results <- err
					return
				}
				left, _ := out.Children()
				v, _ := left.Children()
				id, ok := v.Variable()
				if out.Kind() != n.Or || left.Kind() != n.Not || !ok || id != ^uint32(0) {
					results <- fmt.Errorf("owned value changed during concurrent normalization")
					return
				}
			}
			results <- nil
		}()
	}
	for worker := 0; worker < 8; worker++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}
