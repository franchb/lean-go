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

// DnsHeader.word1 makes the kernel use more than 12 GB; under the default
// cap it must be a failed obligation, and the other obligations still run.
func TestDNSWord1HitsTheMemoryCap(t *testing.T) {
	requireValidate(t)
	result, err := generateExample(t, validateDNS, t.TempDir(), &ValidationOptions{Mode: ValidateReport})
	if err != nil {
		t.Fatal(err)
	}
	r := result.Validation.Report
	byName := map[string]ReportFunction{}
	for _, f := range r.Functions {
		byName[f.Name] = f
	}
	word1 := byName["DnsHeader.word1"]
	if word1.Status != "failed" || word1.ReasonClass != ReasonMemory {
		t.Fatalf("word1: %+v", word1)
	}
	if byName["DnsHeader.parse"].Status != "proved" || byName["DnsHeader.validate"].Status != "proved" {
		t.Fatalf("%v", r.Unproved())
	}
	if r.Summary.RootsProved != 2 || len(result.Validation.Diagnostics.Attempts) < 2 {
		t.Fatalf("%+v %d", r.Summary, len(result.Validation.Diagnostics.Attempts))
	}
}
