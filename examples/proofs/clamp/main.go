package main

import (
	"fmt"
	"io"
	"os"

	clamp "example.com/proved-clamp/clamp"
)

func demonstrate(output io.Writer) error {
	limits := clamp.DefaultLimits()
	const upper uint64 = 42
	for _, value := range []uint64{7, 100, ^uint64(0)} {
		result, err := clamp.RunTyped(value, upper, limits)
		if err != nil {
			return fmt.Errorf("clamp %d to %d: %w", value, upper, err)
		}
		if _, err := fmt.Fprintf(output, "clamp(%d, %d) = %d\n", value, upper, result); err != nil {
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
