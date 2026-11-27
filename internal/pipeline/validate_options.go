package pipeline

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Translation validation (docs/research/translation-validation.md) runs the
// producer-side #lean_go_validate command over an exported program in its own
// Lean process, after the export is complete, and writes a report. The files
// validate_*.go are the runner; they are not part of the pipeline digest
// recorded in IR inputs, because nothing they do can change the IR.

// Validation modes. "off" runs nothing. "report" writes the report and never
// fails because of an unproved obligation. "require" writes the report and
// then fails unless every function and every root is proved.
const (
	ValidateOff     = "off"
	ValidateReport  = "report"
	ValidateRequire = "require"
)

// validationTargets are the Lake targets the validation harness imports
// besides LeanGo.Module.Export. They do not depend on user sources and are
// part of the cached producer build of a validating run.
var validationTargets = []string{"LeanGo.Validate", "LeanGo.Validate.Command"}

// ValidateModes lists the accepted --validate values.
var ValidateModes = []string{ValidateOff, ValidateReport, ValidateRequire}

// Default validation budgets. Heartbeats are per obligation, in thousands
// (Lean's maxHeartbeats unit; 200000 is Lean's default), the obligation
// timeout is per obligation, wall clock. The memory cap is Lean's own -M
// limit for the whole validation process, in MiB; it is deliberately far
// below the memory of a shared development machine. The process timeout
// bounds one validation process (all obligations of a program).
const (
	DefaultValidateHeartbeats     = 200000
	DefaultValidateTimeoutMs      = 120000
	DefaultValidateMemoryMB       = 4096
	DefaultValidateProcessTimeout = 3600 * time.Second
	// MinValidateMemoryMB is the smallest accepted memory cap: below it Lean
	// cannot even load the library, and every run would be a failure.
	MinValidateMemoryMB = 64
)

// ValidationOptions selects one validation run.
type ValidationOptions struct {
	// Mode is ValidateReport or ValidateRequire.
	Mode string
	// Report is the path of the tracked, deterministic report. Generate
	// derives it from the IR path when empty.
	Report string
	// Diagnostics, when non-empty, receives timings and heartbeat counts.
	Diagnostics string
	// Heartbeats, TimeoutMs and MemoryMB are the budgets; zero selects the default.
	Heartbeats int
	TimeoutMs  int
	MemoryMB   int
	// ProcessTimeout bounds one validation process; zero selects the default.
	ProcessTimeout time.Duration
}

// Normalized returns the options with defaults filled in, or an error for an
// invalid mode or budget. A nil result means validation is off.
func (o *ValidationOptions) Normalized() (*ValidationOptions, error) {
	if o == nil || o.Mode == "" || o.Mode == ValidateOff {
		if o != nil && (o.Report != "" || o.Diagnostics != "") {
			return nil, exportError("validation report paths require --validate report or require")
		}
		return nil, nil
	}
	if o.Mode != ValidateReport && o.Mode != ValidateRequire {
		return nil, exportErrorf("unknown validation mode %q: use off, report or require", o.Mode)
	}
	n := *o
	if n.Heartbeats == 0 {
		n.Heartbeats = DefaultValidateHeartbeats
	}
	if n.TimeoutMs == 0 {
		n.TimeoutMs = DefaultValidateTimeoutMs
	}
	if n.MemoryMB == 0 {
		n.MemoryMB = DefaultValidateMemoryMB
	}
	if n.ProcessTimeout == 0 {
		n.ProcessTimeout = DefaultValidateProcessTimeout
	}
	switch {
	case n.Heartbeats < 0:
		return nil, exportError("validation heartbeats must be positive")
	case n.TimeoutMs < 0:
		return nil, exportError("validation obligation timeout must be positive")
	case n.MemoryMB < MinValidateMemoryMB:
		return nil, exportErrorf("validation memory limit must be at least %d MiB", MinValidateMemoryMB)
	case n.ProcessTimeout < time.Second:
		return nil, exportError("validation process timeout must be at least one second")
	}
	return &n, nil
}

// DefaultReportPath is the report next to an IR file: "x.ir.json" becomes
// "x.validation.json"; any other name gets ".validation.json" appended.
func DefaultReportPath(irPath string) string {
	if strings.HasSuffix(irPath, ".ir.json") && len(filepath.Base(irPath)) > len(".ir.json") {
		return strings.TrimSuffix(irPath, ".ir.json") + ".validation.json"
	}
	return irPath + ".validation.json"
}

// harnessCommand is the validation harness the separate Lean process runs.
// Roots are the sorted export roots; skip names obligations not to attempt.
func validationHarness(module string, roots []string, o ValidationOptions, skip []string) string {
	var b strings.Builder
	b.WriteString("import LeanGo.Module.Export\nimport LeanGo.Validate\nimport LeanGo.Validate.Command\n")
	fmt.Fprintf(&b, "import %s\n", module)
	fmt.Fprintf(&b, "#lean_go_validate \"ir.json\" [%s] \"%s\" report \"validation.json\" heartbeats %d timeout_ms %d",
		strings.Join(roots, ", "), module, o.Heartbeats, o.TimeoutMs)
	if len(skip) > 0 {
		fmt.Fprintf(&b, " skip \"%s\"", strings.Join(skip, " "))
	}
	b.WriteString("\n")
	return b.String()
}
