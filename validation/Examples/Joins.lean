import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Joins

/-! Translation validation of `testdata/joins.ir.json`: four local join
points whose continuations jump to each other (j37 → j17/j24 → j5), three
Boolean splits and nested record wrappers.

This was the stress case of the evaluator: unfolding a join copies its closure
(and the closures of every enclosing join) into the goal, and `simp` then
evaluated the copies symbolically under their binders. `validate_obl` now
names each join continuation by a fresh variable (`Code.eval_join_exists`), so
a join body is evaluated only when a jump applies it to arguments. -/

namespace Examples.Joins
open LeanGo.Validate

derive_ir_encoding _root_.Joins.Count _root_.Joins.Nested _root_.Joins.Enabled _root_.Joins.State

validate_ir_program prog from "IR/joins.ir.json"

def spec : List SpecEntry :=
  [.mk "Joins.add" _root_.Joins.add, .mk "Joins.update" _root_.Joins.update]

theorem add_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Joins.add») _root_.Joins.add := by
  validate_obl

theorem update_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Joins.update») _root_.Joins.update := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [add_obl, update_obl]

theorem update_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Joins.update") _root_.Joins.update :=
  run_rel prefixed (name := "Joins.update") _ (by validate_lookup) n

end Examples.Joins
