package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// leftovers lists the staging files a publication left in dir.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lean-go-publish-") {
			out = append(out, entry.Name())
		}
	}
	return out
}

// Staging does everything that can fail and changes nothing visible; commit
// is the rename, and discard cleans up whether or not commit ran.
func TestStageFileSeparatesPreparationFromPublication(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "program.ir.json")
	commit, discard, err := stageFile(target, []byte("new"), true)
	if err != nil || commit == nil {
		t.Fatalf("stage: %v", err)
	}
	if exists(target) || len(leftovers(t, filepath.Dir(target))) != 1 {
		t.Fatal("staging published the file or did not prepare it")
	}
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	discard()
	if readText(t, target) != "new" || len(leftovers(t, filepath.Dir(target))) != 0 {
		t.Fatal("commit did not publish, or discard left the staging file")
	}

	// Identical bytes: nothing to do, nothing staged.
	commit, discard, err = stageFile(target, []byte("new"), true)
	discard()
	if err != nil || commit != nil || len(leftovers(t, filepath.Dir(target))) != 0 {
		t.Fatalf("identical output staged again: %v", err)
	}

	// Discarding without committing keeps the previous file.
	commit, discard, err = stageFile(target, []byte("other"), true)
	if err != nil || commit == nil {
		t.Fatalf("stage: %v", err)
	}
	discard()
	if readText(t, target) != "new" || len(leftovers(t, filepath.Dir(target))) != 0 {
		t.Fatal("discard changed the destination or left the staging file")
	}
}

// The failures a destination can cause are reported by staging, before the
// caller has changed anything else.
func TestStageFileReportsUnwritableDestinations(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	writeText(t, file, "x")
	occupied := filepath.Join(dir, "occupied")
	if err := os.Mkdir(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		path string
		ir   bool
	}{
		"parent is a file":                {filepath.Join(file, "new.ir.json"), true},
		"destination is a directory":      {occupied, true},
		"evidence parent is missing":      {filepath.Join(dir, "missing", "evidence.json"), false},
		"evidence destination is a dir":   {occupied, false},
		"evidence parent is a plain file": {filepath.Join(file, "evidence.json"), false},
	} {
		commit, discard, err := stageFile(c.path, []byte("data"), c.ir)
		discard()
		if err == nil || commit != nil {
			t.Errorf("%s: staged", name)
		}
	}
	if readText(t, file) != "x" || len(leftovers(t, dir)) != 0 || len(leftovers(t, occupied)) != 0 {
		t.Fatal("a failed staging changed something")
	}
}

// The report and the diagnostics file are staged together: when the second
// cannot be written, the first is not published either.
func TestStageValidationIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	writeText(t, file, "x")
	report := filepath.Join(dir, "program.validation.json")
	writeText(t, report, "previous report")
	result := &ValidationResult{ReportJSON: []byte("report"), DiagnosticsJSON: []byte("diagnostics")}
	options := &ValidationOptions{Report: report, Diagnostics: filepath.Join(file, "diagnostics.json")}
	commit, discard, err := stageValidation(options, result)
	discard()
	if err == nil {
		t.Fatal("unwritable diagnostics destination staged")
	}
	_ = commit
	if readText(t, report) != "previous report" || len(leftovers(t, dir)) != 0 {
		t.Fatal("the report was published or its staging file was left behind")
	}

	options.Diagnostics = filepath.Join(dir, "diagnostics.json")
	commit, discard, err = stageValidation(options, result)
	if err != nil {
		t.Fatal(err)
	}
	if readText(t, report) != "previous report" || exists(options.Diagnostics) {
		t.Fatal("staging published")
	}
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	discard()
	if readText(t, report) != "report" || readText(t, options.Diagnostics) != "diagnostics" || len(leftovers(t, dir)) != 0 {
		t.Fatal("commit did not publish both files cleanly")
	}

	// No validation requested: nothing to stage, commit and discard are no-ops.
	commit, discard, err = stageValidation(nil, nil)
	if err != nil || commit() != nil {
		t.Fatal("absent validation is not a no-op")
	}
	discard()
}

// With --replace the previous package is gone once the swap has happened, so
// an IR destination that cannot be written must stop the run before it.
func TestReplaceKeepsThePreviousPackageWhenTheIRCannotBeWritten(t *testing.T) {
	requireModules(t)
	f := newLeanFixture(t)
	f.write("Example.lean", "namespace Example\ndef run (x : UInt32) : UInt32 := x + 1\nend Example\n")
	generate := func(mode, ir string) error {
		_, err := Generate(GenerateOptions{SourceDir: f.source, Module: "Example", Roots: []string{"Example.run"}, Out: f.pkg,
			IROutput: ir, Cache: testCache(t), Emit: EmitOptions{Mode: mode}, Replace: true})
		return err
	}
	if err := generate("metered", f.ir); err != nil {
		t.Fatal(err)
	}
	before := readText(t, filepath.Join(f.pkg, "manifest.json"))
	blocker := filepath.Join(f.dir, "afile")
	writeText(t, blocker, "x")
	// The native package differs from the metered one, so this run would replace it.
	if err := generate("native", filepath.Join(blocker, "new.ir.json")); err == nil {
		t.Fatal("generation succeeded with an unwritable IR destination")
	}
	if readText(t, filepath.Join(f.pkg, "manifest.json")) != before {
		t.Fatal("the previous package was replaced although the run failed")
	}
	if !exists(filepath.Join(f.pkg, "control.go")) || exists(filepath.Join(f.pkg, "functions.go")) {
		t.Fatal("the package directory no longer holds the metered package")
	}
	// The same run with a writable destination replaces the package.
	if err := generate("native", f.ir); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(f.pkg, "functions.go")) || exists(filepath.Join(f.pkg, "control.go")) {
		t.Fatal("replacement did not happen")
	}
}
