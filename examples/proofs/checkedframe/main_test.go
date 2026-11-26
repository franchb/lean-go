package main_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"example.com/lean-go-checkedframe-demo/checkedframe"
	"example.com/lean-go-wire16-demo/frame"
)

func wireReference(flags byte, payload []byte) []byte {
	wire := binary.BigEndian.AppendUint16([]byte{1, flags}, uint16(len(payload)))
	wire = append(wire, payload...)
	var state uint16
	for _, b := range wire {
		for bit := byte(0x80); bit != 0; bit >>= 1 {
			feedback := (state&0x8000 != 0) != (b&bit != 0)
			state <<= 1
			if feedback {
				state ^= 0x1021
			}
		}
	}
	return binary.BigEndian.AppendUint16(wire, state)
}

func TestEncodingAndConcatenatedReads(t *testing.T) {
	for _, length := range []int{0, 1, 255, 256, 65535} {
		payload := make([]byte, length)
		for i := range payload {
			payload[i] = byte(i)
		}
		want := wireReference(3, payload)
		got, err := checkedframe.Encode(3, payload)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("encoding length %d: %v", length, err)
		}
		r := bytes.NewReader(append(bytes.Clone(want), wireReference(0, nil)...))
		first, err := checkedframe.ReadFrameFrom(r)
		flags, ok := first.Header.Flags()
		if err != nil || !ok || flags != 3 || !bytes.Equal(first.Payload, payload) {
			t.Fatalf("first frame length %d: %v", length, err)
		}
		second, err := checkedframe.ReadFrameFrom(r)
		if err != nil || len(second.Payload) != 0 {
			t.Fatalf("second frame: %v", err)
		}
		if _, err = checkedframe.ReadFrameFrom(r); err != io.EOF {
			t.Fatal(err)
		}
	}
	if _, err := checkedframe.Encode(4, nil); !errors.Is(err, frame.ErrReservedFlags) {
		t.Fatal(err)
	}
	if _, err := checkedframe.Encode(0, make([]byte, 65536)); !errors.Is(err, frame.ErrPayloadTooLarge) {
		t.Fatal(err)
	}
}

func TestTruncationAndCorruption(t *testing.T) {
	wire := wireReference(2, []byte("123456789"))
	for cut := range len(wire) {
		want := error(io.ErrUnexpectedEOF)
		if cut == 0 {
			want = io.EOF
		}
		r := iotest.OneByteReader(bytes.NewReader(wire[:cut]))
		got, err := checkedframe.ReadFrameFrom(r)
		if err != want || got.Payload != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
	}
	for _, offset := range []int{1, 4, len(wire) - 2, len(wire) - 1} {
		bad := bytes.Clone(wire)
		bad[offset] ^= 1
		r := bytes.NewReader(append(bad, wire...))
		got, err := checkedframe.ReadFrameFrom(r)
		if !errors.Is(err, checkedframe.ErrChecksum) || got.Payload != nil {
			t.Fatalf("flip %d: %v", offset, err)
		}
		if _, err = checkedframe.ReadFrameFrom(r); err != nil {
			t.Fatalf("following frame: %v", err)
		}
	}
	bad := bytes.Clone(wire)
	bad[0] = 0
	if _, err := checkedframe.ReadFrameFrom(bytes.NewReader(bad)); !errors.Is(err, frame.ErrUnsupportedVersion) {
		t.Fatal(err)
	}
	f, err := checkedframe.ReadFrameFrom(iotest.OneByteReader(bytes.NewReader(wire)))
	if err != nil || string(f.Payload) != "123456789" {
		t.Fatal(err)
	}
}

type lastError struct {
	data []byte
	err  error
}

func (r *lastError) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		panic("read past terminal error")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestDataWithTerminalError(t *testing.T) {
	wire := wireReference(0, []byte("abc"))
	failure := errors.New("stream stopped")
	for _, cut := range []int{4, len(wire) - 2, len(wire) - 1, len(wire)} {
		_, err := checkedframe.ReadFrameFrom(&lastError{wire[:cut], failure})
		if cut == len(wire) {
			if err != nil {
				t.Fatal(err)
			}
		} else if err != failure {
			t.Fatalf("cut %d: %v", cut, err)
		}
	}
}
