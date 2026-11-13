package main

import (
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	flags "example.com/lean-go-flags-demo/flags"
)

// model computes each bit with integer arithmetic, independently of generated code.
func model(state, mask uint32) (set, clear, toggle uint32, contains bool) {
	contains = true
	for bit := uint64(1); bit < 1<<32; bit *= 2 {
		s, m := uint64(state)/bit%2, uint64(mask)/bit%2
		if s+m > 0 {
			set += uint32(bit)
		}
		if s == 1 && m == 0 {
			clear += uint32(bit)
		}
		if s != m {
			toggle += uint32(bit)
		}
		if m == 1 && s == 0 {
			contains = false
		}
	}
	return
}

func checkPair(t *testing.T, state, mask uint32) {
	t.Helper()
	limits := flags.DefaultLimits()
	set, errSet := flags.RunSet(state, mask, limits)
	clear, errClear := flags.RunClear(state, mask, limits)
	toggle, errToggle := flags.RunToggle(state, mask, limits)
	contains, errContains := flags.RunContains(state, mask, limits)
	if err := errors.Join(errSet, errClear, errToggle, errContains); err != nil {
		t.Fatalf("operations(%#x, %#x): %v", state, mask, err)
	}
	ws, wc, wt, wh := model(state, mask)
	if set != ws || clear != wc || toggle != wt || contains != wh {
		t.Fatalf("operations(%#x, %#x) = (%#x, %#x, %#x, %t), model = (%#x, %#x, %#x, %t)",
			state, mask, set, clear, toggle, contains, ws, wc, wt, wh)
	}
	// These are extraction regression checks; the universal proofs concern Lean source.
	if got, err := flags.RunSet(set, mask, limits); err != nil || got != set {
		t.Fatalf("set idempotence: %#x, %v", got, err)
	}
	if got, err := flags.RunClear(clear, mask, limits); err != nil || got != clear {
		t.Fatalf("clear idempotence: %#x, %v", got, err)
	}
	if clear&mask != 0 {
		t.Fatalf("cleared bits remain: %#x & %#x", clear, mask)
	}
	if got, err := flags.RunToggle(toggle, mask, limits); err != nil || got != state {
		t.Fatalf("toggle involution: %#x, %v", got, err)
	}
	for name, got := range map[string]uint32{"set": set, "clear": clear, "toggle": toggle} {
		if got & ^mask != state & ^mask {
			t.Fatalf("%s changed bits outside mask %#x: %#x -> %#x", name, mask, state, got)
		}
	}
	if got, err := flags.RunContains(set, mask, limits); err != nil || !got {
		t.Fatalf("set must contain every masked bit: %t, %v", got, err)
	}
}

func TestFlagOperations(t *testing.T) {
	for _, tc := range []struct {
		state, mask, set, clear, toggle uint32
		contains                        bool
	}{
		{0, 0, 0, 0, 0, true},
		{0x80000001, 0, 0x80000001, 0x80000001, 0x80000001, true},
		{1, 3, 3, 0, 2, false}, // Some bits present is not all bits present.
		{3, 3, 3, 0, 0, true},
		{1, 0x80000000, 0x80000001, 1, 0x80000001, false},
		{0x80000001, 0x80000000, 0x80000001, 1, 1, true},
		{0xffffffff, 0xffffffff, 0xffffffff, 0, 0, true},
		{0, 0xffffffff, 0xffffffff, 0, 0xffffffff, false},
	} {
		s, es := flags.RunSet(tc.state, tc.mask, flags.DefaultLimits())
		c, ec := flags.RunClear(tc.state, tc.mask, flags.DefaultLimits())
		x, ex := flags.RunToggle(tc.state, tc.mask, flags.DefaultLimits())
		h, eh := flags.RunContains(tc.state, tc.mask, flags.DefaultLimits())
		if err := errors.Join(es, ec, ex, eh); err != nil || s != tc.set || c != tc.clear || x != tc.toggle || h != tc.contains {
			t.Fatalf("operations(%#x, %#x) = (%#x, %#x, %#x, %t), %v", tc.state, tc.mask, s, c, x, h, err)
		}
	}
}

func TestFlagClaims(t *testing.T) {
	edges := []uint32{0, 1, 2, 3, 0xff, 0xffff, 0x10000, 0x7fffffff,
		0x80000000, 0x80000001, 0xfffffffe, 0xffffffff}
	for _, state := range edges {
		for _, mask := range edges {
			checkPair(t, state, mask)
		}
	}
	for state := uint32(0); state < 64; state++ {
		for mask := uint32(0); mask < 64; mask++ {
			checkPair(t, state, mask)
		}
	}
	random := rand.New(rand.NewPCG(436, 32))
	for range 512 {
		checkPair(t, random.Uint32(), random.Uint32())
	}
}

func TestFlagErrors(t *testing.T) {
	invalid := flags.DefaultLimits()
	invalid.MaxSteps = 0
	exhausted := flags.DefaultLimits()
	exhausted.MaxSteps = 1
	for _, tc := range []struct {
		name   string
		limits flags.Limits
		want   error
	}{
		{"zero limits", flags.Limits{}, flags.ErrLimits},
		{"invalid steps", invalid, flags.ErrLimits},
		{"exhausted steps", exhausted, flags.ErrResource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for name, run := range map[string]func(uint32, uint32, flags.Limits) (uint32, error){
				"set": flags.RunSet, "clear": flags.RunClear, "toggle": flags.RunToggle,
			} {
				if got, err := run(0x80000001, 0x80000000, tc.limits); !errors.Is(err, tc.want) || got != 0 {
					t.Fatalf("%s returned partial result %#x or wrong error %v; want %v", name, got, err, tc.want)
				}
			}
			if got, err := flags.RunContains(0x80000001, 0x80000000, tc.limits); !errors.Is(err, tc.want) || got {
				t.Fatalf("contains returned partial result %t or wrong error %v; want %v", got, err, tc.want)
			}
		})
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
	// set(0x00000001, 0x80000002) = 0x80000003
	// contains(0x80000003, 0x80000002) = true
	// clear(0x80000003, 0x00000002) = 0x80000001
	// toggle(0x80000001, 0x80000000) = 0x00000001
}
