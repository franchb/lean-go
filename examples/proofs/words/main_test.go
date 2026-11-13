package main

import (
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	words "example.com/lean-go-words-demo/words"
)

const radix uint64 = 4294967296

func importWord(t *testing.T, input *words.Input) words.Value {
	t.Helper()
	value, err := words.Import(input, words.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func rawNumber(t *testing.T, value words.Value, wide bool) uint64 {
	t.Helper()
	if wide {
		number, ok := value.UInt64()
		_, wrongWidth := value.UInt32()
		if !ok || wrongWidth || value.Type() != "UInt64" {
			t.Fatalf("expected UInt64 result, got %q", value.Type())
		}
		return number
	}
	number, ok := value.UInt32()
	_, wrongWidth := value.UInt64()
	if !ok || wrongWidth || value.Type() != "UInt32" {
		t.Fatalf("expected UInt32 result, got %q", value.Type())
	}
	return uint64(number)
}

func checkRawOperations(t *testing.T, hi, lo uint32, word uint64) {
	t.Helper()
	high := importWord(t, &words.Input{Type: "UInt32", UInt32: hi})
	low := importWord(t, &words.Input{Type: "UInt32", UInt32: lo})
	wide := importWord(t, &words.Input{Type: "UInt64", UInt64: word})
	for _, tc := range []struct {
		root string
		args []words.Value
		wide bool
		want uint64
	}{
		{"Words.widen", []words.Value{high}, true, uint64(hi)},
		{"Words.narrow", []words.Value{wide}, false, word % radix},
		{"Words.pack", []words.Value{high, low}, true, uint64(hi)*radix + uint64(lo)},
		{"Words.high", []words.Value{wide}, false, word / radix},
		{"Words.low", []words.Value{wide}, false, word % radix},
	} {
		got, err := words.RunRoot(tc.root, tc.args, words.DefaultLimits())
		if err != nil {
			t.Fatalf("%s: %v", tc.root, err)
		}
		if number := rawNumber(t, got, tc.wide); number != tc.want {
			t.Fatalf("%s(%#x, %#x, %#x) = %#x, want %#x", tc.root, hi, lo, word, number, tc.want)
		}
	}
}

func checkTriple(t *testing.T, hi, lo uint32, word uint64) {
	t.Helper()
	limits := words.DefaultLimits()
	wide, ew := words.RunWiden(hi, limits)
	narrow, en := words.RunNarrow(word, limits)
	packed, ep := words.RunPack(hi, lo, limits)
	high, eh := words.RunHigh(word, limits)
	low, el := words.RunLow(word, limits)
	if err := errors.Join(ew, en, ep, eh, el); err != nil {
		t.Fatal(err)
	}
	// Identity, modulo, multiplication and division are independent of emitted casts/shifts/OR.
	got := [5]uint64{wide, uint64(narrow), packed, uint64(high), uint64(low)}
	want := [5]uint64{uint64(hi), word % radix, uint64(hi)*radix + uint64(lo), word / radix, word % radix}
	if got != want {
		t.Fatalf("operations(%#x, %#x, %#x) = %x, model = %x", hi, lo, word, got, want)
	}
	// The first two source laws are the identity/modulo checks above. These are the other five.
	narrowWide, enw := words.RunNarrow(wide, limits)
	wideNarrow, ewn := words.RunWiden(narrow, limits)
	lowPack, elp := words.RunLow(packed, limits)
	highPack, ehp := words.RunHigh(packed, limits)
	repacked, epu := words.RunPack(high, low, limits)
	if err := errors.Join(enw, ewn, elp, ehp, epu); err != nil {
		t.Fatal(err)
	}
	if narrowWide != hi || wideNarrow != word%radix || lowPack != lo || highPack != hi || repacked != word {
		t.Fatalf("word laws failed for (%#x, %#x, %#x)", hi, lo, word)
	}
	checkRawOperations(t, hi, lo, word)
}

func TestWordClaims(t *testing.T) {
	edges32 := []uint32{0, 1, 2, 0xffff, 0x10000, 0x7ffffffe, 0x7fffffff,
		0x80000000, 0x80000001, 0xaaaaaaaa, 0x55555555, 0xfffffffe, 0xffffffff}
	for _, hi := range edges32 {
		for _, lo := range edges32 {
			checkTriple(t, hi, lo, uint64(hi)*radix+uint64(lo))
		}
	}
	edges64 := []uint64{0, 1, 2, 0x7fffffff, 0x80000000, 0x80000001,
		0xfffffffe, 0xffffffff, 0x100000000, 0x100000001, 0x100000002,
		0x7fffffffffffffff, 0x8000000000000000, 0x8000000000000001,
		0xaaaaaaaaaaaaaaaa, 0x5555555555555555, 0xfffffffffffffffe, 0xffffffffffffffff}
	for _, word := range edges64 {
		checkTriple(t, uint32(word/radix), uint32(word%radix), word)
	}
	random := rand.New(rand.NewPCG(436, 3264))
	for range 512 {
		checkTriple(t, random.Uint32(), random.Uint32(), random.Uint64())
	}
	t.Logf("checked %d boundary pairs, %d wide boundaries and 512 seeded triples (%d total)",
		len(edges32)*len(edges32), len(edges64), len(edges32)*len(edges32)+len(edges64)+512)
}

type operation struct {
	root               string
	args               []words.Value
	wide               bool
	want               uint64
	allocations, steps uint64
	run                func(words.Limits) (uint64, error)
}

func operations(t *testing.T) []operation {
	t.Helper()
	const hi, lo uint32 = 0x80000001, 0xffffffff
	const word = uint64(hi)*radix + uint64(lo)
	high := importWord(t, &words.Input{Type: "UInt32", UInt32: hi})
	low := importWord(t, &words.Input{Type: "UInt32", UInt32: lo})
	wide := importWord(t, &words.Input{Type: "UInt64", UInt64: word})
	return []operation{
		{"Words.widen", []words.Value{high}, true, uint64(hi), 1, 2,
			func(l words.Limits) (uint64, error) { return words.RunWiden(hi, l) }},
		{"Words.narrow", []words.Value{wide}, false, word % radix, 1, 2,
			func(l words.Limits) (uint64, error) { v, err := words.RunNarrow(word, l); return uint64(v), err }},
		{"Words.pack", []words.Value{high, low}, true, word, 5, 6,
			func(l words.Limits) (uint64, error) { return words.RunPack(hi, lo, l) }},
		{"Words.high", []words.Value{wide}, false, word / radix, 3, 4,
			func(l words.Limits) (uint64, error) { v, err := words.RunHigh(word, l); return uint64(v), err }},
		{"Words.low", []words.Value{wide}, false, word % radix, 1, 2,
			func(l words.Limits) (uint64, error) { v, err := words.RunLow(word, l); return uint64(v), err }},
	}
}

func checkFailure(t *testing.T, op operation, limits words.Limits, want error) {
	t.Helper()
	if got, err := op.run(limits); !errors.Is(err, want) || got != 0 {
		t.Fatalf("typed %s returned %#x, %v; want zero, %v", op.root, got, err, want)
	}
	before := append([]words.Value(nil), op.args...)
	got, err := words.RunRoot(op.root, op.args, limits)
	if !errors.Is(err, want) || got.Type() != "" {
		t.Fatalf("raw %s returned %q, %v; want invalid result, %v", op.root, got.Type(), err, want)
	}
	for i, arg := range op.args {
		if arg != before[i] {
			t.Fatalf("%s replaced argument %d", op.root, i)
		}
	}
	// The same owned arguments must still succeed after the failed call.
	unchanged, err := words.RunRoot(op.root, op.args, words.DefaultLimits())
	if err != nil || rawNumber(t, unchanged, op.wide) != op.want {
		t.Fatalf("%s changed owned inputs after failure: %v", op.root, err)
	}
}

func TestWordBudgets(t *testing.T) {
	for _, op := range operations(t) {
		t.Run(op.root, func(t *testing.T) {
			exact := words.DefaultLimits()
			exact.MaxAllocations, exact.MaxSteps = op.allocations, op.steps
			got, err := op.run(exact)
			if err != nil || got != op.want {
				t.Fatalf("exact budgets: %#x, %v; want %#x", got, err, op.want)
			}
			raw, err := words.RunRoot(op.root, op.args, exact)
			if err != nil || rawNumber(t, raw, op.wide) != op.want {
				t.Fatalf("exact raw budgets: %v", err)
			}
			steps := exact
			steps.MaxSteps--
			checkFailure(t, op, steps, words.ErrResource)
			allocations := exact
			allocations.MaxAllocations--
			allocationError := words.ErrResource
			if allocations.MaxAllocations == 0 {
				allocationError = words.ErrLimits
			}
			checkFailure(t, op, allocations, allocationError)
			slots := words.DefaultLimits()
			slots.MaxSlots = 1
			checkFailure(t, op, slots, words.ErrResource)
			checkFailure(t, op, words.Limits{}, words.ErrLimits)
			invalid := words.DefaultLimits()
			invalid.MaxSteps = 0
			checkFailure(t, op, invalid, words.ErrLimits)
			invalid = words.DefaultLimits()
			invalid.MaxNodes++
			checkFailure(t, op, invalid, words.ErrLimits)
		})
	}
}

func TestOwnedInputsAndMalformedCalls(t *testing.T) {
	mutable := &words.Input{Type: "UInt32", UInt32: 0x80000001}
	owned := importWord(t, mutable)
	mutable.UInt32 = 0
	limits := words.DefaultLimits()
	wide, err := words.RunRoot("Words.widen", []words.Value{owned}, limits)
	if err != nil || rawNumber(t, wide, true) != 0x80000001 || rawNumber(t, owned, false) != 0x80000001 {
		t.Fatalf("caller mutation changed the owned value: %v", err)
	}
	for _, tc := range []struct {
		root string
		args []words.Value
	}{
		{"Words.widen", nil},
		{"Words.widen", []words.Value{wide}},
		{"Words.narrow", []words.Value{owned}},
		{"Words.high", []words.Value{owned}},
		{"Words.low", []words.Value{owned}},
		{"Words.pack", []words.Value{owned}},
		{"Words.pack", []words.Value{owned, wide}},
		{"Words.pack", []words.Value{owned, {}}},
		{"Words.unknown", []words.Value{owned}},
	} {
		got, err := words.RunRoot(tc.root, tc.args, limits)
		if !errors.Is(err, words.ErrMalformed) || got.Type() != "" {
			t.Fatalf("malformed %s: %q, %v", tc.root, got.Type(), err)
		}
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
	// widen(0x80000001) = 0x0000000080000001
	// pack(0x80000001, 0xffffffff) = 0x80000001ffffffff
	// high(0x80000001ffffffff) = 0x80000001
	// low(0x80000001ffffffff) = 0xffffffff
	// narrow(0x80000001ffffffff) = 0xffffffff
}
