import ByteCodec
import Lean
import Init.Data.UInt.Bitwise

namespace ByteCodecProofs
open ByteCodec

theorem encodeLE_b0 (w : UInt32) : (encodeLE w).b0.toNat = w.toNat % 256 := by
  simp [encodeLE]
theorem encodeLE_b1 (w : UInt32) : (encodeLE w).b1.toNat = w.toNat / 256 % 256 := by
  simp [encodeLE, Nat.shiftRight_eq_div_pow]
theorem encodeLE_b2 (w : UInt32) : (encodeLE w).b2.toNat = w.toNat / 65536 % 256 := by
  simp [encodeLE, Nat.shiftRight_eq_div_pow]
theorem encodeLE_b3 (w : UInt32) : (encodeLE w).b3.toNat = w.toNat / 16777216 % 256 := by
  simp [encodeLE, Nat.shiftRight_eq_div_pow]
theorem encodeBE_b0 (w : UInt32) : (encodeBE w).b0.toNat = w.toNat / 16777216 % 256 := by
  simp [encodeBE, Nat.shiftRight_eq_div_pow]
theorem encodeBE_b1 (w : UInt32) : (encodeBE w).b1.toNat = w.toNat / 65536 % 256 := by
  simp [encodeBE, Nat.shiftRight_eq_div_pow]
theorem encodeBE_b2 (w : UInt32) : (encodeBE w).b2.toNat = w.toNat / 256 % 256 := by
  simp [encodeBE, Nat.shiftRight_eq_div_pow]
theorem encodeBE_b3 (w : UInt32) : (encodeBE w).b3.toNat = w.toNat % 256 := by
  simp [encodeBE]

private theorem decodeLE_bits (q : ByteQuad) :
    (decodeLE q).toBitVec = q.b3.toBitVec ++ q.b2.toBitVec ++ q.b1.toBitVec ++ q.b0.toBitVec := by
  simp only [decodeLE, UInt32.toBitVec_or, UInt32.toBitVec_shiftLeft, UInt8.toBitVec_toUInt32]
  simpa using (BitVec.setWidth_append_append_append_eq_shiftLeft_setWidth_or
    (b := q.b3.toBitVec) (b' := q.b2.toBitVec) (b'' := q.b1.toBitVec)
    (b''' := q.b0.toBitVec) (w'''' := 32)).symm

private theorem decodeBE_bits (q : ByteQuad) :
    (decodeBE q).toBitVec = q.b0.toBitVec ++ q.b1.toBitVec ++ q.b2.toBitVec ++ q.b3.toBitVec := by
  simp only [decodeBE, UInt32.toBitVec_or, UInt32.toBitVec_shiftLeft, UInt8.toBitVec_toUInt32]
  simpa using (BitVec.setWidth_append_append_append_eq_shiftLeft_setWidth_or
    (b := q.b0.toBitVec) (b' := q.b1.toBitVec) (b'' := q.b2.toBitVec)
    (b''' := q.b3.toBitVec) (w'''' := 32)).symm

end ByteCodecProofs

namespace ByteCodecProofs
open ByteCodec

private theorem join_slices (w : BitVec 32) :
    w.extractLsb' 24 8 ++ w.extractLsb' 16 8 ++ w.extractLsb' 8 8 ++ w.extractLsb' 0 8 = w := by
  rw [BitVec.extractLsb'_append_extractLsb'_eq_extractLsb' (by decide),
      BitVec.extractLsb'_append_extractLsb'_eq_extractLsb' (by decide),
      BitVec.extractLsb'_append_extractLsb'_eq_extractLsb' (by decide)]
  simp

theorem decodeLE_encodeLE (w : UInt32) : decodeLE (encodeLE w) = w := by
  apply UInt32.toBitVec_inj.1
  rw [decodeLE_bits]
  simp only [encodeLE, UInt32.toBitVec_toUInt8, UInt32.toBitVec_shiftRight,
    UInt32.toBitVec_ofNat, BitVec.ushiftRight_eq']
  simp only [BitVec.setWidth_ushiftRight_eq_extractLsb]
  rw [BitVec.setWidth_eq_extractLsb' (by decide)]
  exact join_slices w.toBitVec

theorem decodeBE_encodeBE (w : UInt32) : decodeBE (encodeBE w) = w := by
  apply UInt32.toBitVec_inj.1
  rw [decodeBE_bits]
  simp only [encodeBE, UInt32.toBitVec_toUInt8, UInt32.toBitVec_shiftRight,
    UInt32.toBitVec_ofNat, BitVec.ushiftRight_eq']
  simp only [BitVec.setWidth_ushiftRight_eq_extractLsb]
  rw [BitVec.setWidth_eq_extractLsb' (by decide)]
  exact join_slices w.toBitVec

end ByteCodecProofs

namespace ByteCodecProofs
open ByteCodec

theorem encodeLE_decodeLE (q : ByteQuad) : encodeLE (decodeLE q) = q := by
  cases q with
  | mk b0 b1 b2 b3 =>
    simp only [encodeLE, ByteQuad.mk.injEq]
    refine ⟨?_, ?_, ?_, ?_⟩
    all_goals
      apply UInt8.toBitVec_inj.1
      simp only [UInt32.toBitVec_toUInt8, UInt32.toBitVec_shiftRight, decodeLE_bits,
        UInt32.toBitVec_ofNat, BitVec.ushiftRight_eq',
        BitVec.setWidth_ushiftRight_eq_extractLsb]
      apply BitVec.eq_of_getElem_eq
      intro i hi
      simp only [BitVec.getElem_setWidth, BitVec.getElem_extractLsb']
      repeat rw [BitVec.getLsbD_append]
      have h8 : ¬ 8 + i < 8 := by omega
      have h16 : ¬ 16 + i < 8 := by omega
      have h24 : ¬ 24 + i < 8 := by omega
      have s16 : 16 + i - 8 = 8 + i := by omega
      have s24 : 24 + i - 8 = 16 + i := by omega
      simp [hi, h8, h16, h24, s16, s24]

theorem encodeBE_decodeBE (q : ByteQuad) : encodeBE (decodeBE q) = q := by
  cases q with
  | mk b0 b1 b2 b3 =>
    simp only [encodeBE, ByteQuad.mk.injEq]
    refine ⟨?_, ?_, ?_, ?_⟩
    all_goals
      apply UInt8.toBitVec_inj.1
      simp only [UInt32.toBitVec_toUInt8, UInt32.toBitVec_shiftRight, decodeBE_bits,
        UInt32.toBitVec_ofNat, BitVec.ushiftRight_eq',
        BitVec.setWidth_ushiftRight_eq_extractLsb]
      apply BitVec.eq_of_getElem_eq
      intro i hi
      simp only [BitVec.getElem_setWidth, BitVec.getElem_extractLsb']
      repeat rw [BitVec.getLsbD_append]
      have h8 : ¬ 8 + i < 8 := by omega
      have h16 : ¬ 16 + i < 8 := by omega
      have h24 : ¬ 24 + i < 8 := by omega
      have s16 : 16 + i - 8 = 8 + i := by omega
      have s24 : 24 + i - 8 = 16 + i := by omega
      simp [hi, h8, h16, h24, s16, s24]


end ByteCodecProofs
