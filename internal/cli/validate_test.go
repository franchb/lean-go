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
			IRFileMatches: new(bool), Summary: pipeline.ReportSummary{Functions: 1, Proved: 1, Roots: 1, RootsProved: 1}}, ReportPath: "x.validation.json"}, nil
	}
	if code, _, _ := runMain("validate", "--source-dir", "s", "--module", "M"); code != 2 {
		t.Fatal("missing --ir accepted")
	}
	if code, _, stderr := runMain("validate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--ir", "x.ir.json", "--validate", "off"); code != 2 ||
		!strings.Contains(stderr, `invalid choice: "off" (choose from report, require)`) {
		t.Fatalf("--validate off: %d %q", code, stderr)
	}
	code, stdout, stderr := runMain("validate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--ir", "x.ir.json", "--validate-memory-mb", "2048")
	if code != 0 {
		t.Fatal(stderr)
	}
	if seen.Validation.Mode != pipeline.ValidateReport || seen.Validation.MemoryMB != 2048 || seen.IR != "x.ir.json" || seen.Module != "M" {
		t.Fatalf("%+v", seen)
	}
	if !strings.Contains(stdout, "1/1 functions proved, 1/1 roots established; report in x.validation.json") {
		t.Fatalf("%q", stdout)
	}
	if code, _, _ := runMain("validate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--ir", "x.ir.json", "--validate", "require"); code != 0 || seen.Validation.Mode != pipeline.ValidateRequire {
		t.Fatalf("--validate require: %d %+v", code, seen.Validation)
	}
}

// Each command's --validate line states that command's own choices and default.
func TestValidateHelpStatesTheCommandDefault(t *testing.T) {
	optionLine := func(command string) string {
		t.Helper()
		code, stdout, _ := runMain(command, "--help")
		if code != 0 {
			t.Fatalf("%s --help exits %d", command, code)
		}
		for _, line := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(line, "  --validate ") {
				return strings.Join(strings.Fields(line), " ")
			}
		}
		t.Fatalf("%s --help has no --validate line:\n%s", command, stdout)
		return ""
	}
	if got, want := optionLine("generate"), "--validate off|report|require translation-validate the export (default off); require fails unless everything is proved"; got != want {
		t.Errorf("generate: %q", got)
	}
	if got, want := optionLine("validate"), "--validate report|require report records what was proved (default); require also fails unless everything is proved"; got != want {
		t.Errorf("validate: %q", got)
	}
	// The other validation options are the same list for both commands.
	_, generate, _ := runMain("generate", "--help")
	_, validate, _ := runMain("validate", "--help")
	for _, name := range []string{"--validation-report", "--validation-diagnostics", "--validate-memory-mb", "--validate-heartbeats", "--validate-timeout-ms", "--validate-process-timeout"} {
		if !strings.Contains(generate, "  "+name+" ") || !strings.Contains(validate, "  "+name+" ") {
			t.Errorf("%s is missing from a help text", name)
		}
	}
	if strings.Contains(validate, "default off") {
		t.Errorf("validate --help names a default the command does not have:\n%s", validate)
	}
}
