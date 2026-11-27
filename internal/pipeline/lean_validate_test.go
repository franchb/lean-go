package pipeline

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_VALIDATE=1) tests of translation validation through
// the real pipeline. Run them with -p 1: validation is memory-hungry.

func requireValidate(t *testing.T) { gate(t, "LEAN_GO_VALIDATE") }

type validationExample struct {
	name, source, module string
	roots                []string
}

var (
	validateClamp   = validationExample{"clamp", "../../examples/proofs/clamp/source", "Clamp", []string{"Clamp.clamp"}}
	validateLocal   = validationExample{"local", "../../examples/modules", "Local", []string{"Local.run"}}
	validateAdler32 = validationExample{"adler32", "../../examples/proofs/adler32/source", "Adler32", []string{"Adler32.checksum", "Adler32.initial", "Adler32.update"}}
	validateDNS     = validationExample{"dns", "../../examples/formats/dns/source", "DnsHeader", []string{"DnsHeader.parse", "DnsHeader.serialize", "DnsHeader.validate"}}
)

func generateExample(t *testing.T, e validationExample, out string, validation *ValidationOptions) (*GenerateResult, error) {
	t.Helper()
	return Generate(GenerateOptions{
		SourceDir: e.source, Module: e.module, Roots: e.roots,
		Out: filepath.Join(out, e.name), IROutput: filepath.Join(out, e.name+".ir.json"),
		Cache: testCache(t), Validation: validation,
	})
}

func TestRequireProvesSmallExamples(t *testing.T) {
	requireValidate(t)
	for _, e := range []validationExample{validateClamp, validateLocal, validateAdler32} {
		t.Run(e.name, func(t *testing.T) {
			var reports [][]byte
			for i := 0; i < 2; i++ {
				out := t.TempDir()
				result, err := generateExample(t, e, out, &ValidationOptions{Mode: ValidateRequire, Diagnostics: filepath.Join(out, "diagnostics.json")})
				if err != nil {
					t.Fatal(err)
				}
				report := readText(t, filepath.Join(out, e.name+".validation.json"))
				if report != string(result.Validation.ReportJSON) {
					t.Fatal("published report differs from the result")
				}
				parsed, err := ParseValidationReport([]byte(report))
				if err != nil {
					t.Fatal(err)
				}
				if !parsed.Complete() || parsed.IRSHA256 != Digest(result.IR) || parsed.Summary.Roots != len(e.roots) {
					t.Fatalf("%v", parsed.Unproved())
				}
				if !strings.Contains(readText(t, filepath.Join(out, "diagnostics.json")), `"time_ms"`) {
					t.Fatal("diagnostics lack timings")
				}
				reports = append(reports, []byte(report))
			}
			if !bytes.Equal(reports[0], reports[1]) {
				t.Fatal("two runs gave different tracked reports")
			}
		})
	}
}

