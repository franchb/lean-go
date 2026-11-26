// Package cli implements the lean-go subcommands on top of internal/pipeline.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/franchb/lean-go/internal/pipeline"
	"github.com/franchb/lean-go/internal/wireformat"
)

// Version is the release this checkout prepares.
const Version = "0.1.0a15"

const mainUsage = `usage: lean-go <command> [options]

Generate pure Go from a supported subset of ordinary Lean 4.

commands:
  generate      Lean sources -> IR -> Go package, from a fresh private workspace
  emit          IR file -> Go package
  check-proofs  freshly check source proofs and bind evidence to generated Go
  format        check or generate a wire format from a JSON spec
  regenerate    contributor command: regenerate every tracked artifact
  doctor        check Lean, Lake and the producer cache
  version       print the tool version and identity

Run 'lean-go <command> --help' for the options of one command.
`

// Commands lists the subcommand names Main dispatches.
var Commands = []string{"generate", "emit", "check-proofs", "format", "regenerate", "doctor", "version", "help"}

// IsCommand reports whether name is a subcommand.
func IsCommand(name string) bool {
	return contains(Commands, name) || name == "--version" || name == "--help" || name == "-h"
}

// Main runs one command and returns its exit status.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, mainUsage)
		return 2
	}
	command, rest := args[0], args[1:]
	switch command {
	case "help", "--help", "-h":
		fmt.Fprint(stdout, mainUsage)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, versionLine())
		return 0
	case "generate":
		return generate(rest, stdout, stderr)
	case "emit":
		return emit(rest, stdout, stderr)
	case "check-proofs":
		return checkProofs(rest, stdout, stderr)
	case "format":
		return Format(rest, stdout, stderr)
	case "regenerate":
		return regenerate(rest, stdout, stderr)
	case "doctor":
		return doctor(rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "lean-go: unknown command %q\n%s", command, mainUsage)
	return 2
}

func versionLine() string {
	version := "lean-go " + Version
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version += " (" + info.Main.Version + ")"
	}
	return fmt.Sprintf("%s %s pipeline %s", version, runtime.Version(), pipeline.SourceDigest()[:12])
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "-help" {
			return true
		}
	}
	return false
}

func usageFailure(stderr io.Writer, command, summary string, options []option, err error) int {
	fmt.Fprint(stderr, usageText(command, summary, options))
	fmt.Fprintf(stderr, "lean-go %s: error: %v\n", command, err)
	return 2
}

func cacheFor(noCache bool) *pipeline.Cache {
	if noCache {
		return nil
	}
	return pipeline.DefaultCache()
}

var generateOptions = []option{
	{name: "source-dir", values: 1, meta: "DIR", help: "directory of ordinary .lean sources"},
	{name: "module", values: 1, meta: "NAME", help: "module to extract"},
	{name: "root", values: 1, repeat: true, meta: "NAME", help: "fully qualified root; repeat to share types"},
	{name: "out", values: 1, meta: "DIR", help: "Go package output directory"},
	{name: "ir-output", values: 1, meta: "FILE", help: "separate producer IR output file"},
	{name: "diagnostics", values: 1, choices: []string{"text", "json"}, meta: "text|json", help: "failure report format (default text)"},
	{name: "mode", values: 1, choices: []string{"metered", "native"}, meta: "metered|native", help: "Go emission mode (default native; metered adds resource budgets)"},
	{name: "replace", help: "replace an existing differing generated package after success"},
	{name: "no-cache", help: "build the producer from scratch instead of using the cache"},
	{name: "strict-go", help: "refuse to run unless lean-go was built with the pinned Go release"},
}

const generateSummary = "Generate fresh pure Go from Lean source in a private, fresh Lake workspace."

