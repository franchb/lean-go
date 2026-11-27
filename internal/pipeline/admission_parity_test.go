package pipeline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// admissionCase is one rejected program with the diagnostic that the Python
// pipeline (scripts/export_module.py) reported for it. The cases are those of
// scripts/test_module_admission.py and scripts/test_diagnostics_lean.py; the
// expectations were recorded from the Python pipeline before it was retired.
type admissionCase struct {
	Name    string            `json:"name"`
	Files   map[string]string `json:"files"`
	Module  string            `json:"module"`
	Root    string            `json:"root"`
	Harness *string           `json:"harness"`
	Report  string            `json:"report"`
}

var (
	workspacePath = regexp.MustCompile(`(?:/[^\s:'"]*)?/lean-go-module-[^/\s:'"]+`)
	buildTiming   = regexp.MustCompile(` \(\d+(?:\.\d+)?m?s\)`)
)

// buildErrors keeps the compiler's error lines of a build failure. Lake's
// progress lines name the jobs it ran (fewer with a cached producer) and their
// timings, so they are not comparable between runs.
func buildErrors(message string) []string {
	var out []string
	for _, line := range strings.Split(message, "\n") {
		line = workspacePath.ReplaceAllString(line, "<workspace>")
		line = buildTiming.ReplaceAllString(line, "")
		if strings.Contains(line, "error") || strings.HasPrefix(line, "producer process failed") {
			out = append(out, line)
		}
	}
	return out
}

func TestAdmissionDiagnosticsMatchRecordedPythonReports(t *testing.T) {
	if os.Getenv("LEAN_GO_PARITY") != "1" {
		t.Skip("set LEAN_GO_PARITY=1 for fresh Lean admission builds")
	}
	data, err := os.ReadFile("testdata/admission_diagnostics.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []admissionCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	cache := &Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source")
			if err := os.Mkdir(source, 0o755); err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(c.Files))
			for name := range c.Files {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if err := os.WriteFile(filepath.Join(source, name), []byte(c.Files[name]), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			opts := ExportOptions{SourceDir: source, Module: c.Module, Roots: []string{c.Root}, Cache: cache}
			if c.Harness != nil {
				harness := *c.Harness
				opts.harness = func(string, []string) string { return harness }
			}
			_, err := ExportModule(opts)
			var pe *Error
			if !errors.As(err, &pe) || pe.diagnostic == nil {
				t.Fatalf("expected a diagnostic rejection, got %v", err)
			}
			got, err := EncodeCompactOrdered(Report(err))
			if err != nil {
				t.Fatal(err)
			}
			if pe.diagnostic.Values["code"] != "producer_failed" {
				if string(got) != c.Report {
					t.Fatalf("report differs:\n got %s\nwant %s", got, c.Report)
				}
				return
			}
			// Build failures: every field but the message is exact; the
			// message must carry the same compiler errors.
			want := parseReportItem(t, c.Report)
			have := parseReportItem(t, string(got))
			wantMessage, haveMessage := want.Values["message"].(string), have.Values["message"].(string)
			want.Set("message", "")
			have.Set("message", "")
			a, _ := EncodeCompactOrdered(want)
			b, _ := EncodeCompactOrdered(have)
			if string(a) != string(b) {
				t.Fatalf("diagnostic differs:\n got %s\nwant %s", b, a)
			}
			if strings.Join(buildErrors(haveMessage), "\n") != strings.Join(buildErrors(wantMessage), "\n") {
				t.Fatalf("build errors differ:\n got %q\nwant %q", buildErrors(haveMessage), buildErrors(wantMessage))
			}
		})
	}
}

func parseReportItem(t *testing.T, report string) *Object {
	t.Helper()
	value, err := ParseJSON([]byte(report))
	if err != nil {
		t.Fatal(err)
	}
	return value.(*Object).Values["diagnostics"].([]any)[0].(*Object)
}
