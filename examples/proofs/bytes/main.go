package main

import (
	"fmt"
	"io"
	"os"

	codec "example.com/lean-go-bytes-demo/bytes"
)

func demonstrate(output io.Writer) error {
	limits := codec.DefaultLimits()
	const word uint32 = 0x12345678
	for _, order := range []struct {
		name   string
		encode func(uint32, codec.Limits) (codec.ByteQuad, error)
		decode func(codec.ByteQuad, codec.Limits) (uint32, error)
	}{
		{"LE", codec.RunEncodeLE, codec.RunDecodeLE},
		{"BE", codec.RunEncodeBE, codec.RunDecodeBE},
	} {
		encoded, err := order.encode(word, limits)
		if err != nil {
			return fmt.Errorf("encode %s: %w", order.name, err)
		}
		data, err := encoded.Bytes()
		if err != nil {
			return fmt.Errorf("read %s bytes: %w", order.name, err)
		}
		input, err := codec.NewByteQuadFromBytes(data, limits)
		if err != nil {
			return fmt.Errorf("construct %s input: %w", order.name, err)
		}
		decoded, err := order.decode(input, limits)
		if err != nil {
			return fmt.Errorf("decode %s: %w", order.name, err)
		}
		if _, err := fmt.Fprintf(output, "%s(%#08x) = [% x] -> %#08x\n", order.name, word, data, decoded); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := demonstrate(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
