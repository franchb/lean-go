package artifacts

import (
	"os"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

// Ungated: the tracked validation reports belong to the tracked IR and the
// current validation library, and the coverage table of the design note is
// the one they give.

func TestTrackedValidationReportsMatchTheirIR(t *testing.T) {
	root := checkoutRoot(t)
	formats, err := FormatNames(root)
	if err != nil {
		t.Fatal(err)
	}
	pairs := map[string]string{}
	for _, spec := range Packages {
		pairs[spec.IR] = spec.Validation()
	}
	for _, name := range formats {
		pairs[FormatIR(name)] = FormatValidation(name)
	}
	library := pipeline.ValidationInventory()
	for ir, path := range pairs {
		data, err := os.ReadFile(at(root, path))
		if err != nil {
			t.Errorf("%s: %v (run lean-go regenerate)", path, err)
			continue
		}
		report, err := pipeline.ParseValidationReport(data)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		program, err := os.ReadFile(at(root, ir))
		if err != nil {
			t.Fatal(err)
		}
		if report.IRSHA256 != pipeline.Digest(program) {
			t.Errorf("%s does not belong to the tracked %s (run lean-go regenerate)", path, ir)
		}
		if len(report.Library) != len(library) {
			t.Errorf("%s: library inventory differs (run lean-go regenerate)", path)
		}
		for key, digest := range library {
			if report.Library[key] != digest {
				t.Errorf("%s: %s differs from the current validation library (run lean-go regenerate)", path, key)
			}
		}
		if report.Status != pipeline.ValidationCompleted || report.IRFileMatches == nil || !*report.IRFileMatches {
			t.Errorf("%s: validation did not complete: %v", path, report.Unproved())
		}
		budget := pipeline.ValidationBudget{Heartbeats: pipeline.DefaultValidateHeartbeats, TimeoutMs: pipeline.DefaultValidateTimeoutMs,
			MemoryMB: pipeline.DefaultValidateMemoryMB, ProcessTimeoutMs: pipeline.DefaultValidateProcessTimeout.Milliseconds()}
		if report.Budget != budget {
			t.Errorf("%s: budget %+v is not the default", path, report.Budget)
		}
	}
}

func TestDocumentedCoverageMatchesTrackedReports(t *testing.T) {
	root := checkoutRoot(t)
	rows, err := Coverage(root)
	if err != nil {
		t.Fatal(err)
	}
	documented, err := DocumentedCoverage(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := CoverageTable(rows); strings.TrimSpace(documented) != strings.TrimSpace(want) {
		t.Fatalf("%s §9 is stale; replace the table with the output of lean-go regenerate --coverage:\n%s", CoverageDoc, want)
	}
}
