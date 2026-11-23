package integration_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/ir"
	"github.com/franchb/lean-go/internal/leanversion"
	"github.com/franchb/lean-go/internal/moduleir"
)

// The tracked artifacts are produced by stable Lean. The rc2 oracle changes only
// the compiler pair and the manifest's digest of the corresponding typed IR.
func checkRegeneration(t *testing.T, name string, data []byte, files map[string]string) {
	t.Helper()
	tracked, err := os.ReadFile(filepath.Join("..", "testdata", name+".ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	expectedIR := string(tracked)
	expectedFiles := directoryFiles(t, filepath.Join("..", "generated", name))
	switch selected := os.Getenv("LEAN_GO_TOOLCHAIN"); selected {
	case "", leanversion.Version:
	case "4.36.0-rc2":
		expectedIR = rc2Identity(t, expectedIR, ":")
		manifest := rc2Identity(t, expectedFiles["manifest.json"], ": ")
		expectedFiles["manifest.json"] = replaceJSONField(t, manifest, "ir_sha256", ": ",
			fixtureIRDigest(t, name, tracked), fixtureIRDigest(t, name, []byte(expectedIR)))
	default:
		t.Fatalf("unsupported regeneration toolchain %q", selected)
	}
	if string(data) != expectedIR {
		t.Fatalf("%s IR differs from the fixture for the selected compiler", name)
	}
	sameFiles(t, files, expectedFiles)
}

func rc2Identity(t *testing.T, document, separator string) string {
	t.Helper()
	document = replaceJSONField(t, document, "lean_version", separator, leanversion.Version, "4.36.0-rc2")
	return replaceJSONField(t, document, "lean_commit", separator, leanversion.Commit,
		"b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a")
}

// Preserve formatting and every unrelated byte of the original fixture.
func replaceJSONField(t *testing.T, document, key, separator, old, next string) string {
	t.Helper()
	prefix := fmt.Sprintf("%q%s", key, separator)
	before := fmt.Sprintf("%s%q", prefix, old)
	if strings.Count(document, before) != 1 {
		t.Fatalf("expected one %s field bound to %s in the tracked fixture", key, old)
	}
	return strings.Replace(document, before, fmt.Sprintf("%s%q", prefix, next), 1)
}

func fixtureIRDigest(t *testing.T, name string, wire []byte) string {
	t.Helper()
	var program any = new(moduleir.Program)
	if name == "normalizer" {
		program = new(ir.Program)
	}
	if err := json.Unmarshal(wire, program); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical))
}
