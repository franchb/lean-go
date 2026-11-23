// Package frame reads and writes complete frames using the generated version-1 header codec.
package frame

import (
	"errors"
	"io"
	"math"
	"slices"

	codec "example.com/lean-go-wire16-demo/wire16"
)

var (
	ErrPayloadTooLarge    = errors.New("frame payload exceeds 65535 bytes")
	ErrUnsupportedVersion = errors.New("unsupported frame version")
	ErrReservedFlags      = errors.New("reserved frame flags are set")
)

// Frame contains the parsed header and a view of its payload in the input buffer.
type Frame struct {
	Header  codec.Header
	Payload []byte
}

// ReadFrame reads the first four-byte header and its declared payload.
// On success the payload and rest alias src. The payload's capacity is limited
// to its length, so appending to it cannot overwrite the next frame.
// On any error it returns a zero Frame and the original src. An incomplete
// header or payload returns io.ErrUnexpectedEOF. A complete invalid header
// returns the protocol error even when subsequent bytes are missing.
func ReadFrame(src []byte, limits codec.Limits) (Frame, []byte, error) {
	const headerSize = 4
	if len(src) < headerSize {
		return Frame{}, src, io.ErrUnexpectedEOF
	}
	wire, err := codec.NewByteQuadFromBytes([headerSize]byte(src), limits)
	if err != nil {
		return Frame{}, src, err
	}
	result, err := codec.RunParse(wire, limits)
	if err != nil {
		return Frame{}, src, err
	}
	switch {
	case result.IsUnsupportedVersion():
		return Frame{}, src, ErrUnsupportedVersion
	case result.IsReservedFlags():
		return Frame{}, src, ErrReservedFlags
	}
	header, ok := result.AsParsed()
	if !ok {
		return Frame{}, src, codec.ErrMalformed
	}
	length, ok := header.Length()
	if !ok {
		return Frame{}, src, codec.ErrMalformed
	}
	end := headerSize + int(length)
	if len(src) < end {
		return Frame{}, src, io.ErrUnexpectedEOF
	}
	return Frame{Header: header, Payload: src[headerSize:end:end]}, src[end:], nil
}

// AppendFrame appends a version-1 header and payload to dst. The payload may
// overlap dst, including its unused capacity. On error it returns the original
// dst without modifying its backing array. Lengths above 65,535 are rejected
// before encoding the header.
func AppendFrame(dst []byte, flags uint8, payload []byte, limits codec.Limits) ([]byte, error) {
	if len(payload) > math.MaxUint16 {
		return dst, ErrPayloadTooLarge
	}
	header, err := codec.NewHeader(flags, uint16(len(payload)), limits)
	if err != nil {
		return dst, err
	}
	result, err := codec.RunEncode(header, limits)
	if err != nil {
		return dst, err
	}
	if result.IsReservedFlags() {
		return dst, ErrReservedFlags
	}
	encoded, ok := result.AsEncoded()
	if !ok {
		return dst, codec.ErrMalformed
	}
	wire, err := encoded.Bytes()
	if err != nil {
		return dst, err
	}

	start := len(dst)
	size := len(wire) + len(payload)
	dst = slices.Grow(dst, size)[:start+size]
	// Move the payload first: its source may occupy the future header bytes.
	copy(dst[start+len(wire):], payload)
	copy(dst[start:], wire[:])
	return dst, nil
}
