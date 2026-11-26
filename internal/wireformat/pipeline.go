package wireformat

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runPipeline is the only place that calls the existing lean-go tooling. It
// extracts the runtime Lean in dir into dir/internal/extracted (recording
// the IR next to it), then checks the proof contract against the fresh
// manifest and writes dir/proof-evidence.json.
//
// Today both steps shell out to the checkout's Python entry points. When the
// CLI or schema identifiers change, only this function needs to follow.
func runPipeline(repo, dir string, f *Format) error {
	repo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	at := func(rel string) string { return filepath.Join(dir, rel) }
	generate := []string{"scripts/generate.py", "--source-dir", at("source"), "--module", f.Module}
	for _, root := range f.Roots() {
		generate = append(generate, "--root", root)
	}
	generate = append(generate, "--out", at(f.ExtractedDir()), "--ir-output", at(f.IRPath()))
	check := []string{"scripts/check_proofs.py", "--contract", at(f.ContractFile()),
		"--manifest", at(filepath.Join(f.ExtractedDir(), "manifest.json")), "--output", at(f.EvidencePath())}
	for _, args := range [][]string{generate, check} {
		cmd := exec.Command("uv", append([]string{"run", "--locked", "python"}, args...)...)
		cmd.Dir = repo
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w\n%s", args[0], err, strings.TrimSpace(out.String()))
		}
	}
	return nil
}

// GenerateAll renders a format into a fresh directory, runs the pipeline
// there, and copies every resulting file into out. Files of out that the
// generator does not produce are left alone.
func GenerateAll(repo string, f *Format, out string) error {
	tmp, err := os.MkdirTemp("", "lean-go-format-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := Render(repo, f, tmp); err != nil {
		return err
	}
	return filepath.WalkDir(tmp, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(tmp, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// Render writes generated sources into the empty directory dir and runs the
// pipeline there.
func Render(repo string, f *Format, dir string) error {
	if err := WriteFiles(dir, f.Files()); err != nil {
		return err
	}
	return runPipeline(repo, dir, f)
}
