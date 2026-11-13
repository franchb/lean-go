package benchmarks_test

import (
	nn "github.com/franchb/lean-go/generated/numbers"
	"testing"
)

type numbersResult struct {
	sum, difference, product, shifted    uint32
	equal, less, lessEq                  bool
	twice, zero, maximum, wrapped, mixed uint32
}

func compute(a, b uint32) numbersResult {
	return numbersResult{a + b, a - b, a * b, a << (b & 31), a == b, a < b, a <= b, a * 2, 0, ^uint32(0), 1, (a + 1) * (b - 1)}
}

var numbersHandSink numbersResult
var numbersTypedSink nn.Results
var numbersRawSink nn.Value

func BenchmarkNumbers(b *testing.B) {
	a, c := uint32(123456789), uint32(37)
	l := nn.DefaultLimits()
	ra, err := nn.Import(&nn.Input{Type: "UInt32", UInt32: a}, l)
	if err != nil {
		b.Fatal(err)
	}
	rc, err := nn.Import(&nn.Input{Type: "UInt32", UInt32: c}, l)
	if err != nil {
		b.Fatal(err)
	}
	args := []nn.Value{ra, rc}
	b.Run("Handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			numbersHandSink = compute(a, c)
		}
	})
	b.Run("Typed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nn.RunTyped(a, c, l)
			if err != nil {
				b.Fatal(err)
			}
			numbersTypedSink = v
		}
	})
	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nn.Run(args, l)
			if err != nil {
				b.Fatal(err)
			}
			numbersRawSink = v
		}
	})
}
