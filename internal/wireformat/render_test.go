package wireformat

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every test spec renders (generated Go must parse, see gofmt) and the
// contract lists one theorem per claim.
func TestRenderTestdata(t *testing.T) {
	paths, err := filepath.Glob("testdata/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatal(paths, err)
	}
	for _, path := range paths {
		f, err := Load(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		files := f.Files()
		contract := files[f.ContractPath()]
		proofs := files[f.ProofsPath()]
		for _, c := range f.Claims() {
			if !strings.Contains(contract, "\ndef "+c.Name+" : Prop :=") {
				t.Errorf("%s: contract lacks %s", path, c.Name)
			}
			if !strings.Contains(proofs, "\ntheorem "+c.Name+" ") {
				t.Errorf("%s: proofs lack %s", path, c.Name)
			}
		}
		if n := strings.Count(files[f.ContractFile()], `"theorem"`); n != f.ClaimCount() || n != len(f.Claims()) {
			t.Errorf("%s: %d claims, ClaimCount %d", path, n, f.ClaimCount())
		}
		for _, banned := range []string{"sorry", "native_decide", "bv_decide", "axiom "} {
			if strings.Contains(proofs, banned) || strings.Contains(files[f.SourcePath()], banned) {
				t.Errorf("%s: generated Lean contains %q", path, banned)
			}
		}
	}
}
