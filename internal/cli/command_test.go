package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

// Ported from scripts/test_cli.py (command boundary), the script-boundary
// cases of scripts/test_diagnostics.py and the mode cases of
// scripts/test_native_mode.py.

func runMain(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Main(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func captureGenerate(t *testing.T, result *pipeline.GenerateResult, err error) *pipeline.GenerateOptions {
	t.Helper()
	seen := new(pipeline.GenerateOptions)
	old := generateFunc
	generateFunc = func(opts pipeline.GenerateOptions) (*pipeline.GenerateResult, error) {
		*seen = opts
		return result, err
	}
	t.Cleanup(func() { generateFunc = old })
	return seen
}

func TestGeneratePreservesRootsAndPaths(t *testing.T) {
	seen := captureGenerate(t, &pipeline.GenerateResult{Out: "o", IROutput: "i"}, nil)
	code, _, stderr := runMain("generate", "--source-dir", "source with spaces", "--module", "Example", "--root", "Example.one",
		"--root", "Example.two", "--out", "out space", "--ir-output", "ir space.json")
	if code != 0 {
		t.Fatal(stderr)
	}
	if seen.SourceDir != "source with spaces" || seen.Out != "out space" || seen.IROutput != "ir space.json" ||
		!slices.Equal(seen.Roots, []string{"Example.one", "Example.two"}) || seen.Module != "Example" {
		t.Fatalf("%+v", seen)
	}
	if seen.Emit.Mode != "" || seen.Replace || seen.StrictGo || seen.Cache == nil && os.Getenv("LEAN_GO_NO_CACHE") != "1" {
		t.Fatalf("defaults %+v", seen)
	}
	runMain("generate", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i", "--mode", "native", "--replace", "--no-cache", "--strict-go")
	if seen.Emit.Mode != "native" || !seen.Replace || seen.Cache != nil || !seen.StrictGo {
		t.Fatalf("flags %+v", seen)
	}
}

func TestRelativePathsBelongToTheWorkingDirectory(t *testing.T) {
	wd, _ := os.Getwd()
	source := filepath.Join(t.TempDir(), "source")
	os.Mkdir(source, 0o755)
	os.WriteFile(filepath.Join(source, "Example.lean"), []byte("def run (x : UInt32) := x\n"), 0o644)
	rel, err := filepath.Rel(wd, source)
	if err != nil {
		t.Skip(err)
	}
	// A ".." path without symlinks is normalized: the snapshot accepts it.
	snapshot, err := pipeline.SourceSnapshot(rel)
	if err != nil || len(snapshot) != 1 {
		t.Fatalf("%s: %v", rel, err)
	}
}

func TestModuleDoesNotGainImplicitOutputs(t *testing.T) {
	code, _, stderr := runMain("generate", "--source-dir", "source", "--module", "Example", "--root", "Example.run")
	if code != 1 || !strings.Contains(stderr, "explicit --out and --ir-output") {
		t.Fatal(code, stderr)
	}
}

func TestProofPathsAndModeAreForwarded(t *testing.T) {
	var seen pipeline.CheckProofsOptions
	old := checkProofsFunc
	checkProofsFunc = func(opts pipeline.CheckProofsOptions) (string, error) { seen = opts; return "checked", nil }
	defer func() { checkProofsFunc = old }()
	for _, mode := range []string{"--output", "--evidence"} {
		code, stdout, _ := runMain("check-proofs", "--contract", "project/contract.json", "--manifest", "generated/manifest.json", mode, "proof evidence.json")
		if code != 0 || stdout != "checked\n" || seen.Contract != "project/contract.json" || seen.Manifest != "generated/manifest.json" {
			t.Fatal(code, seen)
		}
		if (mode == "--output") != (seen.Output == "proof evidence.json" && seen.Evidence == "") {
			t.Fatal(seen)
		}
	}
	runMain("check-proofs", "--contract", "c", "--component-manifest", "a", "x.json", "--component-manifest", "b", "y.json", "--output", "o")
	if !slices.Equal(seen.ComponentManifests, [][2]string{{"a", "x.json"}, {"b", "y.json"}}) || seen.Manifest != "" {
		t.Fatal(seen)
	}
	checkProofsFunc = func(pipeline.CheckProofsOptions) (string, error) { return "", errors.New("evidence is stale") }
	code, _, stderr := runMain("check-proofs", "--contract", "c", "--manifest", "m", "--evidence", "e")
	if code != 1 || stderr != "proof check failed: evidence is stale\n" {
		t.Fatal(code, stderr)
	}
}

func TestSymlinkedSourceIsNotResolvedAndOutputSurvives(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.Mkdir(source, 0o755)
	os.WriteFile(filepath.Join(source, "Example.lean"), []byte("def example : UInt32 := 1\n"), 0o644)
	link := filepath.Join(root, "link")
	if err := os.Symlink(source, link); err != nil {
		t.Skip(err)
	}
	output := filepath.Join(root, "output")
	os.Mkdir(output, 0o755)
	os.WriteFile(filepath.Join(output, "sentinel"), []byte("preserve"), 0o644)
	for _, selected := range []string{link, link + "/../source"} {
		code, _, stderr := runMain("generate", "--source-dir", selected, "--module", "Example", "--root", "example",
			"--out", output, "--ir-output", filepath.Join(root, "ir"))
		if code != 1 || !strings.Contains(stderr, "source directory must exist and must not contain symlinks") {
			t.Fatal(selected, code, stderr)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(output, "sentinel")); string(data) != "preserve" {
		t.Fatal("output changed")
	}
	if _, err := os.Stat(filepath.Join(root, "ir")); err == nil {
		t.Fatal("IR written")
	}
}

func TestSymlinkedContractIsRejectedBeforeTools(t *testing.T) {
	root := t.TempDir()
	contract := filepath.Join(root, "contract.json")
	os.WriteFile(contract, []byte("{}"), 0o644)
	link := filepath.Join(root, "linked.json")
	if err := os.Symlink(contract, link); err != nil {
		t.Skip(err)
	}
	output := filepath.Join(root, "evidence.json")
	os.WriteFile(output, []byte("preserve evidence"), 0o644)
	code, _, stderr := runMain("check-proofs", "--contract", link, "--manifest", filepath.Join(root, "manifest.json"), "--output", output)
	if code != 1 || !strings.Contains(stderr, "symlinked input") {
		t.Fatal(code, stderr)
	}
	if data, _ := os.ReadFile(output); string(data) != "preserve evidence" {
		t.Fatal("evidence changed")
	}
}

func TestRepeatedModesFollowTheLastValue(t *testing.T) {
	for _, pair := range [][2]string{{"json", "text"}, {"text", "json"}} {
		first, last := pair[0], pair[1]
		for _, flags := range [][]string{
			{"--diagnostics=" + first, "--diagnostics=" + last},
			{"--diagnostics=" + first, "--diagnostics", last},
			{"--diagnostics", first, "--diagnostics=" + last},
			{"--diagnostics", first, "--diagnostics", last},
		} {
			code, _, stderr := runMain(append([]string{"generate", "--module", "Example"}, flags...)...)
			if code != 1 {
				t.Fatal(flags, code)
			}
			if last == "json" {
				var report map[string]any
				if json.Unmarshal([]byte(stderr), &report) != nil {
					t.Fatalf("%v: %q", flags, stderr)
				}
			} else if !strings.HasPrefix(stderr, "input [invalid_input]") {
				t.Fatalf("%v: %q", flags, stderr)
			}
			if pipeline.RequestedMode(flags) != last {
				t.Fatal(flags)
			}
		}
	}
}

func TestUnexpectedErrorsAreJSONBuildFailures(t *testing.T) {
	old := generateFunc
	generateFunc = func(pipeline.GenerateOptions) (*pipeline.GenerateResult, error) { panic("unexpected test error") }
	defer func() { generateFunc = old }()
	code, stdout, stderr := runMain("generate", "--diagnostics=json", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i")
	var report struct {
		Diagnostics []struct{ Stage, Code, Message string }
	}
	if code != 1 || stdout != "" || json.Unmarshal([]byte(stderr), &report) != nil {
		t.Fatal(code, stdout, stderr)
	}
	if item := report.Diagnostics[0]; item.Stage != "build" || item.Code != "pipeline_failed" || item.Message != "unexpected test error" {
		t.Fatal(item)
	}
}

func TestSuccessKeepsStdoutAndJSONStderrEmpty(t *testing.T) {
	captureGenerate(t, &pipeline.GenerateResult{Out: "/out", IROutput: "/ir.json"}, nil)
	code, stdout, stderr := runMain("generate", "--diagnostics=json", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i")
	if code != 0 || stdout != "Generated /out; recorded producer IR in /ir.json\n" || stderr != "" {
		t.Fatal(code, stdout, stderr)
	}
	captureGenerate(t, &pipeline.GenerateResult{Out: "/out", IROutput: "/ir.json", Warning: "lean-go: warning: built with go9"}, nil)
	_, _, stderr = runMain("generate", "--diagnostics=json", "--source-dir", "s", "--module", "M", "--root", "M.r", "--out", "o", "--ir-output", "i")
	if stderr != "lean-go: warning: built with go9\n" {
		t.Fatalf("JSON-mode warning after success: %q", stderr)
	}
}

func TestChildEnvironmentDropsLeanOverrides(t *testing.T) {
	t.Setenv("LEAN_PATH", "bad")
	t.Setenv("LAKE_HOME", "bad")
	t.Setenv("CGO_ENABLED", "1")
	compiler, _ := pipeline.SelectedCompiler()
	env := pipeline.ChildEnvironment(compiler)
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, bad := range []string{"\nLEAN_PATH=", "\nLAKE_HOME=", "\nCGO_ENABLED=1\n"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("environment keeps %q", bad)
		}
	}
	for _, good := range []string{"\nCGO_ENABLED=0\n", "\nGOTOOLCHAIN=local\n", "\nELAN_TOOLCHAIN=" + compiler.Toolchain() + "\n"} {
		if !strings.Contains(joined, good) {
			t.Fatalf("environment lacks %q", good)
		}
	}
}

// scripts/test_diagnostics_lean.py: a JSON failure is one stderr document
// and preserves existing outputs (LEAN_GO_MODULES=1).
func TestScriptJSONFailureIsOneDocumentAndPreservesOutputs(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh Lean diagnostics")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.Mkdir(source, 0o755)
	os.WriteFile(filepath.Join(source, "Example.lean"), []byte("def run {α : Type} (x : α) : α := x\n"), 0o644)
	pkg, ir := filepath.Join(root, "package"), filepath.Join(root, "program.json")
	os.Mkdir(pkg, 0o755)
	os.WriteFile(filepath.Join(pkg, "previous.go"), []byte("previous package"), 0o644)
	os.WriteFile(ir, []byte("previous IR"), 0o644)
	code, stdout, stderr := runMain("generate", "--diagnostics=json", "--source-dir", source, "--module", "Example", "--root", "run", "--out", pkg, "--ir-output", ir)
	var report struct{ Diagnostics []struct{ Code string } }
	if code != 1 || stdout != "" || json.Unmarshal([]byte(stderr), &report) != nil || report.Diagnostics[0].Code != "unsupported_type" {
		t.Fatal(code, stdout, stderr)
	}
	if data, _ := os.ReadFile(ir); string(data) != "previous IR" {
		t.Fatal("IR changed")
	}
	if data, _ := os.ReadFile(filepath.Join(pkg, "previous.go")); string(data) != "previous package" {
		t.Fatal("package changed")
	}
}

// fakeTools puts scripted lean and lake executables first on PATH.
func fakeTools(t *testing.T, lean, lake string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fakes need a POSIX shell")
	}
	dir := t.TempDir()
	for name, output := range map[string]string{"lean": lean, "lake": lake} {
		if output == "" {
			continue
		}
		script := fmt.Sprintf("#!/bin/sh\necho '%s'\n", output)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("LEAN_GO_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
}

func TestDoctorReportsMissingTools(t *testing.T) {
	fakeTools(t, "", "")
	code, stdout, _ := runMain("doctor")
	if code != 1 || !strings.Contains(stdout, "lean is missing from PATH") || !strings.Contains(stdout, "lake is missing from PATH") {
		t.Fatal(code, stdout)
	}
}

func TestDoctorChecksExactToolsAndLakeEnvironment(t *testing.T) {
	compiler, _ := pipeline.SelectedCompiler()
	version := fmt.Sprintf("Lean (version %s, target, commit %s, Release)", compiler.Version, compiler.Commit)
	fakeTools(t, version, version)
	code, stdout, _ := runMain("doctor")
	if code != 0 || !strings.Contains(stdout, "OK Lake environment") || !strings.Contains(stdout, "OK Producer cache") {
		t.Fatal(code, stdout)
	}
	fakeTools(t, "Lean (version 4.36.0-rc2, target, commit wrong, Release)", "Lean (version 4.34.0, target, commit wrong, Release)")
	code, stdout, _ = runMain("doctor")
	if code != 1 || strings.Count(stdout, "FAIL") != 2 {
		t.Fatal(code, stdout)
	}
}
