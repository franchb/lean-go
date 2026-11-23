package frame_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"example.com/lean-go-wire16-demo/frame"
	codec "example.com/lean-go-wire16-demo/wire16"
)

func ExampleReadFrame() {
	input := []byte{1, 3, 0, 2, 'G', 'o', 1, 0, 0, 0}
	for len(input) > 0 {
		f, rest, err := frame.ReadFrame(input, codec.DefaultLimits())
		if err != nil {
			panic(err)
		}
		flags, _ := f.Header.Flags()
		fmt.Printf("flags=%d payload=%q\n", flags, f.Payload)
		input = rest
	}
	// Output:
	// flags=3 payload="Go"
	// flags=0 payload=""
}

func TestIncompleteConsumesNothing(t *testing.T) {
	packet := []byte{1, 2, 0, 3, 0xff, 0x80, 0}
	for n := 0; n < len(packet); n++ {
		checkFailure(t, packet[:n], codec.DefaultLimits(), io.ErrUnexpectedEOF)
	}
	checkFailure(t, nil, codec.DefaultLimits(), io.ErrUnexpectedEOF)
	checkFailure(t, []byte{1, 3, 0xff, 0xff}, codec.DefaultLimits(), io.ErrUnexpectedEOF)
}

func TestRejectedHeaderAndLimitsConsumeNothing(t *testing.T) {
	l := codec.DefaultLimits()
	checkFailure(t, []byte{2, 0x80, 0xff, 0xff}, l, frame.ErrUnsupportedVersion)
	checkFailure(t, []byte{1, 4, 0xff, 0xff}, l, frame.ErrReservedFlags)
	input := []byte{1, 0, 0, 0}
	checkFailure(t, input, codec.Limits{}, codec.ErrLimits)
	l.MaxSteps = 1
	checkFailure(t, input, l, codec.ErrResource)
}

func checkFailure(t *testing.T, input []byte, limits codec.Limits, want error) {
	t.Helper()
	before := bytes.Clone(input)
	f, rest, err := frame.ReadFrame(input, limits)
	if !errors.Is(err, want) || f.Payload != nil || f.Header != (codec.Header{}) {
		t.Fatalf("input %x: got frame %+v, error %v; want %v", input, f, err, want)
	}
	if (rest == nil) != (input == nil) || len(rest) != len(input) || cap(rest) != cap(input) ||
		!bytes.Equal(input, before) || (cap(rest) > 0 && &rest[:1][0] != &input[:1][0]) {
		t.Fatalf("input %x was changed or consumed on error", before)
	}
}

func TestMaximumPayloadAndRemainder(t *testing.T) {
	payload := bytes.Repeat([]byte{0xab}, 65535)
	packet := append([]byte{1, 3, 0xff, 0xff}, payload...)
	input := append(packet, 1, 0, 0, 0)
	f, rest, err := frame.ReadFrame(input, codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	length, ok := f.Header.Length()
	if !ok || length != 65535 || !bytes.Equal(payload, f.Payload) || cap(f.Payload) != len(f.Payload) {
		t.Fatal("incorrect header or payload view")
	}
	if len(rest) != 4 || &rest[0] != &input[len(packet)] || &f.Payload[0] != &input[4] {
		t.Fatal("payload or remainder does not refer to the input")
	}
	extended := append(f.Payload, 0xfe)
	if extended[len(payload)] != 0xfe || rest[0] != 1 {
		t.Fatal("appending to the payload affected the following frame")
	}
	empty, tail, err := frame.ReadFrame(rest, codec.DefaultLimits())
	if err != nil || len(empty.Payload) != 0 || cap(empty.Payload) != 0 || len(tail) != 0 {
		t.Fatalf("empty second frame: %+v, rest %x, err %v", empty, tail, err)
	}
}
