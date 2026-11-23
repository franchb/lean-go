package frame_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"
	"testing/iotest"

	"example.com/lean-go-wire16-demo/frame"
	codec "example.com/lean-go-wire16-demo/wire16"
)

func ExampleReadFrameFrom() {
	r := bytes.NewReader([]byte{1, 3, 0, 2, 'G', 'o', 1, 0, 0, 0})
	for {
		f, err := frame.ReadFrameFrom(r, codec.DefaultLimits())
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		fmt.Printf("payload=%q\n", f.Payload)
	}
	// Output:
	// payload="Go"
	// payload=""
}

func checkStreamError(t *testing.T, r io.Reader, limits codec.Limits, want error) {
	t.Helper()
	f, err := frame.ReadFrameFrom(r, limits)
	if err != want || f.Header != (codec.Header{}) || f.Payload != nil {
		t.Fatalf("got frame %+v, error %v; want zero frame, %v", f, err, want)
	}
}

func TestStreamEveryCut(t *testing.T) {
	payload := []byte{0xff, 0x80, 0}
	packet := expectedFrame(3, payload)
	for _, tc := range []struct {
		name string
		wrap func(io.Reader) io.Reader
	}{
		{"one byte at a time", iotest.OneByteReader},
		{"EOF with final bytes", iotest.DataErrReader},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := codec.DefaultLimits()
			checkStreamError(t, tc.wrap(bytes.NewReader(nil)), limits, io.EOF)
			for cut := 1; cut < len(packet); cut++ {
				t.Run(fmt.Sprintf("cut-%d", cut), func(t *testing.T) {
					checkStreamError(t, tc.wrap(bytes.NewReader(packet[:cut])), limits, io.ErrUnexpectedEOF)
				})
			}
			r := tc.wrap(bytes.NewReader(packet))
			f, err := frame.ReadFrameFrom(r, limits)
			if err != nil || !bytes.Equal(f.Payload, payload) {
				t.Fatalf("complete frame: payload %x, error %v", f.Payload, err)
			}
			checkStreamError(t, r, limits, io.EOF)
		})
	}
}

func TestStreamBoundariesAndOwnership(t *testing.T) {
	first := expectedFrame(1, []byte("Go"))
	empty := expectedFrame(2, nil)
	maxPayload := bytes.Repeat([]byte{0xab}, math.MaxUint16)
	stream := bytes.Join([][]byte{first, empty, expectedFrame(3, maxPayload)}, nil)
	r := bytes.NewReader(stream)
	limits := codec.DefaultLimits()
	var frames []frame.Frame
	remaining := len(stream)
	for i, want := range [][]byte{[]byte("Go"), nil, maxPayload} {
		f, err := frame.ReadFrameFrom(r, limits)
		if err != nil {
			t.Fatal(err)
		}
		length, lengthOK := f.Header.Length()
		flags, flagsOK := f.Header.Flags()
		if !lengthOK || int(length) != len(want) || !flagsOK || flags != byte(i+1) || !bytes.Equal(f.Payload, want) {
			t.Fatalf("frame %d: flags %d, header length %d, payload length %d", i, flags, length, len(f.Payload))
		}
		remaining -= 4 + len(want)
		if r.Len() != remaining {
			t.Fatalf("after frame %d: %d stream bytes remain, want %d", i, r.Len(), remaining)
		}
		frames = append(frames, f)
	}
	checkStreamError(t, r, limits, io.EOF)
	clear(stream)
	frames[2].Payload[0] = 0
	if string(frames[0].Payload) != "Go" || frames[2].Payload[1] != 0xab {
		t.Fatal("payload storage was shared with the stream or another frame")
	}
}

func TestStreamRejectsHeaderBeforePayload(t *testing.T) {
	limits := codec.DefaultLimits()
	tiny := limits
	tiny.MaxSteps = 1
	for _, tc := range []struct {
		name   string
		header []byte
		limits codec.Limits
		want   error
	}{
		{"unsupported version and reserved flags", []byte{2, 0xff, 0xff, 0xff}, limits, frame.ErrUnsupportedVersion},
		{"reserved flags", []byte{1, 0xff, 0xff, 0xff}, limits, frame.ErrReservedFlags},
		{"invalid limits", []byte{1, 0, 0xff, 0xff}, codec.Limits{}, codec.ErrLimits},
		{"resource limit", []byte{1, 0, 0xff, 0xff}, tiny, codec.ErrResource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(append(tc.header, 0xa5))
			checkStreamError(t, r, tc.limits, tc.want)
			if r.Len() != 1 {
				t.Fatalf("expected one unread payload byte, got %d", r.Len())
			}
		})
	}
}

func TestStreamPreservesReaderErrors(t *testing.T) {
	sentinel := errors.New("reader failed")
	packet := expectedFrame(0, []byte("Go"))
	for cut := 0; cut < len(packet); cut++ {
		t.Run(fmt.Sprintf("cut-%d", cut), func(t *testing.T) {
			r := io.MultiReader(bytes.NewReader(packet[:cut]), iotest.ErrReader(sentinel))
			checkStreamError(t, r, codec.DefaultLimits(), sentinel)
		})
	}
}

// finalErrorReader returns err alongside its last bytes, then EOF on the next read.
type finalErrorReader struct {
	data []byte
	err  error
}

func (r *finalErrorReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestStreamErrorWithLastBytes(t *testing.T) {
	limits := codec.DefaultLimits()
	tiny := limits
	tiny.MaxSteps = 1
	packet := expectedFrame(2, []byte("Go"))
	empty := expectedFrame(2, nil)
	sentinel := errors.New("reader failed")
	for _, tc := range []struct {
		name     string
		data     []byte
		reported error
		limits   codec.Limits
		want     error
	}{
		{"partial header", packet[:3], sentinel, limits, sentinel},
		{"complete header", packet[:4], sentinel, limits, sentinel},
		{"complete header at EOF", packet[:4], io.EOF, limits, io.ErrUnexpectedEOF},
		{"invalid header", []byte{2, 0xff, 0, 2}, sentinel, limits, frame.ErrUnsupportedVersion},
		{"invalid limits", packet[:4], sentinel, codec.Limits{}, codec.ErrLimits},
		{"resource limit", packet[:4], sentinel, tiny, codec.ErrResource},
		{"partial payload", packet[:5], sentinel, limits, sentinel},
		{"empty frame", empty, sentinel, limits, nil},
		{"empty frame at EOF", empty, io.EOF, limits, nil},
		{"complete payload", packet, sentinel, limits, nil},
		{"complete payload at EOF", packet, io.EOF, limits, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &finalErrorReader{tc.data, tc.reported}
			if tc.want != nil {
				checkStreamError(t, r, tc.limits, tc.want)
				return
			}
			f, err := frame.ReadFrameFrom(r, tc.limits)
			flags, ok := f.Header.Flags()
			if err != nil || !ok || flags != 2 || !bytes.Equal(f.Payload, tc.data[4:]) {
				t.Fatalf("got frame %+v, error %v", f, err)
			}
		})
	}
}
