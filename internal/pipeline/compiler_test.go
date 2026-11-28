package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Compiler pins, process failures and symlink destinations. Staging of
// published outputs is tested in publish_stage_test.go.

func TestOnlySelectedVersionAndCommitPass(t *testing.T) {
	for _, compiler := range SupportedCompilers {
		t.Setenv("LEAN_GO_TOOLCHAIN", compiler.Version)
		selected, err := SelectedCompiler()
		if err != nil || selected != compiler {
			t.Fatalf("%s: selected %v, %v", compiler.Version, selected, err)
		}
		current := "Lean (version " + compiler.Version + ", test, commit " + compiler.Commit + ", Release)"
		if err := compiler.CheckVersion(current + "\n"); err != nil {
			t.Fatal(err)
		}
		for _, other := range SupportedCompilers {
			if other == compiler {
				continue
			}
			for _, changed := range []string{strings.Replace(current, compiler.Commit, other.Commit, 1),
				strings.Replace(current, compiler.Version, other.Version, 1)} {
				if compiler.CheckVersion(changed) == nil {
					t.Errorf("%s accepted %q", compiler.Version, changed)
				}
			}
		}
		for _, invalid := range []string{current + "\n" + current, "prefix " + current,
			strings.Replace(current, compiler.Commit, compiler.Commit[:8], 1), strings.Replace(current, "Release", "Debug", 1)} {
			if compiler.CheckVersion(invalid) == nil {
				t.Errorf("%s accepted %q", compiler.Version, invalid)
			}
		}
	}
}

func TestDefaultAndUnknownSelections(t *testing.T) {
	t.Setenv("ELAN_TOOLCHAIN", "unrelated")
	t.Setenv("LEAN_GO_TOOLCHAIN", "")
	os.Unsetenv("LEAN_GO_TOOLCHAIN")
	if selected, err := SelectedCompiler(); err != nil || selected.Version != "4.36.0" {
		t.Fatalf("default %v, %v", selected, err)
	}
	for _, version := range []string{"", "stable", "4.36.0-rc1", "4.37.0"} {
		t.Setenv("LEAN_GO_TOOLCHAIN", version)
		if _, err := SelectedCompiler(); err == nil {
			t.Errorf("LEAN_GO_TOOLCHAIN=%q accepted", version)
		}
	}
}

// helperProcess is set in the environment of this test binary when it is
// run as a failing child process by TestFailedProcessHasStderr.
const helperProcess = "LEAN_GO_PIPELINE_TEST_FAILING_CHILD"

func TestFailedProcessHasStderr(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = runProcess([]string{executable}, t.TempDir(), append(os.Environ(), helperProcess+"=1"))
	expectError(t, err, `producer process failed \(9\): precise compiler stderr\ncompiler stdout`)
}

func TestSymlinkDestinationPreserved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeText(t, target, "keep")
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, target, link)
	for _, ir := range []bool{false, true} {
		commit, discard, err := stageFile(link, []byte("new"), ir)
		discard()
		if err == nil || commit != nil {
			t.Fatalf("ir=%t: a symlink destination was staged", ir)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 || readText(t, target) != "keep" {
			t.Fatal("the symlink or its target changed")
		}
	}
}
