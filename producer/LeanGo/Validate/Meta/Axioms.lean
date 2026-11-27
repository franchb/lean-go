module
public meta import Lean
public meta section

/-!
# Axiom gate

`#assert_standard_axioms M₁ M₂ …` collects the axioms of every declaration
(theorems and definitions, including auxiliary ones) that was added by an
imported module whose name starts with one of the given module prefixes, and
fails the build if any of them depends on an axiom other than `propext`,
`Classical.choice` and `Quot.sound`. This rejects `sorry` (`sorryAx`),
`native_decide` and `bv_decide` (`Lean.ofReduceBool`, `Lean.trustCompiler`)
and any user-declared axiom. It prints the number of declarations checked.
-/

open Lean Elab Command

namespace LeanGo.Validate.Meta

def allowedAxioms : List Name := [``propext, ``Classical.choice, ``Quot.sound]

syntax (name := assertStandardAxioms) "#assert_standard_axioms " ident+ : command

@[command_elab assertStandardAxioms] def elabAssertStandardAxioms : CommandElab := fun stx => do
  let prefixes := stx[1].getArgs.map (·.getId)
  let env ← getEnv
  let modules := env.header.moduleNames
  let mut checked := 0
  let mut bad : Array (Name × Array Name) := #[]
  for (n, info) in env.constants.map₁.toList do
    let some idx := env.getModuleIdxFor? n | continue
    let m := modules[idx.toNat]!
    unless prefixes.any (·.isPrefixOf m) do continue
    -- axioms declared by these modules are themselves violations
    if info matches .axiomInfo _ then
      bad := bad.push (n, #[n])
      continue
    let axs ← liftCoreM <| collectAxioms n
    checked := checked + 1
    let extra := axs.filter (!allowedAxioms.contains ·)
    unless extra.isEmpty do bad := bad.push (n, extra)
  unless bad.isEmpty do
    throwError "non-standard axioms:{bad.toList.map fun (n, a) => m!"\n  {n}: {a.toList}"}"
  if checked == 0 then throwError "#assert_standard_axioms: no declarations found for {prefixes}"
  logInfo m!"axiom check passed: {checked} declarations from {prefixes.toList} use only {allowedAxioms}"

end LeanGo.Validate.Meta
