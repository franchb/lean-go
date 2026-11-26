import Adler32Proofs
import Init.Data.UInt.Bitwise
import Init.Data.Nat.Bitwise.Lemmas

namespace Adler32CombineProofs
open Adler32 Adler32Contract

private theorem low_word (x : UInt32) : (x &&& (65535 : UInt32)).toNat = x.toNat % 65536 := by
  rw [UInt32.toNat_and]
  change (x.toNat &&& (2 ^ 16 - 1)) = x.toNat % 2 ^ 16
  exact Nat.and_two_pow_sub_one_eq_mod x.toNat 16

private theorem high_word (x : UInt32) : (x >>> (16 : UInt32)).toNat = x.toNat / 65536 := by
  rw [UInt32.toNat_shiftRight, Nat.shiftRight_eq_div_pow]
  rfl

-- Bounded machine operations agree with the mathematical limb formula.
private theorem fold_value (x : UInt32) :
    (fold16 x).toNat = x.toNat % 65536 + x.toNat / 65536 * 15 := by
  have hx : x.toNat < 4294967296 := x.toNat_lt_size
  simp only [fold16, UInt32.toNat_add, UInt32.toNat_mul, high_word, low_word]
  change (_ + (_ * 15) % 4294967296) % 4294967296 = _
  omega

private theorem fold_congr (x : UInt32) : (fold16 x).toNat % 65521 = x.toNat % 65521 := by
  rw [fold_value]
  omega

private theorem residue_value (x : UInt32) : (residue x).toNat = x.toNat % 65521 := by
  let y := fold16 (fold16 x)
  have hx : x.toNat < 4294967296 := x.toNat_lt_size
  have hy : y.toNat < 2 * 65521 := by
    dsimp [y]; rw [fold_value (fold16 x), fold_value x]; omega
  have congr : y.toNat % 65521 = x.toNat % 65521 :=
    (fold_congr (fold16 x)).trans (fold_congr x)
  change (reduce y).toNat = _
  rw [← congr]
  unfold reduce
  split
  next h =>
    rw [UInt32.lt_iff_toNat_lt] at h
    exact (Nat.mod_eq_of_lt h).symm
  next h =>
    rw [UInt32.lt_iff_toNat_lt] at h
    have ge : (65521 : UInt32) ≤ y := (UInt32.le_iff_toNat_le).2 (Nat.le_of_not_lt h)
    rw [UInt32.toNat_sub_of_le _ _ ge]
    change ¬ y.toNat < 65521 at h
    change y.toNat - 65521 = y.toNat % 65521
    omega

-- Reduce both 32-bit limbs of the length. Since 2^32 ≡ 225 (mod 65521),
-- this agrees with reducing the original UInt64, including its upper bits.
private theorem length_value (n : UInt64) : (lengthResidue n).toNat = n.toNat % 65521 := by
  have hn : n.toNat < 18446744073709551616 := n.toNat_lt_size
  simp only [lengthResidue, residue_value, UInt32.toNat_add, UInt32.toNat_mul,
    UInt64.toNat_toUInt32, UInt64.toNat_shiftRight, Nat.shiftRight_eq_div_pow]
  change (((n.toNat % 4294967296 % 65521 + (225 * (n.toNat / 4294967296 % 4294967296 % 65521)) % 4294967296) % 4294967296) % 65521) = _
  let lo := n.toNat % 4294967296
  let hi := n.toNat / 4294967296
  have hh : hi < 4294967296 := by dsimp [hi]; omega
  have h1 := Nat.mod_lt hi (by decide : 0 < 65521)
  have h2 := Nat.mod_lt lo (by decide : 0 < 65521)
  change ((lo % 65521 + (225 * (hi % 4294967296 % 65521)) % 4294967296) % 4294967296) % 65521 = _
  rw [Nat.mod_eq_of_lt hh, Nat.mod_eq_of_lt (show 225 * (hi % 65521) < 4294967296 by omega),
    Nat.mod_eq_of_lt (show lo % 65521 + 225 * (hi % 65521) < 4294967296 by omega)]
  have split := Nat.mod_add_div n.toNat 4294967296
  change lo + 4294967296 * hi = n.toNat at split
  conv => rhs; rw [← split, Nat.add_mod, Nat.mul_mod]
  change _ = (lo % 65521 + (225 * (hi % 65521)) % 65521) % 65521
  rw [Nat.add_mod_mod]

/-- Adding d to the starting low sum shifts the high sum by d per byte. -/
private def shift (d h : Nat) (s : Nat × Nat) : Nat × Nat :=
  ((s.1 + d) % 65521, (s.2 + h) % 65521)

private theorem shift_step (d h : Nat) (s : Nat × Nat) (b : UInt8) :
    referenceByte (shift d h s) b = shift d (h + d) (referenceByte s b) := by
  apply Prod.ext <;> simp only [referenceByte, shift, Nat.mod_add_mod, Nat.add_mod_mod]
  · congr 1; ac_rfl
  · congr 1; ac_rfl

private theorem shift_bytes (bs : List UInt8) (s : Nat × Nat) (d h : Nat) :
    bs.foldl referenceByte (shift d h s) =
      shift d (h + bs.length * d) (bs.foldl referenceByte s) := by
  induction bs generalizing s h with
  | nil => simp
  | cons b bs ih =>
    rw [List.foldl_cons, shift_step, ih, List.foldl_cons, List.length_cons, Nat.succ_mul]
    congr 1 <;> omega

