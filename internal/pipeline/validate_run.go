package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The validation runner. After the unchanged export has written ir.json and
// the complete IR has been computed, it writes LeanGo/Module/Validate.lean
// and runs it in its own Lean process, directly (not through `lake env`, so
// that a timeout kills the process that does the work on every OS), with
// Lean's own memory limit (-M). Nothing it does can change the IR, which is
// already final. If the process dies while proving an obligation, the
// progress file names that obligation; it is skipped in a rerun and reported
// failed with the limit it hit. A Lean memory exception that the command
// caught is handled the same way, because the process can stay near its
// limit afterwards and fail later obligations spuriously. A root's
// non-vacuity witnesses are handled the same way: the progress file names the
// root being witnessed, the rerun skips its witnesses, and they are reported
// not attempted with the limit they hit.

const (
	validationHarnessPath = "LeanGo/Module/Validate.lean"
	validationReportName  = "validation.json"
	maxValidationReruns   = 4
	maxProgressBytes      = 4 * 1024 * 1024
	maxAttemptOutput      = 4000
)

// validationRun is everything one validation needs from the export workspace.
type validationRun struct {
	workspace, lake, lean string
	env                   []string
	module                string
	roots                 []string
	ir                    []byte
	compiler              Compiler
	options               ValidationOptions
}

// processOutcome is how one validation process ended.
type processOutcome struct {
	exitCode int // 0 on success, -1 when killed by a signal or unknown
	timedOut bool
	output   string
	err      error // the process could not be started
	elapsed  time.Duration
}

func (o processOutcome) ok() bool { return o.err == nil && !o.timedOut && o.exitCode == 0 }

// runValidationProcess is a variable so tests can stand in for Lean.
var runValidationProcess = runLimitedProcess

func runLimitedProcess(command []string, dir string, env []string, timeout time.Duration) processOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.WaitDelay = 10 * time.Second
	start := time.Now()
	err := cmd.Run()
	outcome := processOutcome{output: universal(output.String()), elapsed: time.Since(start)}
	if ctx.Err() == context.DeadlineExceeded {
		outcome.timedOut, outcome.exitCode = true, -1
		return outcome
	}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		outcome.exitCode = exit.ExitCode()
		if outcome.exitCode < 0 {
			outcome.exitCode = -1
		}
	case err != nil:
		outcome.err, outcome.exitCode = err, -1
	}
	return outcome
}

// leanEnvironment returns the child environment with Lake's LEAN_PATH for
// the workspace, and the Lean executable Lake would run.
func leanEnvironment(lake, lean, workspace string, env []string) ([]string, string, error) {
	text, err := run([]string{lake, "env"}, workspace, env)
	if err != nil {
		return nil, "", err
	}
	leanPath, executable := "", lean
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		switch {
		case !ok:
		case key == "LEAN_PATH":
			leanPath = value
		case key == "LEAN" && filepath.IsAbs(value) && isFile(value):
			executable = value
		}
	}
	if leanPath == "" {
		return nil, "", exportError("lake env did not report LEAN_PATH")
	}
	out := append([]string(nil), env...)
	return append(out, "LEAN_PATH="+leanPath), executable, nil
}

// Kinds of the step a killed process was in (lastStep).
const (
	stepObligation = "obligation"
	stepWitness    = "witness"
)

// lastStep names the step a killed process was in, from the command's
// progress lines: an obligation ("<ms> ms proving F", ended by "<ms> ms F:
// proved in …" or "… failed in …") or a root's witnesses ("<ms> ms
// witnessing R", ended by "<ms> ms R: witnesses <status> in …"). It returns
// empty strings when the last step had ended.
func lastStep(progress string) (kind, name string) {
	lines := strings.Split(strings.TrimRight(progress, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		_, message, ok := strings.Cut(lines[i], " ms ")
		if !ok {
			continue
		}
		if name, ok := strings.CutPrefix(message, "proving "); ok {
			return stepObligation, name
		}
		if name, ok := strings.CutPrefix(message, "witnessing "); ok {
			return stepWitness, name
		}
		if strings.Contains(message, ": proved in ") || strings.Contains(message, ": failed in ") ||
			strings.Contains(message, ": witnesses ") {
			return "", ""
		}
	}
	return "", ""
}

// lastAttempt names the obligation a killed process was proving, or "".
func lastAttempt(progress string) string {
	if kind, name := lastStep(progress); kind == stepObligation {
		return name
	}
	return ""
}

// mentionsMemory recognizes Lean's report of its -M limit.
func mentionsMemory(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "memory")
}

