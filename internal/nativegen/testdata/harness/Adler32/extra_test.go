package diff

import (
	"bytes"
	"hash/adler32"
	"math/rand"
	"testing"

	met "difftest/Adler32/met"
	nat "difftest/Adler32/nat"
)

// TestAgainstHashAdler32 checks the native package against the independent
// standard-library implementation, including streaming in arbitrary chunks.
func TestAgainstHashAdler32(t *testing.T) {
	r := rand.New(rand.NewSource(32))
	random := make([]byte, 1<<20+3)
	r.Read(random)
	inputs := [][]byte{nil, {}, []byte("Wikipedia"), bytes.Repeat([]byte{0xff}, 5552), bytes.Repeat([]byte{0xff}, 65521),
		bytes.Repeat([]byte{0xff}, 1<<20), random}
	for _, n := range []int{0, 1, 255, 256, 257, 4095, 4096} {
		inputs = append(inputs, random[:n])
	}
	for _, data := range inputs {
		if got, want := nat.Checksum(nat.Update(nat.Initial(), data)), adler32.Checksum(data); got != want {
			t.Fatalf("length %d: %08x, want %08x", len(data), got, want)
		}
		s, h := nat.Initial(), adler32.New()
		for pos := 0; pos < len(data); {
			n := min(r.Intn(70000), len(data)-pos)
			s = nat.Update(s, data[pos:pos+n])
			h.Write(data[pos : pos+n])
			pos += n
			if nat.Checksum(s) != h.Sum32() {
				t.Fatalf("length %d, offset %d", len(data), pos)
			}
		}
	}
}

// TestBorrowedInput checks the input is neither modified nor needed after
// the call, and that the fold allocates nothing.
func TestBorrowedInput(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3, 0xff}, 1024)
	before := bytes.Clone(data)
	s := nat.Update(nat.Initial(), data)
	if !bytes.Equal(before, data) {
		t.Fatal("input modified")
	}
	clear(data)
	if nat.Checksum(s) != adler32.Checksum(before) {
		t.Fatal("result depends on the input after return")
	}
	var sink nat.State
	if n := testing.AllocsPerRun(100, func() { sink = nat.Update(sink, before) }); n != 0 {
		t.Fatalf("%v allocations per call", n)
	}
}

func meteredUpdate(t *testing.T, s met.State, data []byte) met.State {
	t.Helper()
	l := met.DefaultLimits()
	var err error
	for first := true; first || len(data) > 0; first = false {
		n := min(len(data), 4095)
		if s, err = met.RunUpdate(s, data[:n], l); err != nil {
			t.Fatal(err)
		}
		data = data[n:]
	}
	return s
}

// FuzzAdler32 checks native folds never panic and agree with both the metered
// package and hash/adler32 from any starting state.
func FuzzAdler32(f *testing.F) {
	f.Add([]byte("Wikipedia"), uint32(1), uint32(0))
	f.Add(bytes.Repeat([]byte{0xff}, 5000), uint32(65520), uint32(65520))
	f.Add([]byte{}, uint32(1<<32-1), uint32(1<<32-1))
	f.Fuzz(func(t *testing.T, data []byte, low, high uint32) {
		got := nat.Update(nat.State{Low: low, High: high}, data)
		start, err := met.NewState(low, high, met.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		want := meteredUpdate(t, start, data)
		wl, _ := want.Low()
		wh, _ := want.High()
		if got.Low != wl || got.High != wh {
			t.Fatalf("native %+v, metered %d/%d", got, wl, wh)
		}
		if low == 1 && high == 0 && nat.Checksum(got) != adler32.Checksum(data) {
			t.Fatal("differs from hash/adler32")
		}
	})
}
