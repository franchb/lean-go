import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Adler32

/-! Translation validation of `examples/proofs/adler32/adler32.ir.json`:
the two-field record state, its step and the byte-list fold. -/

namespace Examples.Adler32
open LeanGo.Validate

derive_ir_encoding _root_.Adler32.State

validate_ir_program prog from "IR/adler32.ir.json"

def spec : List SpecEntry :=
  [.mk "Adler32.initial" _root_.Adler32.initial, .mk "Adler32.reduce" _root_.Adler32.reduce,
   .mk "Adler32.step" _root_.Adler32.step, .mk "Adler32.update" _root_.Adler32.update,
   .mk "Adler32.checksum" _root_.Adler32.checksum]

theorem initial_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Adler32.initial») _root_.Adler32.initial := by
  validate_obl
theorem reduce_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Adler32.reduce») _root_.Adler32.reduce := by
  validate_obl
theorem step_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«Adler32.step») _root_.Adler32.step := by
  validate_obl
theorem update_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Adler32.update») _root_.Adler32.update := by
  validate_obl
theorem checksum_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Adler32.checksum») _root_.Adler32.checksum := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [initial_obl, reduce_obl, step_obl, update_obl, checksum_obl]

theorem initial_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Adler32.initial") _root_.Adler32.initial :=
  run_rel prefixed (name := "Adler32.initial") _ (by validate_lookup) n
theorem update_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Adler32.update") _root_.Adler32.update :=
  run_rel prefixed (name := "Adler32.update") _ (by validate_lookup) n
theorem checksum_sound (n : Nat) :
    Callable.Rel Approx (prog.run n "Adler32.checksum") _root_.Adler32.checksum :=
  run_rel prefixed (name := "Adler32.checksum") _ (by validate_lookup) n

/-- The same statement spelled out for the streaming root. -/
example (n : Nat) (s : _root_.Adler32.State) (bytes : List UInt8) (v : Value)
    (h : prog.run n "Adler32.update" [Encode.encode s, Encode.encode bytes] = some v) :
    v = Encode.encode (_root_.Adler32.update s bytes) :=
  Option.some.inj ((((update_sound n) s bytes).eq_of_some h).symm)

end Examples.Adler32
