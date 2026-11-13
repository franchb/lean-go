package benchmarks_test

import (
	nn "github.com/franchb/lean-go/generated/numbers"
	nq "github.com/franchb/lean-go/generated/quota"
	nt "github.com/franchb/lean-go/generated/trees"
	"reflect"
	"testing"
)

func typedQuotaOutcome(t *testing.T, v nq.Outcome) quotaOutcome {
	t.Helper()
	state, ok := v.State()
	if !ok {
		t.Fatal("missing state")
	}
	capacity, ok := state.Capacity()
	if !ok {
		t.Fatal("missing capacity")
	}
	used, ok := state.Used()
	if !ok {
		t.Fatal("missing used")
	}
	accepted, ok := v.Accepted()
	if !ok {
		t.Fatal("missing accepted")
	}
	return quotaOutcome{quotaState{capacity, used}, accepted}
}
func rawQuotaOutcome(t *testing.T, v nq.Value) quotaOutcome {
	t.Helper()
	fields := v.Fields()
	if v.Constructor() != "Quota.Outcome.mk" || len(fields) != 2 {
		t.Fatal("invalid outcome")
	}
	state := fields[0].Fields()
	if fields[0].Constructor() != "Quota.State.mk" || len(state) != 2 {
		t.Fatal("invalid state")
	}
	capacity, ok := state[0].UInt64()
	if !ok {
		t.Fatal("invalid capacity")
	}
	used, ok := state[1].UInt64()
	if !ok {
		t.Fatal("invalid used")
	}
	accepted := fields[1].Constructor()
	if accepted != "Bool.false" && accepted != "Bool.true" {
		t.Fatal("invalid accepted")
	}
	return quotaOutcome{quotaState{capacity, used}, accepted == "Bool.true"}
}
func TestQuotaParity(t *testing.T) {
	l := nq.DefaultLimits()
	for _, tc := range []struct {
		s      quotaState
		amount uint64
	}{
		{quotaState{1000, 400}, 7}, {quotaState{0, 0}, 0}, {quotaState{1, 0}, 2},
		{quotaState{^uint64(0), ^uint64(0)}, 1}, {quotaState{10, 11}, 0},
	} {
		typed, err := nq.NewState(tc.s.capacity, tc.s.used, l)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := nq.Import(&nq.Input{Type: "Quota.State", Constructor: "Quota.State.mk", Fields: []*nq.Input{{Type: "UInt64", UInt64: tc.s.capacity}, {Type: "UInt64", UInt64: tc.s.used}}}, l)
		if err != nil {
			t.Fatal(err)
		}
		amount, err := nq.Import(&nq.Input{Type: "UInt64", UInt64: tc.amount}, l)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []struct {
			name  string
			typed func(nq.State, uint64, nq.Limits) (nq.Outcome, error)
			hand  func(quotaState, uint64) quotaOutcome
		}{
			{"reserve", nq.RunReserve, reserve}, {"release", nq.RunRelease, release},
		} {
			want := op.hand(tc.s, tc.amount)
			result, err := op.typed(typed, tc.amount, l)
			if err != nil {
				t.Fatal(err)
			}
			rawResult, err := nq.RunRoot("Quota."+op.name, []nq.Value{raw, amount}, l)
			if err != nil {
				t.Fatal(err)
			}
			if got := typedQuotaOutcome(t, result); got != want {
				t.Fatalf("%s typed: got %+v, want %+v", op.name, got, want)
			}
			if got := rawQuotaOutcome(t, rawResult); got != want {
				t.Fatalf("%s raw: got %+v, want %+v", op.name, got, want)
			}
		}
		got, err := nq.RunRemaining(typed, l)
		if err != nil || got != remaining(tc.s) {
			t.Fatalf("remaining typed: %d, %v", got, err)
		}
		result, err := nq.RunRoot("Quota.remaining", []nq.Value{raw}, l)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := result.UInt64()
		if !ok || got != remaining(tc.s) {
			t.Fatalf("remaining raw: %d", got)
		}
	}
}

