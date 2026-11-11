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

func TestCompileNumericSchema(t *testing.T) {
	data, err := os.ReadFile("../../testdata/choice.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var p moduleir.Program
	if err = json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	p.Schema, p.Profile, p.Module, p.Root = "lean-go-ir/v3", "pure-module/v2", "Literal", "Literal.answer"
	p.API = nil
	p.Types = []moduleir.TypeDecl{}
	p.Provenance = []moduleir.Provenance{{Name: p.Root, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}}}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{}, Result: "UInt32", Body: &moduleir.Code{
		Op: "let", Bind: &moduleir.Param{ID: 1, Type: "UInt32"}, Value: &moduleir.Value{Kind: "uint32", Name: "4294967295", Args: []int{}},
		Next: &moduleir.Code{Op: "return", Var: 1},
	}}}
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(data))
	if err != nil || len(files) != 4 {
		t.Fatalf("v3 dispatch: files=%d err=%v", len(files), err)
	}
	p.Schema, p.Profile = "lean-go-ir/v2", "pure-module/v1"
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = compile(bytes.NewReader(data)); err == nil {
		t.Fatal("older profile accepted a numeric literal")
	}
}

func TestCompileAPISchema(t *testing.T) {
	data, err := os.ReadFile("../../testdata/choice.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var p moduleir.Program
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	p.Schema, p.Profile = moduleir.SchemaV5, moduleir.ProfileV3
	p.API = &moduleir.APIMetadata{Records: map[string][]string{}}
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(data))
	if err != nil || len(files["schema.go"]) == 0 {
		t.Fatalf("v5 dispatch: files=%d err=%v", len(files), err)
	}
	p.API = nil
	data, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compile(bytes.NewReader(data)); err == nil {
		t.Fatal("v5 dispatch accepted missing API metadata")
	}
}

func TestCompileJoinSchema(t *testing.T) {
	p := moduleir.Program{
		Schema: moduleir.SchemaV4, Profile: moduleir.ProfileV3, Stage: moduleir.Stage,
		LeanVersion: moduleir.LeanVersion, LeanCommit: moduleir.LeanCommit, Module: "Joins", Root: "Joins.answer",
		Inputs: map[string]string{"Joins.lean": strings.Repeat("0", 64)}, Types: []moduleir.TypeDecl{}, Claims: []moduleir.Claim{}, Rewrites: []moduleir.Rewrite{},
		Provenance: []moduleir.Provenance{{Name: "Joins.answer", Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}}},
		Functions: []moduleir.Function{{Name: "Joins.answer", Params: []moduleir.Param{{ID: 1, Type: "UInt32"}}, Result: "UInt32", Body: &moduleir.Code{
			Op: "join", Join: &moduleir.Join{ID: 2, Params: []moduleir.Param{}, Body: &moduleir.Code{Op: "return", Var: 1}}, Next: &moduleir.Code{Op: "jump", Target: 2, Args: []int{}},
		}}},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(data))
	if err != nil || len(files) != 4 {
		t.Fatalf("v4 dispatch: files=%d err=%v", len(files), err)
	}
	for _, pair := range [][2]string{{moduleir.Schema, moduleir.Profile}, {moduleir.SchemaV3, moduleir.ProfileV2}, {moduleir.SchemaV4, moduleir.ProfileV2}} {
		p.Schema, p.Profile = pair[0], pair[1]
		data, err = json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compile(bytes.NewReader(data)); err == nil {
			t.Fatal("older/mismatched profile admitted joins", pair)
		}
	}
}

func TestCompileSchemaDispatch(t *testing.T) {
	data, err := os.ReadFile("../../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	files, err := compile(bytes.NewReader(data))
	if err != nil || len(files) != 3 {
		t.Fatalf("v1 dispatch: files=%d err=%v", len(files), err)
	}
	for _, data := range [][]byte{
		[]byte(`{"schema":"lean-go-ir/v99"}`),
		[]byte(`{"schema":"lean-go-ir/v2","schema":"lean-go-ir/v1"}`),
		[]byte(`{"schema":"lean-go-ir/v2"} {}`),
		bytes.Repeat([]byte(" "), (4<<20)+1),
	} {
		if _, err := compile(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid envelope accepted")
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
