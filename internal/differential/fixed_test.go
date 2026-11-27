package differential

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Ported from scripts/test_width_campaign.py and scripts/test_byte_campaign.py.

func TestWidthBoundaryOracleAndSampleContract(t *testing.T) {
	if got := WidthModel(Row{4294967295, 4294967295, 18446744073709551615}); !reflect.DeepEqual(got,
		[]uint64{4294967295, 4294967295, 18446744073709551615, 4294967295, 4294967295}) {
		t.Errorf("got %v", got)
	}
	if got := WidthModel(Row{2147483648, 1, 4294967296}); !reflect.DeepEqual(got,
		[]uint64{2147483648, 0, 9223372036854775809, 1, 0}) {
		t.Errorf("got %v", got)
	}
	samples := WidthSamples()
	if !reflect.DeepEqual(samples, WidthSamples()) {
		t.Fatal("samples are not deterministic")
	}
	if len(samples) != 206 {
		t.Fatalf("%d samples, want 206", len(samples))
	}
	words := map[uint64]bool{}
	for _, row := range samples {
		if row[0] >= 1<<32 || row[1] >= 1<<32 {
			t.Fatalf("row %v outside UInt32", row)
		}
		words[row[2]] = true
	}
	for _, word := range []uint64{0, 1 << 31, 1<<32 - 1, 1 << 32, 1<<32 + 1, 1 << 63, 1<<64 - 1, 12297829382473034410, 6148914691236517205} {
		if !words[word] {
			t.Errorf("missing word %d", word)
		}
	}
}

func TestByteModelAndSamples(t *testing.T) {
	if got := ByteModel(Row{255, 8, 4294967295}); !reflect.DeepEqual(got,
		[]uint64{7, 247, 248, 255, 8, 255, 247, 0, 255, 0, 0, 0, 255, 255, 255, 1, 7, 255, 255}) {
		t.Errorf("got %v", got)
	}
	samples := ByteSamples()
	if !reflect.DeepEqual(samples, ByteSamples()) {
		t.Fatal("samples are not deterministic")
	}
	for _, row := range samples {
		if row[0] >= 256 || row[1] >= 256 || row[2] >= 1<<32 {
			t.Fatalf("row %v out of range", row)
		}
	}
	if len(samples) != 353 {
		t.Fatalf("%d samples, want 353", len(samples))
	}
}

func runFixed(t *testing.T, name string) *FixedReport {
	t.Helper()
	requireDifferential(t)
	f, err := FixedCampaign(name)
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunFixed(f, checkoutRoot(t), filepath.Join(t.TempDir(), "evidence"), 300*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestWidthNativeRawTypedAndObservedResourceThresholds(t *testing.T) {
	report := runFixed(t, "widths")
	var budgets map[string][2]int
	if err := json.Unmarshal(report.Budgets, &budgets); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"widen": {1, 2}, "narrow": {1, 2}, "low": {1, 2}, "high": {3, 4}, "pack": {5, 6}}
	if report.Status != "passed" || report.Cases != 206 || !reflect.DeepEqual(budgets, want) {
		t.Fatalf("report %+v budgets %v", report, budgets)
	}
}

func TestByteNativeRawTypedAndResourceThresholds(t *testing.T) {
	report := runFixed(t, "bytes")
	var budgets []int
	if err := json.Unmarshal(report.Budgets, &budgets); err != nil {
		t.Fatal(err)
	}
	if report.Status != "passed" || report.Cases != 353 || !reflect.DeepEqual(budgets, []int{17, 18}) {
		t.Fatalf("report %+v budgets %v", report, budgets)
	}
}
