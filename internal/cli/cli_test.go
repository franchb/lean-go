package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGeneratePreflightIsOneJSONReport(t *testing.T) {
	for args, message := range map[string]string{
		"--module Example --diagnostics json":                             "together",
		"--source-dir s --module M --root M.r --diagnostics=json":         "explicit",
		"--diagnostics json --source-dir s --module M --root M.r --out o": "explicit",
	} {
		var stdout, stderr bytes.Buffer
		if code := Main(append([]string{"generate"}, strings.Fields(args)...), &stdout, &stderr); code != 1 {
			t.Fatalf("%s: exit %d", args, code)
		}
		var report struct {
			Schema      string `json:"schema"`
			Diagnostics []struct{ Stage, Code, Message string }
		}
		if err := json.Unmarshal(stderr.Bytes(), &report); err != nil || stdout.Len() != 0 {
			t.Fatalf("%s: stderr %q stdout %q", args, stderr.String(), stdout.String())
		}
		item := report.Diagnostics[0]
		if report.Schema != "lean-go-diagnostics/v1" || item.Stage != "input" || item.Code != "invalid_input" || !strings.Contains(item.Message, message) {
			t.Fatalf("%s: %+v", args, report)
		}
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"generate", "--bogus"}, {"generate", "--mode", "fast"}, {"generate", "--root"},
		{"check-proofs", "--contract", "c", "--manifest", "m"},
		{"check-proofs", "--contract", "c", "--manifest", "m", "--output", "o", "--evidence", "e"},
		{"check-proofs", "--contract", "c", "--component-manifest", "only-name", "--output", "o"},
		{"emit", "--in", "x"}, {"frobnicate"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Main(args, &stdout, &stderr); code != 2 {
			t.Errorf("%q: exit %d, stderr %s", args, code, stderr.String())
		}
	}
}

func TestHelpAndVersionNeedNoTools(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"--version"}, {"generate", "--help"}, {"check-proofs", "-h"}, {"emit", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := Main(args, &stdout, &stderr); code != 0 || stdout.Len() == 0 {
			t.Errorf("%q: exit %d, stderr %s", args, code, stderr.String())
		}
	}
}

func TestComponentManifestTakesTwoValues(t *testing.T) {
	p, err := parse([]string{"--component-manifest", "a", "x.json", "--component-manifest=b", "--contract=c"}, proofOptions)
	if err == nil {
		t.Fatal("an inline value for a two-value option was accepted")
	}
	p, err = parse([]string{"--component-manifest", "a", "x.json", "--component-manifest", "b", "y.json", "--contract=c"}, proofOptions)
	if err != nil || len(p.pairs("component-manifest")) != 2 || p.one("contract") != "c" {
		t.Fatalf("%v %v", p, err)
	}
}
