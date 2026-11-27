package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

// A plain temporary directory is link-free. On Windows runners its path uses
// an 8.3 short name, which differs from its EvalSymlinks form without any link.
func TestSymlinkFreeAcceptsPlainAndMissingPaths(t *testing.T) {
	dir, err := absolutePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !SymlinkFree(dir) {
		t.Fatalf("plain directory %s reported as containing a link", dir)
	}
	if !SymlinkFree(filepath.Join(dir, "missing", "deeper", "file.json")) {
		t.Fatal("a missing tail below a plain directory reported as containing a link")
	}
	// The snapshot must get past the link check and report the real problem.
	_, err = SourceSnapshot(dir)
	expectError(t, err, "no Lean modules")
}

func TestSymlinkFreeRejectsLinkedComponents(t *testing.T) {
	dir, err := absolutePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, target, link)
	for _, path := range []string{link, filepath.Join(link, "below"), filepath.Join(link, "missing", "file")} {
		if SymlinkFree(path) {
			t.Fatalf("%s goes through a symbolic link but was reported link-free", path)
		}
	}
}
