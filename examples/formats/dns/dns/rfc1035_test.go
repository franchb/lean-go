package dns

// Hand-written checks against RFC 1035 section 4.1.1, written from the RFC's
// header diagram without the generator.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func init() {
	referenceDecoders = append(referenceDecoders, referenceDecoder{"RFC 1035 4.1.1", decodeRFC1035})
}

// decodeRFC1035 reads the 12-byte header:
//
//	                                1  1  1  1  1  1
//	  0  1  2  3  4  5  6  7  8  9  0  1  2  3  4  5
//	+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
//	|                      ID                       |
//	+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
//	|QR|   Opcode  |AA|TC|RD|RA|   Z    |   RCODE   |
//	+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
//	|                    QDCOUNT                    |
//	|                    ANCOUNT                    |
//	|                    NSCOUNT                    |
//	|                    ARCOUNT                    |
//	+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
//
// Bit 0 of the diagram is the most significant. "Z: Reserved for future use.
// Must be zero in all queries and responses."
func decodeRFC1035(data []byte) (Header, bool) {
	if len(data) < 12 {
		return Header{}, false
	}
	flags := binary.BigEndian.Uint16(data[2:4])
	if flags&0x0070 != 0 {
		return Header{}, false
	}
	return Header{
		Id:      binary.BigEndian.Uint16(data[0:2]),
		Qr:      uint8(flags >> 15),
		Opcode:  uint8(flags>>11) & 0xf,
		Aa:      uint8(flags>>10) & 1,
		Tc:      uint8(flags>>9) & 1,
		Rd:      uint8(flags>>8) & 1,
		Ra:      uint8(flags>>7) & 1,
		Rcode:   uint8(flags) & 0xf,
		Qdcount: binary.BigEndian.Uint16(data[4:6]),
		Ancount: binary.BigEndian.Uint16(data[6:8]),
		Nscount: binary.BigEndian.Uint16(data[8:10]),
		Arcount: binary.BigEndian.Uint16(data[10:12]),
	}, true
}

func TestQueryAndResponse(t *testing.T) {
	for _, tc := range []struct {
		wire []byte
		want Header
	}{
		// A recursive query with one question.
		{[]byte{0xab, 0xcd, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}, Header{Id: 0xabcd, Rd: 1, Qdcount: 1}},
		// Its answer: QR, RD and RA set, one question and two answers.
		{[]byte{0xab, 0xcd, 0x81, 0x80, 0, 1, 0, 2, 0, 0, 0, 0}, Header{Id: 0xabcd, Qr: 1, Rd: 1, Ra: 1, Qdcount: 1, Ancount: 2}},
		// NXDOMAIN (RCODE 3) from an authoritative server.
		{[]byte{0x00, 0x07, 0x84, 0x03, 0, 1, 0, 0, 0, 1, 0, 0}, Header{Id: 7, Qr: 1, Aa: 1, Rcode: 3, Qdcount: 1, Nscount: 1}},
	} {
		h, n, err := Parse(append(tc.wire, 0xee))
		if err != nil || h != tc.want || n != 12 {
			t.Fatalf("Parse(% x) = %+v, %d, %v; want %+v", tc.wire, h, n, err, tc.want)
		}
		wire, err := AppendSerialize(nil, h)
		if err != nil || !bytes.Equal(wire, tc.wire) {
			t.Fatalf("AppendSerialize(%+v) = % x, %v", h, wire, err)
		}
	}
}

// RFC 1035 requires Z to be zero. Later RFCs reuse two of its bits (AD and
// CD, RFC 4035), so this strict reading rejects DNSSEC-era headers that set them.
func TestZMustBeZero(t *testing.T) {
	for z := 1; z < 8; z++ {
		wire := []byte{0, 1, 0x81, byte(0x80 | z<<4), 0, 0, 0, 0, 0, 0, 0, 0}
		_, _, err := Parse(wire)
		var e *Error
		if !errors.As(err, &e) || e.Reason != ReasonZNonzero {
			t.Fatalf("Z=%d: %v", z, err)
		}
	}
}

func TestFieldWidths(t *testing.T) {
	for _, tc := range []struct {
		h      Header
		reason Reason
	}{
		{Header{Qr: 2}, ReasonQrTooWide},
		{Header{Opcode: 16}, ReasonOpcodeTooWide},
		{Header{Rcode: 16}, ReasonRcodeTooWide},
	} {
		var e *Error
		if err := Validate(tc.h); !errors.As(err, &e) || e.Reason != tc.reason {
			t.Fatalf("Validate(%+v) = %v, want %v", tc.h, err, tc.reason)
		}
	}
}
