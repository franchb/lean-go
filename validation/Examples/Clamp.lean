import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Clamp

/-! Translation validation of `examples/proofs/clamp/clamp.ir.json`. -/

namespace Examples.Clamp
open LeanGo.Validate

validate_ir_program prog from "IR/clamp.ir.json"

def spec : List SpecEntry :=
  [.mk "Clamp.Bounds.cap" _root_.Clamp.Bounds.cap, .mk "Clamp.clamp" _root_.Clamp.clamp]

theorem cap_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Clamp.Bounds.cap») _root_.Clamp.Bounds.cap := by
  validate_obl

theorem clamp_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Clamp.clamp») _root_.Clamp.clamp := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [cap_obl, clamp_obl]

/-- Whole program: whatever the IR returns for `Clamp.clamp` is the source result. -/
theorem clamp_sound (n : Nat) (a b : UInt64) (v : Value)
    (h : prog.run n "Clamp.clamp" [Encode.encode a, Encode.encode b] = some v) :
    v = Encode.encode (_root_.Clamp.clamp a b) := by
  have := (run_rel prefixed (name := "Clamp.clamp") _root_.Clamp.clamp (by validate_lookup) n) a b
  exact (Option.some.inj ((this.eq_of_some h).symm))

end Examples.Clamp
