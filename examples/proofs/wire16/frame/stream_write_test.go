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

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestWriteFrameToWireBytes(t *testing.T) {
	var stream bytes.Buffer
	limits := codec.DefaultLimits()
	payloads := [][]byte{nil, []byte("Go"), bytes.Repeat([]byte{0xc5}, math.MaxUint16)}
	for i, payload := range payloads {
		start := stream.Len()
		n, err := frame.WriteFrameTo(&stream, uint8(i), payload, limits)
		want := expectedFrame(uint8(i), payload)
		if err != nil || n != len(want) || !bytes.Equal(stream.Bytes()[start:], want) {
			t.Fatalf("payload length %d: wrote %d bytes, err=%v, want %d wire bytes", len(payload), n, err, len(want))
		}
	}
	for i, want := range payloads {
		got, err := frame.ReadFrameFrom(&stream, limits)
		if err != nil || !bytes.Equal(got.Payload, want) {
			t.Fatalf("frame %d: payload length %d, err=%v", i, len(got.Payload), err)
		}
	}
	if _, err := frame.ReadFrameFrom(&stream, limits); err != io.EOF {
		t.Fatalf("after last frame: %v", err)
	}
}

func TestWriteFrameToReportsAcceptedPrefix(t *testing.T) {
	payload := []byte("Go")
	wire := expectedFrame(3, payload)
	transportErr := errors.New("transport failed")
	for accepted := 0; accepted <= len(wire); accepted++ {
		for _, writeErr := range []error{nil, transportErr} {
			var dst bytes.Buffer
			calls := 0
			w := writerFunc(func(p []byte) (int, error) {
				calls++
				n, _ := dst.Write(p[:min(accepted, len(p))])
				return n, writeErr
			})
			n, err := frame.WriteFrameTo(w, 3, payload, codec.DefaultLimits())
			wantErr := writeErr
			if accepted < len(wire) && wantErr == nil {
				wantErr = io.ErrShortWrite
			}
			if n != accepted || err != wantErr || calls != 1 || !bytes.Equal(dst.Bytes(), wire[:accepted]) {
				t.Fatalf("writer returned (%d, %v): got (%d, %v), %d calls, wire %x", accepted, writeErr, n, err, calls, dst.Bytes())
			}
		}
	}
}

func TestWriteFrameToValidatesBeforeWriting(t *testing.T) {
	limits := codec.DefaultLimits()
	tiny := limits
	tiny.MaxSteps = 1
	for _, tc := range []struct {
		name   string
		flags  uint8
		size   int
		limits codec.Limits
		err    error
	}{
		{"oversize before flags", 4, 1 << 16, limits, frame.ErrPayloadTooLarge},
		{"reserved flags", 4, 1, limits, frame.ErrReservedFlags},
		{"invalid limits", 1, 1, codec.Limits{}, codec.ErrLimits},
		{"resource limit", 1, 1, tiny, codec.ErrResource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := writerFunc(func([]byte) (int, error) {
				t.Fatal("invalid frame reached the writer")
				return 0, nil
			})
			n, err := frame.WriteFrameTo(w, tc.flags, make([]byte, tc.size), tc.limits)
			if n != 0 || err != tc.err {
				t.Fatalf("got (%d, %v), want (0, %v)", n, err, tc.err)
			}
		})
	}
}

func ExampleWriteFrameTo() {
	var stream bytes.Buffer
	limits := codec.DefaultLimits()
	n, err := frame.WriteFrameTo(&stream, 3, []byte("Go"), limits)
	if err != nil {
		panic(err)
	}
	f, err := frame.ReadFrameFrom(&stream, limits)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%d wire bytes, payload %q\n", n, f.Payload)
	// Output: 6 wire bytes, payload "Go"
}
