package main

import (
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	clamp "example.com/proved-clamp/clamp"
)

// minimum is an independent Go model; it does not use generated operations.
func minimum(value, limit uint64) uint64 {
	if limit < value {
		return limit
	}
	return value
}

func checkClaims(t *testing.T, value, limit uint64) {
	t.Helper()
	limits := clamp.DefaultLimits()
	got, err := clamp.RunTyped(value, limit, limits)
	if err != nil {
		t.Fatalf("clamp(%d, %d): %v", value, limit, err)
	}
	if want := minimum(value, limit); got != want {
		t.Fatalf("clamp(%d, %d) = %d; independent model = %d", value, limit, got, want)
	}
	if got > limit {
		t.Fatalf("bounded: %d exceeds %d", got, limit)
	}
	if value <= limit && got != value {
		t.Fatalf("preserves: %d changed to %d below limit %d", value, got, limit)
	}
	again, err := clamp.RunTyped(got, limit, limits)
	if err != nil || again != got {
		t.Fatalf("idempotent: clamp(%d, %d) = %d, %v", got, limit, again, err)
	}
}

func TestClampClaims(t *testing.T) {
	edges := []uint64{0, 1, 2, 41, 42, 43, 1<<32 - 1, 1 << 32,
		1<<53 + 1, 1<<63 - 1, 1 << 63, ^uint64(0) - 1, ^uint64(0)}
	for _, value := range edges {
		for _, limit := range edges {
			checkClaims(t, value, limit)
		}
	}
	random := rand.New(rand.NewPCG(35027, 64))
	for range 512 {
		checkClaims(t, random.Uint64(), random.Uint64())
	}
}

func TestClampErrors(t *testing.T) {
	if got, err := clamp.RunTyped(100, 42, clamp.Limits{}); !errors.Is(err, clamp.ErrLimits) || got != 0 {
		t.Fatalf("invalid limits returned %d, %v", got, err)
	}
	limits := clamp.DefaultLimits()
	limits.MaxSteps = 1
	if got, err := clamp.RunTyped(100, 42, limits); !errors.Is(err, clamp.ErrResource) || got != 0 {
		t.Fatalf("exhausted budget returned %d, %v", got, err)
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
	// clamp(7, 42) = 7
	// clamp(100, 42) = 42
	// clamp(18446744073709551615, 42) = 42
}
