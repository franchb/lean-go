package frame_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"example.com/lean-go-wire16-demo/frame"
	codec "example.com/lean-go-wire16-demo/wire16"
)

// expectedFrame encodes the wire specification independently of the generated codec.
func expectedFrame(flags byte, payload []byte) []byte {
	wire := binary.BigEndian.AppendUint16([]byte{1, flags}, uint16(len(payload)))
	return append(wire, payload...)
}

func TestAppendFrameBoundaries(t *testing.T) {
	for _, length := range []int{0, 1, 255, 256, math.MaxUint16} {
		payload := bytes.Repeat([]byte{0xa7}, length)
		for flags := byte(0); flags < 4; flags++ {
			got, err := frame.AppendFrame([]byte{0x55}, flags, payload, codec.DefaultLimits())
			want := append([]byte{0x55}, expectedFrame(flags, payload)...)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("length %d flags %d: len=%d err=%v", length, flags, len(got), err)
			}
		}
	}
}

func TestAppendFrameRejectsWithoutWriting(t *testing.T) {
	limits := codec.DefaultLimits()
	tiny := limits
	tiny.MaxSteps = 1
	for _, tc := range []struct {
		name   string
		flags  byte
		length int
		limits codec.Limits
		want   error
	}{
		{"oversize", 3, 65536, limits, frame.ErrPayloadTooLarge},
		{"reserved flags", 4, 1, limits, frame.ErrReservedFlags},
		{"invalid limits", 0, 1, codec.Limits{}, codec.ErrLimits},
		{"resource budget", 0, 1, tiny, codec.ErrResource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backing := bytes.Repeat([]byte{0xad}, 20)
			before := bytes.Clone(backing)
			for _, dst := range [][]byte{nil, backing[:0], backing[:2]} {
				got, err := frame.AppendFrame(dst, tc.flags, make([]byte, tc.length), tc.limits)
				if !errors.Is(err, tc.want) || (got == nil) != (dst == nil) ||
					len(got) != len(dst) || cap(got) != cap(dst) ||
					(cap(got) != 0 && &got[:1][0] != &dst[:1][0]) || !bytes.Equal(backing, before) {
					t.Fatalf("destination changed or wrong error: got %v, want %v", err, tc.want)
				}
			}
		})
	}
}

func TestAppendFrameOverlappingPayload(t *testing.T) {
	for _, tc := range []struct{ capacity, from, to int }{
		{32, 0, 4}, {32, 2, 11}, {32, 4, 13}, {32, 9, 15}, {4, 4, 13},
	} {
		buf := []byte("abcdefghijklmnopqrstuvwxyz012345")
		dst, payload := buf[:4:tc.capacity], buf[tc.from:tc.to]
		want := append(bytes.Clone(dst), expectedFrame(1, payload)...)
		got, err := frame.AppendFrame(dst, 1, payload, codec.DefaultLimits())
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("cap %d source [%d:%d]: %x, %v", tc.capacity, tc.from, tc.to, got, err)
		}
	}
}
