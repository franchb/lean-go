package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Ported from validation/check_ir_copies.py: the programs the Lean
// translation-validation library checks (validation/Examples/IR) are copies
// of tracked IR, so that its evidence stays reproducible while the tracked
// files are regenerated. The two sections its loader reads, types and
// functions, must still match; metadata may differ. When this fails, copy
// the tracked IR again and rerun `lake build` in validation/.
func TestValidationIRCopiesMatchTrackedPrograms(t *testing.T) {
	copies := map[string]string{
		"clamp.ir.json":    "examples/proofs/clamp/clamp.ir.json",
		"flags.ir.json":    "examples/proofs/flags/flags.ir.json",
		"words.ir.json":    "examples/proofs/words/words.ir.json",
		"crc16.ir.json":    "examples/proofs/crc16/crc16.ir.json",
		"adler32.ir.json":  "examples/proofs/adler32/adler32.ir.json",
		"quota.ir.json":    "testdata/quota.ir.json",
		"trees.ir.json":    "testdata/trees.ir.json",
		"counters.ir.json": "testdata/counters.ir.json",
		"records.ir.json":  "testdata/records.ir.json",
		"reduced.ir.json":  "testdata/reduced.ir.json",
		"joins.ir.json":    "testdata/joins.ir.json",
	}
	semantic := func(path string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var program map[string]any
		if err := json.Unmarshal(data, &program); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return map[string]any{"types": program["types"], "functions": program["functions"]}
	}
	entries, err := os.ReadDir("../validation/Examples/IR")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(copies) {
		t.Errorf("validation/Examples/IR holds %d files, %d are listed here", len(entries), len(copies))
	}
	for copied, tracked := range copies {
		if !reflect.DeepEqual(semantic(filepath.Join("../validation/Examples/IR", copied)), semantic(filepath.Join("..", filepath.FromSlash(tracked)))) {
			t.Errorf("%s: differs from validation/Examples/IR/%s", tracked, copied)
		}
	}
}
