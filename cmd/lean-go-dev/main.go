// Command lean-go-dev runs the developer harnesses of a lean-go checkout:
// the generated-program differential campaign, the fixed width and byte
// campaigns, the compiler-compatibility corpus and the proof demos with an
// explicit Lean installation. It is not part of the lean-go command users
// install; run it from a checkout with `go run ./cmd/lean-go-dev`.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/franchb/lean-go/internal/compatibility"
	"github.com/franchb/lean-go/internal/differential"
	"github.com/franchb/lean-go/internal/smoke"
)

const usage = `usage: go run ./cmd/lean-go-dev <command> [options]

Developer harnesses of a lean-go checkout. This command is not part of the
lean-go command users install.

commands:
  differential       generate, compare, shrink and replay small Lean programs
  widths             fixed mixed-width conversion campaign
  bytes              fixed UInt8 campaign
  compatibility      compare a candidate Lean with the production baseline
  demos              regenerate and run every proof demo with one Lean installation
  installed-cli      build and install lean-go, then run it outside the checkout on
                     copies of the proof demos
  release-candidate  source-proof-to-Go walkthrough; prints a JSON receipt

Run 'go run ./cmd/lean-go-dev <command> -h' for the options of one command.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "differential":
		return differentialCommand(args[1:], stdout, stderr)
	case "widths", "bytes":
		return fixedCommand(args[0], args[1:], stdout, stderr)
	case "compatibility":
		return compatibilityCommand(args[1:], stdout, stderr)
	case "demos":
		return demosCommand(args[1:], stdout, stderr)
	case "installed-cli":
		return installedCommand(args[1:], stdout, stderr)
	case "release-candidate":
		return releaseCommand(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "lean-go-dev: unknown command %q\n%s", args[0], usage)
	return 2
}

// bounded parses a decimal integer in lower..upper; "1.0" and "true" are not integers.
type bounded struct {
	lower, upper uint64
	values       *[]uint64
}

func (b bounded) String() string { return "" }

func (b bounded) Set(text string) error {
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil || n < b.lower || n > b.upper {
		return fmt.Errorf("must be an integer in %d..%d", b.lower, b.upper)
	}
	*b.values = append(*b.values, n)
	return nil
}

func last(values []uint64, fallback uint64) uint64 {
	if len(values) == 0 {
		return fallback
	}
	return values[len(values)-1]
}

func newFlags(name, summary string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: go run ./cmd/lean-go-dev %s [options]\n\n%s\n(A developer harness, not part of the installed lean-go command.)\n\noptions:\n", name, summary)
		flags.PrintDefaults()
	}
	return flags
}

func repoFlag(flags *flag.FlagSet) *string {
	return flags.String("repo", "", "lean-go checkout (default: the one containing the working directory)")
}

func repository(flagValue string) (string, error) {
	if flagValue == "" {
		flagValue = "."
	}
	return compatibility.RepositoryRoot(flagValue)
}

// parseDifferential turns the command line into campaign options.
func parseDifferential(args []string, stderr io.Writer) (differential.Options, string, error) {
	flags := newFlags("differential", "Generate small Lean programs from seeds, compare native Lean, the metered Go package\n"+
		"(raw and typed APIs), the same APIs without the fast path (the state machine) and\n"+
		"the native-mode Go package with an independent model,\n"+
		"shrink the first mismatch and save replayable artifacts.", stderr)
	o := differential.DefaultOptions()
	var seeds, widths, depth, cases, attempts, timeout []uint64
	flags.Var(bounded{0, math.MaxUint64, &seeds}, "seed", "repeatable program seed (default: 0 and 1)")
	flags.Var(bounded{8, 64, &widths}, "width", "repeatable unsigned width 8, 16, 32 or 64 (default: all four)")
	flags.Var(bounded{0, 4, &depth}, "depth", "generation depth 0..4 (default 3)")
	flags.Var(bounded{1, differential.MaxInputs, &cases}, "cases", "total input pairs per program, including boundaries (default 32)")
	flags.StringVar(&o.Artifacts, "artifacts", o.Artifacts, "parent for persistent, uniquely named campaign artifacts")
	flags.StringVar(&o.Replay, "replay", "", "replay a saved case using its full AST and inputs")
	flags.BoolVar(&o.Shrink, "shrink", false, "reduce the first semantic mismatch")
	flags.Var(bounded{0, differential.MaxShrinkAttempts, &attempts}, "shrink-attempts", "shrink budget 0..1000 (default 20)")
	flags.Var(bounded{1, 3600, &timeout}, "timeout", "per-command timeout in seconds, 1..3600 (default 300)")
	repo := repoFlag(flags)
	if err := flags.Parse(args); err != nil {
		return o, "", err
	}
	if flags.NArg() != 0 {
		return o, "", fmt.Errorf("unexpected arguments %v", flags.Args())
	}
	if seeds != nil {
		o.Seeds = seeds
	}
	for _, w := range widths {
		if w != 8 && w != 16 && w != 32 && w != 64 {
			return o, "", errors.New("invalid value for -width: must be 8, 16, 32 or 64")
		}
		o.Widths = append(o.Widths, int(w))
	}
	o.Depth = int(last(depth, uint64(o.Depth)))
	o.Cases = int(last(cases, uint64(o.Cases)))
	o.ShrinkAttempts = int(last(attempts, uint64(o.ShrinkAttempts)))
	o.Timeout = time.Duration(last(timeout, uint64(o.Timeout/time.Second))) * time.Second
	return o, *repo, nil
}

func differentialCommand(args []string, stdout, stderr io.Writer) int {
	o, repoValue, err := parseDifferential(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "lean-go-dev differential:", err)
		return 2
	}
	repo, err := repository(repoValue)
	if err == nil {
		var status int
		status, err = differential.NewCampaign(o, repo, stdout).Run()
		if err == nil {
			return status
		}
	}
	fmt.Fprintf(stderr, "differential campaign failed: %v\n", err)
	return 1
}

func fixedCommand(name string, args []string, stdout, stderr io.Writer) int {
	f, _ := differential.FixedCampaign(name)
	flags := newFlags(name, "Compare native Lean, the raw and typed Go APIs and an arithmetic model on the\n"+
		f.Source+" corpus source, and find the smallest successful resource budgets.", stderr)
	artifacts := flags.String("artifacts", "", "new directory for retained evidence (required)")
	var timeout []uint64
	flags.Var(bounded{1, 3600, &timeout}, "timeout", "per-command timeout in seconds, 1..3600 (default 300)")
	repoValue := repoFlag(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *artifacts == "" || flags.NArg() != 0 {
		fmt.Fprintf(stderr, "lean-go-dev %s: --artifacts DIR is required\n", name)
		return 2
	}
	repo, err := repository(*repoValue)
	if err == nil {
		var report *differential.FixedReport
		report, err = differential.RunFixed(f, repo, *artifacts, time.Duration(last(timeout, 300))*time.Second)
		if err == nil {
			text, _ := jsonIndent(report)
			fmt.Fprintln(stdout, text)
			return 0
		}
	}
	fmt.Fprintf(stderr, "%s campaign failed: %v\n", name, err)
	return 1
}

func compatibilityCommand(args []string, stdout, stderr io.Writer) int {
	flags := newFlags("compatibility", "Compare an explicitly installed Lean candidate against the strict production baseline\n"+
		"on the compatibility corpus. Exit 0: compatible (or the baseline passed);\n"+
		"1: a candidate difference; 2: harness precondition or baseline failure.", stderr)
	o := compatibility.Options{}
	flags.StringVar(&o.BaselineBin, "baseline-bin", "", "pinned Lean installation or bin directory (required)")
	flags.StringVar(&o.CandidateBin, "candidate-bin", "", "existing candidate installation or bin directory; no downloads")
	flags.StringVar(&o.Artifacts, "artifacts", "", "new output directory outside the checkout and toolchains (required)")
	flags.StringVar(&o.Case, "case", "", "focus a replay on one named corpus case")
	timeout := flags.Float64("timeout", 300, "per-command process-tree timeout in seconds")
	repoValue := repoFlag(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if o.BaselineBin == "" || o.Artifacts == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "lean-go-dev compatibility: --baseline-bin and --artifacts are required")
		return 2
	}
	if math.IsNaN(*timeout) || math.IsInf(*timeout, 0) || *timeout <= 0 || *timeout > 86400 {
		fmt.Fprintln(stderr, "lean-go-dev compatibility: --timeout must be finite and positive")
		return 2
	}
	o.Timeout = time.Duration(*timeout * float64(time.Second))
	repo, err := repository(*repoValue)
	if err != nil {
		fmt.Fprintf(stderr, "Compatibility harness precondition failed: %v\n", err)
		return 2
	}
	code, report, err := compatibility.Run(o, repo)
	if err != nil {
		fmt.Fprintf(stderr, "Compatibility harness precondition failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "Compatibility report: %s\n", report)
	return code
}

func demosCommand(args []string, stdout, stderr io.Writer) int {
	flags := newFlags("demos", "Regenerate every proof demo with an explicit, supported Lean installation, freshly\n"+
		"check and verify its source proofs and run its Go consumer with only Go on PATH.", stderr)
	leanBin := flags.String("lean-bin", "", "Lean installation or bin directory (required)")
	var timeout []uint64
	flags.Var(bounded{1, 3600, &timeout}, "timeout", "per-command timeout in seconds for Go and native Lean runs (default 600)")
	repoValue := repoFlag(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *leanBin == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "lean-go-dev demos: --lean-bin DIR is required")
		return 2
	}
	repo, err := repository(*repoValue)
	if err == nil {
		err = compatibility.CheckDemos(compatibility.DemoOptions{LeanBin: *leanBin, Repo: repo,
			Timeout: time.Duration(last(timeout, 600)) * time.Second, Stdout: stdout})
	}
	if err != nil {
		fmt.Fprintf(stderr, "Compiler demo verification failed: %v\n", err)
		return 1
	}
	return 0
}

func installedCommand(args []string, stdout, stderr io.Writer) int {
	flags := newFlags("installed-cli", "Build lean-go with go build, install it with go install from a module archive of the\n"+
		"tracked files (as go install of a release tag does), or take an installed binary;\n"+
		"then run each binary from a read-only directory outside the checkout: help and\n"+
		"version without tools, doctor's missing-tool report, generate and check-proofs on a\n"+
		"copied clamp project through relative paths with spaces, a Go-only consumer, and\n"+
		"every standalone proof demo and the checked frame regenerated and compared byte\n"+
		"for byte with the tracked artifacts.", stderr)
	o := smoke.InstallOptions{Stdout: stdout}
	flags.StringVar(&o.CLI, "cli", "", "an installed lean-go binary to check instead of building one")
	flags.StringVar(&o.ExpectTag, "expect-tag", "", "release tag the --cli binary must report (vMAJOR.MINOR.PATCH-alpha.N)")
	flags.BoolVar(&o.WithoutLean, "without-lean", false, "check the binaries, help and missing-tool errors only")
	repoValue := repoFlag(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (o.ExpectTag != "" && o.CLI == "") {
		fmt.Fprintln(stderr, "lean-go-dev installed-cli: unexpected arguments, or --expect-tag without --cli")
		return 2
	}
	repo, err := repository(*repoValue)
	if err == nil {
		o.Repo = repo
		err = smoke.InstalledCLI(o)
	}
	if err != nil {
		fmt.Fprintf(stderr, "installed CLI check failed: %v\n", err)
		return 1
	}
	return 0
}

func releaseCommand(args []string, stdout, stderr io.Writer) int {
	flags := newFlags("release-candidate", "Copy the clamp proof demo into a temporary external project, generate fresh Go,\n"+
		"check and verify its source claims and run a Go-only consumer against an\n"+
		"independent model; print a JSON receipt. A failed stage prints no receipt.", stderr)
	repoValue := repoFlag(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "lean-go-dev release-candidate: unexpected arguments %v\n", flags.Args())
		return 2
	}
	repo, err := repository(*repoValue)
	if err == nil {
		err = smoke.Walkthrough(repo, stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "release walkthrough failed: %v\n", err)
		return 1
	}
	return 0
}

func jsonIndent(v any) (string, error) {
	var b strings.Builder
	if err := compatibility.EncodeJSON(&b, v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
