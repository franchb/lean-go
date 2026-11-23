import Wire16
import Lean
import Init.Data.UInt.Bitwise

namespace Wire16Proofs
open Wire16

theorem encode_be_bytes (n : UInt16) :
    (encodeBE n).hi.toNat = n.toNat / 256 ∧ (encodeBE n).lo.toNat = n.toNat % 256 := by
  have hn := n.toNat_lt
  simp [encodeBE, Nat.shiftRight_eq_div_pow]
  omega

theorem decode_be_value (p : BytePair) :
    (decodeBE p).toNat = p.hi.toNat * 256 + p.lo.toNat := by
  have hhi := p.hi.toNat_lt
  have hlo := p.lo.toNat_lt
  simp only [decodeBE, UInt16.toNat_or, UInt16.toNat_shiftLeft, UInt8.toNat_toUInt16,
    UInt16.reduceToNat, Nat.reduceMod, Nat.shiftLeft_eq, Nat.reducePow]
  rw [Nat.mod_eq_of_lt (by omega), Nat.mul_comm, ← Nat.two_pow_add_eq_or_of_lt (i := 8) hlo]

theorem decode_encode_be (n : UInt16) : decodeBE (encodeBE n) = n := by
  apply UInt16.toNat_inj.mp
  rw [decode_be_value, (encode_be_bytes n).1, (encode_be_bytes n).2]
  omega

theorem encode_decode_be (p : BytePair) : encodeBE (decodeBE p) = p := by
  cases p with
  | mk hi lo =>
    apply BytePair.mk.injEq .. |>.mpr
    have ⟨hhi, hlo⟩ := encode_be_bytes (decodeBE ⟨hi, lo⟩)
    rw [decode_be_value] at hhi hlo
    have hlow := lo.toNat_lt
    constructor
    · apply UInt8.toNat_inj.mp
      exact hhi.trans (by dsimp; omega)
    · apply UInt8.toNat_inj.mp
      exact hlo.trans (by dsimp; omega)

private theorem valid_flags (f : UInt8) : (f < 4) ↔ f.toNat < 4 := Iff.rfl

theorem parse_success_iff (q : ByteQuad) (h : Header) : parse q = .parsed h ↔
    q.b0 = 1 ∧ q.b1.toNat < 4 ∧ h.flags = q.b1 ∧
      h.length.toNat = q.b2.toNat * 256 + q.b3.toNat := by
  cases h with
  | mk flags length =>
    by_cases hv : q.b0 = 1 <;> by_cases hf : q.b1.toNat < 4 <;>
      simp [parse, valid_flags, hf, hv]
    rw [← UInt16.toNat_inj, decode_be_value]
    simp [eq_comm]

theorem parse_unsupported_version_iff (q : ByteQuad) :
    parse q = .unsupportedVersion ↔ q.b0 ≠ 1 := by
  by_cases hv : q.b0 = 1 <;> simp [parse, hv] <;> split <;> simp

theorem parse_reserved_flags_iff (q : ByteQuad) :
    parse q = .reservedFlags ↔ q.b0 = 1 ∧ 4 ≤ q.b1.toNat := by
  by_cases hv : q.b0 = 1 <;> by_cases hf : q.b1.toNat < 4 <;>
    simp [parse, valid_flags, hf, hv] <;> omega

theorem encode_success_iff (h : Header) (q : ByteQuad) : encode h = .encoded q ↔
    h.flags.toNat < 4 ∧ q.b0 = 1 ∧ q.b1 = h.flags ∧
      q.b2.toNat = h.length.toNat / 256 ∧ q.b3.toNat = h.length.toNat % 256 := by
  cases q with
  | mk b0 b1 b2 b3 =>
    by_cases hf : h.flags.toNat < 4 <;> simp [encode, valid_flags, hf]
    rw [← UInt8.toNat_inj (a := (encodeBE h.length).hi),
      ← UInt8.toNat_inj (a := (encodeBE h.length).lo),
      (encode_be_bytes h.length).1, (encode_be_bytes h.length).2]
    simp [eq_comm]

theorem encode_reserved_flags_iff (h : Header) :
    encode h = .reservedFlags ↔ 4 ≤ h.flags.toNat := by
  by_cases hf : h.flags.toNat < 4 <;> simp [encode, valid_flags, hf] <;> omega

theorem parse_encode_roundtrip (h : Header) (hf : h.flags.toNat < 4) :
    ∃ q : ByteQuad, encode h = .encoded q ∧ parse q = .parsed h := by
  let p := encodeBE h.length
  refine ⟨⟨1, h.flags, p.hi, p.lo⟩, ?_, ?_⟩
  · simp [encode, valid_flags, hf, p]
  · simp [parse, valid_flags, hf, p]
    change Header.mk h.flags (decodeBE (encodeBE h.length)) = h
    rw [decode_encode_be]

theorem encode_parse_roundtrip (q : ByteQuad) (hv : q.b0 = 1) (hf : q.b1.toNat < 4) :
    ∃ h : Header, parse q = .parsed h ∧ encode h = .encoded q := by
  refine ⟨⟨q.b1, decodeBE ⟨q.b2, q.b3⟩⟩, ?_, ?_⟩
  · simp [parse, valid_flags, hv, hf]
  · simp [encode, valid_flags, hf, encode_decode_be, ← hv]

theorem need_more_iff (header : Header) (available missing : UInt32) :
    classify header available = .needMore missing ↔
      available.toNat < header.length.toNat ∧ missing.toNat = header.length.toNat - available.toNat := by
  by_cases short : available < header.length.toUInt32
  · have lt : available.toNat < header.length.toNat := short
    simp [classify, short, lt, ← UInt32.toNat_inj, UInt32.toNat_sub_of_le _ _ (UInt32.le_of_lt short),
      eq_comm]
  · have enough : ¬ available.toNat < header.length.toNat := short
    simp [classify, short, enough]

theorem complete_iff (header : Header) (available consumed : UInt32) :
    classify header available = .complete consumed ↔
      header.length.toNat ≤ available.toNat ∧ consumed.toNat = header.length.toNat := by
  by_cases short : available < header.length.toUInt32
  · have lt : available.toNat < header.length.toNat := short
    simp [classify, short, Nat.not_le_of_lt lt]
  · have enough : header.length.toNat ≤ available.toNat := Nat.le_of_not_gt short
    simp [classify, short, enough, ← UInt32.toNat_inj, eq_comm]

end Wire16Proofs
