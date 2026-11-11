package ir

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
)

//go:embed *.go
var sources embed.FS

// SourceDigest identifies the compiled validator's non-test Go source snapshots.
// Files are ordered by name and framed with their byte lengths.
func SourceDigest() string {
	h := sha256.New()
	entries, err := sources.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := sources.ReadFile(entry.Name())
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(h, "%d:%s%d:", len(entry.Name()), entry.Name(), len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
