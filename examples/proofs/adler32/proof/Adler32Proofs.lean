import Adler32Contract
import Init.Data.UInt.Lemmas

namespace Adler32Proofs
open Adler32Contract

private theorem add_reduce (a b : UInt32) (ha : a.toNat < 65521) (hb : b.toNat < 65521) :
    (Adler32.reduce (a + b)).toNat = (a.toNat + b.toNat) % 65521 := by
  have sum : (a + b).toNat = a.toNat + b.toNat := by
    rw [UInt32.toNat_add, Nat.mod_eq_of_lt (by omega)]
  unfold Adler32.reduce
  split
  next h =>
    rw [UInt32.lt_iff_toNat_lt] at h
    change (a + b).toNat < 65521 at h
    rw [sum, Nat.mod_eq_of_lt (by omega)]
  next h =>
    have ge : (65521 : UInt32) ≤ a + b := by
      rw [UInt32.le_iff_toNat_le]
      rw [UInt32.lt_iff_toNat_lt] at h
      exact Nat.le_of_not_lt h
    rw [UInt32.toNat_sub_of_le _ _ ge, sum]
    change a.toNat + b.toNat - 65521 = (a.toNat + b.toNat) % 65521
    rw [UInt32.le_iff_toNat_le, sum] at ge
    change 65521 ≤ a.toNat + b.toNat at ge
    rw [Nat.mod_eq_sub_mod ge, Nat.mod_eq_of_lt (by omega)]

theorem step_spec : Adler32Contract.step_spec := by
  intro s b hs
  obtain ⟨ha, hb⟩ := hs
  have byte : b.toUInt32.toNat < 65521 := by
    have h : b.toNat < 256 := b.toNat_lt_size
    simpa only [UInt8.toNat_toUInt32] using (show b.toNat < 65521 by omega)
  have low := add_reduce s.low b.toUInt32 ha byte
  rw [UInt8.toNat_toUInt32] at low
  have bound : (Adler32.reduce (s.low + b.toUInt32)).toNat < 65521 := by
    rw [low]; exact Nat.mod_lt _ (by decide)
  simp only [observe, Adler32.step, referenceByte, add_reduce _ _ hb bound, low]

private theorem preserves (s : Adler32.State) (b : UInt8) (hs : reduced s) :
    reduced (Adler32.step s b) := by
  have h := step_spec s b hs
  have lo := congrArg Prod.fst h
  have hi := congrArg Prod.snd h
  change (observe _).1 < 65521 ∧ (observe _).2 < 65521
  rw [lo, hi]
  exact ⟨Nat.mod_lt _ (by decide), Nat.mod_lt _ (by decide)⟩

private theorem bytes (bs : List UInt8) (s : Adler32.State) (hs : reduced s) :
    reduced (Adler32.update s bs) ∧
    observe (Adler32.update s bs) = bs.foldl referenceByte (observe s) := by
  induction bs generalizing s with
  | nil => exact ⟨hs, rfl⟩
  | cons b bs ih =>
    obtain ⟨bound, eqn⟩ := ih (Adler32.step s b) (preserves s b hs)
    exact ⟨bound, eqn.trans (by rw [step_spec s b hs]; rfl)⟩

private theorem chunks (cs : List (List UInt8)) (s : Adler32.State) (hs : reduced s) :
    observe (cs.foldl Adler32.update s) = cs.flatten.foldl referenceByte (observe s) := by
  induction cs generalizing s with
  | nil => rfl
  | cons c cs ih =>
    obtain ⟨bound, eqn⟩ := bytes c s hs
    simp only [List.foldl_cons, List.flatten_cons, List.foldl_append, ih _ bound, eqn]

theorem chunks_spec : Adler32Contract.chunks_spec := fun cs => chunks cs Adler32.initial (by decide : 1 < 65521 ∧ 0 < 65521)

end Adler32Proofs