// The compiler's constant folder rewrites arithmetic before export
// (docs/support-matrix.md, "Compiler rewrites"). Validation brings both sides
// to one form, so each of these source functions is proved equal to its
// rewritten IR.
func TestValidationBridgesCompilerArithmetic(t *testing.T) {
	requireValidate(t)
	source := t.TempDir()
	writeText(t, filepath.Join(source, "Forms.lean"), `namespace Forms
def leftLiteral (x : UInt32) : UInt32 := 16 * x
def powers (x : UInt64) : UInt64 := x / 2 + x / 1024 + x * 8
def constant (x : UInt16) : UInt16 := x + (100 / 7 + 100 % 0)
def zeros (x : UInt64) : UInt64 := x * 0 + x % 1 + x
def identities (x : UInt8) : UInt8 := (x % 0) / 1 * 1 + 0
def wrapped (x : UInt8) : UInt8 := x / 256 + 0 / x
end Forms
`)
	roots := []string{"Forms.constant", "Forms.identities", "Forms.leftLiteral", "Forms.powers", "Forms.wrapped", "Forms.zeros"}
	out := t.TempDir()
	result, err := generateExample(t, validationExample{"forms", source, "Forms", roots}, out, &ValidationOptions{Mode: ValidateRequire})
	if err != nil {
		t.Fatal(err)
	}
	// The export must show the rewrites, or the test proves nothing about them.
	ir := string(result.IR)
	for _, want := range []string{`"UInt32.shiftLeft"`, `"UInt64.shiftRight"`, `"kind":"uint16","name":"114"`, `"UInt8.div"`} {
		if !strings.Contains(ir, want) {
			t.Errorf("the IR lacks %s: the compiler did not rewrite as expected", want)
		}
	}
	for _, gone := range []string{`"UInt32.mul"`, `"UInt64.div"`, `"UInt64.mod"`, `"UInt16.div"`, `"UInt8.mod"`, `"UInt8.mul"`} {
		if strings.Contains(ir, gone) {
			t.Errorf("the IR still has %s", gone)
		}
	}
	parsed, err := ParseValidationReport(result.Validation.ReportJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Complete() || parsed.Summary.Roots != len(roots) {
		t.Fatalf("%v", parsed.Unproved())
	}
}

func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		data, err := os.ReadFile(path)
		out[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestValidationLeavesIRAndGoUnchanged(t *testing.T) {
	requireValidate(t)
	for _, e := range []validationExample{validateLocal, validateAdler32} {
		t.Run(e.name, func(t *testing.T) {
			plain, validated := t.TempDir(), t.TempDir()
			a, err := generateExample(t, e, plain, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := generateExample(t, e, validated, &ValidationOptions{Mode: ValidateReport})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a.IR, b.IR) || a.Validation != nil || b.Validation == nil {
				t.Fatal("validation changed the IR")
			}
			x, y := readFiles(t, filepath.Join(plain, e.name)), readFiles(t, filepath.Join(validated, e.name))
			if len(x) != len(y) {
				t.Fatal("validation changed the package file set")
			}
			for name, data := range x {
				if y[name] != data {
					t.Fatalf("validation changed %s", name)
				}
			}
			if readText(t, filepath.Join(plain, e.name+".ir.json")) != readText(t, filepath.Join(validated, e.name+".ir.json")) {
				t.Fatal("validation changed the IR file")
			}
			if strings.Contains(string(b.IR), "Validate") {
				t.Fatal("validation files leaked into the IR inventory")
			}
		})
	}
}

func TestTinyMemoryCapIsAFailedValidationNotAnOOM(t *testing.T) {
	requireValidate(t)
	out := t.TempDir()
	result, err := generateExample(t, validateClamp, out, &ValidationOptions{Mode: ValidateReport, MemoryMB: MinValidateMemoryMB})
	if err != nil {
		t.Fatal("report mode failed:", err)
	}
	r := result.Validation.Report
	if r.Status != ValidationProcessFailed || r.ReasonClass != ReasonMemory {
		t.Fatalf("%s %s", r.Status, r.ReasonClass)
	}
	if !strings.Contains(result.Validation.Diagnostics.Reason, "memory limit of 64 MiB") {
		t.Fatal(result.Validation.Diagnostics.Reason)
	}
	// require: the report is written, then extraction fails; nothing else is published.
	out = t.TempDir()
	_, err = generateExample(t, validateClamp, out, &ValidationOptions{Mode: ValidateRequire, MemoryMB: MinValidateMemoryMB})
	var pe *Error
	if !errors.As(err, &pe) || pe.diagnostic.Values["code"] != "validation_failed" {
		t.Fatalf("require succeeded or failed wrongly: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "clamp.validation.json")); err != nil {
		t.Fatal("report not written:", err)
	}
	if _, err := os.Stat(filepath.Join(out, "clamp.ir.json")); err == nil {
		t.Fatal("IR published by a failed require run")
	}
}

// DnsHeader.word1 and DnsHeader.serialize used to exhaust the kernel's memory
// (more than 12 GB, and about 6 GB), until the evaluation steps of
// validate_obl got explicit proofs. Under the default budget the whole
// program must now be proved in one process attempt. No tracked obligation
// reaches the cap any more, so the memory path is exercised with a cap the
// process cannot meet: it must still end as a reported memory-limit failure,
// not an OOM. (The skip-and-rerun of a single obligation that hits the cap is
// covered with a fake validator: TestCaughtMemoryFailureIsIsolatedByARerun,
// TestKilledProcessSkipsTheObligationAndReruns.)
func TestDNSProvesUnderTheDefaultBudget(t *testing.T) {
	requireValidate(t)
	result, err := generateExample(t, validateDNS, t.TempDir(), &ValidationOptions{Mode: ValidateRequire})
	if err != nil {
		t.Fatal(err)
	}
	r := result.Validation.Report
	if !r.Complete() || r.Summary.Proved != 14 || r.Summary.RootsProved != 3 {
		t.Fatalf("%+v %v", r.Summary, r.Unproved())
	}
	if len(result.Validation.Diagnostics.Attempts) != 1 {
		t.Fatalf("attempts %+v", result.Validation.Diagnostics.Attempts)
	}

	result, err = generateExample(t, validateDNS, t.TempDir(), &ValidationOptions{Mode: ValidateReport, MemoryMB: MinValidateMemoryMB})
	if err != nil {
		t.Fatal("report mode failed:", err)
	}
	r = result.Validation.Report
	if r.Status != ValidationProcessFailed || r.ReasonClass != ReasonMemory || r.Complete() {
		t.Fatalf("%s %s", r.Status, r.ReasonClass)
	}
}