private theorem split_checksum (s : State) (hs : reduced s) :
    ((checksum s &&& (65535 : UInt32)) = s.low) ∧ ((checksum s >>> (16 : UInt32)) = s.high) := by
  obtain ⟨hl, hh⟩ := hs
  have packed : (checksum s).toNat = s.high.toNat * 65536 + s.low.toNat := by
    simp only [checksum, UInt32.toNat_or, UInt32.toNat_shiftLeft]
    change ((s.high.toNat <<< 16) % 4294967296 ||| s.low.toNat) = _
    have bound : s.high.toNat <<< 16 < 4294967296 := by rw [Nat.shiftLeft_eq]; omega
    rw [Nat.mod_eq_of_lt bound, ← Nat.shiftLeft_add_eq_or_of_lt (show s.low.toNat < 2^16 by omega),
      Nat.shiftLeft_eq]
  constructor <;> apply UInt32.toNat_inj.mp
  · rw [low_word, packed]; omega
  · rw [high_word, packed]; omega

private theorem add_exact (a b : UInt32) (bound : a.toNat + b.toNat < 4294967296) :
    (a + b).toNat = a.toNat + b.toNat := by
  rw [UInt32.toNat_add, Nat.mod_eq_of_lt bound]

-- delta is the left low sum minus the independent right seed of one.
-- The high sum gains rightLength * delta over the right-hand chunk.
private theorem combine_state (a b : State) (n : UInt64) (ha : reduced a) (hb : reduced b) :
    let d := (a.low.toNat + 65520) % 65521
    observe (combineState a b n) =
      shift d (a.high.toNat + n.toNat * d) (observe b) := by
  obtain ⟨al, ah⟩ := ha
  obtain ⟨bl, bh⟩ := hb
  let d := residue (a.low + 65520)
  have hd : d.toNat = (a.low.toNat + 65520) % 65521 := by
    rw [show d = residue (a.low + 65520) from rfl, residue_value, add_exact]
    · rfl
    · change a.low.toNat + 65520 < 4294967296; omega
  have hdb : d.toNat ≤ 65520 := by rw [hd]; omega
  have hn : (lengthResidue n).toNat ≤ 65520 := by rw [length_value]; omega
  let cross := residue (d * lengthResidue n)
  have hc : cross.toNat = (n.toNat * d.toNat) % 65521 := by
    have bound := Nat.mul_le_mul hdb hn
    dsimp only [cross]
    rw [residue_value, UInt32.toNat_mul, Nat.mod_eq_of_lt (show d.toNat * (lengthResidue n).toNat < 2^32 by omega), length_value,
      Nat.mul_mod_mod, Nat.mul_comm]
  have hcBound : cross.toNat < 65521 := by rw [hc]; exact Nat.mod_lt _ (by decide)
  have highSum := add_exact a.high b.high (by omega)
  dsimp only
  rw [← hd]
  apply Prod.ext
  · change (residue (d + b.low)).toNat = (b.low.toNat + d.toNat) % 65521
    rw [residue_value, add_exact _ _ (by omega), Nat.add_comm]
  · change (residue (a.high + b.high + cross)).toNat = _ % 65521
    rw [residue_value, add_exact _ _ (by omega), highSum, hc, Nat.add_mod_mod]
    congr 1; ac_rfl

private theorem state_eq (a b : State) (h : observe a = observe b) : a = b := by
  have hl := congrArg Prod.fst h
  have hh := congrArg Prod.snd h
  cases a; cases b
  congr <;> apply UInt32.toNat_inj.mp <;> assumption

private theorem unpack_checksum (s : State) (hs : reduced s) : unpack (checksum s) = s := by
  obtain ⟨hl, hh⟩ := split_checksum s hs
  cases s
  simp only [unpack, hl, hh]

theorem combine_spec : Adler32Contract.combine_spec := by
  intro xs ys n hn
  let a := update initial xs
  let b := update initial ys
  have seed : reduced initial := (by decide : 1 < 65521 ∧ 0 < 65521)
  obtain ⟨ha, _⟩ := Adler32Proofs.update_reference xs initial seed
  change reduced a at ha
  obtain ⟨hb, beq⟩ := Adler32Proofs.update_reference ys initial seed
  have ⟨_, abeq⟩ := Adler32Proofs.update_reference ys a ha
  change combine (checksum a) (checksum b) n = _
  rw [combine, unpack_checksum a ha, unpack_checksum b hb]
  apply congrArg checksum
  apply state_eq
  rw [show update initial (xs ++ ys) = update a ys by simp [update, a, List.foldl_append]]
  rw [combine_state a b n ha hb, abeq]
  let d := (a.low.toNat + 65520) % 65521
  have start : shift d a.high.toNat (observe initial) = observe a := by
    obtain ⟨lo, hi⟩ := ha
    apply Prod.ext
    · change (1 + (a.low.toNat + 65520) % 65521) % 65521 = a.low.toNat
      omega
    · change (0 + a.high.toNat) % 65521 = a.high.toNat
      omega
  change shift d (a.high.toNat + n.toNat * d) (observe b) = ys.foldl referenceByte (observe a)
  rw [show observe b = _ from beq, hn, ← shift_bytes, start]

end Adler32CombineProofs
