package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Validation reports come in two parts. The report proper is deterministic:
// two runs on the same inputs and budgets write identical bytes, so it can be
// tracked next to the IR and a change in proof coverage shows up as a diff.
// It holds statuses, reason classes, statements and axioms, never timings.
// The optional diagnostics part holds what varies between runs: wall-clock
// times, heartbeat counts, the full failure messages and the process attempts.

const (
	ValidationSchema            = "lean-go-validation/v2"
	ValidationDiagnosticsSchema = "lean-go-validation-diagnostics/v1"
	maxValidationReportBytes    = 16 * 1024 * 1024
)

// Report statuses (ValidationReport.Status).
const (
	ValidationCompleted     = "completed"
	ValidationProcessFailed = "process_failed"
)

// Reason classes of unproved functions, roots and runs.
const (
	ReasonStuck            = "stuck"
	ReasonUnsolved         = "unsolved_goals"
	ReasonHeartbeats       = "heartbeat_limit"
	ReasonObligationTime   = "obligation_time_limit"
	ReasonMemory           = "memory_limit"
	ReasonProcessTime      = "process_time_limit"
	ReasonProcessFailed    = "process_failed"
	ReasonNoEncoding       = "no_encoding"
	ReasonNoSpecification  = "no_specification"
	ReasonLoadFailed       = "load_failed"
	ReasonExportFailed     = "export_failed"
	ReasonAxioms           = "non_standard_axioms"
	ReasonProofRejected    = "proof_rejected"
	ReasonSkipped          = "skipped"
	ReasonDeriveFailed     = "derive_failed"
	ReasonOther            = "other"
	reasonMissingFunctions = "missing_functions"
	// ReasonNotReported marks an IR function or a requested root that the
	// validation command's report does not mention.
	ReasonNotReported = "not_reported"
)

// reasonWords renders a reason class for people ("failed: memory limit").
func reasonWords(class string) string { return strings.ReplaceAll(class, "_", " ") }

// classifyReason maps the command's (or the runner's) message to a class.
func classifyReason(text string) string {
	t := strings.ToLower(text)
	switch {
	case text == "":
		return ""
	case strings.HasPrefix(text, "validate_obl: stuck"):
		return ReasonStuck
	case strings.Contains(t, "unsolved goals"):
		return ReasonUnsolved
	case text == "heartbeat budget exhausted" || strings.Contains(t, "maxheartbeats") || strings.Contains(t, "heartbeat"):
		return ReasonHeartbeats
	case text == "wall-clock budget exhausted":
		return ReasonObligationTime
	case strings.Contains(t, "memory"):
		return ReasonMemory
	case strings.HasPrefix(text, "no encoding for"):
		return ReasonNoEncoding
	case strings.HasPrefix(text, "IR not loaded"):
		return ReasonLoadFailed
	case strings.HasPrefix(text, "non-standard axioms"):
		return ReasonAxioms
	case strings.HasPrefix(text, "skipped by the caller"):
		return ReasonSkipped
	case strings.Contains(t, "metavariables") || strings.Contains(t, "sorry") || strings.Contains(t, "(kernel)"):
		return ReasonProofRejected
	case strings.HasPrefix(text, "specification") || strings.HasPrefix(text, "source signature") ||
		strings.HasPrefix(text, "no source declaration") || strings.HasPrefix(text, "root ") ||
		strings.Contains(text, "helper evidence rejected") || strings.HasPrefix(text, "arity mismatch") ||
		strings.HasPrefix(text, "parameter ") || strings.HasPrefix(text, "result: ") ||
		strings.HasPrefix(text, "no inhabitant") || strings.HasPrefix(text, "retained position") ||
		strings.HasPrefix(text, "capture ") || strings.HasPrefix(text, "source lambda") ||
		strings.HasPrefix(text, "no source binder") || strings.HasPrefix(text, "no let-bound") ||
		strings.HasPrefix(text, "several let-bound") || strings.HasPrefix(text, "source owner"):
		return ReasonNoSpecification
	}
	return ReasonOther
}

