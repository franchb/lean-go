import HeaderCodec
import Lean
import Init.Data.UInt.Bitwise

namespace HeaderCodecProofs
open HeaderCodec

private theorem length_of_bytes (hi lo : UInt8) :
    (hi.toUInt32 * 256 + lo.toUInt32).toNat = hi.toNat * 256 + lo.toNat := by
  have hhi := hi.toNat_lt
  have hlo := lo.toNat_lt
  simp
  omega

private theorem length_hi (n : UInt32) (hn : n.toNat < 65536) :
    (n >>> (8 : UInt32)).toUInt8.toNat = n.toNat / 256 := by
  simp [Nat.shiftRight_eq_div_pow]
  omega

private theorem valid_flags (f : UInt8) : (f < 4) ↔ f.toNat < 4 := Iff.rfl
private theorem valid_length (n : UInt32) : (n < 65536) ↔ n.toNat < 65536 := Iff.rfl

theorem parse_success_iff (q : ByteQuad) (h : Header) : parse q = .parsed h ↔
    q.b0 = 1 ∧ q.b1.toNat < 4 ∧ h.flags = q.b1 ∧
      h.length.toNat = q.b2.toNat * 256 + q.b3.toNat := by
  cases h with
  | mk flags length =>
    by_cases hv : q.b0 = 1 <;> by_cases hf : q.b1.toNat < 4 <;>
      simp [parse, valid_flags, hf, hv]
    rw [← UInt32.toNat_inj, length_of_bytes]
    simp [eq_comm]

theorem parse_unsupported_version_iff (q : ByteQuad) :
    parse q = .unsupportedVersion ↔ q.b0 ≠ 1 := by
  by_cases hv : q.b0 = 1 <;> simp [parse, hv] <;> split <;> simp

theorem parse_reserved_flags_iff (q : ByteQuad) :
    parse q = .reservedFlags ↔ q.b0 = 1 ∧ 4 ≤ q.b1.toNat := by
  by_cases hv : q.b0 = 1 <;> by_cases hf : q.b1.toNat < 4 <;>
    simp [parse, valid_flags, hf, hv] <;> omega

theorem parsed_length_bound (q : ByteQuad) (h : Header) (hp : parse q = .parsed h) :
    h.length.toNat < 65536 := by
  obtain ⟨_, _, _, hl⟩ := (parse_success_iff q h).mp hp
  have hhi := q.b2.toNat_lt
  have hlo := q.b3.toNat_lt
  omega

theorem encode_success_iff (h : Header) (q : ByteQuad) : encode h = .encoded q ↔
    h.flags.toNat < 4 ∧ h.length.toNat < 65536 ∧ q.b0 = 1 ∧ q.b1 = h.flags ∧
      q.b2.toNat = h.length.toNat / 256 ∧ q.b3.toNat = h.length.toNat % 256 := by
  cases q with
  | mk b0 b1 b2 b3 =>
    by_cases hf : h.flags.toNat < 4 <;> by_cases hl : h.length.toNat < 65536 <;>
      simp [encode, valid_flags, valid_length, hf, hl]
    rw [← UInt8.toNat_inj (a := (h.length >>> (8 : UInt32)).toUInt8),
      ← UInt8.toNat_inj (a := h.length.toUInt8), length_hi _ hl]
    simp [eq_comm]

theorem encode_reserved_flags_iff (h : Header) :
    encode h = .reservedFlags ↔ 4 ≤ h.flags.toNat := by
  by_cases hf : h.flags.toNat < 4 <;> by_cases hl : h.length.toNat < 65536 <;>
    simp [encode, valid_flags, valid_length, hf, hl] <;> omega

theorem encode_length_overflow_iff (h : Header) :
    encode h = .lengthOverflow ↔ h.flags.toNat < 4 ∧ 65536 ≤ h.length.toNat := by
  by_cases hf : h.flags.toNat < 4 <;> by_cases hl : h.length.toNat < 65536 <;>
    simp [encode, valid_flags, valid_length, hf, hl] <;> omega

theorem parse_encode_roundtrip (h : Header) (hf : h.flags.toNat < 4) (hl : h.length.toNat < 65536) :
    ∃ q : ByteQuad, encode h = .encoded q ∧ parse q = .parsed h := by
  let q : ByteQuad := ⟨1, h.flags, (h.length >>> (8 : UInt32)).toUInt8, h.length.toUInt8⟩
  refine ⟨q, (encode_success_iff h q).mpr ⟨hf, hl, rfl, rfl, length_hi _ hl, rfl⟩, ?_⟩
  apply (parse_success_iff q h).mpr
  refine ⟨rfl, hf, rfl, ?_⟩
  simp [q, length_hi _ hl]
  omega

theorem encode_parse_roundtrip (q : ByteQuad) (hv : q.b0 = 1) (hf : q.b1.toNat < 4) :
    ∃ h : Header, parse q = .parsed h ∧ encode h = .encoded q := by
  let h : Header := ⟨q.b1, q.b2.toUInt32 * 256 + q.b3.toUInt32⟩
  have hp : parse q = .parsed h := (parse_success_iff q h).mpr ⟨hv, hf, rfl, length_of_bytes ..⟩
  refine ⟨h, hp, (encode_success_iff h q).mpr ⟨hf, parsed_length_bound _ _ hp, hv, rfl, ?_, ?_⟩⟩
  all_goals
    have hlo := q.b3.toNat_lt
    simp only [h, length_of_bytes]
    omega

end HeaderCodecProofs
