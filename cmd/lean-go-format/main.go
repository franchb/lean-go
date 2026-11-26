// Command lean-go-format generates Lean definitions, proofs and a Go []byte
// adapter from a JSON wire-format spec. See docs/wire-formats.md.
//
// It is kept for compatibility: it is the same code as "lean-go format".
package main

import (
	"fmt"
	"os"

	"github.com/franchb/lean-go/internal/cli"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lean-go-format:", err)
		os.Exit(1)
	}
}

func run(args []string) error { return cli.FormatRun(args, os.Stdout) }
