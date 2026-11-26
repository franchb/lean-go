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
	ToolInput           = "tool/lean-go/pipeline"
	ProofToolInput      = "tool/lean-go/proofcheck"
	ValidationToolInput = "tool/lean-go/validate"
)

// SourceDigest identifies the extraction pipeline: this package's non-test Go
// sources except the proof checker (proof_*.go) and the translation-validation
// runner (validate_*.go), together with the shared compiler policy. Files are
// ordered by name and framed with their byte lengths, in the style of
// internal/moduleir.SourceDigest. The validation runner starts only after the
// IR is complete and cannot change it; validation reports record its digest.
func SourceDigest() string { return sourceDigest("") }

// ProofSourceDigest identifies the proof checker sources (proof_*.go).
func ProofSourceDigest() string { return sourceDigest("proof_") }

// ValidationSourceDigest identifies the validation runner sources (validate_*.go).
func ValidationSourceDigest() string { return sourceDigest("validate_") }

// sourceFamily is the digest family of a non-test pipeline source file.
func sourceFamily(name string) string {
	for _, prefix := range []string{"proof_", "validate_"} {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return ""
}

func sourceDigest(family string) string {
	h := sha256.New()
	leanversion.HashSource(h)
	entries, err := pipelineSources.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_test.go") || sourceFamily(entry.Name()) != family {
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

// ValidationInventory names the translation-validation library a validation
// report depends on: every embedded validation file under its checkout path
// ("producer/LeanGo/Validate/X.lean") and the validation runner digest. It is
// recorded in validation reports only, never in IR inputs or manifests.
func ValidationInventory() map[string]string {
	out := map[string]string{ValidationToolInput: ValidationSourceDigest()}
	for name, data := range producer.ValidationFiles() {
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
