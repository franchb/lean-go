import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Quota

/-! Translation validation of `testdata/quota.ir.json` against
`libraries/quota/source/Quota.lean`. -/

namespace Examples.Quota
open LeanGo.Validate

derive_ir_encoding _root_.Quota.State _root_.Quota.Outcome

validate_ir_program prog from "IR/quota.ir.json"

def spec : List SpecEntry :=
  [.mk "Quota.create" _root_.Quota.create, .mk "Quota.reserve" _root_.Quota.reserve,
   .mk "Quota.release" _root_.Quota.release, .mk "Quota.remaining" _root_.Quota.remaining]

theorem create_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Quota.create») _root_.Quota.create := by
  validate_obl
theorem reserve_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Quota.reserve») _root_.Quota.reserve := by
  validate_obl
theorem release_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Quota.release») _root_.Quota.release := by
  validate_obl
theorem remaining_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Quota.remaining») _root_.Quota.remaining := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [create_obl, reserve_obl, release_obl, remaining_obl]

theorem create_sound (n : Nat) : Callable.Rel Approx (prog.run n "Quota.create") _root_.Quota.create :=
  run_rel prefixed (name := "Quota.create") _ (by validate_lookup) n
theorem reserve_sound (n : Nat) : Callable.Rel Approx (prog.run n "Quota.reserve") _root_.Quota.reserve :=
  run_rel prefixed (name := "Quota.reserve") _ (by validate_lookup) n
theorem release_sound (n : Nat) : Callable.Rel Approx (prog.run n "Quota.release") _root_.Quota.release :=
  run_rel prefixed (name := "Quota.release") _ (by validate_lookup) n
theorem remaining_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Quota.remaining") _root_.Quota.remaining :=
  run_rel prefixed (name := "Quota.remaining") _ (by validate_lookup) n

end Examples.Quota
