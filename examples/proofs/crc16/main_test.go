package main

import (
	"bytes"
	"testing"

	"example.com/lean-go-crc16-demo/checksum"
	generated "example.com/lean-go-crc16-demo/crc16"
)

// Feed message bits individually, most significant first, into the polynomial register.
func reference(state uint16, input []byte) uint16 {
	for _, b := range input {
		for i := 0; i < 8; i++ {
			feedback := (state>>15 != 0) != (b&0x80 != 0)
			state <<= 1
			b <<= 1
			if feedback {
				state ^= 0x1021
			}
		}
	}
	return state
}

func update(t *testing.T, state uint16, input []byte) uint16 {
	t.Helper()
	got, err := checksum.Update(state, input)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestXmodemCheckVector(t *testing.T) {
	input := []byte("123456789")
	if got := update(t, 0, input); got != 0x31c3 {
		t.Fatalf("check value = %04x, want 31c3", got)
	}
	if got := update(t, 0, append(input, 0x31, 0xc3)); got != 0 {
		t.Fatalf("codeword residue = %04x, want 0000", got)
	}
}

func TestEveryByteFromBoundaryStates(t *testing.T) {
	for _, state := range []uint16{0, 1, 0x00ff, 0x1021, 0x5555, 0x7fff, 0x8000, 0xaaaa, 0xffff} {
		for b := range 256 {
			input := []byte{byte(b)}
			if got, want := update(t, state, input), reference(state, input); got != want {
				t.Fatalf("state %04x, byte %02x: got %04x, want %04x", state, b, got, want)
			}
		}
	}
}

func TestGeneratedFoldLimits(t *testing.T) {
	data := []byte{128, 0, 255}
	l := generated.DefaultLimits()
	acc, err := generated.Import(&generated.Input{Type: "UInt16", UInt16: 0xa123}, l)
	if err != nil {
		t.Fatal(err)
	}
	list := &generated.Input{Type: "List UInt8", Constructor: "List.nil"}
	for i := len(data) - 1; i >= 0; i-- {
		list = &generated.Input{Type: "List UInt8", Constructor: "List.cons", Fields: []*generated.Input{{Type: "UInt8", UInt8: data[i]}, list}}
	}
	input, err := generated.Import(list, l)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []*uint64{&l.MaxNodes, &l.MaxDepth, &l.MaxOutputNodes, &l.MaxFrames, &l.MaxSlots, &l.MaxSteps, &l.MaxAllocations} {
		ceiling := *limit
		for n := uint64(0); n <= min(ceiling, uint64(400)); n++ {
			*limit = n
			got, e := generated.RunUpdate(0xa123, data, l)
			raw, w := generated.RunRoot("Crc16.update", []generated.Value{acc, input}, l)
			if e != w {
				t.Fatalf("limits %+v: slice %v, raw %v", l, e, w)
			}
			if e == nil {
				want, ok := raw.UInt16()
				if !ok || got != want || got != reference(0xa123, data) {
					t.Fatal(got, want)
				}
			}
		}
		*limit = ceiling
	}
	for _, data := range [][]byte{nil, {}, bytes.Repeat([]byte{0xff}, 65539)} {
		if got := update(t, 0xa123, data); got != reference(0xa123, data) {
			t.Fatal(len(data), got)
		}
	}
}

func TestStreamingSplits(t *testing.T) {
	increasing := make([]byte, 256)
	for i := range increasing {
		increasing[i] = byte(i)
	}
	for _, input := range [][]byte{nil, []byte("123456789"), bytes.Repeat([]byte{0xff}, 13), increasing} {
		for _, seed := range []uint16{0, 0x8001} {
			want := reference(seed, input)
			state := seed
			for i := range input {
				state = update(t, state, input[i:i+1])
			}
			if state != want {
				t.Fatalf("one-byte chunks: got %04x, want %04x", state, want)
			}
			for split := 0; split <= len(input); split++ {
				prefix := update(t, seed, input[:split])
				got := update(t, prefix, input[split:])
				if got != want {
					t.Fatalf("seed %04x, length %d, split %d: got %04x, want %04x", seed, len(input), split, got, want)
				}
			}
		}
	}
}
