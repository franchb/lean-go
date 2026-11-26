package wireformat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Load reads, decodes and validates a spec file.
func Load(path string) (*Format, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	spec, err := DecodeSpec(data)
	if err != nil {
		return nil, err
	}
	return Build(spec)
}

// Paths of generated files, relative to the format's directory.
func (f *Format) SourcePath() string   { return filepath.Join("source", f.Module+".lean") }
func (f *Format) ContractPath() string { return filepath.Join("proof", f.Module+"Contract.lean") }
func (f *Format) ProofsPath() string   { return filepath.Join("proof", f.Module+"Proofs.lean") }
func (f *Format) ExtractedDir() string { return filepath.Join("internal", "extracted") }
func (f *Format) IRPath() string       { return f.Name + ".ir.json" }
func (f *Format) EvidencePath() string { return "proof-evidence.json" }
func (f *Format) GoModulePath() string { return "example.com/lean-go-format-" + f.Name }
func (f *Format) AdapterDir() string   { return f.Name }
func (f *Format) ContractFile() string { return "proof-contract.json" }
func (f *Format) ExtractedImport() string {
	return f.GoModulePath() + "/internal/extracted"
}

// Files renders every generated source file. Extraction output (the
// extracted package, its IR and the proof evidence) is produced separately
// by the pipeline.
func (f *Format) Files() map[string]string {
	files := map[string]string{
		"go.mod":         fmt.Sprintf("module %s\n\ngo 1.27.4\n", f.GoModulePath()),
		f.ContractFile(): f.ContractJSON(),
		f.SourcePath():   f.RuntimeLean(),
		f.ContractPath(): f.ContractLean(),
		f.ProofsPath():   f.ProofsLean(),
	}
	for name, text := range f.GoFiles() {
		files[filepath.Join(f.AdapterDir(), name)] = text
	}
	return files
}

// WriteFiles writes files below dir, creating directories as needed.
func WriteFiles(dir string, files map[string]string) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if filepath.IsAbs(name) || !filepath.IsLocal(name) {
			return errors.New("refusing non-local output path " + name)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			return err
		}
	}
	return nil
}
