package pipeline

import (
	"bytes"
	"strings"
	"testing"
)

// The quota contract's selection, and the real quota proofs through the
// shared gate (LEAN_GO_QUOTA=1).

const quotaContract = "../../libraries/quota/proof-contract.json"

func TestQuotaContractSelectsTenClaimsAndFourOperations(t *testing.T) {
	contract, err := LoadContract(quotaContract)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Module != "Quota" || strings.Join(contract.Roots, " ") != "Quota.create Quota.release Quota.remaining Quota.reserve" {
		t.Fatalf("contract selects %s %v", contract.Module, contract.Roots)
	}
	if len(contract.Claims) != 10 {
		t.Fatalf("%d claims", len(contract.Claims))
	}
	for _, claim := range contract.Claims {
		if !strings.HasPrefix(claim[0], "QuotaProofs.") || !strings.HasPrefix(claim[1], "QuotaContract.") {
			t.Errorf("claim %v", claim)
		}
	}
}

func quotaFixture(t *testing.T) (*Contract, *ProjectSnapshot, *toolInputs, Compiler) {
	t.Helper()
	gate(t, "LEAN_GO_QUOTA")
	contract, err := LoadContract(quotaContract)
	if err != nil {
		t.Fatal(err)
	}
	project, err := SnapshotProject(contract)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := proofTools()
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := SelectedCompiler()
	if err != nil {
		t.Fatal(err)
	}
	return contract, project, tools, compiler
}

// replaceCreate replaces the create_valid theorem.
func replaceCreate(t *testing.T, project *ProjectSnapshot, declaration string) *ProjectSnapshot {
	t.Helper()
	return replaceProofDeclaration(t, project, "QuotaProofs", "theorem create_valid ", "theorem reserve_accepted ", declaration)
}

func TestQuotaClaimsCompileDeterministically(t *testing.T) {
	contract, project, tools, compiler := quotaFixture(t)
	first, err := prove(contract.Claims, project, tools, compiler)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prove(contract.Claims, project, tools, compiler)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := EncodeCompactOrdered(first)
	b, _ := EncodeCompactOrdered(second)
	if !bytes.Equal(a, b) {
		t.Fatal("the quota claims are not deterministic")
	}
	if len(first) != 10 {
		t.Fatalf("%d claims", len(first))
	}
	for _, claim := range first {
		axioms, ok := claim.(*Object).Values["axioms"].([]any)
		if !ok {
			t.Fatalf("claim %v has no axioms list", claim)
		}
		for _, axiom := range axioms {
			if name, _ := axiom.(string); !approvedAxioms[name] {
				t.Errorf("claim depends on %v", axiom)
			}
		}
	}
}

func TestQuotaWeakenedTrueStatementIsRejected(t *testing.T) {
	contract, project, tools, compiler := quotaFixture(t)
	_, err := prove(contract.Claims, replaceCreate(t, project, "theorem create_valid : True := by trivial"), tools, compiler)
	expectError(t, err, "Theorem type changed")
}

func TestQuotaSameTypeAxiomIsNotATheorem(t *testing.T) {
	contract, project, tools, compiler := quotaFixture(t)
	changed := replaceCreate(t, project, "axiom create_valid (capacity : UInt64) :\n  (Quota.create capacity).used ≤ (Quota.create capacity).capacity")
	_, err := prove(contract.Claims, changed, tools, compiler)
	expectError(t, err, "Claim is not a theorem")
}

func TestQuotaSameTypeSorryAndCustomAxiomAreRejected(t *testing.T) {
	contract, project, tools, compiler := quotaFixture(t)
	statement := "(capacity : UInt64) :\n  (Quota.create capacity).used ≤ (Quota.create capacity).capacity"
	for _, declaration := range []string{
		"theorem create_valid " + statement + " := by sorry",
		"axiom customTrust " + statement + "\n\ntheorem create_valid " + statement + " := customTrust capacity",
	} {
		_, err := prove(contract.Claims, replaceCreate(t, project, declaration), tools, compiler)
		expectError(t, err, "Unapproved theorem axiom")
	}
}

func TestQuotaNativeDecideDependencyIsRejected(t *testing.T) {
	contract, project, tools, compiler := quotaFixture(t)
	changed := replaceCreate(t, project, `theorem create_valid (capacity : UInt64) :
    (Quota.create capacity).used ≤ (Quota.create capacity).capacity := by
  have seed : (0 : UInt64) = 0 := by native_decide
  exact Eq.mp (congrArg (fun n : UInt64 => n ≤ capacity) seed) (by simp)`)
	_, err := prove(contract.Claims, changed, tools, compiler)
	expectError(t, err, "Unapproved theorem axiom.*native_decide")
}

func TestQuotaSemanticSourceMutationFailsProofs(t *testing.T) {
	contract, project, tools, compiler := quotaFixture(t)
	changed := &ProjectSnapshot{Runtime: map[string]Source{}, Proofs: project.Proofs}
	for name, source := range project.Runtime {
		changed.Runtime[name] = source
	}
	source := changed.Runtime["Quota"]
	mutated := strings.Replace(string(source.Data), "used := state.used + amount", "used := state.used", 1)
	if mutated == string(source.Data) {
		t.Fatal("missing source mutation target")
	}
	source.Data = []byte(mutated)
	changed.Runtime["Quota"] = source
	_, err := prove(contract.Claims, changed, tools, compiler)
	expectError(t, err, "QuotaProofs.lean")
}

// The tracked evidence is freshly verified against the tracked generated
// package.
func TestQuotaTrackedEvidenceRechecks(t *testing.T) {
	gate(t, "LEAN_GO_QUOTA")
	summary, err := CheckProofs(CheckProofsOptions{Contract: quotaContract,
		Manifest: "../../generated/quota/manifest.json", Evidence: "../../libraries/quota/proof-evidence.json"})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(summary)
}
