package main

import (
	"bytes"
	"hash/adler32"
	"math/rand"
	"testing"

	x "example.com/lean-go-adler32-demo/adler32"
)

func TestChecksum(t *testing.T) {
	l := x.DefaultLimits()
	initial, err := x.RunInitial(l)
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 8193)
	rand.New(rand.NewSource(1950)).Read(random)
	for _, data := range [][]byte{nil, {}, []byte("Wikipedia"), bytes.Repeat([]byte{255}, 257),
		bytes.Repeat([]byte{255}, 65521), random} {
		for _, width := range []int{255, 256, 257, len(data) + 1} {
			s := initial
			h := adler32.New()
			for pos := 0; pos < len(data); pos += width {
				part := data[pos:min(pos+width, len(data))]
				s, err = update(s, part)
				if err != nil {
					t.Fatal(err)
				}
				h.Write(part)
				got, err := x.RunChecksum(s, l)
				if err != nil || got != h.Sum32() {
					t.Fatalf("length %d, offset %d: %08x, %v; want %08x", len(data), pos, got, err, h.Sum32())
				}
			}
			s, err = x.RunUpdate(s, nil, l)
			if err != nil {
				t.Fatal(err)
			}
			got, err := x.RunChecksum(s, l)
			if err != nil || got != adler32.Checksum(data) {
				t.Fatalf("length %d: %08x, %v", len(data), got, err)
			}
		}
	}
}

func rawArgs(t *testing.T, low, high uint32, data []byte) []x.Value {
	t.Helper()
	state := &x.Input{Type: "Adler32.State", Constructor: "Adler32.State.mk", Fields: []*x.Input{
		{Type: "UInt32", UInt32: low}, {Type: "UInt32", UInt32: high}}}
	list := &x.Input{Type: "List UInt8", Constructor: "List.nil"}
	for i := len(data) - 1; i >= 0; i-- {
		list = &x.Input{Type: "List UInt8", Constructor: "List.cons", Fields: []*x.Input{{Type: "UInt8", UInt8: data[i]}, list}}
	}
	args := make([]x.Value, 2)
	for i, input := range []*x.Input{state, list} {
		v, err := x.Import(input, x.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		args[i] = v
	}
	return args
}

func TestRecordLimits(t *testing.T) {
	// For every budget, find the smallest passing raw call and check both sides
	// of that boundary through the slice API. Include the malformed zero state.
	for _, data := range [][]byte{nil, {255}, {0, 1, 127, 255}} {
		const low, high = 65520, 65519
		state, err := x.NewState(low, high, x.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		args := rawArgs(t, low, high, data)
		fields := []func(*x.Limits) *uint64{
			func(l *x.Limits) *uint64 { return &l.MaxNodes }, func(l *x.Limits) *uint64 { return &l.MaxDepth },
			func(l *x.Limits) *uint64 { return &l.MaxOutputNodes }, func(l *x.Limits) *uint64 { return &l.MaxAllocations },
			func(l *x.Limits) *uint64 { return &l.MaxSteps }, func(l *x.Limits) *uint64 { return &l.MaxFrames },
			func(l *x.Limits) *uint64 { return &l.MaxSlots },
		}
		check := func(l x.Limits) error {
			t.Helper()
			raw, rawErr := x.RunRoot("Adler32.update", args, l)
			got, err := x.RunUpdate(state, data, l)
			if err != rawErr {
				t.Fatalf("len %d, %+v: typed %v, raw %v", len(data), l, err, rawErr)
			}
			a, aOK := got.Low()
			b, bOK := got.High()
			if err == nil {
				fs := raw.Fields()
				ra, okA := fs[0].UInt32()
				rb, okB := fs[1].UInt32()
				if !aOK || !bOK || !okA || !okB || a != ra || b != rb {
					t.Fatal("state mismatch")
				}
			} else if aOK || bOK {
				t.Fatal("partial state on error")
			}
			return err
		}
		for _, field := range fields {
			l := x.DefaultLimits()
			lo, hi := uint64(1), *field(&l)
			if check(l) != nil {
				t.Fatal("default limits failed")
			}
			for lo < hi {
				mid := lo + (hi-lo)/2
				*field(&l) = mid
				if check(l) == nil {
					hi = mid
				} else {
					lo = mid + 1
				}
			}
			*field(&l) = lo
			if check(l) != nil {
				t.Fatal("first passing budget failed")
			}
			*field(&l) = lo - 1
			if check(l) == nil {
				t.Fatal("budget below minimum succeeded")
			}
			*field(&l) = 0
			if check(l) != x.ErrLimits {
				t.Fatal("invalid budget accepted")
			}
		}
		if a, _ := state.Low(); a != low {
			t.Fatal("input state changed")
		}
		if b, _ := state.High(); b != high {
			t.Fatal("input state changed")
		}
	}
	l := x.DefaultLimits()
	l.MaxNodes = 1
	if _, err := x.RunUpdate(x.State{}, []byte{0}, l); err != x.ErrMalformed {
		t.Fatal(err)
	}
	l.MaxNodes = 0
	if _, err := x.RunUpdate(x.State{}, nil, l); err != x.ErrLimits {
		t.Fatal(err)
	}
}