func generate(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		fmt.Fprint(stdout, usageText("generate", generateSummary, generateOptions))
		return 0
	}
	p, err := parse(args, generateOptions)
	if err != nil {
		return usageFailure(stderr, "generate", generateSummary, generateOptions, err)
	}
	mode := p.one("diagnostics")
	if mode == "" {
		mode = "text"
	}
	// A JSON failure report must stay the only stderr document, so in JSON
	// mode the toolchain warning is printed only after success.
	if warning := pipeline.GoWarning(); warning != "" && mode == "text" && !p.has("strict-go") {
		fmt.Fprintln(stderr, warning)
	}
	result, err := runGenerate(p)
	if err != nil {
		pipeline.EmitDiagnostics(stderr, err, mode)
		return 1
	}
	if result.Warning != "" && mode == "json" {
		fmt.Fprintln(stderr, result.Warning)
	}
	fmt.Fprintln(stdout, result.Summary())
	return 0
}

// generateFunc and checkProofsFunc are variables so tests can observe the
// requests and inject implementation failures.
var checkProofsFunc = pipeline.CheckProofs

var generateFunc = pipeline.Generate

func runGenerate(p *parsed) (result *pipeline.GenerateResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result, err = nil, pipeline.Unexpected(fmt.Errorf("%v", recovered))
		}
	}()
	selected := []bool{p.has("source-dir"), p.has("module"), p.has("root")}
	if !(selected[0] && selected[1] && selected[2]) {
		return nil, errors.New("--source-dir, --module and --root are required and must be supplied together")
	}
	if !p.has("out") || !p.has("ir-output") {
		return nil, errors.New("module generation requires explicit --out and --ir-output paths")
	}
	return generateFunc(pipeline.GenerateOptions{
		SourceDir: p.one("source-dir"), Module: p.one("module"), Roots: p.all("root"),
		Out: p.one("out"), IROutput: p.one("ir-output"),
		Emit:    pipeline.EmitOptions{Mode: p.one("mode")},
		Replace: p.has("replace"), Cache: cacheFor(p.has("no-cache")), StrictGo: p.has("strict-go"),
	})
}

var emitOptions = []option{
	{name: "in", values: 1, meta: "FILE", help: "input extraction IR JSON file"},
	{name: "out", values: 1, meta: "DIR", help: "output Go package directory (existing output must be identical)"},
	{name: "mode", values: 1, choices: []string{"metered", "native"}, meta: "metered|native", help: "Go emission mode (default native; metered adds resource budgets)"},
	{name: "native-max-depth", values: 1, meta: "N", help: "native mode: nested calls allowed along a recursive call chain"},
	{name: "replace", help: "replace an existing differing generated package"},
}

const emitSummary = "Compile validated module IR into a standalone Go package."

func emit(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		fmt.Fprint(stdout, usageText("emit", emitSummary, emitOptions))
		return 0
	}
	p, err := parse(args, emitOptions)
	if err == nil && (p.one("in") == "" || p.one("out") == "") {
		err = errors.New("--in and --out are required")
	}
	depth := 0
	if err == nil && p.has("native-max-depth") {
		_, scanErr := fmt.Sscanf(p.one("native-max-depth"), "%d", &depth)
		if scanErr != nil {
			err = fmt.Errorf("invalid --native-max-depth %q", p.one("native-max-depth"))
		}
	}
	if err != nil {
		return usageFailure(stderr, "emit", emitSummary, emitOptions, err)
	}
	if err := pipeline.EmitFile(p.one("in"), p.one("out"), pipeline.EmitOptions{Mode: p.one("mode"), NativeMaxDepth: depth}, p.has("replace")); err != nil {
		fmt.Fprintln(stderr, "lean-go:", err)
		return 1
	}
	return 0
}

var proofOptions = []option{
	{name: "contract", values: 1, meta: "FILE", help: "proof contract (v1) or composition contract (v2)"},
	{name: "manifest", values: 1, meta: "FILE", help: "generated manifest for a v1 contract"},
	{name: "component-manifest", values: 2, repeat: true, meta: "NAME PATH", help: "bind a v2 component by name; repeat once per component"},
	{name: "output", values: 1, meta: "FILE", help: "write freshly checked evidence"},
	{name: "evidence", values: 1, meta: "FILE", help: "freshly verify existing evidence"},
}

const proofSummary = "Check source-proof contracts freshly and bind evidence to generated Go."

