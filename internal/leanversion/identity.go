// Package leanversion identifies the compiler releases admitted by the IR validators.
package leanversion

import (
	_ "embed"
	"fmt"
	"hash"
)

const (
	Version = "4.36.0"
	Commit  = "611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9"
)

func Supported(version, commit string) bool {
	return version == Version && commit == Commit ||
		version == "4.36.0-rc2" && commit == "b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a"
}

//go:embed identity.go
var source string

// HashSource binds the shared compiler policy into each validator's source digest.
func HashSource(h hash.Hash) {
	const name = "internal/leanversion/identity.go"
	fmt.Fprintf(h, "%d:%s%d:%s", len(name), name, len(source), source)
}
