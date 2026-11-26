import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Words

/-! Translation validation of `examples/proofs/words/words.ir.json`. -/

namespace Examples.Words
open LeanGo.Validate

validate_ir_program prog from "IR/words.ir.json"

def spec : List SpecEntry :=
  [.mk "Words.widen" _root_.Words.widen, .mk "Words.narrow" _root_.Words.narrow,
   .mk "Words.pack" _root_.Words.pack, .mk "Words.high" _root_.Words.high,
   .mk "Words.low" _root_.Words.low]

theorem widen_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Words.widen») _root_.Words.widen := by
  validate_obl
theorem narrow_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Words.narrow») _root_.Words.narrow := by
  validate_obl
theorem pack_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Words.pack») _root_.Words.pack := by
  validate_obl
theorem high_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Words.high») _root_.Words.high := by
  validate_obl
theorem low_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Words.low») _root_.Words.low := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [widen_obl, narrow_obl, pack_obl, high_obl, low_obl]

theorem widen_sound (n : Nat) : Callable.Rel Approx (prog.run n "Words.widen") _root_.Words.widen :=
  run_rel prefixed (name := "Words.widen") _ (by validate_lookup) n
theorem narrow_sound (n : Nat) : Callable.Rel Approx (prog.run n "Words.narrow") _root_.Words.narrow :=
  run_rel prefixed (name := "Words.narrow") _ (by validate_lookup) n
theorem pack_sound (n : Nat) : Callable.Rel Approx (prog.run n "Words.pack") _root_.Words.pack :=
  run_rel prefixed (name := "Words.pack") _ (by validate_lookup) n
theorem high_sound (n : Nat) : Callable.Rel Approx (prog.run n "Words.high") _root_.Words.high :=
  run_rel prefixed (name := "Words.high") _ (by validate_lookup) n
theorem low_sound (n : Nat) : Callable.Rel Approx (prog.run n "Words.low") _root_.Words.low :=
  run_rel prefixed (name := "Words.low") _ (by validate_lookup) n

end Examples.Words
