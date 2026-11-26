package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DiagnosticsSchema identifies the versioned report envelope; see docs/diagnostics.md.
const DiagnosticsSchema = "lean-go-diagnostics/v1"

var admissionCodes = map[string]bool{"source_audit": true, "unsupported_type": true, "unsupported_signature": true,
	"unsupported_lcnf_value": true, "unsupported_lcnf_control": true}
var diagnosticStages = map[string]bool{"input": true, "toolchain": true, "build": true, "admission": true, "emission": true}
var diagnosticFields = []string{"code", "stage", "message", "declaration", "compiler_form", "location", "hint"}

// Error is a pipeline failure. Without a diagnostic it reports as an input error.
type Error struct {
	message    string
	diagnostic *Object
	cause      error
}

func (e *Error) Error() string { return e.message }
func (e *Error) Unwrap() error { return e.cause }

// Diagnostic returns the structured report item, filling the input default.
func (e *Error) Diagnostic() *Object {
	if e.diagnostic != nil {
		return e.diagnostic
	}
	return newDiagnostic(e.message, "input", "invalid_input", nil, nil, nil, nil)
}

func exportErrorf(format string, args ...any) *Error {
	return &Error{message: fmt.Sprintf(format, args...)}
}

func exportError(message string) *Error { return &Error{message: message} }

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func newDiagnostic(message, stage, code string, declaration, form *string, location any, hint *string) *Object {
	o := NewObject()
	o.Set("code", code)
	o.Set("stage", stage)
	o.Set("message", message)
	o.Set("declaration", nullable(declaration))
	o.Set("compiler_form", nullable(form))
	o.Set("location", location)
	o.Set("hint", nullable(hint))
	return o
}

func str(s string) *string { return &s }

// diagnosticError builds an error that carries exactly one report item.
func diagnosticError(item *Object, cause error) *Error {
	message, _ := item.Values["message"].(string)
	return &Error{message: message, diagnostic: item, cause: cause}
}

// staged enriches an error with a stage and code unless it already carries a
// diagnostic, mirroring the Python stage() context manager.
func staged(stage, code string, err error) error {
	return stagedHint(stage, code, nil, err)
}

func stagedHint(stage, code string, hint *string, err error) error {
	if err == nil {
		return nil
	}
	var pe *Error
	if errors.As(err, &pe) && pe.diagnostic != nil {
		return err
	}
	return &Error{message: err.Error(), diagnostic: newDiagnostic(err.Error(), stage, code, nil, nil, nil, hint), cause: err}
}

// Report returns the diagnostics envelope for any error.
func Report(err error) *Object {
	var item *Object
	var pe *Error
	if errors.As(err, &pe) {
		item = pe.Diagnostic()
	} else {
		item = newDiagnostic(err.Error(), "input", "invalid_input", nil, nil, nil, nil)
	}
	report := NewObject()
	report.Set("schema", DiagnosticsSchema)
	report.Set("success", false)
	report.Set("diagnostics", []any{item})
	return report
}

// Unexpected marks an implementation failure; it is never an admission.
func Unexpected(err error) error {
	return &Error{message: err.Error(), cause: err, diagnostic: newDiagnostic(err.Error(), "build", "pipeline_failed", nil, nil, nil,
		str("The pipeline failed unexpectedly; report this error with the pinned tool versions."))}
}

// EmitDiagnostics writes one report in text or JSON form.
func EmitDiagnostics(w io.Writer, err error, mode string) {
	report := Report(err)
	if mode == "json" {
		data, encodeErr := EncodeCompactOrdered(report)
		if encodeErr != nil {
			fmt.Fprintln(w, err.Error())
			return
		}
		w.Write(append(data, '\n'))
		return
	}
	for _, raw := range report.Values["diagnostics"].([]any) {
		item := raw.(*Object)
		where := ""
		if location, ok := item.Values["location"].(*Object); ok {
			start, _ := location.Values["start"].(*Object)
			where = fmt.Sprintf("%s:%s:%s: ", textOf(location.Values["path"]), textOf(start.Values["line"]), textOf(start.Values["column"]))
		}
		declaration := ""
		if d, ok := item.Values["declaration"].(string); ok && d != "" {
			declaration = " (" + d + ")"
		}
		form := ""
		if f, ok := item.Values["compiler_form"].(string); ok && f != "" {
			form = "; " + f
		}
		fmt.Fprintf(w, "%s%s [%s%s]%s: %s\n", where, textOf(item.Values["stage"]), textOf(item.Values["code"]), form, declaration, textOf(item.Values["message"]))
		if hint, ok := item.Values["hint"].(string); ok && hint != "" {
			fmt.Fprintf(w, "Hint: %s\n", hint)
		}
	}
}

func textOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return string(t)
	case int:
		return strconv.Itoa(t)
	case nil:
		return "None"
	}
	return fmt.Sprint(v)
}

// integer reports whether v is a JSON integer (Python: type(v) is int).
func integer(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok || strings.ContainsAny(string(n), ".eE") {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	return i, err == nil
}

func validPosition(v any, mapped bool) bool {
	o, ok := v.(*Object)
	if !ok {
		return false
	}
	fields := []string{"line", "column"}
	if mapped {
		fields = append(fields, "utf16_column", "byte_offset")
	}
	if !o.HasExactly(fields...) {
		return false
	}
	for _, key := range fields {
		n, ok := integer(o.Values[key])
		minimum := int64(0)
		if key == "line" {
			minimum = 1
		}
		if !ok || n < minimum {
			return false
		}
	}
	return true
}

func validLocation(v any, mapped bool) bool {
	if v == nil {
		return true
	}
	o, ok := v.(*Object)
	pathKey := "module"
	if mapped {
		pathKey = "path"
	}
	if !ok || !o.HasExactly(pathKey, "kind", "declaration", "start", "end") {
		return false
	}
	if _, ok := o.Values[pathKey].(string); !ok {
		return false
	}
	kind, ok := o.Values["kind"].(string)
	if !ok || (kind != "declaration" && kind != "source_owner" && kind != "compiler") {
		return false
	}
	if d := o.Values["declaration"]; d != nil {
		if _, ok := d.(string); !ok {
			return false
		}
	}
	return validPosition(o.Values["start"], mapped) && validPosition(o.Values["end"], mapped)
}

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validDiagnostic(v any, mapped bool) bool {
	item, ok := v.(*Object)
	if !ok || !item.HasExactly(diagnosticFields...) {
		return false
	}
	code, ok := item.Values["code"].(string)
	if !ok || !codePattern.MatchString(code) || strings.HasSuffix(code, "\n") {
		return false
	}
	stage, ok := item.Values["stage"].(string)
	if !ok || !diagnosticStages[stage] {
		return false
	}
	if stage == "admission" && !admissionCodes[code] {
		return false
	}
	if _, ok := item.Values["message"].(string); !ok {
		return false
	}
	for _, key := range []string{"declaration", "compiler_form", "hint"} {
		if value := item.Values[key]; value != nil {
			if _, ok := value.(string); !ok {
				return false
			}
		}
	}
	return validLocation(item.Values["location"], mapped)
}

// ValidReport reports whether data is one complete diagnostics document.
func ValidReport(data []byte) bool {
	value, err := ParseJSON(data)
	if err != nil {
		return false
	}
	o, ok := value.(*Object)
	if !ok || !o.HasExactly("schema", "success", "diagnostics") || o.Values["schema"] != DiagnosticsSchema || o.Values["success"] != false {
		return false
	}
	items, ok := o.Values["diagnostics"].([]any)
	return ok && len(items) == 1 && validDiagnostic(items[0], true)
}

// producerDiagnostic accepts only the exact producer protocol as an admission rejection.
func producerDiagnostic(value any) (*Object, error) {
	o, ok := value.(*Object)
	if !ok || !o.HasExactly("schema", "diagnostic") || o.Values["schema"] != DiagnosticsSchema {
		return nil, errors.New("invalid producer diagnostic envelope")
	}
	item := o.Values["diagnostic"]
	if !validDiagnostic(item, false) {
		return nil, errors.New("invalid producer diagnostic fields or classification")
	}
	d := item.(*Object)
	if _, ok := d.Values["hint"].(string); !ok || d.Values["stage"] != "admission" {
		return nil, errors.New("invalid producer diagnostic fields or classification")
	}
	copy := NewObject()
	for _, k := range d.Keys {
		copy.Set(k, d.Values[k])
	}
	return copy, nil
}

// mappedSource is one staged module: its original relative path and mapping.
type mappedSource struct {
	module   string
	relative string
	mapping  *SourceMap
}

func findSource(sources []mappedSource, module string) *mappedSource {
	for i := range sources {
		if sources[i].module == module {
			return &sources[i]
		}
	}
	return nil
}

func mapLocation(location any, sources []mappedSource) any {
	if location == nil || !validLocation(location, false) {
		return nil
	}
	o := location.(*Object)
	source := findSource(sources, o.Values["module"].(string))
	if source == nil {
		return nil
	}
	positions := map[string]*Object{}
	for _, key := range []string{"start", "end"} {
		raw := o.Values[key].(*Object)
		line, _ := integer(raw.Values["line"])
		column, _ := integer(raw.Values["column"])
		position := source.mapping.Position(line, column)
		if position == nil {
			return nil
		}
		positions[key] = position
	}
	startOffset, _ := integer(positions["start"].Values["byte_offset"])
	endOffset, _ := integer(positions["end"].Values["byte_offset"])
	if endOffset < startOffset {
		return nil
	}
	out := NewObject()
	out.Set("path", source.relative)
	out.Set("kind", o.Values["kind"])
	out.Set("declaration", o.Values["declaration"])
	out.Set("start", positions["start"])
	out.Set("end", positions["end"])
	return out
}

const compilerErrorPrefix = `error(?:\([A-Za-z][A-Za-z0-9_.]*\))?: `

// compilerLocation matches only explicit Lean/Lake position prefixes for a
// path verified against the fresh workspace.
func compilerLocation(message string, sources []mappedSource, workspace string) any {
	for _, source := range sources {
		absolute := strings.TrimRight(workspace, `/\`) + "/" + source.relative
		paths := []string{source.relative, strings.ReplaceAll(source.relative, "/", `\`), absolute, strings.ReplaceAll(absolute, "/", `\`), nativeJoin(workspace, source.relative)}
		for _, path := range paths {
			pattern := regexp.MustCompile(`(?:^|\n)(` + compilerErrorPrefix + `)?` + regexp.QuoteMeta(path) + `:(\d+):(\d+): (` + compilerErrorPrefix + `)?`)
			for _, match := range pattern.FindAllStringSubmatch(message, -1) {
				if match[1] == "" && match[4] == "" {
					continue
				}
				point := func() *Object {
					p := NewObject()
					p.Set("line", json.Number(strings.TrimLeft(match[2], "0")+zeroIfEmpty(match[2])))
					p.Set("column", json.Number(strings.TrimLeft(match[3], "0")+zeroIfEmpty(match[3])))
					return p
				}
				location := NewObject()
				location.Set("module", source.module)
				location.Set("kind", "compiler")
				location.Set("declaration", nil)
				location.Set("start", point())
				location.Set("end", point())
				return mapLocation(location, sources)
			}
		}
	}
	return nil
}

func zeroIfEmpty(digits string) string {
	if strings.TrimLeft(digits, "0") == "" {
		return "0"
	}
	return ""
}

// RequestedMode follows argparse's last explicit --diagnostics value.
func RequestedMode(args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		switch value := args[i]; {
		case value == "--diagnostics=text" || value == "--diagnostics=json":
			return strings.TrimPrefix(value, "--diagnostics=")
		case value == "--diagnostics" && i+1 < len(args) && (args[i+1] == "text" || args[i+1] == "json"):
			return args[i+1]
		}
	}
	return "text"
}

func nativeJoin(workspace, relative string) string {
	return filepath.Join(workspace, filepath.FromSlash(relative))
}
