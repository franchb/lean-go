import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Records

/-! Translation validation of `testdata/records.ir.json`: record updates,
wrapper re-boxing around calls, a `Bool` flip. -/

namespace Examples.Records
open LeanGo.Validate

derive_ir_encoding _root_.Records.Count _root_.Records.Nested _root_.Records.Enabled
  _root_.Records.State

validate_ir_program prog from "IR/records.ir.json"

def spec : List SpecEntry :=
  [.mk "Records.addCount" _root_.Records.addCount, .mk "Records.flip" _root_.Records.flip,
   .mk "Records.update" _root_.Records.update]

theorem addCount_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Records.addCount») _root_.Records.addCount := by
  validate_obl
theorem flip_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Records.flip») _root_.Records.flip := by
  validate_obl
theorem update_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Records.update») _root_.Records.update := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [addCount_obl, flip_obl, update_obl]

theorem update_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Records.update") _root_.Records.update :=
  run_rel prefixed (name := "Records.update") _ (by validate_lookup) n

end Examples.Records