func checkProofs(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		fmt.Fprint(stdout, usageText("check-proofs", proofSummary, proofOptions))
		return 0
	}
	p, err := parse(args, proofOptions)
	if err == nil {
		switch {
		case !p.has("contract"):
			err = errors.New("the following arguments are required: --contract")
		case p.has("manifest") == p.has("component-manifest"):
			err = errors.New("one of the arguments --manifest --component-manifest is required, and they are exclusive")
		case p.has("output") == p.has("evidence"):
			err = errors.New("one of the arguments --output --evidence is required, and they are exclusive")
		}
	}
	if err != nil {
		return usageFailure(stderr, "check-proofs", proofSummary, proofOptions, err)
	}
	summary, err := checkProofsFunc(pipeline.CheckProofsOptions{
		Contract: p.one("contract"), Manifest: p.one("manifest"), ComponentManifests: p.pairs("component-manifest"),
		Output: p.one("output"), Evidence: p.one("evidence"),
	})
	if err != nil {
		fmt.Fprintf(stderr, "proof check failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, summary)
	return 0
}

const formatUsage = `usage:
  lean-go format check -spec FILE
  lean-go format generate -spec FILE -out DIR [-sources-only] [-no-cache]

check validates a spec and prints a summary.
generate writes the Lean runtime, contract, proofs and Go adapter to DIR,
then (unless -sources-only) runs the extractor and the proof checker
in-process.
`

var formatOptions = []option{
	{name: "spec", values: 1, meta: "FILE", help: "format spec JSON file"},
	{name: "out", values: 1, meta: "DIR", help: "output directory"},
	{name: "sources-only", help: "write generated sources without running extraction and the proof check"},
	{name: "no-cache", help: "build the producer from scratch instead of using the cache"},
	{name: "strict-go", help: "refuse to run unless lean-go was built with the pinned Go release"},
	{name: "repo", values: 1, meta: "DIR", help: "ignored; kept for compatibility with lean-go-format"},
}

// Format implements "lean-go format" and the lean-go-format compatibility command.
func Format(args []string, stdout, stderr io.Writer) int {
	if err := FormatRun(args, stdout); err != nil {
		var usage *usageError
		if errors.As(err, &usage) {
			fmt.Fprintf(stderr, "lean-go format: %v\n%s", err, formatUsage)
			return 2
		}
		fmt.Fprintln(stderr, "lean-go format:", err)
		return 1
	}
	return 0
}

// FormatRun is Format returning its error.
func FormatRun(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &usageError{"missing command"}
	}
	command, rest := args[0], args[1:]
	if command != "check" && command != "generate" {
		return &usageError{fmt.Sprintf("unknown command %q", command)}
	}
	p, err := parse(rest, formatOptions)
	if err != nil {
		return err
	}
	if p.one("spec") == "" {
		return &usageError{"missing -spec"}
	}
	f, err := wireformat.Load(p.one("spec"))
	if err != nil {
		return err
	}
	if command == "check" {
		fmt.Fprintf(stdout, "%s: %d-byte header, %d fields, %d parse checks, %d validation checks, %d theorems\n",
			f.Name, f.Size, len(f.Fields), len(f.Parse), len(f.Valid), f.ClaimCount())
		return nil
	}
	if p.one("out") == "" {
		return &usageError{"missing -out"}
	}
	if p.has("sources-only") {
		return wireformat.WriteFiles(p.one("out"), f.Files())
	}
	return wireformat.GenerateAll(f, p.one("out"), wireformat.Options{Cache: cacheFor(p.has("no-cache")), StrictGo: p.has("strict-go")})
}

// repositoryRoot finds the lean-go checkout containing dir.
func repositoryRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for current := dir; ; current = filepath.Dir(current) {
		data, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/franchb/lean-go\n") {
			return current, nil
		}
		if filepath.Dir(current) == current {
			return "", fmt.Errorf("run regenerate inside a lean-go checkout (no github.com/franchb/lean-go go.mod above %s)", dir)
		}
	}
}