// The raw report #lean_go_validate writes (validation.json in the workspace).
type rawValidationReport struct {
	Module            string         `json:"module"`
	Roots             []string       `json:"roots"`
	Status            string         `json:"status"`
	Reason            string         `json:"reason"`
	IRFileMatches     *bool          `json:"ir_file_matches"`
	Program           string         `json:"program"`
	SpecificationList string         `json:"specification_list"`
	Encodings         []rawEncoding  `json:"encodings"`
	Functions         []rawFunction  `json:"functions"`
	Programs          []rawProgram   `json:"programs"`
	Summary           map[string]any `json:"summary"`
}

type rawEncoding struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type rawFunction struct {
	Name          string   `json:"name"`
	Params        []string `json:"params"`
	Result        string   `json:"result"`
	Kind          string   `json:"kind"`
	SourceOwner   string   `json:"source_owner"`
	Specification string   `json:"specification"`
	Status        string   `json:"status"`
	Reason        string   `json:"reason"`
	TimeMs        int64    `json:"time_ms"`
	Heartbeats    int64    `json:"heartbeats_used"`
	Theorem       *string  `json:"theorem"`
	Statement     string   `json:"statement"`
	Axioms        []string `json:"axioms"`
}

type rawProgram struct {
	Root              string   `json:"root"`
	Closure           []string `json:"closure"`
	Status            string   `json:"status"`
	Missing           []string `json:"missing"`
	Reason            string   `json:"reason"`
	PrefixedTheorem   string   `json:"prefixed_theorem"`
	Theorem           string   `json:"theorem"`
	Statement         string   `json:"statement"`
	ExplicitTheorem   string   `json:"explicit_theorem"`
	ExplicitStatement string   `json:"explicit_statement"`
	Axioms            []string `json:"axioms"`
	TimeMs            int64    `json:"time_ms"`
}

func parseRawReport(data []byte) (*rawValidationReport, error) {
	if len(data) > maxValidationReportBytes {
		return nil, exportError("validation report exceeds 16 MiB")
	}
	var raw rawValidationReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, exportErrorf("invalid validation report: %v", err)
	}
	if raw.Status == "" || raw.Module == "" {
		return nil, exportError("invalid validation report: missing module or status")
	}
	return &raw, nil
}

// ValidationReport is the tracked, deterministic report.
type ValidationReport struct {
	Schema            string            `json:"schema"`
	Module            string            `json:"module"`
	Roots             []string          `json:"roots"`
	Status            string            `json:"status"`
	ReasonClass       string            `json:"reason_class,omitempty"`
	IRFileMatches     *bool             `json:"ir_file_matches"`
	IRSHA256          string            `json:"ir_sha256"`
	LeanVersion       string            `json:"lean_version"`
	LeanCommit        string            `json:"lean_commit"`
	Budget            ValidationBudget  `json:"budget"`
	Library           map[string]string `json:"library"`
	Program           string            `json:"program,omitempty"`
	SpecificationList string            `json:"specification_list,omitempty"`
	Encodings         []ReportEncoding  `json:"encodings"`
	Functions         []ReportFunction  `json:"functions"`
	Programs          []ReportProgram   `json:"programs"`
	Summary           ReportSummary     `json:"summary"`
}

// ValidationBudget records the limits a report was produced under.
type ValidationBudget struct {
	Heartbeats       int   `json:"heartbeats"`
	TimeoutMs        int   `json:"timeout_ms"`
	MemoryMB         int   `json:"memory_mb"`
	ProcessTimeoutMs int64 `json:"process_timeout_ms"`
}

type ReportEncoding struct {
	Type        string `json:"type"`
	Status      string `json:"status"`
	ReasonClass string `json:"reason_class,omitempty"`
}

type ReportFunction struct {
	Name          string   `json:"name"`
	Params        []string `json:"params"`
	Result        string   `json:"result"`
	Kind          string   `json:"kind"`
	SourceOwner   string   `json:"source_owner"`
	Specification string   `json:"specification"`
	Status        string   `json:"status"`
	ReasonClass   string   `json:"reason_class,omitempty"`
	Theorem       string   `json:"theorem,omitempty"`
	Statement     string   `json:"statement"`
	Axioms        []string `json:"axioms"`
}