// Where a process was when it ended (killReason).
const (
	duringObligation = "while proving this obligation"
	duringWitness    = "while checking this root's witnesses"
)

// killReason explains why a process ended during one step (during is
// duringObligation or duringWitness).
func killReason(o processOutcome, opts ValidationOptions, during string) killedObligation {
	switch {
	case o.timedOut:
		return killedObligation{fmt.Sprintf("validation process exceeded its wall-clock limit of %s %s", opts.ProcessTimeout, during), ReasonProcessTime}
	case mentionsMemory(o.output):
		return killedObligation{fmt.Sprintf("validation process exceeded its memory limit of %d MiB %s", opts.MemoryMB, during), ReasonMemory}
	case o.exitCode == 137 || o.exitCode == -1:
		return killedObligation{fmt.Sprintf("validation process was killed (exit %d) %s, most likely by the system's memory limit", o.exitCode, during), ReasonMemory}
	}
	return killedObligation{fmt.Sprintf("validation process exited (%d) %s", o.exitCode, during), ReasonProcessFailed}
}

// processFailure describes a process that ended without a report.
func processFailure(o processOutcome, opts ValidationOptions) (string, string) {
	tail := o.output
	if len(tail) > maxAttemptOutput {
		tail = tail[len(tail)-maxAttemptOutput:]
	}
	switch {
	case o.err != nil:
		return "validation process could not run: " + o.err.Error(), ReasonProcessFailed
	case o.timedOut:
		return fmt.Sprintf("validation process exceeded its wall-clock limit of %s", opts.ProcessTimeout), ReasonProcessTime
	case mentionsMemory(o.output) || o.exitCode == 137:
		return fmt.Sprintf("validation process exceeded its memory limit of %d MiB (exit %d): %s", opts.MemoryMB, o.exitCode, tail), ReasonMemory
	}
	return fmt.Sprintf("validation process failed (exit %d): %s", o.exitCode, tail), ReasonProcessFailed
}

// memoryVictim is the first function, in proof order, that the command
// reported failed for memory and that has not been skipped yet.
func memoryVictim(raw *rawValidationReport, killed map[string]killedObligation) string {
	for _, f := range raw.Functions {
		if _, done := killed[f.Name]; !done && f.Status == "failed" && classifyReason(f.Reason) == ReasonMemory {
			return f.Name
		}
	}
	return ""
}

// witnessMemoryVictim is the first root, in report order, whose witnesses the
// command reported not attempted for memory and that has not been skipped yet.
func witnessMemoryVictim(raw *rawValidationReport, killed map[string]killedObligation) string {
	for _, p := range raw.Programs {
		if _, done := killed[p.Root]; !done && p.Witnesses != nil && p.Witnesses.Status == WitnessNotAttempted &&
			classifyReason(p.Witnesses.Reason) == ReasonMemory {
			return p.Root
		}
	}
	return ""
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, exportErrorf("%s exceeds %d bytes", filepath.Base(path), limit)
	}
	return os.ReadFile(path)
}

func sortedNames(m map[string]killedObligation) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validSkipName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\" \t\r\n\\")
}

