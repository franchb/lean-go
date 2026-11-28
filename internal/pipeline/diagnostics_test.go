package pipeline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The diagnostics protocol and source mapping, independent of the Lean
// toolchain. The command-boundary cases live in internal/cli.

func decodeReport(t *testing.T, text string) map[string]any {
	t.Helper()
	var report map[string]any
	if err := json.Unmarshal([]byte(text), &report); err != nil {
		t.Fatalf("not one JSON report: %q", text)
	}
	return report
}

func TestUnknownContextIsNullAndJSONIsOneReport(t *testing.T) {
	var b strings.Builder
	EmitDiagnostics(&b, errors.New("failed\nwith detail"), "json")
	report := decodeReport(t, b.String())
	if report["schema"] != DiagnosticsSchema || report["success"] != false {
		t.Fatal(report)
	}
	item := obj(arr(report["diagnostics"])[0])
	if item["stage"] != "input" || item["message"] != "failed\nwith detail" {
		t.Fatal(item)
	}
	for _, field := range []string{"declaration", "compiler_form", "location"} {
		if item[field] != nil {
			t.Fatalf("%s is %v", field, item[field])
		}
	}
	if strings.Count(b.String(), "\n") != 1 {
		t.Fatal("report spans several lines")
	}
}

func TestContextPreservesErrorAndReason(t *testing.T) {
	original := errors.New("old exact reason")
	err := staged("toolchain", "toolchain_mismatch", original)
	if err.Error() != "old exact reason" || !errors.Is(err, original) {
		t.Fatal(err)
	}
	if Report(err).Values["diagnostics"].([]any)[0].(*Object).Values["stage"] != "toolchain" {
		t.Fatal("stage lost")
	}
	// An existing diagnostic is never overwritten by an outer stage.
	if Report(staged("emission", "emission_failed", err)).Values["diagnostics"].([]any)[0].(*Object).Values["stage"] != "toolchain" {
		t.Fatal("outer stage replaced the diagnostic")
	}
	var b strings.Builder
	EmitDiagnostics(&b, err, "text")
	if !strings.Contains(b.String(), "toolchain_mismatch") || !strings.Contains(b.String(), "old exact reason") {
		t.Fatal(b.String())
	}
}

func TestReadableErrorIncludesKnownCompilerForm(t *testing.T) {
	err := diagnosticError(newDiagnostic("unsupported control", "admission", "unsupported_lcnf_control", nil, str("LCNF.Code.fun"), nil, nil), nil)
	var b strings.Builder
	EmitDiagnostics(&b, err, "text")
	if !strings.Contains(b.String(), "[unsupported_lcnf_control; LCNF.Code.fun]") {
		t.Fatal(b.String())
	}
}

