package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

const clampDir = "../examples/proofs/clamp"

// The native clamp package deploys with Go alone next to the metered one and
// passes the same independent checks.
func TestStandaloneClampNativeDemo(t *testing.T) {
	testStandaloneDemo(t, "clamp", "clamp(7, 42) = 7\nclamp(100, 42) = 42\nclamp(18446744073709551615, 42) = 42\n",
		"native_test.go", "clampnative/functions.go", "clampnative/native.go", "clampnative/types.go")
}

func readEvidence(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(clampDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

// Without Lean: the tracked native evidence is bound to the tracked native
// manifest, which binds every native file, and it checks the same claims over
// the same sources as the metered evidence.
func TestClampNativeEvidenceIsBound(t *testing.T) {
	contract, err := pipeline.LoadContract(filepath.Join(clampDir, "proof-contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := pipeline.SnapshotProject(contract)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := pipeline.SelectedCompiler()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pipeline.VerifyManifest(filepath.Join(clampDir, "clampnative", "manifest.json"), contract, project, compiler)
	if err != nil {
		t.Fatal(err)
	}
	native, metered := readEvidence(t, "native-proof-evidence.json"), readEvidence(t, "proof-evidence.json")
	if native["generated_mode"] != "native" || native["generated_manifest_sha256"] != pipeline.Digest(manifest) {
		t.Fatalf("native evidence binds mode %v, manifest %v", native["generated_mode"], native["generated_manifest_sha256"])
	}
	if _, ok := metered["generated_mode"]; ok {
		t.Fatal("metered evidence records a mode")
	}
	for _, key := range []string{"claims", "contract_sha256", "project_inputs", "module", "roots", "tool_inputs"} {
		if !reflect.DeepEqual(native[key], metered[key]) {
			t.Errorf("%s differs between the native and metered evidence", key)
		}
	}
}

// With Lean: the proofs are freshly rechecked against the native package and
// the tracked evidence must match byte for byte.
func TestClampNativeEvidenceRechecks(t *testing.T) {
	if os.Getenv("LEAN_GO_PROOFS") != "1" {
		t.Skip("set LEAN_GO_PROOFS=1 to recheck the clamp proofs against the native package")
	}
	summary, err := pipeline.CheckProofs(pipeline.CheckProofsOptions{
		Contract: filepath.Join(clampDir, "proof-contract.json"),
		Manifest: filepath.Join(clampDir, "clampnative", "manifest.json"),
		Evidence: filepath.Join(clampDir, "native-proof-evidence.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(summary)
}
