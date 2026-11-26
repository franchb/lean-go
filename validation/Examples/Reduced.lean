import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Reduced

/-! Translation validation of `testdata/reduced.ir.json`. The compiler's
`._redArg` helpers have no source declaration of their own; their
specification is the source owner with the removed (unused) arguments fixed
to arbitrary values. This is exactly the shape the producer's reduced-argument
evidence (retained-argument map) would have to emit. -/

namespace Examples.Reduced
open LeanGo.Validate

derive_ir_encoding _root_.Reduced.Count

validate_ir_program prog from "IR/reduced.ir.json"

/-- Specification of `Reduced.add._redArg`: retained arguments 1 and 3. -/
def addRed (left right : _root_.Reduced.Count) : _root_.Reduced.Count :=
  _root_.Reduced.add false left 0 right ⟨0⟩

/-- Specification of `Reduced.run._redArg`: argument 1 (`_unused`) removed. -/
def runRed (flag : Bool) (left right bias : _root_.Reduced.Count) : _root_.Reduced.Count :=
  _root_.Reduced.run flag ⟨0⟩ left right bias

def spec : List SpecEntry :=
  [.mk "Reduced.add._redArg" addRed, .mk "Reduced.run._redArg" runRed,
   .mk "Reduced.run" _root_.Reduced.run]

theorem addRed_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Reduced.add._redArg») addRed := by
  validate_obl [_root_.Reduced.add]
theorem runRed_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Reduced.run._redArg») runRed := by
  validate_obl [_root_.Reduced.run, addRed]
theorem run_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Reduced.run») _root_.Reduced.run := by
  validate_obl [runRed]

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [addRed_obl, runRed_obl, run_obl]

theorem run_sound (n : Nat) : Callable.Rel Approx (prog.run n "Reduced.run") _root_.Reduced.run :=
  run_rel prefixed (name := "Reduced.run") _ (by validate_lookup) n

end Examples.Reduced
