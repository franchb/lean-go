package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func multiRootFixture() *moduleir.Program {
	p := &moduleir.Program{
		Schema: moduleir.SchemaV7, Profile: moduleir.ProfileV5, Stage: moduleir.Stage,
		LeanVersion: moduleir.LeanVersion, LeanCommit: moduleir.LeanCommit,
		Module: "Exports", Roots: []string{"Exports.first", "Exports.second"},
		Inputs: map[string]string{"Exports.lean": strings.Repeat("0", 64)}, Types: []moduleir.TypeDecl{},
		Claims: []moduleir.Claim{}, Rewrites: []moduleir.Rewrite{},
		API: &moduleir.APIMetadata{Records: map[string][]string{}},
	}
	for _, name := range p.Roots {
		p.Functions = append(p.Functions, moduleir.Function{Name: name, Params: []moduleir.Param{{ID: 1, Type: "UInt64"}}, Result: "UInt64", Body: &moduleir.Code{Op: "return", Var: 1}})
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: name, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	return p
}

func TestCompileMultiRootSchema(t *testing.T) {
	raw, err := json.Marshal(multiRootFixture())
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(raw))
	if err != nil {
		t.Fatal("multi-root envelope dispatch", err)
	}
	if len(files) != 5 || !bytes.Contains(files["api.go"], []byte("func RunFirst(")) || !bytes.Contains(files["api.go"], []byte("func RunSecond(")) {
		t.Fatal("both typed roots must be emitted")
	}
	if !bytes.Contains(files["runtime.go"], []byte("func RunRoot(")) || bytes.Contains(files["api.go"], []byte("func RunTyped(")) {
		t.Fatal("multi-root interface must not choose an implicit default")
	}
}

func TestMalformedSecondRootDoesNotPublish(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.json"), filepath.Join(dir, "extracted")
	p := multiRootFixture()
	p.Roots[1] = "Exports.unknown"
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-in", input, "-out", output}); err == nil {
		t.Fatal("accepted nonexistent secondary root")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed multi-root validation published output", err)
	}
}
