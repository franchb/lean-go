import Crc16Contract
import Init.Data.UInt.Bitwise

namespace Crc16Proofs
open Crc16Contract

private def align (b : BitVec 8) : BitVec 16 := b.shiftLeftZeroExtend 8

private theorem align_shift (b : BitVec 8) : align (b <<< 1) = align b <<< 1 := by
  apply BitVec.eq_of_toNat_eq
  simp [align, BitVec.toNat_shiftLeftZeroExtend, BitVec.toNat_shiftLeft, Nat.shiftLeft_eq]
  omega

private theorem step_bits (r : UInt16) :
    (Crc16.step r).toBitVec = bitStep r.toBitVec false := by
  have cond : r.toUInt32 < 0x8000 ↔ r.toBitVec.msb = false := by
    rw [UInt32.lt_iff_toNat_lt, BitVec.msb_eq_decide]
    change r.toNat < 32768 ↔ decide (32768 ≤ r.toNat) = false
    simp
  unfold Crc16.step bitStep
  simp only [cond]
  cases r.toBitVec.msb <;> simp

/-- XORing the unconsumed byte into the high half of the runtime register
    maintains the same feedback bit as feeding the message bit separately. -/
private theorem step_loaded (r : UInt16) (s : BitVec 16) (b : BitVec 8)
    (h : r.toBitVec = s ^^^ align b) :
    (Crc16.step r).toBitVec = bitStep s b.msb ^^^ align (b <<< 1) := by
  rw [step_bits, h, align_shift]
  simp only [bitStep, Bool.xor_false, BitVec.shiftLeft_xor_distrib, BitVec.msb_xor,
    align, BitVec.msb_shiftLeftZeroExtend]
  ac_rfl

private def steps : Nat → UInt16 → UInt16
  | 0, r => r
  | n + 1, r => steps n (Crc16.step r)

private theorem steps_loaded (n : Nat) (r : UInt16) (s : BitVec 16) (b : BitVec 8)
    (h : r.toBitVec = s ^^^ align b) :
    (steps n r).toBitVec = consumeBits n s b ^^^ align (b <<< n) := by
  induction n generalizing r s b with
  | zero => simpa [steps, consumeBits] using h
  | succ n ih =>
    rw [steps, consumeBits, ih _ _ _ (step_loaded r s b h)]
    rw [← BitVec.shiftLeft_add, Nat.add_comm 1 n]

theorem update_byte_spec (state : UInt16) (byte : UInt8) :
    (Crc16.updateByte state byte).toBitVec = referenceByte state.toBitVec byte := by
  let loaded := state ^^^ (byte.toUInt16 <<< 8)
  have preload : loaded.toBitVec = state.toBitVec ^^^ align byte.toBitVec := by
    simp [loaded, align, BitVec.shiftLeftZeroExtend_eq]
  change (steps 8 loaded).toBitVec = _
  rw [steps_loaded 8 _ _ _ preload]
  simp [align, referenceByte, BitVec.shiftLeftZeroExtend_eq]

private theorem fold_bytes (bytes : List UInt8) (state : UInt16) :
    (bytes.foldl Crc16.updateByte state).toBitVec = bytes.foldl referenceByte state.toBitVec := by
  induction bytes generalizing state with
  | nil => rfl
  | cons byte bytes ih => simp only [List.foldl_cons, ih, update_byte_spec]

theorem chunks_spec (state : UInt16) (chunks : List (List UInt8)) :
    (chunks.foldl (fun acc chunk => chunk.foldl Crc16.updateByte acc) state).toBitVec =
      chunks.flatten.foldl referenceByte state.toBitVec := by
  induction chunks generalizing state with
  | nil => rfl
  | cons chunk chunks ih => simp only [List.foldl_cons, List.flatten_cons, List.foldl_append, ih, fold_bytes]

end Crc16Proofs
