import Quota
import Init.Data.UInt.Lemmas
import Lean.Elab.Tactic.Omega

namespace QuotaProofs

-- The guard's subtraction is exact because the input state is valid.
private theorem room_bound (used capacity amount : UInt64)
    (valid : used ≤ capacity) (fits : amount ≤ capacity - used) :
    used.toNat + amount.toNat ≤ capacity.toNat := by
  have hvalid := UInt64.le_iff_toNat_le.mp valid
  have hfits := UInt64.le_iff_toNat_le.mp fits
  rw [UInt64.toNat_sub_of_le capacity used valid] at hfits
  omega

-- Fitting within a UInt64 capacity makes the modular addition exact in Nat.
private theorem reserve_no_wrap (used capacity amount : UInt64)
    (valid : used ≤ capacity) (fits : amount ≤ capacity - used) :
    (used + amount).toNat = used.toNat + amount.toNat := by
  have bound := room_bound used capacity amount valid fits
  have limit := capacity.toNat_lt
  rw [UInt64.toNat_add, Nat.mod_eq_of_lt]
  omega

theorem create_valid (capacity : UInt64) :
    (Quota.create capacity).used ≤ (Quota.create capacity).capacity := by
  simp [Quota.create]

theorem reserve_accepted (state : Quota.State) (amount : UInt64) :
    (Quota.reserve state amount).accepted = true ↔
      state.used ≤ state.capacity ∧ amount ≤ state.capacity - state.used := by
  by_cases valid : state.used ≤ state.capacity
  · by_cases fits : amount ≤ state.capacity - state.used
    · simp [Quota.reserve, valid, fits]
    · simp [Quota.reserve, valid, fits]
  · simp [Quota.reserve, valid]

theorem reserve_preserves (state : Quota.State) (amount : UInt64)
    (valid : state.used ≤ state.capacity) :
    (Quota.reserve state amount).state.used ≤ (Quota.reserve state amount).state.capacity := by
  by_cases fits : amount ≤ state.capacity - state.used
  · simp only [Quota.reserve, valid, fits, ↓reduceIte]
    apply UInt64.le_iff_toNat_le.mpr
    rw [reserve_no_wrap state.used state.capacity amount valid fits]
    exact room_bound state.used state.capacity amount valid fits
  · simp [Quota.reserve, valid, fits]

theorem reserve_rejected (state : Quota.State) (amount : UInt64)
    (rejected : (Quota.reserve state amount).accepted = false) :
    (Quota.reserve state amount).state = state := by
  by_cases valid : state.used ≤ state.capacity
  · by_cases fits : amount ≤ state.capacity - state.used
    · simp [Quota.reserve, valid, fits] at rejected
    · simp [Quota.reserve, valid, fits]
  · simp [Quota.reserve, valid]

theorem reserve_exact (state : Quota.State) (amount : UInt64)
    (accepted : (Quota.reserve state amount).accepted = true) :
    (Quota.reserve state amount).state.capacity = state.capacity ∧
      (Quota.reserve state amount).state.used.toNat = state.used.toNat + amount.toNat := by
  obtain ⟨valid, fits⟩ := (reserve_accepted state amount).mp accepted
  simp only [Quota.reserve, valid, fits, ↓reduceIte, true_and]
  exact reserve_no_wrap state.used state.capacity amount valid fits

theorem release_accepted (state : Quota.State) (amount : UInt64) :
    (Quota.release state amount).accepted = true ↔
      state.used ≤ state.capacity ∧ amount ≤ state.used := by
  by_cases valid : state.used ≤ state.capacity
  · by_cases fits : amount ≤ state.used
    · simp [Quota.release, valid, fits]
    · simp [Quota.release, valid, fits]
  · simp [Quota.release, valid]

theorem release_preserves (state : Quota.State) (amount : UInt64)
    (valid : state.used ≤ state.capacity) :
    (Quota.release state amount).state.used ≤ (Quota.release state amount).state.capacity := by
  by_cases fits : amount ≤ state.used
  · simp only [Quota.release, valid, fits, ↓reduceIte]
    apply UInt64.le_iff_toNat_le.mpr
    rw [UInt64.toNat_sub_of_le state.used amount fits]
    have hvalid := UInt64.le_iff_toNat_le.mp valid
    omega
  · simp [Quota.release, valid, fits]

theorem release_rejected (state : Quota.State) (amount : UInt64)
    (rejected : (Quota.release state amount).accepted = false) :
    (Quota.release state amount).state = state := by
  by_cases valid : state.used ≤ state.capacity
  · by_cases fits : amount ≤ state.used
    · simp [Quota.release, valid, fits] at rejected
    · simp [Quota.release, valid, fits]
  · simp [Quota.release, valid]

theorem release_exact (state : Quota.State) (amount : UInt64)
    (accepted : (Quota.release state amount).accepted = true) :
    (Quota.release state amount).state.capacity = state.capacity ∧
      (Quota.release state amount).state.used.toNat + amount.toNat = state.used.toNat := by
  obtain ⟨valid, fits⟩ := (release_accepted state amount).mp accepted
  simp only [Quota.release, valid, fits, ↓reduceIte, true_and]
  rw [UInt64.toNat_sub_of_le state.used amount fits]
  have hfits := UInt64.le_iff_toNat_le.mp fits
  omega

theorem remaining_exact (state : Quota.State) (valid : state.used ≤ state.capacity) :
    (Quota.remaining state).toNat + state.used.toNat = state.capacity.toNat := by
  simp only [Quota.remaining, valid, ↓reduceIte]
  rw [UInt64.toNat_sub_of_le state.capacity state.used valid]
  have hvalid := UInt64.le_iff_toNat_le.mp valid
  omega

end QuotaProofs
