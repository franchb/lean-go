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
	"github.com/franchb/lean-go/internal/nativegen"
)

func run(args []string) error {
	flags := flag.NewFlagSet("lean-go", flag.ContinueOnError)
	input := flags.String("in", "", "input extraction IR JSON file")
	output := flags.String("out", "", "output Go package directory (existing output must be identical)")
	mode := flags.String("mode", "metered", "Go emission mode for module IR: metered or native")
	maxDepth := flags.Int("native-max-depth", 0, "native mode: nested calls allowed along a recursive call chain (0 selects the default)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" || *output == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: lean-go -in FILE -out DIR [-mode metered|native]")
	}
	if *mode != "metered" && *mode != "native" {
		return fmt.Errorf("unknown mode %q: use metered or native", *mode)
	}
	if *maxDepth != 0 && *mode != "native" {
		return fmt.Errorf("-native-max-depth requires -mode native")
	}
	f, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer f.Close()
	files, err := compileMode(f, *mode, *maxDepth)
	if err != nil {
		return err
	}
	return gogen.Publish(*output, files)
}

// The envelope selects a decoder only. That decoder revalidates every original
// byte, including duplicate keys and schema, before anything can be published.
func compile(r io.Reader) (map[string][]byte, error) { return compileMode(r, "metered", 0) }

// compileMode emits module IR as metered or native Go; the pilot IR is metered only.
func compileMode(r io.Reader, mode string, maxDepth int) (map[string][]byte, error) {
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
		if mode == "native" {
			return nil, fmt.Errorf("native mode requires module IR, not %q", envelope.Schema)
		}
		program, err := ir.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return gogen.Generate(program)
	case moduleir.Schema:
		program, err := moduleir.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if mode == "native" {
			return nativegen.Generate(program, nativegen.Options{MaxDepth: maxDepth})
		}
		return modulegen.Generate(program)
	default:
		return nil, fmt.Errorf("unsupported IR schema %q; regenerate the IR with the current lean-go tool (module schema %s)", envelope.Schema, moduleir.Schema)
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lean-go:", err)
		os.Exit(1)
	}
}
