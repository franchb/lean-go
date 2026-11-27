import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Crc16

/-! Translation validation of `examples/proofs/crc16/crc16.ir.json`: the
register step, the one-byte update and the byte-list fold. -/

namespace Examples.Crc16

open LeanGo.Validate

validate_ir_program prog from "IR/crc16.ir.json"

def spec : List SpecEntry :=
  [.mk "Crc16.step" _root_.Crc16.step, .mk "Crc16.updateByte" _root_.Crc16.updateByte,
   .mk "Crc16.update" _root_.Crc16.update]

theorem step_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Crc16.step») _root_.Crc16.step := by
  validate_obl
theorem updateByte_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Crc16.updateByte») _root_.Crc16.updateByte := by
  validate_obl
theorem update_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Crc16.update») _root_.Crc16.update := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [step_obl, updateByte_obl, update_obl]

theorem step_sound (n : Nat) : Callable.Rel Approx (prog.run n "Crc16.step") _root_.Crc16.step :=
  run_rel prefixed (name := "Crc16.step") _ (by validate_lookup) n
theorem updateByte_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Crc16.updateByte") _root_.Crc16.updateByte :=
  run_rel prefixed (name := "Crc16.updateByte") _ (by validate_lookup) n
/-- Whole program for the streaming root: for every state and byte list,
any value the IR returns is the encoded fold result. -/
theorem update_sound (n : Nat) : Callable.Rel Approx (prog.run n "Crc16.update") _root_.Crc16.update :=
  run_rel prefixed (name := "Crc16.update") _ (by validate_lookup) n

end Examples.Crc16
