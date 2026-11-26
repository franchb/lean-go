package wireformat

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/franchb/lean-go/internal/pipeline"
)

// runPipeline is the only place that calls the lean-go extraction pipeline.
// It extracts the runtime Lean in dir into dir/internal/extracted (recording
// the IR next to it), then checks the proof contract against the fresh
// manifest and writes dir/proof-evidence.json. Both steps run in-process;
// cache selects the producer build cache (nil disables it).
func runPipeline(dir string, f *Format, cache *pipeline.Cache) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	at := func(rel string) string { return filepath.Join(dir, rel) }
	if _, err := pipeline.Generate(pipeline.GenerateOptions{
		SourceDir: at("source"), Module: f.Module, Roots: f.Roots(),
		Out: at(f.ExtractedDir()), IROutput: at(f.IRPath()), Cache: cache,
	}); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}
	if _, err := pipeline.CheckProofs(pipeline.CheckProofsOptions{
		Contract: at(f.ContractFile()), Manifest: at(filepath.Join(f.ExtractedDir(), "manifest.json")),
		Output: at(f.EvidencePath()),
	}); err != nil {
		return fmt.Errorf("proof check failed: %w", err)
	}
	return nil
}

// GenerateAll renders a format into a fresh directory, runs the pipeline
// there, and copies every resulting file into out. Files of out that the
// generator does not produce are left alone.
func GenerateAll(f *Format, out string, cache *pipeline.Cache) error {
	tmp, err := os.MkdirTemp("", "lean-go-format-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := Render(f, tmp, cache); err != nil {
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
func Render(f *Format, dir string, cache *pipeline.Cache) error {
	if err := WriteFiles(dir, f.Files()); err != nil {
		return err
	}
	return runPipeline(dir, f, cache)
}
