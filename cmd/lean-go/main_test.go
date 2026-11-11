package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
