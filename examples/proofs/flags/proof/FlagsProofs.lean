import Flags
import Init.Data.UInt.Bitwise

namespace FlagsProofs

theorem set_idempotent (state mask : UInt32) :
    Flags.set (Flags.set state mask) mask = Flags.set state mask := by
  simp [Flags.set, UInt32.or_assoc]

theorem clear_idempotent (state mask : UInt32) :
    Flags.clear (Flags.clear state mask) mask = Flags.clear state mask := by
  simp [Flags.clear, UInt32.and_assoc]

theorem cleared_bits_absent (state mask : UInt32) : Flags.clear state mask &&& mask = 0 := by
  simp [Flags.clear, UInt32.and_assoc]

theorem toggle_involution (state mask : UInt32) :
    Flags.toggle (Flags.toggle state mask) mask = state := by
  simp [Flags.toggle, UInt32.xor_assoc]

theorem clear_untouched (state mask : UInt32) :
    Flags.clear state mask &&& ~~~mask = state &&& ~~~mask := by
  simp [Flags.clear, UInt32.and_assoc]

theorem set_untouched (state mask : UInt32) :
    Flags.set state mask &&& ~~~mask = state &&& ~~~mask := by
  simp [Flags.set, ← UInt32.toBitVec_inj, BitVec.and_or_distrib_right]

theorem toggle_untouched (state mask : UInt32) :
    Flags.toggle state mask &&& ~~~mask = state &&& ~~~mask := by
  apply UInt32.toBitVec_inj.1
  simp only [Flags.toggle, UInt32.toBitVec_and, UInt32.toBitVec_xor, UInt32.toBitVec_not]
  apply BitVec.eq_of_getElem_eq
  intro i hi
  simp only [BitVec.getElem_and, BitVec.getElem_xor, BitVec.getElem_not]
  cases state.toBitVec[i] <;> cases mask.toBitVec[i] <;> rfl

theorem contains_set (state mask : UInt32) : Flags.contains (Flags.set state mask) mask = true := by
  simp only [Flags.contains, beq_iff_eq]
  apply UInt32.toBitVec_inj.1
  simp only [Flags.set, UInt32.toBitVec_and, UInt32.toBitVec_or]
  apply BitVec.eq_of_getElem_eq
  intro i hi
  simp only [BitVec.getElem_and, BitVec.getElem_or]
  cases state.toBitVec[i] <;> cases mask.toBitVec[i] <;> rfl

end FlagsProofs
