import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Joins

/-! Translation validation of `testdata/joins.ir.json`: four local join
points, jumps between them, nested record wrappers.

This file is in the non-default `Open` target (`lake build Open`): it contains
an unproved obligation and therefore uses `sorry`. -/

namespace Open.Joins
open LeanGo.Validate

derive_ir_encoding _root_.Joins.Count _root_.Joins.Nested _root_.Joins.Enabled _root_.Joins.State

validate_ir_program prog from "../Examples/IR/joins.ir.json"

def spec : List SpecEntry :=
  [.mk "Joins.add" _root_.Joins.add, .mk "Joins.update" _root_.Joins.update]

theorem add_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Joins.add») _root_.Joins.add := by
  validate_obl

-- OPEN: `validate_obl` does not finish on `Joins.update`. The body declares
-- four join points whose continuations jump to each other (j37 → j17/j24 →
-- j5); the evaluator inlines a join body at every jump site, and the three
-- Boolean case splits (`flag`, `enabled`, `decide (selected < raw)`)
-- multiply the goals. With the default heartbeat limit simp times out after
-- the first split; with `maxHeartbeats 0` the Lean process is killed for
-- lack of memory after about 12 minutes on the shared machine. The failure
-- is resource exhaustion; no stuck (false) goal was observed, so this is not
-- evidence of a wrong lowering either way. Likely fixes: evaluate each join
-- body once, symbolically in its parameters, and use that equation at every
-- jump (a `Code.eval_join` lemma specialised per join), or split on the
-- Booleans before unfolding the joins.
theorem update_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Joins.update») _root_.Joins.update := by
  sorry

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [add_obl, update_obl]

theorem update_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Joins.update") _root_.Joins.update :=
  run_rel prefixed (name := "Joins.update") _ (by validate_lookup) n

end Open.Joins
