import Words
import Init.Data.UInt.Bitwise
namespace WordsProofs
open Words
theorem widen_preserves (word : UInt32) : (widen word).toNat = word.toNat := rfl

theorem narrow_truncates (word : UInt64) : (narrow word).toNat = word.toNat % 2^32 := rfl

theorem narrow_widen (word : UInt32) : narrow (widen word) = word := by
  simp [narrow, widen]

theorem widen_narrow (word : UInt64) : widen (narrow word) = word % 4294967296 := rfl

theorem pack_bits (hi lo : UInt32) : (pack hi lo).toBitVec = hi.toBitVec ++ lo.toBitVec := by
  change ((hi.toUInt64 <<< (32 : UInt64)) ||| lo.toUInt64).toBitVec = _
  simp only [UInt64.toBitVec_or, UInt64.toBitVec_shiftLeft, UInt32.toBitVec_toUInt64]
  simpa using (BitVec.setWidth_append_eq_shiftLeft_setWidth_or (b := hi.toBitVec) (b' := lo.toBitVec) (w'' := 64)).symm

theorem low_pack (hi lo : UInt32) : low (pack hi lo) = lo := by
  apply UInt32.toBitVec_inj.1
  simp only [low, UInt64.toBitVec_toUInt32, pack_bits]
  apply BitVec.eq_of_getElem_eq
  intro i hi
  simp only [BitVec.getElem_setWidth]
  rw [BitVec.getLsbD_append]
  simp [hi]

theorem high_pack (hi lo : UInt32) : high (pack hi lo) = hi := by
  apply UInt32.toBitVec_inj.1
  simp [high, pack_bits, BitVec.setWidth_ushiftRight_eq_extractLsb]
  apply BitVec.eq_of_getElem_eq
  intro i hi
  simp only [BitVec.getElem_extractLsb']
  rw [BitVec.getLsbD_append]
  simp [show ¬32 + i < 32 by omega, hi]

theorem pack_unpack (word : UInt64) : pack (high word) (low word) = word := by
  apply UInt64.toBitVec_inj.1
  simp only [pack_bits, high, low, UInt64.toBitVec_toUInt32, UInt64.toBitVec_shiftRight]
  simp only [UInt64.toBitVec_ofNat,
    BitVec.ushiftRight_eq']
  rw [BitVec.setWidth_ushiftRight_eq_extractLsb, BitVec.setWidth_eq_extractLsb' (by decide)]
  exact BitVec.extractLsb'_append_extractLsb'
end WordsProofs
