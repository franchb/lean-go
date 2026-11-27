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
// the IR next to it) in native mode, checks that the extracted Go API is the
// one lean.go calls, then checks the proof contract against the fresh
// manifest and writes dir/proof-evidence.json. Every step runs in-process.
func runPipeline(dir string, f *Format, opts Options) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	at := func(rel string) string { return filepath.Join(dir, rel) }
	result, err := pipeline.Generate(pipeline.GenerateOptions{
		SourceDir: at("source"), Module: f.Module, Roots: f.Roots(),
		Out: at(f.ExtractedDir()), IROutput: at(f.IRPath()), Emit: pipeline.EmitOptions{Mode: "native"},
		Cache: opts.Cache, StrictGo: opts.StrictGo,
	})
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}
	if err := checkNativeAPI(f, result.Files["manifest.json"]); err != nil {
		return err
	}
	if _, err := pipeline.CheckProofs(pipeline.CheckProofsOptions{
		Contract: at(f.ContractFile()), Manifest: at(filepath.Join(f.ExtractedDir(), "manifest.json")),
		Output: at(f.EvidencePath()),
	}); err != nil {
		return fmt.Errorf("proof check failed: %w", err)
	}
	return nil
}

// Options configures extraction: the producer build cache (nil disables it)
// and whether the binary must have been built with the pinned Go release.
type Options struct {
	Cache    *pipeline.Cache
	StrictGo bool
}

// GenerateAll renders a format into a fresh directory, runs the pipeline
// there, and copies every resulting file into out. The extracted package
// directory is replaced as a whole (it must be a previous lean-go package),
// so files of an earlier emission mode do not survive; other files of out
// that the generator does not produce are left alone.
func GenerateAll(f *Format, out string, opts Options) error {
	tmp, err := os.MkdirTemp("", "lean-go-format-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := Render(f, tmp, opts); err != nil {
		return err
	}
	extracted := map[string][]byte{}
	err = filepath.WalkDir(tmp, func(path string, d fs.DirEntry, err error) error {
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
		if filepath.Dir(rel) == f.ExtractedDir() {
			extracted[filepath.Base(rel)] = data
			return nil
		}
		target := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return err
	}
	target := filepath.Join(out, f.ExtractedDir())
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return pipeline.PublishPackage(target, extracted, true)
}

// Render writes generated sources into the empty directory dir and runs the
// pipeline there.
func Render(f *Format, dir string, opts Options) error {
	if err := WriteFiles(dir, f.Files()); err != nil {
		return err
	}
	return runPipeline(dir, f, opts)
}
