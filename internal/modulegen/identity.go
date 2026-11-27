package modulegen

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/nativegen"
)

//go:embed *.go templates/*
var sourceFiles embed.FS

// SourceDigest identifies generator source and templates, excluding tests,
// and the native emitter whose output the fast path embeds.
func SourceDigest() string {
	var names []string
	fs.WalkDir(sourceFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !strings.HasSuffix(path, "_test.go") {
			names = append(names, path)
		}
		return nil
	})
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		data, _ := sourceFiles.ReadFile(name)
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	// Metered packages embed native code for the fast path.
	h.Write([]byte("nativegen\x00" + nativegen.SourceDigest() + "\x00"))
	return hex.EncodeToString(h.Sum(nil))
}