func parseValue(t *testing.T, text string) any {
	t.Helper()
	value, err := ParseJSON([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

const validProducerItem = `{"code":"unsupported_type","stage":"admission","message":"rejected","declaration":null,"compiler_form":null,"location":null,"hint":"fix source"}`

func envelope(item string) string {
	return `{"schema":"lean-go-diagnostics/v1","diagnostic":` + item + `}`
}

func TestChildProtocolChecksTypesCodesAndLocationContext(t *testing.T) {
	rawLocation := `{"module":"Example","kind":"declaration","declaration":123,"start":{"line":1,"column":0},"end":{"line":1,"column":1}}`
	withLocation := strings.Replace(validProducerItem, `"location":null`, `"location":`+rawLocation, 1)
	if _, err := producerDiagnostic(parseValue(t, envelope(withLocation))); err == nil {
		t.Fatal("numeric location declaration accepted")
	}
	if mapLocation(parseValue(t, rawLocation), []mappedSource{{module: "Example"}}) != nil {
		t.Fatal("invalid location mapped")
	}
	// A forwarded report is validated as a whole: invented codes, wrong types
	// or an unmapped location are rejected.
	for _, changed := range []string{
		strings.Replace(validProducerItem, `"unsupported_type"`, `"fake_rejection"`, 1),
		strings.Replace(validProducerItem, `"rejected"`, `[]`, 1),
		strings.Replace(validProducerItem, `"admission"`, `[]`, 1),
		withLocation,
	} {
		if ValidReport([]byte(`{"schema":"lean-go-diagnostics/v1","success":false,"diagnostics":[` + changed + `]}`)) {
			t.Errorf("invalid child report accepted: %s", changed)
		}
	}
	if !ValidReport([]byte(`{"schema":"lean-go-diagnostics/v1","success":false,"diagnostics":[` + validProducerItem + `]}`)) {
		t.Fatal("valid report rejected")
	}
}

func TestInvalidProducerProtocolNeverBecomesAdmission(t *testing.T) {
	for _, value := range []string{`{}`, `{"code":"source_audit"}`, `{"schema":"future"}`} {
		if _, err := producerDiagnostic(parseValue(t, value)); err == nil {
			t.Errorf("%s accepted", value)
		}
	}
}

func TestMalformedContextTypesAreNotInterpreted(t *testing.T) {
	valid := strings.Replace(validProducerItem, `"unsupported_type"`, `"source_audit"`, 1)
	for _, change := range [][2]string{{`"code":"source_audit"`, `"code":[]`}, {`"code":"source_audit"`, `"code":{}`},
		{`"declaration":null`, `"declaration":4`}, {`"hint":"fix source"`, `"hint":null`}} {
		if _, err := producerDiagnostic(parseValue(t, envelope(strings.Replace(valid, change[0], change[1], 1)))); err == nil {
			t.Errorf("%s accepted", change[1])
		}
	}
	if mapLocation(parseValue(t, `{"module":[]}`), nil) != nil {
		t.Fatal("malformed module mapped")
	}
	if mapLocation(parseValue(t, `{"module":"Example","kind":[]}`), []mappedSource{{module: "Example"}}) != nil {
		t.Fatal("malformed kind mapped")
	}
}

func mapped(t *testing.T, original string) *SourceMap {
	t.Helper()
	m, _, err := instrumentSource([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func position(t *testing.T, m *SourceMap, line, column int64) string {
	t.Helper()
	p := m.Position(line, column)
	if p == nil {
		return "null"
	}
	data, _ := EncodeCompactOrdered(p)
	return string(data)
}

func TestUnicodeCRLFAndSameLineImport(t *testing.T) {
	original := "/- 😀 header -/ module\r\npublic import Init /- 😀 inline -/ public def «😀α» (x : UInt32) : UInt32 := x\r\n"
	m := mapped(t, original)
	want := `{"line":2,"column":45,"utf16_column":46,"byte_offset":` + itoa(strings.Index(original, "«")) + `}`
	if got := position(t, m, 5, 27); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if !strings.Contains(position(t, m, 1, 3), `"byte_offset":3`) {
		t.Fatal(position(t, m, 1, 3))
	}
	for _, p := range [][2]int64{{3, 0}, {0, 0}, {999, 0}, {1, 999}} {
		if position(t, m, p[0], p[1]) != "null" {
			t.Errorf("%v mapped", p)
		}
	}
	if string(m.Original) != original {
		t.Fatal("original changed")
	}
}

func itoa(n int) string { return string(numberOf(n)) }

func TestUnchangedPrefixAndEndExclusive(t *testing.T) {
	m := mapped(t, "import Init\ndef f := 0\n")
	for _, c := range []struct {
		line, column int64
		offset       int
	}{{1, 0, 0}, {5, 4, 16}, {6, 0, len(m.Original)}} {
		if !strings.Contains(position(t, m, c.line, c.column), `"byte_offset":`+itoa(c.offset)+`}`) {
			t.Errorf("%d:%d -> %s", c.line, c.column, position(t, m, c.line, c.column))
		}
	}
}

func TestLocationOnlyNamesSnapshottedSources(t *testing.T) {
	sources := []mappedSource{{"Example", "Example.lean", mapped(t, "def f := 0\n")}}
	raw := `{"module":"Example","kind":"declaration","declaration":"f","start":{"line":4,"column":4},"end":{"line":4,"column":5}}`
	location, ok := mapLocation(parseValue(t, raw), sources).(*Object)
	if !ok || location.Values["path"] != "Example.lean" {
		t.Fatal(location)
	}
	start, end := location.Values["start"].(*Object), location.Values["end"].(*Object)
	if textOf(start.Values["line"]) != "1" || textOf(end.Values["column"]) != "5" {
		t.Fatal(start, end)
	}
	if mapLocation(parseValue(t, strings.Replace(raw, `"Example"`, `"Lean"`, 1)), sources) != nil {
		t.Fatal("unsnapshotted module mapped")
	}
	if mapLocation(parseValue(t, strings.Replace(raw, `"start":{"line":4,"column":4}`, `"start":{"line":2,"column":0}`, 1)), sources) != nil {
		t.Fatal("position inside the insertion mapped")
	}
}

func TestBothCompilerLocationFormatsAndWorkspaceIdentity(t *testing.T) {
	sources := []mappedSource{{"Example", "Example.lean", mapped(t, "def f := broken\n")}}
	check := func(message, workspace string) {
		t.Helper()
		location, ok := compilerLocation(message, sources, workspace).(*Object)
		if !ok {
			t.Fatalf("%q: no location", message)
		}
		start := location.Values["start"].(*Object)
		if textOf(start.Values["line"]) != "1" || textOf(start.Values["column"]) != "9" {
			t.Fatalf("%q: %v", message, start)
		}
	}
	for _, message := range []string{"Example.lean:4:9: error: unknown identifier",
		"Example.lean:4:9: error(lean.unknownIdentifier): Unknown identifier",
		"error(lean.unknownIdentifier): Example.lean:4:9: Unknown identifier",
		"error: Example.lean:4:9: unknown identifier",
		"error: /fresh/Example.lean:4:9: unexpected token"} {
		check(message, "/fresh")
	}
	check(`error: C:\fresh\Example.lean:4:9: unexpected token`, `C:\fresh`)
	if compilerLocation("error: /unrelated/Example.lean:4:9: unexpected token", sources, "/fresh") != nil {
		t.Fatal("unrelated path mapped")
	}
	for _, label := range []string{"warning", "warning(lean.unknownIdentifier)", "error()", "error(lean unknownIdentifier)", "information"} {
		if compilerLocation("Example.lean:4:9: "+label+": message", sources, "/fresh") != nil {
			t.Errorf("%s mapped", label)
		}
	}
}

func TestUnknownFailureAndInvalidProtocolAreBuildFailures(t *testing.T) {
	workspace := t.TempDir()
	defer func(old func([]string, string, []string) (string, error)) { run = old }(run)
	run = func([]string, string, []string) (string, error) { return "", exportError("internal compiler crash") }
	err := runCompiler([]string{"lake"}, workspace, nil, nil, true)
	item := Report(err).Values["diagnostics"].([]any)[0].(*Object)
	if item.Values["stage"] != "build" || item.Values["code"] != "producer_failed" {
		t.Fatal(item.Values)
	}
	run = func(_ []string, dir string, _ []string) (string, error) {
		os.WriteFile(filepath.Join(dir, diagnosticProtocol), []byte(`{"schema":1,"schema":2}`), 0o644)
		return "", exportError("failed")
	}
	err = runCompiler([]string{"lake"}, workspace, nil, nil, true)
	if Report(err).Values["diagnostics"].([]any)[0].(*Object).Values["code"] != "producer_protocol" {
		t.Fatal(err)
	}
}
