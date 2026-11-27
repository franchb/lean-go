package main

import (
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	clock "example.com/proved-clock/clock"
)

// model is an independent Go model: repeated subtraction, no division.
func model(total uint32) (hours, minutes, seconds uint32) {
	n := uint64(total)
	for step := uint64(1 << 20); step > 0; step >>= 1 {
		if n >= step*3600 {
			n -= step * 3600
			hours += uint32(step)
		}
	}
	for n >= 60 {
		n -= 60
		minutes++
	}
	return hours, minutes, uint32(n)
}

func checkClaims(t *testing.T, total uint32) {
	t.Helper()
	limits := clock.DefaultLimits()
	time, err := clock.RunSplit(total, limits)
	if err != nil {
		t.Fatalf("split(%d): %v", total, err)
	}
	hours, minutes, seconds, err := fields(time)
	if err != nil {
		t.Fatal(err)
	}
	if h, m, s := model(total); hours != h || minutes != m || seconds != s {
		t.Fatalf("split(%d) = %d:%d:%d; independent model = %d:%d:%d", total, hours, minutes, seconds, h, m, s)
	}
	if minutes >= 60 || seconds >= 60 {
		t.Fatalf("bounds: split(%d) = %d:%d:%d", total, hours, minutes, seconds)
	}
	back, err := clock.RunJoin(time, limits)
	if err != nil || back != total {
		t.Fatalf("recombines: join(split(%d)) = %d, %v", total, back, err)
	}
	built, err := clock.NewHms(hours, minutes, seconds, limits)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := clock.RunJoin(built, limits); err != nil || again != total {
		t.Fatalf("join(%d, %d, %d) = %d, %v", hours, minutes, seconds, again, err)
	}
}

func TestClockClaims(t *testing.T) {
	edges := []uint32{0, 1, 59, 60, 61, 3599, 3600, 3601, 3661, 86399, 86400,
		1<<31 - 1, 1 << 31, 4294965599, 4294965600, ^uint32(0) - 1, ^uint32(0)}
	for _, total := range edges {
		checkClaims(t, total)
	}
	random := rand.New(rand.NewPCG(3600, 60))
	for range 1024 {
		checkClaims(t, random.Uint32())
	}
}

// join wraps like UInt32 outside the range of split, and still never divides.
func TestJoinWraps(t *testing.T) {
	limits := clock.DefaultLimits()
	hours := ^uint32(0)
	time, err := clock.NewHms(hours, 59, 59, limits)
	if err != nil {
		t.Fatal(err)
	}
	got, err := clock.RunJoin(time, limits)
	if want := uint32((uint64(hours)*3600 + 59*60 + 59) % (1 << 32)); err != nil || got != want {
		t.Fatalf("join = %d, %v; want %d", got, err, want)
	}
}

func TestClockErrors(t *testing.T) {
	if _, err := clock.RunSplit(3661, clock.Limits{}); !errors.Is(err, clock.ErrLimits) {
		t.Fatalf("invalid limits returned %v", err)
	}
	limits := clock.DefaultLimits()
	limits.MaxSteps = 1
	if _, err := clock.RunSplit(3661, limits); !errors.Is(err, clock.ErrResource) {
		t.Fatalf("exhausted budget returned %v", err)
	}
	if _, err := clock.RunJoin(clock.Hms{}, clock.DefaultLimits()); err == nil {
		t.Fatal("zero Hms accepted")
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
	// 0 s = 0 h 0 min 0 s = 0 s
	// 3661 s = 1 h 1 min 1 s = 3661 s
	// 86399 s = 23 h 59 min 59 s = 86399 s
	// 4294967295 s = 1193046 h 28 min 15 s = 4294967295 s
}
