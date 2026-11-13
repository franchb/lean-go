package benchmarks_test

import (
	nq "github.com/franchb/lean-go/generated/quota"
	"testing"
)

type quotaState struct{ capacity, used uint64 }
type quotaOutcome struct {
	state    quotaState
	accepted bool
}

func reserve(s quotaState, amount uint64) quotaOutcome {
	if s.used <= s.capacity && amount <= s.capacity-s.used {
		return quotaOutcome{quotaState{s.capacity, s.used + amount}, true}
	}
	return quotaOutcome{s, false}
}
func release(s quotaState, amount uint64) quotaOutcome {
	if s.used <= s.capacity && amount <= s.used {
		return quotaOutcome{quotaState{s.capacity, s.used - amount}, true}
	}
	return quotaOutcome{s, false}
}
func remaining(s quotaState) uint64 {
	if s.used <= s.capacity {
		return s.capacity - s.used
	}
	return 0
}

var quotaHandSink quotaOutcome
var quotaTypedSink nq.Outcome
var quotaRawSink nq.Value
var uint64Sink uint64

func quotaInputs(t testing.TB) (quotaState, nq.State, nq.Value, nq.Value) {
	t.Helper()
	s := quotaState{1000, 400}
	l := nq.DefaultLimits()
	typed, err := nq.NewState(s.capacity, s.used, l)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := nq.Import(&nq.Input{Type: "Quota.State", Constructor: "Quota.State.mk", Fields: []*nq.Input{{Type: "UInt64", UInt64: s.capacity}, {Type: "UInt64", UInt64: s.used}}}, l)
	if err != nil {
		t.Fatal(err)
	}
	amount, err := nq.Import(&nq.Input{Type: "UInt64", UInt64: 7}, l)
	if err != nil {
		t.Fatal(err)
	}
	return s, typed, raw, amount
}

// Each operation uses a separate loop so dispatch is outside the measurement.
func BenchmarkQuotaReserve(b *testing.B) {
	s, typed, raw, amount := quotaInputs(b)
	l := nq.DefaultLimits()
	args := []nq.Value{raw, amount}
	b.Run("Handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			quotaHandSink = reserve(s, 7)
		}
	})
	b.Run("Typed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nq.RunReserve(typed, 7, l)
			if err != nil {
				b.Fatal(err)
			}
			quotaTypedSink = v
		}
	})
	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nq.RunRoot("Quota.reserve", args, l)
			if err != nil {
				b.Fatal(err)
			}
			quotaRawSink = v
		}
	})
}
func BenchmarkQuotaRelease(b *testing.B) {
	s, typed, raw, amount := quotaInputs(b)
	l := nq.DefaultLimits()
	args := []nq.Value{raw, amount}
	b.Run("Handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			quotaHandSink = release(s, 7)
		}
	})
	b.Run("Typed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nq.RunRelease(typed, 7, l)
			if err != nil {
				b.Fatal(err)
			}
			quotaTypedSink = v
		}
	})
	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nq.RunRoot("Quota.release", args, l)
			if err != nil {
				b.Fatal(err)
			}
			quotaRawSink = v
		}
	})
}
func BenchmarkQuotaRemaining(b *testing.B) {
	s, typed, raw, _ := quotaInputs(b)
	l := nq.DefaultLimits()
	args := []nq.Value{raw}
	b.Run("Handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			uint64Sink = remaining(s)
		}
	})
	b.Run("Typed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nq.RunRemaining(typed, l)
			if err != nil {
				b.Fatal(err)
			}
			uint64Sink = v
		}
	})
	b.Run("Raw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			v, err := nq.RunRoot("Quota.remaining", args, l)
			if err != nil {
				b.Fatal(err)
			}
			quotaRawSink = v
		}
	})
}
