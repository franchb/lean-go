import Lean
import Lean.Util.CollectAxioms
import QuotaProofs

open Lean Meta Elab Command

namespace QuotaEvidence

-- These raw propositions are deliberately independent of aliases in the proof file.
def expectedCreate : Prop := ∀ c : UInt64,
  (Quota.create c).used ≤ (Quota.create c).capacity

def expectedReserveAccepted : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.reserve s n).accepted = true ↔ s.used ≤ s.capacity ∧ n ≤ s.capacity - s.used

def expectedReservePreserves : Prop := ∀ (s : Quota.State) (n : UInt64),
  s.used ≤ s.capacity → (Quota.reserve s n).state.used ≤ (Quota.reserve s n).state.capacity

def expectedReserveRejected : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.reserve s n).accepted = false → (Quota.reserve s n).state = s

def expectedReserveExact : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.reserve s n).accepted = true →
  (Quota.reserve s n).state.capacity = s.capacity ∧
    (Quota.reserve s n).state.used.toNat = s.used.toNat + n.toNat

def expectedReleaseAccepted : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.release s n).accepted = true ↔ s.used ≤ s.capacity ∧ n ≤ s.used

def expectedReleasePreserves : Prop := ∀ (s : Quota.State) (n : UInt64),
  s.used ≤ s.capacity → (Quota.release s n).state.used ≤ (Quota.release s n).state.capacity

def expectedReleaseRejected : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.release s n).accepted = false → (Quota.release s n).state = s

def expectedReleaseExact : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.release s n).accepted = true →
  (Quota.release s n).state.capacity = s.capacity ∧
    (Quota.release s n).state.used.toNat + n.toNat = s.used.toNat

def expectedRemaining : Prop := ∀ s : Quota.State, s.used ≤ s.capacity →
  (Quota.remaining s).toNat + s.used.toNat = s.capacity.toNat

def checks : List (Name × Name) := [
  (``QuotaProofs.create_valid, ``expectedCreate),
  (``QuotaProofs.reserve_accepted, ``expectedReserveAccepted),
  (``QuotaProofs.reserve_preserves, ``expectedReservePreserves),
  (``QuotaProofs.reserve_rejected, ``expectedReserveRejected),
  (``QuotaProofs.reserve_exact, ``expectedReserveExact),
  (``QuotaProofs.release_accepted, ``expectedReleaseAccepted),
  (``QuotaProofs.release_preserves, ``expectedReleasePreserves),
  (``QuotaProofs.release_rejected, ``expectedReleaseRejected),
  (``QuotaProofs.release_exact, ``expectedReleaseExact),
  (``QuotaProofs.remaining_exact, ``expectedRemaining)]

def checkTheorem (name expected : Name) : MetaM Json := do
  let info ← getConstInfo name
  unless info matches .thmInfo _ do throwError "Claim is not a theorem: {name}"
  let witness ← getConstInfo expected
  let some statement := witness.value? | throwError "Missing expected statement: {expected}"
  unless ← isDefEq info.type statement do throwError "Theorem type changed: {name}"
  let axioms ← collectAxioms name
  for axiomName in axioms do
    unless [``propext, ``Classical.choice, ``Quot.sound].contains axiomName do
      throwError "Unapproved theorem axiom: {name}: {axiomName}"
  let ordered := (axioms.map Name.toString).qsort (· < ·)
  return Json.mkObj [("name", toJson name.toString),
    ("statement", toJson (← ppExpr info.type).pretty), ("axioms", toJson ordered)]

elab "#quota_check" : command => do
  let claims ← liftTermElabM <| checks.mapM fun (name, expected) => checkTheorem name expected
  IO.FS.writeFile "claims.json" ((toJson claims).pretty ++ "\n")

#quota_check

end QuotaEvidence
