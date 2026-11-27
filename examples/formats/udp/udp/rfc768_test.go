package udp

// Hand-written checks against RFC 768, written from the RFC's "Format"
// section without the generator.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func init() {
	referenceDecoders = append(referenceDecoders, referenceDecoder{"RFC 768", decodeRFC768})
}

// decodeRFC768 reads the four 16-bit header fields in network byte order:
//
//	 0      7 8     15 16    23 24    31
//	+--------+--------+--------+--------+
//	|     Source      |   Destination   |
//	|      Port       |      Port       |
//	+--------+--------+--------+--------+
//	|     Length      |    Checksum     |
//	+--------+--------+--------+--------+
//
// "Length is the length in octets of this user datagram including this
// header and the data. (This means the minimum value of the length is
// eight.)"
func decodeRFC768(data []byte) (Header, bool) {
	if len(data) < 8 {
		return Header{}, false
	}
	h := Header{
		SrcPort:  binary.BigEndian.Uint16(data[0:2]),
		DstPort:  binary.BigEndian.Uint16(data[2:4]),
		Length:   binary.BigEndian.Uint16(data[4:6]),
		Checksum: binary.BigEndian.Uint16(data[6:8]),
	}
	return h, h.Length >= 8
}

// A DNS query for example.com from port 50000 to port 53: 8 header bytes
// and 29 bytes of DNS message.
var query = []byte{
	0xc3, 0x50, 0x00, 0x35, 0x00, 0x25, 0x1d, 0x3b,
	0xab, 0xcd, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00,
	0x00, 0x01, 0x00, 0x01,
}

func TestDatagram(t *testing.T) {
	data := append(append([]byte(nil), query...), 0xff, 0xff) // trailing bytes stay unread
	h, n, payload, err := Parse(data)
	want := Header{SrcPort: 50000, DstPort: 53, Length: 37, Checksum: 0x1d3b}
	if err != nil || h != want || n != len(query) || !bytes.Equal(payload, query[8:]) {
		t.Fatalf("Parse = %+v, %d, % x, %v", h, n, payload, err)
	}
	frame, err := AppendFrame(nil, h, payload)
	if err != nil || !bytes.Equal(frame, query) {
		t.Fatalf("AppendFrame = % x, %v", frame, err)
	}
}

func TestLengthBelowEight(t *testing.T) {
	for length := range uint16(8) {
		data := []byte{0, 1, 0, 2, 0, byte(length), 0, 0}
		_, _, _, err := Parse(data)
		var e *Error
		if !errors.As(err, &e) || e.Reason != ReasonLengthTooSmall {
			t.Fatalf("length %d: %v", length, err)
		}
		if err := Validate(Header{Length: length}); err == nil {
			t.Fatalf("length %d validated", length)
		}
	}
	// Exactly eight: a datagram without data.
	h, n, payload, err := Parse([]byte{0, 1, 0, 2, 0, 8, 0, 0})
	if err != nil || h.Length != 8 || n != 8 || len(payload) != 0 {
		t.Fatalf("empty datagram: %+v, %d, %v", h, n, err)
	}
}

func TestShortDatagram(t *testing.T) {
	_, n, _, err := Parse(query[:20])
	var e *Error
	if !errors.As(err, &e) || e.Reason != ReasonPayloadTruncated || e.Missing != uint64(len(query)-20) || n != 0 {
		t.Fatalf("Parse(short) = %d, %v", n, err)
	}
}
