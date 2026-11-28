package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/artifacts"
	"github.com/franchb/lean-go/internal/pipeline"
)

// tracked is a path of the checkout this package belongs to.
func tracked(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

func manifestOf(pkg string) string { return tracked(filepath.FromSlash(pkg), "manifest.json") }

// editedReport writes a copy of a tracked report after edit changed it.
func editedReport(t *testing.T, source string, edit func(report map[string]any)) string {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	edit(report)
	out, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "edited.validation.json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The tracked reports were written by the validation runner, which digests the
// IR file. Every tracked package, metered or native, must be found covered by
// the report of its IR: this is the relation between a manifest's program and
// a report's ir_sha256 that the option relies on.
func TestEveryTrackedPackageIsCoveredByItsValidationReport(t *testing.T) {
	for _, p := range artifacts.Packages {
		lines, err := requireValidation([]string{manifestOf(p.Package)}, []string{tracked(filepath.FromSlash(p.Validation()))})
		if err != nil {
			t.Errorf("%s: %v", p.Package, err)
			continue
		}
		if len(lines) != 1 || !strings.Contains(lines[0], "covers the IR of module "+p.Module+": ") || !strings.HasSuffix(lines[0], " roots established.") {
			t.Errorf("%s: %q", p.Package, lines)
		}
	}
	names, err := artifacts.FormatNames(tracked())
	if err != nil || len(names) == 0 {
		t.Fatal(names, err)
	}
	for _, name := range names {
		manifest := tracked(artifacts.FormatsDir, name, "internal", "extracted", "manifest.json")
		report := tracked(artifacts.FormatsDir, name, name+".validation.json")
		if _, err := requireValidation([]string{manifest}, []string{report}); err != nil {
			t.Errorf("format %s: %v", name, err)
		}
	}
}

func TestRequireValidationRejectsReportsThatDoNotCoverTheManifest(t *testing.T) {
	quotaManifest := manifestOf("generated/quota")
	quotaReport := tracked("testdata", "quota.validation.json")
	programs := func(report map[string]any) []any { return report["programs"].([]any) }
	functions := func(report map[string]any) []any { return report["functions"].([]any) }
	for name, c := range map[string]struct {
		report string
		want   string
	}{
		"the report of another program": {tracked("examples", "proofs", "clamp", "clamp.validation.json"), "validates another IR"},
		"another IR digest": {editedReport(t, quotaReport, func(r map[string]any) {
			r["ir_sha256"] = strings.Repeat("0", 64)
		}), "validates another IR (ir_sha256 " + strings.Repeat("0", 64)},
		"a failed function": {editedReport(t, quotaReport, func(r map[string]any) {
			f := functions(r)[3].(map[string]any)
			f["status"], f["reason_class"] = "failed", "heartbeat_limit"
			delete(f, "theorem")
		}), "not fully proved (validation: 4/4 functions proved, 4/4 roots established): Quota.reserve: failed: heartbeat limit"},
		"a function that was not attempted": {editedReport(t, quotaReport, func(r map[string]any) {
			f := functions(r)[0].(map[string]any)
			f["status"], f["reason_class"] = "not_attempted", "no_encoding"
		}), "Quota.create: not_attempted: no encoding"},
		"a root that is not established": {editedReport(t, quotaReport, func(r map[string]any) {
			p := programs(r)[1].(map[string]any)
			p["status"], p["missing"] = "not_established", []any{"Quota.release"}
		}), "root Quota.release: not established (needs Quota.release)"},
		"a root without its entry": {editedReport(t, quotaReport, func(r map[string]any) {
			r["programs"] = programs(r)[:3]
		}), "root Quota.reserve: not reported"},
		"a run that did not complete": {editedReport(t, quotaReport, func(r map[string]any) {
			r["status"], r["reason_class"] = "process_failed", "memory_limit"
		}), "validation process_failed (memory limit)"},
		"a program that is not the exported IR": {editedReport(t, quotaReport, func(r map[string]any) {
			r["ir_file_matches"] = false
		}), "the validated program is not the exported IR"},
		"roots trimmed together with their entries": {editedReport(t, quotaReport, func(r map[string]any) {
			r["roots"], r["programs"] = r["roots"].([]any)[:3], programs(r)[:3]
		}), "its module or roots are not those of the IR in"},
		"another module name": {editedReport(t, quotaReport, func(r map[string]any) {
			r["module"] = "Quotas"
		}), "its module or roots are not those of the IR in"},
		"a function left out": {editedReport(t, quotaReport, func(r map[string]any) {
			r["functions"] = functions(r)[1:]
		}), "does not account for IR function Quota.create"},
		"another validation library": {editedReport(t, quotaReport, func(r map[string]any) {
			r["library"].(map[string]any)[pipeline.ValidationToolInput] = strings.Repeat("0", 64)
		}), "written by another lean-go version"},
		"a validation library file left out": {editedReport(t, quotaReport, func(r map[string]any) {
			delete(r["library"].(map[string]any), "producer/LeanGo/Validate/Semantics.lean")
		}), "written by another lean-go version"},
		"an earlier schema": {editedReport(t, quotaReport, func(r map[string]any) {
			r["schema"] = "lean-go-validation/v1"
		}), `unsupported validation report schema "lean-go-validation/v1"`},
		"an unknown field": {editedReport(t, quotaReport, func(r map[string]any) {
			r["proved_by_hand"] = true
		}), "invalid validation report"},
		"a manifest in place of a report": {quotaManifest, "invalid validation report"},
		"no file":                         {filepath.Join(t.TempDir(), "absent.validation.json"), "absent.validation.json"},
		"a directory":                     {t.TempDir(), "not a regular file"},
	} {
		lines, err := requireValidation([]string{quotaManifest}, []string{c.report})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %q %v", name, lines, err)
		}
	}
	// The unedited copy passes, so the rejections above are due to the edits.
	if _, err := requireValidation([]string{quotaManifest}, []string{editedReport(t, quotaReport, func(map[string]any) {})}); err != nil {
		t.Fatal(err)
	}
}

func TestRequireValidationNeedsAManifestWithItsProgram(t *testing.T) {
	report := tracked("testdata", "quota.validation.json")
	directory := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, c := range map[string]struct{ manifest, want string }{
		"not JSON":                {write("a.json", "manifest"), "invalid JSON"},
		"not an object":           {write("b.json", "[]"), "not a generated manifest with its program"},
		"no program":              {write("c.json", `{"ir_sha256":"00"}`), "not a generated manifest with its program"},
		"a program by digest":     {write("d.json", `{"program":"00"}`), "not a generated manifest with its program"},
		"a program without roots": {write("e.json", `{"program":{"module":"Quota","functions":[]}}`), "not a generated manifest with its program"},
		"a validation report":     {report, "not a generated manifest with its program"},
		"no file":                 {filepath.Join(directory, "absent.json"), "absent.json"},
	} {
		if _, err := requireValidation([]string{c.manifest}, []string{report}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := requireValidation(nil, []string{report}); err == nil {
		t.Error("accepted a report without a manifest")
	}
}

func TestRequireValidationCoversEveryComponent(t *testing.T) {
	wire16, crc16 := manifestOf("examples/proofs/wire16/wire16"), manifestOf("examples/proofs/crc16/crc16")
	wire16Report := tracked("examples", "proofs", "wire16", "wire16.validation.json")
	crc16Report := tracked("examples", "proofs", "crc16", "crc16.validation.json")
	clampReport := tracked("examples", "proofs", "clamp", "clamp.validation.json")
	lines, err := requireValidation([]string{wire16, crc16}, []string{crc16Report, wire16Report})
	if err != nil || len(lines) != 2 || !strings.Contains(lines[0], "module Crc16") || !strings.Contains(lines[1], "module Wire16") {
		t.Fatal(lines, err)
	}
	if _, err := requireValidation([]string{wire16, crc16}, []string{wire16Report}); err == nil ||
		!strings.Contains(err.Error(), "crc16") || !strings.Contains(err.Error(), "no validation report given for its IR") {
		t.Errorf("a component without a report: %v", err)
	}
	if _, err := requireValidation([]string{wire16, crc16}, []string{wire16Report, crc16Report, clampReport}); err == nil ||
		!strings.Contains(err.Error(), "validates another IR") {
		t.Errorf("a report for no component: %v", err)
	}
	if _, err := requireValidation([]string{wire16, crc16}, nil); err == nil {
		t.Error("accepted components without reports")
	}
	// The metered and the native package of one IR share its report.
	clamp, clampNative := manifestOf("examples/proofs/clamp/clamp"), manifestOf("examples/proofs/clamp/clampnative")
	if lines, err := requireValidation([]string{clamp, clampNative}, []string{clampReport}); err != nil || len(lines) != 1 {
		t.Error(lines, err)
	}
}

func TestRequireValidationRejectsSymlinkedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symbolic link needs a privilege on Windows")
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := filepath.Abs(manifestOf("generated/quota"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := filepath.Abs(tracked("testdata", "quota.validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"manifest.json": manifest, "quota.validation.json": report} {
		if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := requireValidation([]string{manifest}, []string{filepath.Join(directory, "quota.validation.json")}); err == nil ||
		!strings.Contains(err.Error(), "must not contain symlinks") {
		t.Errorf("a linked report: %v", err)
	}
	if _, err := requireValidation([]string{filepath.Join(directory, "manifest.json")}, []string{report}); err == nil ||
		!strings.Contains(err.Error(), "must not contain symlinks") {
		t.Errorf("a linked manifest: %v", err)
	}
}

func TestCheckProofsRequireValidationOption(t *testing.T) {
	calls := 0
	var seen pipeline.CheckProofsOptions
	old := checkProofsFunc
	checkProofsFunc = func(opts pipeline.CheckProofsOptions) (string, error) {
		calls++
		seen = opts
		return "checked", nil
	}
	t.Cleanup(func() { checkProofsFunc = old })
	manifest, report := manifestOf("generated/quota"), tracked("testdata", "quota.validation.json")

	code, stdout, stderr := runMain("check-proofs", "--contract", "c.json", "--manifest", manifest, "--evidence", "e.json", "--require-validation", report)
	want := "checked\n" + report + " covers the IR of module Quota: 4/4 functions proved, 4/4 roots established.\n"
	if code != 0 || stdout != want || stderr != "" || calls != 1 {
		t.Fatalf("%d %q %q %d", code, stdout, stderr, calls)
	}
	if seen.Contract != "c.json" || seen.Manifest != manifest || seen.Evidence != "e.json" || seen.Output != "" {
		t.Fatalf("%+v", seen)
	}

	// A requirement that fails stops before the proof check: nothing is built
	// and no evidence is written.
	other := tracked("examples", "proofs", "clamp", "clamp.validation.json")
	code, stdout, stderr = runMain("check-proofs", "--contract", "c.json", "--manifest", manifest, "--output", "e.json", "--require-validation", other)
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "validation requirement failed: "+other+": validates another IR") || calls != 1 {
		t.Fatalf("%d %q %q %d", code, stdout, stderr, calls)
	}

	// A composition names one report per component IR.
	wire16, crc16 := manifestOf("examples/proofs/wire16/wire16"), manifestOf("examples/proofs/crc16/crc16")
	composition := []string{"check-proofs", "--contract", "c.json", "--component-manifest", "wire16", wire16, "--component-manifest", "crc16", crc16, "--evidence", "e.json",
		"--require-validation", tracked("examples", "proofs", "wire16", "wire16.validation.json")}
	if code, _, stderr := runMain(composition...); code != 1 || !strings.Contains(stderr, "no validation report given for its IR") || calls != 1 {
		t.Fatalf("%d %q %d", code, stderr, calls)
	}
	composition = append(composition, "--require-validation", tracked("examples", "proofs", "crc16", "crc16.validation.json"))
	if code, stdout, _ := runMain(composition...); code != 0 || strings.Count(stdout, " covers the IR of module ") != 2 || calls != 2 {
		t.Fatalf("%d %q %d", code, stdout, calls)
	}

	// A failed proof check prints no validation line.
	checkProofsFunc = func(pipeline.CheckProofsOptions) (string, error) { return "", os.ErrInvalid }
	code, stdout, stderr = runMain("check-proofs", "--contract", "c.json", "--manifest", manifest, "--evidence", "e.json", "--require-validation", report)
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "proof check failed: ") {
		t.Fatalf("%d %q %q", code, stdout, stderr)
	}

	if code, _, _ := runMain("check-proofs", "--contract", "c.json", "--manifest", manifest, "--evidence", "e.json", "--require-validation"); code != 2 {
		t.Fatal("--require-validation without a report accepted")
	}
	if code, stdout, _ := runMain("check-proofs", "--help"); code != 0 || !strings.Contains(stdout, "--require-validation REPORT") {
		t.Fatal("help lacks --require-validation")
	}
}
