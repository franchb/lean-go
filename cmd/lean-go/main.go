// Command lean-go compiles validated pilot IR into a standalone normalizer.
// It validates IR metadata, not the current producer source snapshots.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/franchb/lean-go/internal/gogen"
	"github.com/franchb/lean-go/internal/ir"
)

func run(args []string) error {
	flags := flag.NewFlagSet("lean-go", flag.ContinueOnError)
	input := flags.String("in", "", "input pilot IR JSON file")
	output := flags.String("out", "", "output normalizer directory (existing output must be identical)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" || *output == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: lean-go -in FILE -out DIR")
	}
	f, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer f.Close()
	program, err := ir.Decode(f)
	if err != nil {
		return err
	}
	files, err := gogen.Generate(program)
	if err != nil {
		return err
	}
	return gogen.Publish(*output, files)
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lean-go:", err)
		os.Exit(1)
	}
}
