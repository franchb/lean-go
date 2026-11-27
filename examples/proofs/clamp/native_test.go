package main

import (
	"math/rand/v2"
	"testing"

	clamp "example.com/proved-clamp/clamp"
	native "example.com/proved-clamp/clampnative"
)

// The native package (plain Go, no limits) is checked against the same
// independent model and source claims as the metered package, and against the
// metered package itself.
func checkNativeClaims(t *testing.T, value, limit uint64) {
	t.Helper()
	got := native.Clamp(value, limit)
	if want := minimum(value, limit); got != want {
		t.Fatalf("native clamp(%d, %d) = %d; independent model = %d", value, limit, got, want)
	}
	if got > limit {
		t.Fatalf("bounded: %d exceeds %d", got, limit)
	}
	if value <= limit && got != value {
		t.Fatalf("preserves: %d changed to %d below limit %d", value, got, limit)
	}
	if again := native.Clamp(got, limit); again != got {
		t.Fatalf("idempotent: clamp(%d, %d) = %d", got, limit, again)
	}
	metered, err := clamp.RunTyped(value, limit, clamp.DefaultLimits())
	if err != nil || metered != got {
		t.Fatalf("clamp(%d, %d): native %d, metered %d, %v", value, limit, got, metered, err)
	}
}

func TestNativeClampClaims(t *testing.T) {
	edges := []uint64{0, 1, 2, 41, 42, 43, 1<<32 - 1, 1 << 32,
		1<<53 + 1, 1<<63 - 1, 1 << 63, ^uint64(0) - 1, ^uint64(0)}
	for _, value := range edges {
		for _, limit := range edges {
			checkNativeClaims(t, value, limit)
		}
	}
	random := rand.New(rand.NewPCG(35027, 64))
	for range 512 {
		checkNativeClaims(t, random.Uint64(), random.Uint64())
	}
}

// The native function has no budgets: it neither fails nor allocates.
func TestNativeClampAllocatesNothing(t *testing.T) {
	var sink uint64
	if n := testing.AllocsPerRun(100, func() { sink += native.Clamp(sink, 42) }); n != 0 {
		t.Fatalf("native clamp allocates %v times", n)
	}
}

func BenchmarkClamp(b *testing.B) {
	var sink uint64
	b.Run("native", func(b *testing.B) {
		for i := uint64(0); b.Loop(); i++ {
			sink += native.Clamp(i, 42)
		}
	})
	b.Run("metered", func(b *testing.B) {
		limits := clamp.DefaultLimits()
		for i := uint64(0); b.Loop(); i++ {
			v, _ := clamp.RunTyped(i, 42, limits)
			sink += v
		}
	})
	_ = sink
}
