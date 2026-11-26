package main

import (
	"bytes"
	"testing"

	"example.com/lean-go-crc16-demo/checksum"
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
