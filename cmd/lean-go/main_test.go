package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

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
