package quota_test

import (
	"testing"

	"github.com/franchb/lean-go/quota"
)

var benchmarkState quota.State
var benchmarkUnits uint64

func BenchmarkReserve(b *testing.B) {
	state, err := quota.New(^uint64(0))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		next, accepted, err := state.Reserve(1)
		if err != nil || !accepted {
			b.Fatalf("reserve: %t, %v", accepted, err)
		}
		benchmarkState = next
	}
}

func BenchmarkRelease(b *testing.B) {
	state, err := quota.New(^uint64(0))
	if err != nil {
		b.Fatal(err)
	}
	state, accepted, err := state.Reserve(1)
	if err != nil || !accepted {
		b.Fatalf("setup reserve: %t, %v", accepted, err)
	}
	b.ReportAllocs()
	for b.Loop() {
		next, accepted, err := state.Release(1)
		if err != nil || !accepted {
			b.Fatalf("release: %t, %v", accepted, err)
		}
		benchmarkState = next
	}
}

func BenchmarkRemaining(b *testing.B) {
	state, err := quota.New(^uint64(0))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		value, err := state.Remaining()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkUnits = value
	}
}
