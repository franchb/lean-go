// Command lean-go compiles validated extraction IR into a standalone Go package.
// It validates IR metadata, not the current producer source snapshots.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/franchb/lean-go/internal/gogen"
	"github.com/franchb/lean-go/internal/ir"
	"github.com/franchb/lean-go/internal/modulegen"
	"github.com/franchb/lean-go/internal/moduleir"
)

func run(args []string) error {
	flags := flag.NewFlagSet("lean-go", flag.ContinueOnError)
	input := flags.String("in", "", "input extraction IR JSON file")
	output := flags.String("out", "", "output Go package directory (existing output must be identical)")
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
	files, err := compile(f)
	if err != nil {
		return err
	}
	return gogen.Publish(*output, files)
}

// The envelope selects a decoder only. That decoder revalidates every original
// byte, including duplicate keys and schema, before anything can be published.
func compile(r io.Reader) (map[string][]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, ir.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > ir.MaxBytes {
		return nil, fmt.Errorf("IR exceeds %d bytes", ir.MaxBytes)
	}
	var envelope struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("IR envelope: %w", err)
	}
	switch envelope.Schema {
	case ir.Schema:
		program, err := ir.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return gogen.Generate(program)
	case moduleir.Schema, moduleir.SchemaV3, moduleir.SchemaV4, moduleir.SchemaV5, moduleir.SchemaV6:
		program, err := moduleir.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return modulegen.Generate(program)
	default:
		return nil, fmt.Errorf("unsupported IR schema %q", envelope.Schema)
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lean-go:", err)
		os.Exit(1)
	}
}
