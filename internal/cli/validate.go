package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/franchb/lean-go/internal/pipeline"
)

// validationOptions are the translation validation options of "generate" and
// of the standalone "validate" command. The two differ in --validate only:
// generate validates on request, validate always does.
func validationOptions(modes []string, help string) []option {
	return []option{
		{name: "validate", values: 1, choices: modes, meta: strings.Join(modes, "|"), help: help},
		{name: "validation-report", values: 1, meta: "FILE", help: "tracked validation report (default: next to the IR, X.ir.json -> X.validation.json)"},
		{name: "validation-diagnostics", values: 1, meta: "FILE", help: "also write untracked timings and full failure messages"},
		{name: "validate-memory-mb", values: 1, meta: "N", help: fmt.Sprintf("memory limit of the validation process in MiB (default %d)", pipeline.DefaultValidateMemoryMB)},
		{name: "validate-heartbeats", values: 1, meta: "N", help: fmt.Sprintf("heartbeats per obligation, in thousands (default %d)", pipeline.DefaultValidateHeartbeats)},
		{name: "validate-timeout-ms", values: 1, meta: "N", help: fmt.Sprintf("wall-clock limit per obligation (default %d)", pipeline.DefaultValidateTimeoutMs)},
		{name: "validate-process-timeout", values: 1, meta: "SECONDS", help: fmt.Sprintf("wall-clock limit of one validation process (default %d)", int(pipeline.DefaultValidateProcessTimeout.Seconds()))},
	}
}

func init() {
	generateOptions = append(generateOptions, validationOptions(pipeline.ValidateModes,
		"translation-validate the export (default off); require fails unless everything is proved")...)
}

func positive(p *parsed, name string) (int, error) {
	if !p.has(name) {
		return 0, nil
	}
	n, err := strconv.Atoi(p.one(name))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("argument --%s: expected a positive integer, got %q", name, p.one(name))
	}
	return n, nil
}

// validationFromFlags returns the requested validation, or nil when it is off.
func validationFromFlags(p *parsed, mode string) (*pipeline.ValidationOptions, error) {
	if p.has("validate") {
		mode = p.one("validate")
	}
	o := &pipeline.ValidationOptions{Mode: mode, Report: p.one("validation-report"), Diagnostics: p.one("validation-diagnostics")}
	var err error
	if o.MemoryMB, err = positive(p, "validate-memory-mb"); err != nil {
		return nil, err
	}
	if o.Heartbeats, err = positive(p, "validate-heartbeats"); err != nil {
		return nil, err
	}
	if o.TimeoutMs, err = positive(p, "validate-timeout-ms"); err != nil {
		return nil, err
	}
	seconds, err := positive(p, "validate-process-timeout")
	if err != nil {
		return nil, err
	}
	o.ProcessTimeout = time.Duration(seconds) * time.Second
	if mode == "" || mode == pipeline.ValidateOff {
		for _, name := range []string{"validation-report", "validation-diagnostics", "validate-memory-mb", "validate-heartbeats", "validate-timeout-ms", "validate-process-timeout"} {
			if p.has(name) {
				return nil, fmt.Errorf("--%s requires --validate report or --validate require", name)
			}
		}
		return nil, nil
	}
	return o, nil
}

func printValidation(stdout io.Writer, result *pipeline.ValidationResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(stdout, "%s; report in %s\n", result.Report.SummaryLine(), result.ReportPath)
	for _, line := range result.Report.Unproved() {
		fmt.Fprintf(stdout, "  %s\n", line)
	}
}

var validateCommandOptions = append([]option{
	{name: "source-dir", values: 1, meta: "DIR", help: "directory of ordinary .lean sources"},
	{name: "module", values: 1, meta: "NAME", help: "module the IR was extracted from"},
	{name: "root", values: 1, repeat: true, meta: "NAME", help: "fully qualified root; repeat for every root of the IR"},
	{name: "ir", values: 1, meta: "FILE", help: "existing IR file; the sources must export to exactly these bytes"},
	{name: "diagnostics", values: 1, choices: []string{"text", "json"}, meta: "text|json", help: "failure report format (default text)"},
	{name: "no-cache", help: "build the producer from scratch instead of using the cache"},
}, validationOptions([]string{pipeline.ValidateReport, pipeline.ValidateRequire},
	"report records what was proved (default); require also fails unless everything is proved")...)

const validateSummary = `Translation-validate an existing IR file against its Lean sources.

The module is exported again in a fresh private workspace and must give the
IR file byte for byte; then #lean_go_validate proves, in a separate Lean
process with a memory cap, that each IR function returns the encoding of its
source function's result whenever it returns (partial correctness). The
report is written next to the IR unless --validation-report is given.`

var validateFunc = pipeline.Validate

func validateCommand(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		fmt.Fprint(stdout, usageText("validate", validateSummary, validateCommandOptions))
		return 0
	}
	p, err := parse(args, validateCommandOptions)
	var validation *pipeline.ValidationOptions
	if err == nil && (!p.has("source-dir") || !p.has("module") || !p.has("root") || !p.has("ir")) {
		err = errors.New("--source-dir, --module, --root and --ir are required")
	}
	if err == nil {
		// Never nil: the command's --validate choices do not include off.
		validation, err = validationFromFlags(p, pipeline.ValidateReport)
	}
	if err != nil {
		return usageFailure(stderr, "validate", validateSummary, validateCommandOptions, err)
	}
	mode := p.one("diagnostics")
	if mode == "" {
		mode = "text"
	}
	result, err := validateFunc(pipeline.ValidateOptions{
		SourceDir: p.one("source-dir"), Module: p.one("module"), Roots: p.all("root"), IR: p.one("ir"),
		Validation: *validation, Cache: cacheFor(p.has("no-cache")),
	})
	if err != nil {
		if result != nil && mode == "text" {
			printValidation(stdout, result)
		}
		pipeline.EmitDiagnostics(stderr, err, mode)
		return 1
	}
	printValidation(stdout, result)
	return 0
}
