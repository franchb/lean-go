import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Counters

/-! Translation validation of `testdata/counters.ir.json` (nested records,
a `Bool` field scrutinized by `cases`, a helper call). -/

namespace Examples.Counters
open LeanGo.Validate

derive_ir_encoding _root_.Counters.Count _root_.Counters.State

validate_ir_program prog from "IR/counters.ir.json"

def spec : List SpecEntry :=
  [.mk "Counters.step" _root_.Counters.step, .mk "Counters.initial" _root_.Counters.initial,
   .mk "Counters.advance" _root_.Counters.advance, .mk "Counters.read" _root_.Counters.read,
   .mk "Counters.total" _root_.Counters.total]

theorem step_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Counters.step») _root_.Counters.step := by
  validate_obl
theorem initial_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Counters.initial») _root_.Counters.initial := by
  validate_obl
theorem advance_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Counters.advance») _root_.Counters.advance := by
  validate_obl
theorem read_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Counters.read») _root_.Counters.read := by
  validate_obl
theorem total_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Counters.total») _root_.Counters.total := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [step_obl, initial_obl, advance_obl, read_obl, total_obl]

theorem advance_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Counters.advance") _root_.Counters.advance :=
  run_rel prefixed (name := "Counters.advance") _ (by validate_lookup) n

end Examples.Counters
