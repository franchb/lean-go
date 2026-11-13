package main

import (
	"fmt"
	"io"
	"os"

	words "example.com/lean-go-words-demo/words"
)

func demonstrate(output io.Writer) error {
	limits := words.DefaultLimits()
	const hi, lo uint32 = 0x80000001, 0xffffffff
	wide, err := words.RunWiden(hi, limits)
	if err != nil {
		return fmt.Errorf("widen word: %w", err)
	}
	packed, err := words.RunPack(hi, lo, limits)
	if err != nil {
		return fmt.Errorf("pack words: %w", err)
	}
	high, err := words.RunHigh(packed, limits)
	if err != nil {
		return fmt.Errorf("unpack high word: %w", err)
	}
	low, err := words.RunLow(packed, limits)
	if err != nil {
		return fmt.Errorf("unpack low word: %w", err)
	}
	narrow, err := words.RunNarrow(packed, limits)
	if err != nil {
		return fmt.Errorf("narrow word: %w", err)
	}
	_, err = fmt.Fprintf(output, "widen(%#08x) = %#016x\npack(%#08x, %#08x) = %#016x\n"+
		"high(%#016x) = %#08x\nlow(%#016x) = %#08x\nnarrow(%#016x) = %#08x\n",
		hi, wide, hi, lo, packed, packed, high, packed, low, packed, narrow)
	return err
}

func main() {
	if err := demonstrate(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
