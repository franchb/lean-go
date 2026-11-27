package main

import (
	"fmt"
	"io"
	"os"

	clock "example.com/proved-clock/clock"
)

func fields(time clock.Hms) (hours, minutes, seconds uint32, err error) {
	var ok [3]bool
	hours, ok[0] = time.Hours()
	minutes, ok[1] = time.Minutes()
	seconds, ok[2] = time.Seconds()
	if !ok[0] || !ok[1] || !ok[2] {
		return 0, 0, 0, clock.ErrMalformed
	}
	return hours, minutes, seconds, nil
}

func demonstrate(output io.Writer) error {
	limits := clock.DefaultLimits()
	for _, total := range []uint32{0, 3661, 86399, ^uint32(0)} {
		time, err := clock.RunSplit(total, limits)
		if err != nil {
			return fmt.Errorf("split %d: %w", total, err)
		}
		hours, minutes, seconds, err := fields(time)
		if err != nil {
			return err
		}
		back, err := clock.RunJoin(time, limits)
		if err != nil {
			return fmt.Errorf("join %d: %w", total, err)
		}
		if _, err := fmt.Fprintf(output, "%d s = %d h %d min %d s = %d s\n", total, hours, minutes, seconds, back); err != nil {
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
