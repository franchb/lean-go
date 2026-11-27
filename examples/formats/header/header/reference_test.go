package header

// Hand-written checks: the rules of the original demo in examples/proofs/header,
// restated without the generator.

import (
	"encoding/binary"
	"errors"
	"testing"
)

func init() {
	referenceDecoders = append(referenceDecoders, referenceDecoder{"examples/proofs/header rules", decodeDemo})
}

// decodeDemo restates examples/proofs/header: byte 0 is the version and must
// be 1, byte 1 keeps two flag bits with the six bits above them clear, and
// bytes 2..3 hold the payload length, high byte first.
func decodeDemo(data []byte) (Header, bool) {
	if len(data) < 4 || data[0] != 1 || data[1] >= 4 {
		return Header{}, false
	}
	return Header{Flags: data[1], Length: binary.BigEndian.Uint16(data[2:4])}, true
}

// headerDecision parses a 4-byte header and reports acceptance even when the
// declared payload is missing.
func headerDecision(t *testing.T, wire []byte) (Header, Reason) {
	t.Helper()
	h, _, _, err := Parse(wire)
	if err == nil {
		return h, 0
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("Parse(% x): %v", wire, err)
	}
	if e.Reason == ReasonPayloadTruncated {
		h, _ = decodeDemo(wire)
		return h, 0
	}
	return Header{}, e.Reason
}

func TestDemoExamples(t *testing.T) {
	data := []byte{0x01, 0x03, 0x00, 0x02, 'h', 'i', '!'}
	h, n, payload, err := Parse(data)
	if err != nil || h != (Header{Flags: 3, Length: 2}) || n != 6 || string(payload) != "hi" {
		t.Fatalf("Parse(% x) = %+v, %d, %q, %v", data, h, n, payload, err)
	}
	for _, tc := range []struct {
		wire   []byte
		reason Reason
	}{
		{[]byte{0x02, 0x80, 0x00, 0x00}, ReasonVersionMismatch}, // the version wins over reserved bits
		{[]byte{0x01, 0x04, 0x00, 0x00}, ReasonReservedNonzero},
		{[]byte{0x01, 0x03, 0x12, 0x34}, ReasonPayloadTruncated},
	} {
		_, _, _, err := Parse(tc.wire)
		var e *Error
		if !errors.As(err, &e) || e.Reason != tc.reason {
			t.Fatalf("Parse(% x) = %v, want %v", tc.wire, err, tc.reason)
		}
	}
	if _, err := AppendSerialize(nil, Header{Flags: 4}); err == nil {
		t.Fatal("flags 4 serialized")
	}
}

// Every version and flags byte, as in the original demo's tests.
func TestEveryVersionAndFlagsByte(t *testing.T) {
	for b0 := range 256 {
		for b1 := range 256 {
			wire := []byte{byte(b0), byte(b1), 0x12, 0x34}
			got, reason := headerDecision(t, wire)
			want, ok := decodeDemo(wire)
			if ok != (reason == 0) || got != want {
				t.Fatalf("Parse(% x) = %+v, %v; demo rules give %+v, %v", wire, got, reason, want, ok)
			}
			switch {
			case b0 != 1 && reason != ReasonVersionMismatch,
				b0 == 1 && b1 >= 4 && reason != ReasonReservedNonzero:
				t.Fatalf("Parse(% x) reason %v", wire, reason)
			}
		}
	}
	for flags := range uint8(4) {
		for _, length := range []uint16{0, 1, 255, 256, 4660, 65535} {
			h := Header{Flags: flags, Length: length}
			wire, err := AppendSerialize(nil, h)
			want := []byte{1, flags, byte(length >> 8), byte(length)}
			if err != nil || string(wire) != string(want) {
				t.Fatalf("AppendSerialize(%+v) = % x, %v", h, wire, err)
			}
		}
	}
}
