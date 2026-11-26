package pipeline

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/franchb/lean-go/internal/leanversion"
	"github.com/franchb/lean-go/producer"
)

//go:embed *.go
var pipelineSources embed.FS

// Provenance keys naming the Go tool itself. Extraction records the pipeline
// digest; proof evidence records both.
const (
	ToolInput      = "tool/lean-go/pipeline"
	ProofToolInput = "tool/lean-go/proofcheck"
)

// SourceDigest identifies the extraction pipeline: this package's non-test Go
// sources except the proof checker (proof_*.go), together with the shared
// compiler policy. Files are ordered by name and framed with their byte
// lengths, in the style of internal/moduleir.SourceDigest.
func SourceDigest() string { return sourceDigest(false) }

// ProofSourceDigest identifies the proof checker sources (proof_*.go).
func ProofSourceDigest() string { return sourceDigest(true) }

func sourceDigest(proofs bool) string {
	h := sha256.New()
	leanversion.HashSource(h)
	entries, err := pipelineSources.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_test.go") || strings.HasPrefix(entry.Name(), "proof_") != proofs {
			continue
		}
		data, err := pipelineSources.ReadFile(entry.Name())
		if err != nil {
			panic(err)
		}
		name := "internal/pipeline/" + entry.Name()
		fmt.Fprintf(h, "%d:%s%d:", len(name), name, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Digest is the lowercase SHA-256 of data.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ProducerInventory returns the trusted tool half of a module IR's inputs:
// every embedded module producer file under its checkout path
// ("producer/LeanGo/Module/X.lean") and the pipeline source digest.
// It never contains machine paths.
func ProducerInventory() map[string]string {
	out := map[string]string{ToolInput: SourceDigest()}
	for name, data := range producer.ModuleFiles() {
		out["producer/"+name] = Digest(data)
	}
	return out
}

// InputInventory is the complete IR inputs map for a source snapshot.
func InputInventory(sources Snapshot) map[string]string {
	out := ProducerInventory()
	for _, source := range sources {
		out["source/"+source.Relative] = Digest(source.Data)
	}
	return out
}
