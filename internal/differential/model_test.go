package differential

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

// Ported from scripts/test_differential_model.py.

func lit(v uint64) Expr { return LitU(v) }

func bigLit(shift uint, plus int64) Expr {
	return Lit(new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), shift), big.NewInt(plus)))
}

func program(expr Expr, width int, helper ...Expr) Program {
	h := Op("add", A(), B())
	if len(helper) > 0 {
		h = helper[0]
	}
	return Program{Seed: 0, Width: width, Expr: expr, Helper: h}
}

func maxOf(width int) uint64 { return uint64(1)<<(width-1)<<1 - 1 }

func mustEval(t *testing.T, p Program, a, b uint64) uint64 {
	t.Helper()
	v, err := Evaluate(p, a, b)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustRender(t *testing.T, p Program) string {
	t.Helper()
	s, err := RenderLean(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustComplexity(t *testing.T, p Program) Complexity {
	t.Helper()
	c, err := ProgramComplexity(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDivisionAndRemainderFollowLeanIncludingZeroDivisors(t *testing.T) {
	for _, width := range Widths {
		maximum := maxOf(width)
		for _, c := range []struct {
			op             string
			a, b, expected uint64
		}{
			{"div", 7, 2, 3}, {"mod", 7, 2, 1}, {"div", maximum, 1, maximum},
			{"div", maximum, maximum, 1}, {"mod", maximum, maximum, 0},
			{"div", 0, 0, 0}, {"mod", 0, 0, 0}, {"div", maximum, 0, 0}, {"mod", maximum, 0, maximum},
			{"div", 1, maximum, 0}, {"mod", 1, maximum, 1},
		} {
			if got := mustEval(t, program(Op(c.op, A(), B()), width), c.a, c.b); got != c.expected {
				t.Errorf("width %d %s(%d, %d) = %d, want %d", width, c.op, c.a, c.b, got, c.expected)
			}
		}
		// A literal divisor of 2^width wraps to zero before dividing.
		if got := mustEval(t, program(Op("div", A(), bigLit(uint(width), 0)), width), 5, 0); got != 0 {
			t.Errorf("width %d: 5 / 2^width = %d", width, got)
		}
		if got := mustEval(t, program(Op("mod", A(), bigLit(uint(width), 0)), width), 5, 0); got != 5 {
			t.Errorf("width %d: 5 %% 2^width = %d", width, got)
		}
	}
}

func TestRenderingUsesOrdinaryDivisionNotation(t *testing.T) {
	for _, width := range Widths {
		source := mustRender(t, program(Op("mod", Op("div", A(), B()), lit(0)), width))
		for _, want := range []string{fmt.Sprintf("((a / b) %% (0 : UInt%d))", width), fmt.Sprintf("def run (a b : UInt%d) : UInt%d", width, width)} {
			if !strings.Contains(source, want) {
				t.Errorf("missing %q in\n%s", want, source)
			}
		}
	}
}

func TestUnsignedArithmeticWrapsAtEachWidth(t *testing.T) {
	for _, width := range Widths {
		maximum := maxOf(width)
		for _, c := range []struct {
			op             string
			a, b, expected uint64
		}{{"add", maximum, 1, 0}, {"sub", 0, 1, maximum}, {"mul", maximum, maximum, 1}, {"mul", 1 << (width - 1), 2, 0}} {
			if got := mustEval(t, program(Op(c.op, A(), B()), width), c.a, c.b); got != c.expected {
				t.Errorf("width %d %s = %d, want %d", width, c.op, got, c.expected)
			}
		}
	}
}

func TestShiftCountsAreReducedBeforeMathematicalMultiplication(t *testing.T) {
	for _, width := range Widths {
		p := program(Op("shl", A(), B()), width)
		w := uint64(width)
		for _, c := range [][2]uint64{{0, 1}, {w - 1, 1 << (w - 1)}, {w, 1}, {w + 1, 2}} {
			if got := mustEval(t, p, 1, c[0]); got != c[1] {
				t.Errorf("width %d: 1 <<< %d = %d, want %d", width, c[0], got, c[1])
			}
		}
		if got := mustEval(t, p, 2, w-1); got != 0 {
			t.Errorf("width %d: 2 <<< %d = %d", width, w-1, got)
		}
	}
}

func TestComparisonsAreUnsignedAndFeedSelectedExpression(t *testing.T) {
	for _, width := range Widths {
		maximum := maxOf(width)
		for _, c := range []struct {
			op                    string
			left, right, expected uint64
		}{{"lt", maximum, 0, 9}, {"lt", 0, maximum, 7}, {"le", maximum, maximum, 7}, {"eq", maximum, maximum, 7}, {"eq", maximum, 0, 9}} {
			expression := Op("if", Op(c.op, A(), B()), lit(7), lit(9))
			if got := mustEval(t, program(expression, width), c.left, c.right); got != c.expected {
				t.Errorf("width %d %s: %d, want %d", width, c.op, got, c.expected)
			}
		}
	}
}

func TestLiteralsReduceExactlyWithoutFloatConversion(t *testing.T) {
	if got := mustEval(t, program(bigLit(32, 5), 32), 0, 0); got != 5 {
		t.Errorf("got %d", got)
	}
	if got := mustEval(t, program(bigLit(64, 5), 64), 0, 0); got != 5 {
		t.Errorf("got %d", got)
	}
	value := uint64(1)<<53 + 1
	if got := mustEval(t, program(lit(value), 64), 0, 0); got != value {
		t.Errorf("got %d", got)
	}
}

func TestRecordsUpdateAndHelperArgumentBinding(t *testing.T) {
	expr := Op("helper", Op("record", Op("wrap", A()), B()), Op("update", A(), B(), lit(10)))
	// helper arguments are 3+4 and updated.inner(10)+other(4), not original a,b.
	if got := mustEval(t, program(expr, 32, Op("sub", B(), A())), 3, 4); got != 7 {
		t.Fatalf("got %d", got)
	}
}

// Python also rejected -1, True, 1.0 and "1": Go's uint64 parameters cannot
// hold them, so they are checked where untyped data enters, in
// TestInvalidInputsFailBeforeEvaluation (ParseInputs) and the case loader.
func TestInvalidInputsDoNotSilentlyWrap(t *testing.T) {
	for _, value := range []uint64{1 << 32, 1<<32 + 5, ^uint64(0)} {
		if _, err := Evaluate(program(A(), 32), value, 0); err == nil {
			t.Errorf("%d accepted as a UInt32 input", value)
		}
		if _, err := Evaluate(program(A(), 32), 0, value); err == nil {
			t.Errorf("%d accepted as a UInt32 input", value)
		}
	}
}

func TestDeterministicBoundedVariedPrograms(t *testing.T) {
	sources := map[string]bool{}
	for _, width := range Widths {
		for seed := uint64(0); seed < 20; seed++ {
			generated, err := Generate(seed, width, 3)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := Generate(seed, width, 3)
			if !generated.Equal(again) {
				t.Fatalf("seed %d width %d is not deterministic", seed, width)
			}
			replayed, err := roundTrip(generated)
			if err != nil || !generated.Equal(replayed) {
				t.Fatalf("serialization round trip failed: %v", err)
			}
			if mustComplexity(t, generated).Nodes > MaxNodes {
				t.Fatal("node limit exceeded")
			}
			pairs, err := Inputs(seed, width, 32)
			if err != nil {
				t.Fatal(err)
			}
			for _, pair := range pairs {
				v := mustEval(t, generated, pair[0], pair[1])
				if checkInput(width, v, "result") != nil {
					t.Fatalf("result %d outside width %d", v, width)
				}
			}
			sources[mustRender(t, generated)] = true
		}
	}
	if len(sources) != 80 {
		t.Fatalf("%d distinct sources, want 80", len(sources))
	}
}

// Python also rejected a negative or boolean seed, which uint64 cannot
// express; the serialized form rejects them in TestExactKeysAndSchemaRequired.
func TestDepthLimitsAndArgumentValidation(t *testing.T) {
	for _, depth := range []int{0, 4} {
		p, err := Generate(1, 64, depth)
		if err != nil {
			t.Fatal(err)
		}
		if mustComplexity(t, p).Nodes > MaxNodes {
			t.Fatal("node limit exceeded")
		}
	}
	for _, c := range []struct{ width, depth int }{{24, 3}, {128, 3}, {32, -1}, {32, 5}} {
		if _, err := Generate(0, c.width, c.depth); err == nil {
			t.Errorf("Generate(0, %d, %d) accepted", c.width, c.depth)
		}
	}
}

func contains(pairs []Pair, pair Pair) bool {
	for _, p := range pairs {
		if p == pair {
			return true
		}
	}
	return false
}

func TestInputPrefixAndExactCount(t *testing.T) {
	for _, width := range Widths {
		pairs, err := Inputs(123, width, 32)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := Inputs(123, width, 32)
		if fmt.Sprint(pairs) != fmt.Sprint(again) || len(pairs) != 32 {
			t.Fatal("inputs are not deterministic or have the wrong count")
		}
		distinct := map[Pair]bool{}
		for _, p := range pairs {
			distinct[p] = true
		}
		if len(distinct) != 32 {
			t.Fatal("inputs are not distinct")
		}
		w, maximum, high := uint64(width), maxOf(width), uint64(1)<<(width-1)
		for _, c := range []struct {
			prefix int
			pair   Pair
		}{{4, Pair{maximum, 1}}, {8, Pair{1, w}}, {8, Pair{1, w + 1}}, {16, Pair{maximum, 0}}, {16, Pair{high, 0}}} {
			if !contains(pairs[:c.prefix], c.pair) {
				t.Errorf("width %d: %v not in the first %d pairs", width, c.pair, c.prefix)
			}
		}
		if width == 64 && !contains(pairs[:16], Pair{1<<53 + 1, 1 << 53}) {
			t.Error("missing the pair above 2^53")
		}
		one, _ := Inputs(123, width, 1)
		if len(one) != 1 || one[0] != (Pair{0, 0}) {
			t.Errorf("Inputs(123, %d, 1) = %v", width, one)
		}
	}
	for _, count := range []int{0, 4097, -1} {
		if _, err := Inputs(0, 32, count); err == nil {
			t.Errorf("count %d accepted", count)
		}
	}
}

func TestLeanRenderingKeepsNamedRecordsAndDataDependentCall(t *testing.T) {
	expr := Op("helper", Op("if", Op("lt", A(), B()), A(), B()), Op("update", Op("wrap", A()), B(), lit(5)))
	source := mustRender(t, program(expr, 64))
	for _, fragment := range []string{"namespace Generated", "structure Box", "inner : Box",
		"@[noinline] def helper", "def run (a b : UInt64)",
		"(decide (a < b))", "{ state with inner := Box.mk (5 : UInt64) }",
		"(helper (if", "(unbox (Box.mk a))", "readState changed",
		"@[noinline] def readState", "@[noinline] def unbox"} {
		if !strings.Contains(source, fragment) {
			t.Errorf("missing %q", fragment)
		}
	}
	if strings.Contains(source, "Float") {
		t.Error("source mentions Float")
	}
}

func TestShiftRenderingUsesOrdinaryNotation(t *testing.T) {
	for _, width := range Widths {
		source := mustRender(t, program(Op("shl", A(), B()), width))
		if !strings.Contains(source, "(a <<< b)") || strings.Contains(source, fmt.Sprintf("UInt%d.shiftLeft", width)) {
			t.Errorf("width %d: %s", width, source)
		}
	}
}

func serializedMap(t *testing.T, p Program) map[string]any {
	t.Helper()
	s, err := Serialize(p)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(s)
	value, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}

func with(m map[string]any, key string, value any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	out[key] = value
	return out
}

func TestExactKeysAndSchemaRequired(t *testing.T) {
	valid := serializedMap(t, program(A(), 32))
	if _, err := Deserialize(valid); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field string
		value any
	}{{"schema", "future"}, {"seed", true}, {"seed", json.Number("-1")}, {"seed", json.Number("1.0")},
		{"seed", json.Number("18446744073709551616")}, {"width", json.Number("7")}, {"width", json.Number("128")}, {"width", true}} {
		if _, err := Deserialize(with(valid, c.field, c.value)); err == nil {
			t.Errorf("%s=%v accepted", c.field, c.value)
		}
	}
	if _, err := Deserialize(with(valid, "unexpected", json.Number("1"))); err == nil {
		t.Error("unexpected program field accepted")
	}
	data := serializedMap(t, program(A(), 32))
	data["expr"].(map[string]any)["unexpected"] = json.Number("1")
	if _, err := Deserialize(data); err == nil {
		t.Error("unexpected expression field accepted")
	}
}

func TestBadArityTypesLiteralsAndRecursiveHelperRejected(t *testing.T) {
	expressions := []Expr{
		Op("add", A()), Op("add", Op("eq", A(), B()), B()),
		Op("if", A(), A(), B()), Op("eq", A(), B()), {Op: "unknown"},
		{Op: "lit", Value: big.NewInt(-1)},
		bigLit(65, 0), {Op: "a", Value: big.NewInt(1)},
	}
	for _, e := range expressions {
		if _, err := Serialize(program(e, 32)); err == nil {
			t.Errorf("%s accepted", e.Key())
		}
	}
	_, err := Serialize(program(A(), 32, Op("helper", A(), B())))
	if err == nil || !strings.Contains(err.Error(), "recursive helper") {
		t.Fatalf("recursive helper: %v", err)
	}
	// A boolean literal payload (Python's value=True) is rejected on load.
	data := serializedMap(t, program(lit(0), 32))
	data["expr"].(map[string]any)["value"] = true
	if _, err := Deserialize(data); err == nil {
		t.Error("boolean literal accepted")
	}
}

func TestParseLimitsApplyBeforeConstruction(t *testing.T) {
	data := serializedMap(t, program(A(), 32))
	deep := map[string]any{"op": "a", "args": []any{}, "value": json.Number("0")}
	for i := 0; i < MaxDepth+1; i++ {
		deep = map[string]any{"op": "wrap", "args": []any{deep}, "value": json.Number("0")}
	}
	data["expr"] = deep
	if _, err := Deserialize(data); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("deep: %v", err)
	}
	wide := map[string]any{"op": "a", "args": []any{}, "value": json.Number("0")}
	for i := 0; i < 11; i++ {
		wide = map[string]any{"op": "add", "args": []any{wide, wide}, "value": json.Number("0")}
	}
	data["expr"] = wide
	if _, err := Deserialize(data); err == nil || !strings.Contains(err.Error(), "node") {
		t.Fatalf("wide: %v", err)
	}
}

func TestEveryCandidateIsValidUniqueAndStrictlySmaller(t *testing.T) {
	original, err := Generate(7, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := ShrinkCandidates(original)
	if err != nil || len(candidates) == 0 {
		t.Fatalf("no candidates: %v", err)
	}
	again, _ := ShrinkCandidates(original)
	keys := map[string]bool{}
	baseline := mustComplexity(t, original)
	for i, c := range candidates {
		if keys[c.Key()] {
			t.Fatal("duplicate candidate")
		}
		keys[c.Key()] = true
		if !c.Equal(again[i]) {
			t.Fatal("candidates are not deterministic")
		}
		if !mustComplexity(t, c).Less(baseline) {
			t.Fatal("candidate is not smaller")
		}
		replayed, err := roundTrip(c)
		if err != nil || !replayed.Equal(c) {
			t.Fatalf("candidate does not round-trip: %v", err)
		}
	}
	if len(again) != len(candidates) {
		t.Fatal("candidates are not deterministic")
	}
}

func TestCanReduceBothHelperAndLiteralPayload(t *testing.T) {
	original := program(lit(100), 32, Op("add", A(), B()))
	candidates, err := ShrinkCandidates(original)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, c := range candidates {
		keys[c.Key()] = true
	}
	for _, want := range []Program{program(lit(0), 32), program(lit(50), 32), program(lit(100), 32, A())} {
		if !keys[want.Key()] {
			t.Errorf("missing candidate %s", want.Key())
		}
	}
	none, _ := ShrinkCandidates(program(A(), 32, B()))
	if len(none) != 0 {
		t.Fatalf("unexpected candidates %v", none)
	}
}

// The model must stay independent of the generators under test: the model,
// serialization and comparison files import only the standard library.
func TestModelIsIndependentOfTheGenerators(t *testing.T) {
	for _, file := range []string{"model.go", "serialize.go", "cases.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if strings.Contains(path, ".") || strings.Contains(path, "lean-go") {
				t.Errorf("%s imports %s", file, path)
			}
		}
	}
}
