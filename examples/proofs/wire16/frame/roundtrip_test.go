package frame_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"

	"example.com/lean-go-wire16-demo/frame"
	codec "example.com/lean-go-wire16-demo/wire16"
)

func ExampleAppendFrame() {
	l := codec.DefaultLimits()
	buf, err := frame.AppendFrame(nil, 3, []byte("Go"), l)
	if err != nil {
		panic(err)
	}
	buf, err = frame.AppendFrame(buf, 0, nil, l)
	if err != nil {
		panic(err)
	}
	f, rest, err := frame.ReadFrame(buf, l)
	if err != nil {
		panic(err)
	}
	fmt.Printf("payload=%q next=% x\n", f.Payload, rest)
	// Output: payload="Go" next=01 00 00 00
}

// checkChunks presents three successive prefixes of the same byte stream. The
// reader may advance only after the complete next frame fits in a prefix.
func checkChunks(t *testing.T, payloads [2][]byte, flags [2]byte, cut1, cut2 uint32) {
	t.Helper()
	limits := codec.DefaultLimits()
	var stream []byte
	for i, payload := range payloads {
		start := len(stream)
		var err error
		stream, err = frame.AppendFrame(stream, flags[i], payload, limits)
		if err != nil || !bytes.Equal(stream[start:], expectedFrame(flags[i], payload)) {
			t.Fatalf("frame %d encoded incorrectly: %v", i, err)
		}
	}
	size := uint32(len(stream) + 1)
	a, b := int(cut1%size), int(cut2%size)
	consumed, index := 0, 0
	for _, end := range []int{min(a, b), max(a, b), len(stream)} {
		for index < len(payloads) {
			src := stream[consumed:end]
			frameLen := 4 + len(payloads[index])
			if len(src) < frameLen {
				checkFailure(t, src, limits, io.ErrUnexpectedEOF)
				break
			}
			f, rest, err := frame.ReadFrame(src, limits)
			if err != nil {
				t.Fatalf("complete frame %d: %v", index, err)
			}
			gotFlags, flagsOK := f.Header.Flags()
			length, lengthOK := f.Header.Length()
			if !flagsOK || gotFlags != flags[index] || !lengthOK || int(length) != len(payloads[index]) ||
				!bytes.Equal(f.Payload, payloads[index]) || !bytes.Equal(rest, src[frameLen:]) {
				t.Fatalf("frame %d decoded incorrectly at byte %d", index, end)
			}
			consumed += frameLen
			index++
		}
	}
	if index != len(payloads) || consumed != len(stream) {
		t.Fatalf("read %d frames and consumed %d of %d bytes", index, consumed, len(stream))
	}
}

func TestEveryShortFrameSplit(t *testing.T) {
	payloads := [2][]byte{[]byte{0x00, 0xff, 0x80}, []byte("Go")}
	size := uint32(8 + len(payloads[0]) + len(payloads[1]))
	for first := uint32(0); first <= size; first++ {
		for second := first; second <= size; second++ {
			checkChunks(t, payloads, [2]byte{3, 1}, first, second)
		}
	}
}

func FuzzFrameRoundTrip(f *testing.F) {
	f.Add([]byte("Go"), []byte{}, byte(3), uint32(3), uint32(5))
	f.Add([]byte{}, []byte{0x00, 0xff}, byte(4), uint32(4), uint32(9))
	f.Add(bytes.Repeat([]byte{0x80}, math.MaxUint16), []byte{1}, byte(15), uint32(65538), uint32(65539))
	f.Add(make([]byte, math.MaxUint16+1), []byte{}, byte(7), uint32(1), uint32(3))
	f.Fuzz(func(t *testing.T, first, second []byte, masks byte, a, b uint32) {
		payloads := [2][]byte{first, second}
		flags := [2]byte{masks & 3, (masks >> 2) & 3}
		for i, payload := range payloads {
			if len(payload) > math.MaxUint16 {
				got, err := frame.AppendFrame(nil, flags[i], payload, codec.DefaultLimits())
				if !errors.Is(err, frame.ErrPayloadTooLarge) || got != nil {
					t.Fatalf("oversize payload of %d bytes: len=%d, err=%v", len(payload), len(got), err)
				}
				return
			}
		}
		checkChunks(t, payloads, flags, a, b)
	})
}
