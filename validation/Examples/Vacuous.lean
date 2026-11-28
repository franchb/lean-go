import LeanGo.Validate
import LeanGo.Validate.Meta
import LeanGo.Validate.Command

/-! A hand-written IR program for which partial correctness holds vacuously,
and the witness step of `#lean_go_validate` that catches it.

`Handwritten/vacuous.ir.json` has two functions of type `UInt32 → UInt32`:
`Examples.Vacuous.spin` calls itself with its argument and returns the result,
so it returns for no fuel; `Examples.Vacuous.same` returns its argument. Both
validate against the source `fun x => x`: the obligation of `spin` holds
because its callee is answered by the specification, and its whole-program
theorem holds because a run that never returns satisfies it.
`#lean_go_witnesses` runs the witness step of the command on both roots:
`spin` is `failed` (the kernel accepts
`(prog.run 64 "Examples.Vacuous.spin" [encode c]).isSome = false`), `same` is
`checked`. -/

namespace Examples.Vacuous
open LeanGo.Validate

def spin (x : UInt32) : UInt32 := x
def same (x : UInt32) : UInt32 := x

validate_ir_program prog from "Handwritten/vacuous.ir.json"

def spec : List SpecEntry :=
  [.mk "Examples.Vacuous.spin" spin, .mk "Examples.Vacuous.same" same]

theorem spin_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Examples.Vacuous.spin») spin := by
  validate_obl
theorem same_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Examples.Vacuous.same») same := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [spin_obl, same_obl]

/-- The explicit theorem of `spin`: true, and vacuous. -/
theorem spin_explicit (n : Nat) (x : UInt32) (v : Value)
    (h : prog.run n "Examples.Vacuous.spin" [Encode.encode x] = some v) :
    v = Encode.encode (spin x) :=
  Option.some.inj (((run_rel prefixed (name := "Examples.Vacuous.spin") spin
    (by validate_lookup) n) x).eq_of_some h).symm

theorem same_explicit (n : Nat) (x : UInt32) (v : Value)
    (h : prog.run n "Examples.Vacuous.same" [Encode.encode x] = some v) :
    v = Encode.encode (same x) :=
  Option.some.inj (((run_rel prefixed (name := "Examples.Vacuous.same") same
    (by validate_lookup) n) x).eq_of_some h).symm

#lean_go_witnesses prog "Examples.Vacuous.spin" spin spin_explicit
  namespace Examples.Vacuous.Spin expect failed
#lean_go_witnesses prog "Examples.Vacuous.same" same same_explicit
  namespace Examples.Vacuous.Same expect checked

/-- What the failed witness establishes, restated by hand: on input 0 the
program does not return within the fuel. -/
example : (prog.run 64 "Examples.Vacuous.spin" [Encode.encode (0 : UInt32)]).isSome = false :=
  Examples.Vacuous.Spin.nonwitness_0_0

example : prog.run 64 "Examples.Vacuous.same" [Encode.encode (0 : UInt32)] =
    some (Encode.encode (same 0)) :=
  Examples.Vacuous.Same.witness_0_0

end Examples.Vacuous
