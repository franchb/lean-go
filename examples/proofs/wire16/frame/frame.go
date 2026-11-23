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

// Frame contains a validated header and the declared payload.
type Frame struct {
	Header  codec.Header
	Payload []byte
}

const headerSize = 4

func parseHeader(data [headerSize]byte, limits codec.Limits) (codec.Header, error) {
	wire, err := codec.NewByteQuadFromBytes(data, limits)
	if err != nil {
		return codec.Header{}, err
	}
	result, err := codec.RunParse(wire, limits)
	if err != nil {
		return codec.Header{}, err
	}
	switch {
	case result.IsUnsupportedVersion():
		return codec.Header{}, ErrUnsupportedVersion
	case result.IsReservedFlags():
		return codec.Header{}, ErrReservedFlags
	}
	header, ok := result.AsParsed()
	if !ok {
		return codec.Header{}, codec.ErrMalformed
	}
	return header, nil
}

// payloadCount returns the declared size on complete, or the deficit on needMore.
func payloadCount(header codec.Header, available uint32, limits codec.Limits) (count uint32, complete bool, err error) {
	result, err := codec.RunClassify(header, available, limits)
	if err != nil {
		return 0, false, err
	}
	if n, ok := result.AsComplete(); ok {
		return n, true, nil
	}
	if n, ok := result.AsNeedMore(); ok {
		return n, false, nil
	}
	return 0, false, codec.ErrMalformed
}

// ReadFrame reads the first four-byte header and its declared payload.
// On success the payload and rest alias src. The payload's capacity is limited
// to its length, so appending to it cannot overwrite the next frame.
// On any error it returns a zero Frame and the original src. An incomplete
// header or payload returns io.ErrUnexpectedEOF. A complete invalid header
// returns the protocol error even when subsequent bytes are missing.
func ReadFrame(src []byte, limits codec.Limits) (Frame, []byte, error) {
	if len(src) < headerSize {
		return Frame{}, src, io.ErrUnexpectedEOF
	}
	header, err := parseHeader([headerSize]byte(src), limits)
	if err != nil {
		return Frame{}, src, err
	}
	// The largest declared payload is 65,535 bytes. Saturate before narrowing len.
	available := uint32(min(len(src)-headerSize, math.MaxUint16))
	count, complete, err := payloadCount(header, available, limits)
	if err != nil {
		return Frame{}, src, err
	}
	if !complete {
		return Frame{}, src, io.ErrUnexpectedEOF
	}
	end := headerSize + int(count)
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
