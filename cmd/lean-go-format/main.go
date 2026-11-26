// Command lean-go-format generates Lean definitions, proofs and a Go []byte
// adapter from a JSON wire-format spec. See docs/wire-formats.md.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/franchb/lean-go/internal/wireformat"
)

const usage = `usage:
  lean-go-format check -spec FILE
  lean-go-format generate -spec FILE -out DIR [-sources-only] [-repo DIR]

check validates a spec and prints a summary.
generate writes the Lean runtime, contract, proofs and Go adapter to DIR,
then (unless -sources-only) runs the lean-go extractor and proof checker
from the repository at -repo (default: the current directory).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lean-go-format:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing command\n%s", usage)
	}
	cmd, args := args[0], args[1:]
	flags := flag.NewFlagSet("lean-go-format "+cmd, flag.ContinueOnError)
	specPath := flags.String("spec", "", "format spec JSON file")
	out := flags.String("out", "", "output directory")
	sourcesOnly := flags.Bool("sources-only", false, "write generated sources without running extraction and the proof check")
	repo := flags.String("repo", ".", "lean-go checkout that provides scripts/generate.py and scripts/check_proofs.py")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *specPath == "" || flags.NArg() != 0 {
		return fmt.Errorf("missing -spec\n%s", usage)
	}
	f, err := wireformat.Load(*specPath)
	if err != nil {
		return err
	}
	switch cmd {
	case "check":
		fmt.Printf("%s: %d-byte header, %d fields, %d parse checks, %d validation checks, %d theorems\n",
			f.Name, f.Size, len(f.Fields), len(f.Parse), len(f.Valid), f.ClaimCount())
		return nil
	case "generate":
		if *out == "" {
			return fmt.Errorf("missing -out\n%s", usage)
		}
		if *sourcesOnly {
			return wireformat.WriteFiles(*out, f.Files())
		}
		return wireformat.GenerateAll(*repo, f, *out)
	}
	return fmt.Errorf("unknown command %q\n%s", cmd, usage)
}
