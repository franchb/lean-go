package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

// declare sets the exact feature list a valid program uses, as the producer does.
func declare(p *moduleir.Program) *moduleir.Program {
	if features, err := moduleir.UsedFeatures(p); err == nil {
		p.Features = features
	}
	return p
}

func TestCompileNumericSchema(t *testing.T) {
	data, err := os.ReadFile("../../testdata/choice.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var p moduleir.Program
	if err = json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	p.Module, p.Roots = "Literal", []string{"Literal.answer"}
	p.Types = []moduleir.TypeDecl{}
	p.Provenance = []moduleir.Provenance{{Name: p.Roots[0], Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}}}
	p.Functions = []moduleir.Function{{Name: p.Roots[0], Params: []moduleir.Param{}, Result: "UInt32", Body: &moduleir.Code{
		Op: "let", Bind: &moduleir.Param{ID: 1, Type: "UInt32"}, Value: &moduleir.Value{Kind: "uint32", Name: "4294967295", Args: []int{}},
		Next: &moduleir.Code{Op: "return", Var: 1},
	}}}
	data, err = json.Marshal(declare(&p))
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(data))
	if err != nil || len(files) != 5 {
		t.Fatalf("numeric dispatch: files=%d err=%v", len(files), err)
	}
	p.API = nil
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compile(bytes.NewReader(data)); err == nil {
		t.Fatal("dispatch accepted missing API metadata")
	}
}

func TestCompileJoinSchema(t *testing.T) {
	p := moduleir.Program{
		Schema: moduleir.Schema, Profile: moduleir.Profile, Stage: moduleir.Stage,
		LeanVersion: moduleir.LeanVersion, LeanCommit: moduleir.LeanCommit, Module: "Joins", Roots: []string{"Joins.answer"},
		Inputs: map[string]string{"Joins.lean": strings.Repeat("0", 64)}, Types: []moduleir.TypeDecl{}, Claims: []moduleir.Claim{}, Rewrites: []moduleir.Rewrite{},
		API:        &moduleir.APIMetadata{Records: map[string][]string{}},
		Provenance: []moduleir.Provenance{{Name: "Joins.answer", Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}}},
		Functions: []moduleir.Function{{Name: "Joins.answer", Params: []moduleir.Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32", Body: &moduleir.Code{
			Op: "join", Join: &moduleir.Join{ID: 2, Params: []moduleir.Param{}, Body: &moduleir.Code{Op: "return", Var: 1}}, Next: &moduleir.Code{Op: "jump", Target: 2, Args: []int{}},
		}}},
	}
	data, err := json.Marshal(declare(&p))
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(data))
	if err != nil || len(files) != 5 {
		t.Fatalf("join dispatch: files=%d err=%v", len(files), err)
	}
	p.Features = []string{moduleir.FeatureUInt32}
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compile(bytes.NewReader(data)); err == nil {
		t.Fatal("undeclared join feature accepted")
	}
}

func TestCompileSchemaDispatch(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"schema":"lean-go-ir/v99"}`),
		[]byte(`{"schema":"lean-go-ir/v24","schema":"lean-go-ir/v1"}`),
		[]byte(`{"schema":"lean-go-ir/v24"} {}`),
		bytes.Repeat([]byte(" "), (4<<20)+1),
	} {
		if _, err := compile(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	for n := 1; n < 26; n++ {
		if n == 24 {
			continue
		}
		schema := fmt.Sprintf("lean-go-ir/v%d", n)
		_, err := compile(strings.NewReader(`{"schema":"` + schema + `","root":"Example.root"}`))
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%q", schema)) || !strings.Contains(err.Error(), "regenerate") {
			t.Fatalf("stale schema %s: %v", schema, err)
		}
	}
}

func TestRunRejectsIncompleteAndMalformed(t *testing.T) {
	for _, args := range [][]string{nil, {"-in", "missing"}, {"-out", "missing"}, {"-unknown"}, {"-in", "missing", "-out", "unused", "extra"}} {
		if err := run(args); err == nil {
			t.Fatalf("accepted flags %v", args)
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "bad.json")
	destination := filepath.Join(dir, "prior")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "keep")
	if err := os.WriteFile(sentinel, []byte("previous output"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"schema":"unsupported"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-in", source, "-out", destination}); err == nil {
		t.Fatal("malformed IR accepted")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "previous output" {
		t.Fatal("failed generation changed output", err)
	}
}

func TestCompileUInt64Schema(t *testing.T) {
	p := moduleir.Program{Schema: moduleir.Schema, Profile: moduleir.Profile, Stage: moduleir.Stage, LeanVersion: moduleir.LeanVersion, LeanCommit: moduleir.LeanCommit,
		Module: "Wide", Roots: []string{"Wide.answer"}, Inputs: map[string]string{"Wide.lean": strings.Repeat("0", 64)}, Types: []moduleir.TypeDecl{}, Claims: []moduleir.Claim{}, Rewrites: []moduleir.Rewrite{}, API: &moduleir.APIMetadata{Records: map[string][]string{}},
		Provenance: []moduleir.Provenance{{Name: "Wide.answer", Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}}},
		Functions:  []moduleir.Function{{Name: "Wide.answer", Params: []moduleir.Param{}, Result: "UInt64", Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 1, Type: "UInt64"}, Value: &moduleir.Value{Kind: "uint64", Name: "18446744073709551615", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 1}}}},
	}
	raw, err := json.Marshal(declare(&p))
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(raw))
	if err != nil || len(files) != 5 || !bytes.Contains(files["api.go"], []byte("RunTyped(l Limits) (uint64, error)")) {
		t.Fatal("UInt64 CLI dispatch", err)
	}
	p.API = nil
	raw, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = compile(bytes.NewReader(raw)); err == nil {
		t.Fatal("accepted absent API")
	}
}
