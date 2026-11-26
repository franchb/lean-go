// Package checkedframe reads and encodes Wire16 frames with a CRC-16/XMODEM trailer.
package checkedframe

import (
	"errors"
	"io"

	"example.com/lean-go-crc16-demo/checksum"
	"example.com/lean-go-wire16-demo/frame"
	codec "example.com/lean-go-wire16-demo/wire16"
)

// ErrChecksum reports a complete frame with a mismatched two-byte trailer.
var ErrChecksum = errors.New("frame checksum mismatch")

// Encode returns a four-byte Wire16 header, payload and big-endian CRC over both.
func Encode(flags byte, payload []byte) ([]byte, error) {
	limits := codec.DefaultLimits()
	wire, err := frame.AppendFrame(nil, flags, payload, limits)
	if err != nil {
		return nil, err
	}
	state, err := checksum.Update(0, wire)
	if err != nil {
		return nil, err
	}
	pair, err := codec.RunEncodeBE(state, limits)
	if err != nil {
		return nil, err
	}
	hi, hiOK := pair.Hi()
	lo, loOK := pair.Lo()
	if !hiOK || !loOK {
		return nil, codec.ErrMalformed
	}
	return append(wire, hi, lo), nil
}

// crcReader preserves a terminal read error across the header, payload and trailer.
// This includes an error delivered together with the last payload byte.
type crcReader struct {
	source io.Reader
	err    error
	state  uint16
	inBody bool
}

func (r *crcReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.source.Read(p)
	r.err = err
	if r.inBody && n > 0 {
		state, updateErr := checksum.Update(r.state, p[:n])
		if updateErr != nil {
			r.err = updateErr
			return 0, updateErr
		}
		r.state = state
	}
	return n, err
}

// ReadFrameFrom consumes exactly one frame and returns its owned payload. Clean
// EOF is reported at a frame boundary. A partial trailer returns UnexpectedEOF.
// A checksum failure consumes that frame and returns a zero Frame.
func ReadFrameFrom(src io.Reader) (frame.Frame, error) {
	r := crcReader{source: src, inBody: true}
	limits := codec.DefaultLimits()
	f, err := frame.ReadFrameFrom(&r, limits)
	if err != nil {
		return frame.Frame{}, err
	}
	r.inBody = false
	var trailer [2]byte
	if _, err = io.ReadFull(&r, trailer[:]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return frame.Frame{}, err
	}
	pair, err := codec.NewBytePair(trailer[0], trailer[1], limits)
	if err != nil {
		return frame.Frame{}, err
	}
	got, err := codec.RunDecodeBE(pair, limits)
	if err != nil {
		return frame.Frame{}, err
	}
	if got != r.state {
		return frame.Frame{}, ErrChecksum
	}
	return f, nil
}
