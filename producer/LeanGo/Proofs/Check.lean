import Lean
import Lean.Util.CollectAxioms

open Lean Meta

namespace LeanGo.Proofs

private def checkTheorem (name expected : Name) : MetaM Json := do
  let .thmInfo claim ← getConstInfo name
    | throwError "Claim is not a theorem: {name}"
  unless claim.levelParams.isEmpty do
    throwError "Polymorphic theorem is unsupported: {name}"
  let .defnInfo specification ← getConstInfo expected
    | throwError "Expected statement is not a definition: {expected}"
  unless specification.levelParams.isEmpty && specification.type == .sort .zero &&
      !specification.value.hasFVar && !specification.value.hasMVar &&
      !specification.value.hasLooseBVars do
    throwError "Expected statement must be a closed nonpolymorphic Prop definition: {expected}"
  unless ← isDefEq claim.type specification.value do
    throwError "Theorem type changed: {name}"
  let axioms ← collectAxioms name
  for axiomName in axioms do
    unless [``propext, ``Classical.choice, ``Quot.sound].contains axiomName do
      throwError "Unapproved theorem axiom: {name}: {axiomName}"
  let ordered := (axioms.map Name.toString).qsort (· < ·)
  return Json.mkObj [("name", toJson name.toString), ("expected", toJson expected.toString),
    ("statement", toJson (← ppExpr claim.type).pretty), ("axioms", toJson ordered)]

/-- Check reviewed closed propositions against theorem types and transitive axioms.
This establishes source statements; it does not certify an extraction backend. -/
def checkTheorems (claims : List (Name × Name)) : MetaM (List Json) :=
  claims.mapM fun (name, expected) => checkTheorem name expected

end LeanGo.Proofs
