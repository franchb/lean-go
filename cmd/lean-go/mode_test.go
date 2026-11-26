package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeMode(t *testing.T) {
	out := filepath.Join(t.TempDir(), "adler32")
	in := filepath.Join("..", "..", "examples", "proofs", "adler32", "adler32.ir.json")
	if err := run([]string{"-in", in, "-out", out, "--mode", "native", "--native-max-depth", "500"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Mode     string `json:"mode"`
		MaxDepth int    `json:"max_depth"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Mode != "native" || manifest.MaxDepth != 500 {
		t.Fatalf("manifest %+v, %v", manifest, err)
	}
	functions, err := os.ReadFile(filepath.Join(out, "functions.go"))
	if err != nil || !strings.Contains(string(functions), "func Update(state State, data []byte) State {") {
		t.Fatalf("native fold missing: %v", err)
	}
}

func TestModeErrors(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join("..", "..", "testdata", "trees.ir.json")
	pilot := filepath.Join("..", "..", "testdata", "normalizer.ir.json")
	for _, args := range [][]string{
		{"-in", module, "-out", filepath.Join(dir, "a"), "-mode", "fast"},
		{"-in", module, "-out", filepath.Join(dir, "b"), "-native-max-depth", "5"},
		{"-in", pilot, "-out", filepath.Join(dir, "c"), "-mode", "native"},
		{"-in", module, "-out", filepath.Join(dir, "d"), "-mode", "native", "-native-max-depth", "-1"},
	} {
		if err := run(args); err == nil {
			t.Fatalf("%v: accepted", args)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected runs wrote output: %v %v", entries, err)
	}
}

func TestMeteredRemainsDefault(t *testing.T) {
	in := filepath.Join("..", "..", "testdata", "trees.ir.json")
	out := filepath.Join(t.TempDir(), "trees")
	if err := run([]string{"-in", in, "-out", out}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want, err := compile(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil || string(got) != string(want["manifest.json"]) || strings.Contains(string(got), `"mode"`) {
		t.Fatalf("default output is not the metered package: %v", err)
	}
}
