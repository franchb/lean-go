import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Flags

/-! Translation validation of `examples/proofs/flags/flags.ir.json`. -/

namespace Examples.Flags
open LeanGo.Validate

validate_ir_program prog from "IR/flags.ir.json"

def spec : List SpecEntry :=
  [.mk "Flags.set" _root_.Flags.set, .mk "Flags.clear" _root_.Flags.clear,
   .mk "Flags.toggle" _root_.Flags.toggle, .mk "Flags.contains" _root_.Flags.contains]

theorem set_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Flags.set») _root_.Flags.set := by
  validate_obl
theorem clear_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Flags.clear») _root_.Flags.clear := by
  validate_obl
theorem toggle_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Flags.toggle») _root_.Flags.toggle := by
  validate_obl
theorem contains_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Flags.contains») _root_.Flags.contains := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [set_obl, clear_obl, toggle_obl, contains_obl]

theorem set_sound (n : Nat) : Callable.Rel Approx (prog.run n "Flags.set") _root_.Flags.set :=
  run_rel prefixed (name := "Flags.set") _ (by validate_lookup) n
theorem clear_sound (n : Nat) : Callable.Rel Approx (prog.run n "Flags.clear") _root_.Flags.clear :=
  run_rel prefixed (name := "Flags.clear") _ (by validate_lookup) n
theorem toggle_sound (n : Nat) : Callable.Rel Approx (prog.run n "Flags.toggle") _root_.Flags.toggle :=
  run_rel prefixed (name := "Flags.toggle") _ (by validate_lookup) n
theorem contains_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Flags.contains") _root_.Flags.contains :=
  run_rel prefixed (name := "Flags.contains") _ (by validate_lookup) n

end Examples.Flags