// runValidation validates the exported program in the export workspace.
func runValidation(r validationRun) (*ValidationResult, error) {
	opts := r.options
	env, lean, err := leanEnvironment(r.lake, r.lean, r.workspace, r.env)
	if err != nil {
		return nil, staged("build", "producer_failed", err)
	}
	harness := filepath.Join(r.workspace, filepath.FromSlash(validationHarnessPath))
	reportPath := filepath.Join(r.workspace, validationReportName)
	progressPath := reportPath + ".progress"
	killed := map[string]killedObligation{}
	killedWitnesses := map[string]killedObligation{}
	var attempts []ValidationAttempt
	var raw *rawValidationReport
	processReason, processClass := "", ""
	for attempt := 0; ; attempt++ {
		skip, skipWitnesses := sortedNames(killed), sortedNames(killedWitnesses)
		if err := os.WriteFile(harness, []byte(validationHarness(r.module, r.roots, opts, skip, skipWitnesses)), 0o644); err != nil {
			return nil, err
		}
		for _, path := range []string{reportPath, progressPath} {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
		}
		command := []string{lean, "-M", strconv.Itoa(opts.MemoryMB), "-DElab.async=false", validationHarnessPath}
		outcome := runValidationProcess(command, r.workspace, env, opts.ProcessTimeout)
		record := ValidationAttempt{Skip: nonNil(skip), SkipWitnesses: nonNil(skipWitnesses), ExitCode: outcome.exitCode, TimedOut: outcome.timedOut, WallMs: outcome.elapsed.Milliseconds()}
		if !outcome.ok() {
			record.Output = outcome.output
			if len(record.Output) > maxAttemptOutput {
				record.Output = record.Output[len(record.Output)-maxAttemptOutput:]
			}
		}
		attempts = append(attempts, record)
		var current *rawValidationReport
		if data, err := readBounded(reportPath, maxValidationReportBytes); err == nil {
			current, err = parseRawReport(data)
			if err != nil {
				return nil, staged("build", "producer_protocol", err)
			}
			if current.Module != r.module || strings.Join(current.Roots, "\x00") != strings.Join(r.roots, "\x00") {
				return nil, staged("build", "producer_protocol", exportError("validation report names a different module or roots"))
			}
		}
		last := attempt >= maxValidationReruns
		if current != nil {
			raw = current
			if last {
				break
			}
			if victim := memoryVictim(current, killed); victim != "" && validSkipName(victim) {
				killed[victim] = killedObligation{fmt.Sprintf("validation process exceeded its memory limit of %d MiB %s", opts.MemoryMB, duringObligation), ReasonMemory}
				continue
			}
			if victim := witnessMemoryVictim(current, killedWitnesses); victim != "" && validSkipName(victim) {
				killedWitnesses[victim] = killedObligation{fmt.Sprintf("validation process exceeded its memory limit of %d MiB %s", opts.MemoryMB, duringWitness), ReasonMemory}
				continue
			}
			break
		}
		progress, _ := readBounded(progressPath, maxProgressBytes)
		kind, victim := lastStep(string(progress))
		target, during := killed, duringObligation
		if kind == stepWitness {
			target, during = killedWitnesses, duringWitness
		}
		_, repeated := target[victim]
		if outcome.ok() || victim == "" || repeated || last || !validSkipName(victim) {
			processReason, processClass = processFailure(outcome, opts)
			if outcome.ok() {
				processReason, processClass = "validation process wrote no report", ReasonProcessFailed
			}
			break
		}
		target[victim] = killReason(outcome, opts, during)
	}
	report, diagnostics := splitReport(raw, reportInputs{
		module: r.module, roots: r.roots, irSHA256: Digest(r.ir), compiler: r.compiler, options: opts,
		library: ValidationInventory(), functions: irFunctionNames(r.ir), killed: killed,
		killedWitnesses: killedWitnesses, attempts: attempts,
		processReason: processReason, processClass: processClass,
	})
	return encodeValidation(report, diagnostics)
}

// irFunctionNames lists the functions of an exported IR program in order.
// Unreadable IR yields nil; the export step has already rejected that.
func irFunctionNames(ir []byte) []string {
	var program struct {
		Functions []struct {
			Name string `json:"name"`
		} `json:"functions"`
	}
	if json.Unmarshal(ir, &program) != nil {
		return nil
	}
	names := make([]string, 0, len(program.Functions))
	for _, f := range program.Functions {
		names = append(names, f.Name)
	}
	return names
}

func encodeValidation(report *ValidationReport, diagnostics *ValidationDiagnostics) (*ValidationResult, error) {
	reportJSON, err := encodeReport(report)
	if err != nil {
		return nil, err
	}
	diagnosticsJSON, err := encodeReport(diagnostics)
	if err != nil {
		return nil, err
	}
	return &ValidationResult{Report: report, Diagnostics: diagnostics, ReportJSON: reportJSON, DiagnosticsJSON: diagnosticsJSON}, nil
}
