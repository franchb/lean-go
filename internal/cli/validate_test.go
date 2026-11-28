package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/franchb/lean-go/internal/pipeline"
)

func TestGenerateValidationIsOffByDefault(t *testing.T) {
	seen := captureGenerate(t, &pipeline.GenerateResult{Out: "o", IROutput: "i"}, nil)
	if code, _, stderr := runMain("generate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i"); code != 0 {
		t.Fatal(stderr)
	}
	if seen.Validation != nil {
		t.Fatalf("validation requested by default: %+v", seen.Validation)
	}
	if code, _, _ := runMain("generate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i", "--validate", "off"); code != 0 || seen.Validation != nil {
		t.Fatal("--validate off requested validation")
	}
}

func TestGenerateValidationFlagsAreForwarded(t *testing.T) {
	seen := captureGenerate(t, &pipeline.GenerateResult{Out: "o", IROutput: "i"}, nil)
	code, _, stderr := runMain("generate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i",
		"--validate", "require", "--validation-report", "r.json", "--validation-diagnostics", "d.json",
		"--validate-memory-mb", "1024", "--validate-heartbeats", "5000", "--validate-timeout-ms", "300", "--validate-process-timeout", "60")
	if code != 0 {
		t.Fatal(stderr)
	}
	want := pipeline.ValidationOptions{Mode: "require", Report: "r.json", Diagnostics: "d.json", MemoryMB: 1024, Heartbeats: 5000, TimeoutMs: 300, ProcessTimeout: time.Minute}
	if seen.Validation == nil || *seen.Validation != want {
		t.Fatalf("%+v", seen.Validation)
	}
}

func TestValidationFlagErrors(t *testing.T) {
	captureGenerate(t, &pipeline.GenerateResult{Out: "o", IROutput: "i"}, nil)
	base := []string{"generate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i"}
	for _, extra := range [][]string{
		{"--validate", "strict"},
		{"--validation-report", "r.json"},
		{"--validate-memory-mb", "64"},
		{"--validate", "report", "--validate-memory-mb", "lots"},
		{"--validate", "report", "--validate-heartbeats", "0"},
		{"--validate", "report", "--validate-timeout-ms", "-3"},
	} {
		code, _, stderr := runMain(append(append([]string{}, base...), extra...)...)
		if code == 0 || stderr == "" {
			t.Errorf("%v accepted", extra)
		}
	}
}

func TestValidateCommand(t *testing.T) {
	var seen pipeline.ValidateOptions
	old := validateFunc
	t.Cleanup(func() { validateFunc = old })
	validateFunc = func(opts pipeline.ValidateOptions) (*pipeline.ValidationResult, error) {
		seen = opts
		return &pipeline.ValidationResult{Report: &pipeline.ValidationReport{Status: "completed",
			IRFileMatches: new(bool), Summary: pipeline.ReportSummary{Functions: 1, Proved: 1, Roots: 1, RootsProved: 1, RootsWitnessed: 1}}, ReportPath: "x.validation.json"}, nil
	}
	if code, _, _ := runMain("validate", "--source-dir", "s", "--module", "M"); code != 2 {
		t.Fatal("missing --ir accepted")
	}
	if code, _, _ := runMain("validate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--ir", "x.ir.json", "--validate", "off"); code != 2 {
		t.Fatal("--validate off accepted")
	}
	code, stdout, stderr := runMain("validate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--ir", "x.ir.json", "--validate-memory-mb", "2048")
	if code != 0 {
		t.Fatal(stderr)
	}
	if seen.Validation.Mode != pipeline.ValidateReport || seen.Validation.MemoryMB != 2048 || seen.IR != "x.ir.json" || seen.Module != "M" {
		t.Fatalf("%+v", seen)
	}
	if !strings.Contains(stdout, "1/1 functions proved, 1/1 roots established, 1/1 roots witnessed; report in x.validation.json") {
		t.Fatalf("%q", stdout)
	}
	if code, stdout, _ := runMain("validate", "--help"); code != 0 || !strings.Contains(stdout, "--validate-memory-mb") {
		t.Fatal("help")
	}
	if code, stdout, _ := runMain("generate", "--help"); code != 0 || !strings.Contains(stdout, "--validate off|report|require") {
		t.Fatal("generate help lacks --validate")
	}
}

func TestPrintValidationListsWitnesses(t *testing.T) {
	yes := true
	report := &pipeline.ValidationReport{Status: pipeline.ValidationCompleted, IRFileMatches: &yes, Roots: []string{"M.f", "M.g"},
		Programs: []pipeline.ReportProgram{
			{Root: "M.f", Status: "proved", Witnesses: &pipeline.ReportWitnesses{Status: pipeline.WitnessNotAttempted, ReasonClass: "heartbeat_limit"}},
			{Root: "M.g", Status: "proved", Witnesses: &pipeline.ReportWitnesses{Status: pipeline.WitnessFailed, ReasonClass: "returns_none"}},
		},
		Summary: pipeline.ReportSummary{Roots: 2, RootsProved: 2}}
	var out strings.Builder
	printValidation(&out, &pipeline.ValidationResult{Report: report, ReportPath: "r.json"})
	want := "validation: 0/0 functions proved, 2/2 roots established, 0/2 roots witnessed; report in r.json\n" +
		"  root M.g: witnesses failed: returns none\n" +
		"  root M.f: witnesses not attempted: heartbeat limit\n"
	if out.String() != want {
		t.Fatalf("%q", out.String())
	}
}
