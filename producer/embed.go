// Package producer embeds the trusted Lean producer sources so the lean-go
// command can stage them into a private workspace without a checkout.
//
// The Lean files next to this package are the only producer inputs; Lake
// ignores this Go file. Embedding cannot reach parent directories, which is
// why the package lives here.
package producer

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed lean-toolchain LeanGo/Module/*.lean LeanGo/Proofs/Check.lean
var files embed.FS

// The translation-validation library and the #lean_go_validate command are
// embedded separately: they are staged and hashed only when a run requests
// validation, so the extraction inputs of other runs are unchanged.
//
//go:embed LeanGo/Validate.lean LeanGo/Validate/*.lean LeanGo/Validate/Meta/*.lean
var validation embed.FS

// ToolchainFile is the checkout's default compiler pin file.
const ToolchainFile = "lean-toolchain"

// CheckerFile is the proof checker module, relative to the producer directory.
const CheckerFile = "LeanGo/Proofs/Check.lean"

// Read returns one embedded file by its path relative to the producer directory.
func Read(name string) ([]byte, error) { return files.ReadFile(name) }

// ModuleFiles returns every embedded module-extraction producer file, keyed by
// its path relative to the producer directory, for example
// "LeanGo/Module/Export.lean".
func ModuleFiles() map[string][]byte {
	out := map[string][]byte{}
	entries, err := fs.ReadDir(files, "LeanGo/Module")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".lean") {
			continue
		}
		name := "LeanGo/Module/" + entry.Name()
		data, err := files.ReadFile(name)
		if err != nil {
			panic(err)
		}
		out[name] = data
	}
	return out
}

// ModuleNames returns the sorted keys of ModuleFiles.
func ModuleNames() []string {
	var names []string
	for name := range ModuleFiles() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidationFiles returns every embedded translation-validation file, keyed
// by its path relative to the producer directory, for example
// "LeanGo/Validate/Command.lean". These files are not part of ModuleFiles.
func ValidationFiles() map[string][]byte {
	out := map[string][]byte{}
	err := fs.WalkDir(validation, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(name, ".lean") {
			return err
		}
		data, err := validation.ReadFile(name)
		out[name] = data
		return err
	})
	if err != nil {
		panic(err)
	}
	return out
}

// ValidationNames returns the sorted keys of ValidationFiles.
func ValidationNames() []string {
	var names []string
	for name := range ValidationFiles() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
