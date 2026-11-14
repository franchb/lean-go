package main

import (
	"fmt"
	"io"
	"os"

	codec "example.com/lean-go-bytes-demo/bytes"
)

func octets(q codec.ByteQuad) ([4]byte, error) {
	var out [4]byte
	for i, field := range []func() (uint8, bool){q.B0, q.B1, q.B2, q.B3} {
		value, ok := field()
		if !ok {
			return [4]byte{}, fmt.Errorf("read byte %d: invalid ByteQuad", i)
		}
		out[i] = value
	}
	return out, nil
}

func demonstrate(output io.Writer) error {
	limits := codec.DefaultLimits()
	const word uint32 = 0x12345678
	le, err := codec.RunEncodeLE(word, limits)
	if err != nil {
		return fmt.Errorf("encode little endian: %w", err)
	}
	be, err := codec.RunEncodeBE(word, limits)
	if err != nil {
		return fmt.Errorf("encode big endian: %w", err)
	}
	leWord, err := codec.RunDecodeLE(le, limits)
	if err != nil {
		return fmt.Errorf("decode little endian: %w", err)
	}
	beWord, err := codec.RunDecodeBE(be, limits)
	if err != nil {
		return fmt.Errorf("decode big endian: %w", err)
	}
	leBytes, err := octets(le)
	if err != nil {
		return err
	}
	beBytes, err := octets(be)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "LE(%#08x) = [% x] -> %#08x\nBE(%#08x) = [% x] -> %#08x\n",
		word, leBytes, leWord, word, beBytes, beWord)
	return err
}

func main() {
	if err := demonstrate(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