type ReportProgram struct {
	Root              string   `json:"root"`
	Closure           []string `json:"closure"`
	Status            string   `json:"status"`
	ReasonClass       string   `json:"reason_class,omitempty"`
	Missing           []string `json:"missing,omitempty"`
	PrefixedTheorem   string   `json:"prefixed_theorem,omitempty"`
	Theorem           string   `json:"theorem,omitempty"`
	Statement         string   `json:"statement,omitempty"`
	ExplicitTheorem   string   `json:"explicit_theorem,omitempty"`
	ExplicitStatement string   `json:"explicit_statement,omitempty"`
	Axioms            []string `json:"axioms,omitempty"`
}

type ReportSummary struct {
	Functions    int `json:"functions"`
	Proved       int `json:"proved"`
	Failed       int `json:"failed"`
	NotAttempted int `json:"not_attempted"`
	Roots        int `json:"roots"`
	RootsProved  int `json:"roots_proved"`
}

// ValidationDiagnostics is the optional, untracked part of a report.
type ValidationDiagnostics struct {
	Schema    string               `json:"schema"`
	Module    string               `json:"module"`
	Roots     []string             `json:"roots"`
	IRSHA256  string               `json:"ir_sha256"`
	Reason    string               `json:"reason,omitempty"`
	TimeMs    int64                `json:"time_ms"`
	Attempts  []ValidationAttempt  `json:"attempts"`
	Encodings []DiagnosticEncoding `json:"encodings"`
	Functions []DiagnosticFunction `json:"functions"`
	Programs  []DiagnosticProgram  `json:"programs"`
}

// ValidationAttempt is one validation process.
type ValidationAttempt struct {
	Skip     []string `json:"skip"`
	ExitCode int      `json:"exit_code"`
	TimedOut bool     `json:"timed_out"`
	WallMs   int64    `json:"wall_ms"`
	Output   string   `json:"output,omitempty"`
}

type DiagnosticEncoding struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type DiagnosticFunction struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
	TimeMs     int64  `json:"time_ms"`
	Heartbeats int64  `json:"heartbeats_used"`
}

type DiagnosticProgram struct {
	Root   string `json:"root"`
	Reason string `json:"reason"`
	TimeMs int64  `json:"time_ms"`
}

// ValidationResult is a finished validation run: both parts, encoded.
type ValidationResult struct {
	Report          *ValidationReport
	Diagnostics     *ValidationDiagnostics
	ReportJSON      []byte
	DiagnosticsJSON []byte
	// ReportPath and DiagnosticsPath are where the parts were written.
	ReportPath, DiagnosticsPath string
}

// Unproved lists, in report order, every function and root that is not proved,
// with its reason ("M.f: failed: memory limit"). A run that did
// not complete or whose IR check failed is listed as well.
func (r *ValidationReport) Unproved() []string {
	var out []string
	if r.Status != ValidationCompleted {
		out = append(out, fmt.Sprintf("validation %s (%s)", r.Status, reasonWords(r.ReasonClass)))
	}
	if r.IRFileMatches == nil || !*r.IRFileMatches {
		if r.Status == ValidationCompleted {
			out = append(out, "the validated program is not the exported IR")
		}
	}
	for _, f := range r.Functions {
		if f.Status != "proved" {
			line := f.Name + ": " + f.Status
			if f.ReasonClass != "" {
				line += ": " + reasonWords(f.ReasonClass)
			}
			out = append(out, line)
		}
	}
	reported := make(map[string]bool, len(r.Programs))
	for _, p := range r.Programs {
		reported[p.Root] = true
		if p.Status != "proved" {
			line := "root " + p.Root + ": " + strings.ReplaceAll(p.Status, "_", " ")
			if len(p.Missing) > 0 {
				line += " (needs " + strings.Join(p.Missing, ", ") + ")"
			} else if p.ReasonClass != "" {
				line += ": " + reasonWords(p.ReasonClass)
			}
			out = append(out, line)
		}
	}
	// A root without a program entry is not established, whatever else the
	// report says: completeness is judged against the roots that were asked
	// for, not against the entries that happen to be present.
	if r.Status == ValidationCompleted {
		for _, root := range r.Roots {
			if !reported[root] {
				out = append(out, "root "+root+": "+reasonWords(ReasonNotReported))
			}
		}
	}
	return out
}