func typedNumbers(t *testing.T, v nn.Results) numbersResult {
	t.Helper()
	s, d, p, sh, e, ls, le, tw, z, mx, w, m, ok := v.AsMk()
	if !ok {
		t.Fatal("invalid numeric result")
	}
	return numbersResult{s, d, p, sh, e, ls, le, tw, z, mx, w, m}
}
func rawNumbers(t *testing.T, v nn.Value) numbersResult {
	t.Helper()
	fields := v.Fields()
	if v.Constructor() != "Numbers.Results.mk" || len(fields) != 12 {
		t.Fatal("invalid numeric result")
	}
	scalar := func(i int) uint32 {
		t.Helper()
		n, ok := fields[i].UInt32()
		if !ok {
			t.Fatalf("invalid scalar at %d", i)
		}
		return n
	}
	boolean := func(i int) bool {
		t.Helper()
		c := fields[i].Constructor()
		if c != "Bool.true" && c != "Bool.false" {
			t.Fatalf("invalid bool at %d", i)
		}
		return c == "Bool.true"
	}
	return numbersResult{scalar(0), scalar(1), scalar(2), scalar(3), boolean(4), boolean(5), boolean(6), scalar(7), scalar(8), scalar(9), scalar(10), scalar(11)}
}
func TestNumbersParity(t *testing.T) {
	l := nn.DefaultLimits()
	for _, a := range []uint32{0, 1, 123456789, ^uint32(0)} {
		for _, b := range []uint32{0, 1, 31, 32, 37, ^uint32(0)} {
			want := compute(a, b)
			typed, err := nn.RunTyped(a, b, l)
			if err != nil {
				t.Fatal(err)
			}
			x, err := nn.Import(&nn.Input{Type: "UInt32", UInt32: a}, l)
			if err != nil {
				t.Fatal(err)
			}
			y, err := nn.Import(&nn.Input{Type: "UInt32", UInt32: b}, l)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := nn.Run([]nn.Value{x, y}, l)
			if err != nil {
				t.Fatal(err)
			}
			if got := typedNumbers(t, typed); got != want {
				t.Fatalf("typed (%d,%d): got %+v, want %+v", a, b, got, want)
			}
			if got := rawNumbers(t, raw); got != want {
				t.Fatalf("raw (%d,%d): got %+v, want %+v", a, b, got, want)
			}
		}
	}
}

func readRawTree(t *testing.T, v nt.Value) *tree {
	t.Helper()
	fields := v.Fields()
	switch v.Constructor() {
	case "Trees.Tree.leaf":
		if len(fields) != 1 {
			t.Fatal("invalid leaf")
		}
		var color uint8
		switch fields[0].Constructor() {
		case "Trees.Color.red":
			color = 0
		case "Trees.Color.green":
			color = 1
		case "Trees.Color.blue":
			color = 2
		default:
			t.Fatal("invalid color")
		}
		return &tree{color: color}
	case "Trees.Tree.branch":
		if len(fields) != 2 {
			t.Fatal("invalid branch")
		}
		return &tree{left: readRawTree(t, fields[0]), right: readRawTree(t, fields[1])}
	default:
		t.Fatal("invalid tree")
		return nil
	}
}
func TestTreesParity(t *testing.T) {
	for _, shape := range []string{"balanced", "skew"} {
		t.Run(shape, func(t *testing.T) {
			seed := uint8(0)
			n := balanced(7, &seed)
			if shape == "skew" {
				n = skew(64, &seed)
			}
			l := nt.DefaultLimits()
			input := typedTree(t, n)
			typed, err := nt.RunTyped(input, l)
			if err != nil {
				t.Fatal(err)
			}
			rawInput, err := nt.Import(rawTree(n), l)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := nt.Run([]nt.Value{rawInput}, l)
			if err != nil {
				t.Fatal(err)
			}
			want := mirror(n)
			if !reflect.DeepEqual(want, readTree(t, typed)) {
				t.Fatal("typed tree differs")
			}
			if !reflect.DeepEqual(want, readRawTree(t, raw)) {
				t.Fatal("raw tree differs")
			}
			if !reflect.DeepEqual(n, readTree(t, input)) || !reflect.DeepEqual(n, readRawTree(t, rawInput)) {
				t.Fatal("input mutated")
			}
		})
	}
}
