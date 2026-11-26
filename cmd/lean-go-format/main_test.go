package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAndSourcesOnly(t *testing.T) {
	spec := "../../examples/formats/dns/spec.json"
	if err := run([]string{"check", "-spec", spec}); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := run([]string{"generate", "-sources-only", "-spec", spec, "-out", out}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "proof-contract.json", "source/DnsHeader.lean",
		"proof/DnsHeaderContract.lean", "proof/DnsHeaderProofs.lean", "dns/dns.go", "dns/lean.go", "dns/dns_gen_test.go"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Error(err)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"check"}, {"generate", "-spec", "x.json"}, {"frobnicate", "-spec", "../../examples/formats/dns/spec.json"}} {
		if err := run(args); err == nil {
			t.Errorf("run(%q) succeeded", args)
		}
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"name": "demo", "fields": [{"name": "a", "type": "uint8", "colour": 1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"check", "-spec", bad}); err == nil || !strings.Contains(err.Error(), `unknown field "colour"`) {
		t.Fatalf("bad spec: %v", err)
	}
}