// Complete reports whether every function and every root is proved.
func (r *ValidationReport) Complete() bool { return len(r.Unproved()) == 0 }

// SummaryLine is the one-line result printed by the commands.
func (r *ValidationReport) SummaryLine() string {
	if r.Status != ValidationCompleted {
		return fmt.Sprintf("validation %s (%s)", r.Status, reasonWords(r.ReasonClass))
	}
	return fmt.Sprintf("validation: %d/%d functions proved, %d/%d roots established",
		r.Summary.Proved, r.Summary.Functions, r.Summary.RootsProved, r.Summary.Roots)
}

// ParseValidationReport reads a tracked report.
func ParseValidationReport(data []byte) (*ValidationReport, error) {
	if len(data) > maxValidationReportBytes {
		return nil, exportError("validation report exceeds 16 MiB")
	}
	var r ValidationReport
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return nil, exportErrorf("invalid validation report: %v", err)
	}
	if r.Schema != ValidationSchema {
		return nil, exportErrorf("unsupported validation report schema %q", r.Schema)
	}
	return &r, nil
}

// encodeReport is the canonical encoding of both report parts: indented by
// one space, keys in declaration order (maps sorted), no HTML escaping.
func encodeReport(value any) ([]byte, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", " ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// killedObligation is an obligation the runner skipped after it exhausted a
// process limit, with the message and class reported for it.
type killedObligation struct {
	reason, class string
}

// reportInputs is what the runner adds to the command's raw report.
type reportInputs struct {
	module   string
	roots    []string
	irSHA256 string
	compiler Compiler
	options  ValidationOptions
	library  map[string]string
	// functions names every function of the exported IR; the report must
	// account for each of them.
	functions []string
	killed    map[string]killedObligation
	attempts  []ValidationAttempt
	// processReason is set when no report could be obtained.
	processReason, processClass string
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// splitReport turns the command's raw report (nil when the process produced
// none) into the tracked report and the diagnostics.
func splitReport(raw *rawValidationReport, in reportInputs) (*ValidationReport, *ValidationDiagnostics) {
	report := &ValidationReport{
		Schema: ValidationSchema, Module: in.module, Roots: nonNil(in.roots), IRSHA256: in.irSHA256,
		LeanVersion: in.compiler.Version, LeanCommit: in.compiler.Commit,
		Budget: ValidationBudget{Heartbeats: in.options.Heartbeats, TimeoutMs: in.options.TimeoutMs,
			MemoryMB: in.options.MemoryMB, ProcessTimeoutMs: in.options.ProcessTimeout.Milliseconds()},
		Library:   in.library,
		Encodings: []ReportEncoding{}, Functions: []ReportFunction{}, Programs: []ReportProgram{},
	}
	diagnostics := &ValidationDiagnostics{
		Schema: ValidationDiagnosticsSchema, Module: in.module, Roots: nonNil(in.roots), IRSHA256: in.irSHA256,
		Attempts: in.attempts, Encodings: []DiagnosticEncoding{}, Functions: []DiagnosticFunction{}, Programs: []DiagnosticProgram{},
	}
	if diagnostics.Attempts == nil {
		diagnostics.Attempts = []ValidationAttempt{}
	}
	if raw == nil {
		report.Status = ValidationProcessFailed
		report.ReasonClass = in.processClass
		diagnostics.Reason = in.processReason
		return report, diagnostics
	}
	report.Status = raw.Status
	report.IRFileMatches = raw.IRFileMatches
	report.Program = raw.Program
	report.SpecificationList = raw.SpecificationList
	if raw.Status != ValidationCompleted {
		report.ReasonClass = classifyReason(raw.Reason)
		if raw.Status == "export_failed" {
			report.ReasonClass = ReasonExportFailed
		}
		diagnostics.Reason = raw.Reason
	}
	if t, ok := raw.Summary["time_ms"].(float64); ok {
		diagnostics.TimeMs = int64(t)
	}
	for _, e := range raw.Encodings {
		class := ""
		if e.Status != "available" {
			class = ReasonDeriveFailed
			diagnostics.Encodings = append(diagnostics.Encodings, DiagnosticEncoding{Type: e.Type, Reason: e.Reason})
		}
		report.Encodings = append(report.Encodings, ReportEncoding{Type: e.Type, Status: e.Status, ReasonClass: class})
	}
	for _, f := range raw.Functions {
		status, reason := f.Status, f.Reason
		class := classifyReason(reason)
		if k, ok := in.killed[f.Name]; ok {
			status, reason, class = "failed", k.reason, k.class
		}
		theorem := ""
		if f.Theorem != nil && status == "proved" {
			theorem = *f.Theorem
		}
		axioms := nonNil(f.Axioms)
		sort.Strings(axioms)
		report.Functions = append(report.Functions, ReportFunction{
			Name: f.Name, Params: nonNil(f.Params), Result: f.Result, Kind: f.Kind, SourceOwner: f.SourceOwner,
			Specification: f.Specification, Status: status, ReasonClass: class, Theorem: theorem,
			Statement: f.Statement, Axioms: axioms,
		})
		diagnostics.Functions = append(diagnostics.Functions, DiagnosticFunction{
			Name: f.Name, Status: status, Reason: reason, TimeMs: f.TimeMs, Heartbeats: f.Heartbeats})
		switch status {
		case "proved":
			report.Summary.Proved++
		case "failed":
			report.Summary.Failed++
		default:
			report.Summary.NotAttempted++
		}
	}
	// The command is expected to report every IR function and every root.
	// The runner does not take that on trust: whatever a completed report
	// leaves out is entered as not attempted, so that it counts as unproved.
	if raw.Status == ValidationCompleted {
		reported := make(map[string]bool, len(raw.Functions))
		for _, f := range raw.Functions {
			reported[f.Name] = true
		}
		for _, name := range in.functions {
			if reported[name] {
				continue
			}
			reported[name] = true
			report.Functions = append(report.Functions, ReportFunction{
				Name: name, Params: []string{}, Status: "not_attempted", ReasonClass: ReasonNotReported, Axioms: []string{}})
			diagnostics.Functions = append(diagnostics.Functions, DiagnosticFunction{
				Name: name, Status: "not_attempted", Reason: "the validation command did not report this function"})
			report.Summary.NotAttempted++
		}
	}
	report.Summary.Functions = len(report.Functions)
	for _, p := range raw.Programs {
		program := ReportProgram{Root: p.Root, Closure: nonNil(p.Closure), Status: p.Status}
		switch p.Status {
		case "proved":
			axioms := append([]string(nil), p.Axioms...)
			sort.Strings(axioms)
			program.PrefixedTheorem, program.Theorem, program.Statement = p.PrefixedTheorem, p.Theorem, p.Statement
			program.ExplicitTheorem, program.ExplicitStatement, program.Axioms = p.ExplicitTheorem, p.ExplicitStatement, nonNil(axioms)
			report.Summary.RootsProved++
		case "not_established":
			program.Missing = nonNil(p.Missing)
			program.ReasonClass = reasonMissingFunctions
		default:
			_, detail, _ := strings.Cut(p.Reason, ": ")
			program.ReasonClass = classifyReason(detail)
		}
		report.Programs = append(report.Programs, program)
		diagnostics.Programs = append(diagnostics.Programs, DiagnosticProgram{Root: p.Root, Reason: p.Reason, TimeMs: p.TimeMs})
	}
	if raw.Status == ValidationCompleted {
		reported := make(map[string]bool, len(raw.Programs))
		for _, p := range raw.Programs {
			reported[p.Root] = true
		}
		for _, root := range in.roots {
			if reported[root] {
				continue
			}
			reported[root] = true
			report.Programs = append(report.Programs, ReportProgram{
				Root: root, Closure: []string{}, Status: "not_established", ReasonClass: ReasonNotReported})
			diagnostics.Programs = append(diagnostics.Programs, DiagnosticProgram{
				Root: root, Reason: "the validation command did not report this root"})
		}
	}
	report.Summary.Roots = len(in.roots)
	return report, diagnostics
}
