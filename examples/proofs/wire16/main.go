package main

import (
	"fmt"
	"io"
	"os"

	codec "example.com/lean-go-wire16-demo/wire16"
)

func showWord(output io.Writer, n uint16) error {
	limits := codec.DefaultLimits()
	p, err := codec.RunEncodeBE(n, limits)
	if err != nil {
		return err
	}
	hi, okHi := p.Hi()
	lo, okLo := p.Lo()
	if !okHi || !okLo {
		return fmt.Errorf("invalid encoded pair")
	}
	decoded, err := codec.RunDecodeBE(p, limits)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "0x%04x -> [%02x %02x] -> 0x%04x\n", n, hi, lo, decoded)
	return err
}

func showHeader(output io.Writer, wire [4]byte) error {
	limits := codec.DefaultLimits()
	q, err := codec.NewByteQuadFromBytes(wire, limits)
	if err != nil {
		return err
	}
	result, err := codec.RunParse(q, limits)
	if err != nil {
		return err
	}
	switch {
	case result.IsUnsupportedVersion():
		_, err = fmt.Fprintf(output, "[% x] -> unsupported version\n", wire)
	case result.IsReservedFlags():
		_, err = fmt.Fprintf(output, "[% x] -> reserved flags\n", wire)
	default:
		h, ok := result.AsParsed()
		if !ok {
			return fmt.Errorf("invalid parse result")
		}
		flags, okFlags := h.Flags()
		length, okLength := h.Length()
		if !okFlags || !okLength {
			return fmt.Errorf("invalid parsed header")
		}
		encoded, err := codec.RunEncode(h, limits)
		if err != nil {
			return err
		}
		bytes, ok := encoded.AsEncoded()
		if !ok {
			return fmt.Errorf("encoder rejected parsed header")
		}
		again, err := bytes.Bytes()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "[% x] -> flags=%d length=%d -> [% x]\n",
			wire, flags, length, again)
		return err
	}
	return err
}

func demonstrate(output io.Writer) error {
	if err := showWord(output, 0xabcd); err != nil {
		return err
	}
	for _, wire := range [][4]byte{{1, 3, 0x12, 0x34}, {2, 0x80, 0, 0}, {1, 4, 0, 0}} {
		if err := showHeader(output, wire); err != nil {
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
