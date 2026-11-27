package diff

import (
	"math/rand"
	"testing"

	nat "difftest/Crc16/nat"
)

// xmodemTable is an independent, table-driven CRC-16/XMODEM (polynomial
// 0x1021, initial value 0, no reflection, no final XOR) built from the
// polynomial definition rather than from the Lean source.
var xmodemTable = func() (table [256]uint16) {
	for i := range table {
		crc := uint16(i) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}()

func xmodem(crc uint16, data []byte) uint16 {
	for _, b := range data {
		crc = crc<<8 ^ xmodemTable[byte(crc>>8)^b]
	}
	return crc
}

func TestCRC16XModem(t *testing.T) {
	if got := nat.Update(0, []byte("123456789")); got != 0x31c3 {
		t.Fatalf("check value %04x, want 31c3", got)
	}
	r := rand.New(rand.NewSource(16))
	for _, n := range []int{0, 1, 255, 256, 257, 4095, 4096, 65536, 1 << 20} {
		data := make([]byte, n)
		r.Read(data)
		for _, state := range []uint16{0, 1, 0x8000, 0xffff, uint16(r.Uint32())} {
			if got, want := nat.Update(state, data), xmodem(state, data); got != want {
				t.Fatalf("length %d from %04x: %04x, want %04x", n, state, got, want)
			}
		}
	}
	for state := range 1 << 16 {
		for _, b := range []byte{0, 1, 0x80, 0xff} {
			if got, want := nat.UpdateByte(uint16(state), b), xmodem(uint16(state), []byte{b}); got != want {
				t.Fatalf("UpdateByte(%04x, %02x) = %04x, want %04x", state, b, got, want)
			}
		}
	}
}
