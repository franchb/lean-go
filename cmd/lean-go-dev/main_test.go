package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Budgets and timeouts on the command line must be whole numbers: booleans,
// fractions and non-finite values are refused.

func TestDifferentialDefaultsAndRepeatedSelections(t *testing.T) {
	o, _, err := parseDifferential(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Seeds != nil || o.Widths != nil || o.Depth != 3 || o.Cases != 32 || o.ShrinkAttempts != 20 || o.Timeout != 300*time.Second ||
		o.Artifacts != ".differential" || o.Shrink {
		t.Fatalf("defaults %+v", o)
	}
	o, _, err = parseDifferential([]string{"--seed", "0", "--seed", "1", "--width", "8", "--width", "64", "--cases", "32",
		"--shrink", "--shrink-attempts", "12", "--timeout", "60", "--artifacts", "x", "--seed=18446744073709551615"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Seeds, []uint64{0, 1, 18446744073709551615}) || !reflect.DeepEqual(o.Widths, []int{8, 64}) ||
		!o.Shrink || o.ShrinkAttempts != 12 || o.Timeout != time.Minute || o.Artifacts != "x" {
		t.Fatalf("parsed %+v", o)
	}
}

func TestDifferentialRejectsNonIntegerAndOutOfRangeValues(t *testing.T) {
	for _, args := range [][]string{
		{"--shrink-attempts", "-1"}, {"--shrink-attempts", "1001"}, {"--shrink-attempts", "true"}, {"--shrink-attempts", "1.0"},
		{"--timeout", "0"}, {"--timeout", "nan"}, {"--timeout", "inf"}, {"--timeout", "true"}, {"--timeout", "3601"},
		{"--cases", "0"}, {"--cases", "4097"}, {"--depth", "5"}, {"--width", "24"}, {"--width", "128"},
		{"--seed", "-1"}, {"--seed", "18446744073709551616"}, {"--seed", "1.0"}, {"extra"},
	} {
		if _, _, err := parseDifferential(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestReplayWithSelectionFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"differential", "--replay", "unused.json", "--seed", "1", "--artifacts", t.TempDir()}, &stdout, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "combined") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
}

func TestCompatibilityRejectsInvalidTimeoutsAndMissingArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--baseline-bin", "/b", "--artifacts", "/a", "--timeout", "nan"},
		{"--baseline-bin", "/b", "--artifacts", "/a", "--timeout", "inf"},
		{"--baseline-bin", "/b", "--artifacts", "/a", "--timeout", "0"},
		{"--baseline-bin", "/b", "--artifacts", "/a", "--timeout", "-1"},
		{"--baseline-bin", "/b", "--artifacts", "/a", "--timeout", "true"},
		{"--artifacts", "/a"}, {"--baseline-bin", "/b"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"compatibility"}, args...), &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func TestHelpSaysThisIsNotTheInstalledCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "not part of the\nlean-go command users install") {
		t.Fatalf("exit %d: %s", code, stdout.String())
	}
	for _, command := range []string{"differential", "widths", "bytes", "compatibility", "demos", "installed-cli", "release-candidate"} {
		stderr.Reset()
		if code := run([]string{command, "-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "not part of the installed lean-go command") {
			t.Errorf("%s: exit %d: %s", command, code, stderr.String())
		}
	}
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Errorf("unknown command: exit %d", code)
	}
}
