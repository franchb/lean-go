package main

import (
	"fmt"
	"io"
	"os"

	flags "example.com/lean-go-flags-demo/flags"
)

func demonstrate(output io.Writer) error {
	limits := flags.DefaultLimits()
	const initial, mask uint32 = 1, 0x80000002
	set, err := flags.RunSet(initial, mask, limits)
	if err != nil {
		return fmt.Errorf("set flags: %w", err)
	}
	contains, err := flags.RunContains(set, mask, limits)
	if err != nil {
		return fmt.Errorf("contains flags: %w", err)
	}
	clear, err := flags.RunClear(set, 2, limits)
	if err != nil {
		return fmt.Errorf("clear flags: %w", err)
	}
	toggle, err := flags.RunToggle(clear, 0x80000000, limits)
	if err != nil {
		return fmt.Errorf("toggle flags: %w", err)
	}
	_, err = fmt.Fprintf(output, "set(%#08x, %#08x) = %#08x\n"+
		"contains(%#08x, %#08x) = %t\nclear(%#08x, %#08x) = %#08x\n"+
		"toggle(%#08x, %#08x) = %#08x\n",
		initial, mask, set, set, mask, contains, set, uint32(2), clear, clear, uint32(0x80000000), toggle)
	return err
}

func main() {
	if err := demonstrate(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
