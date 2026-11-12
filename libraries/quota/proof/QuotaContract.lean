import Quota

namespace QuotaContract

-- These raw propositions are deliberately independent of aliases in the proof file.
def expectedCreate : Prop := ∀ c : UInt64,
  (Quota.create c).used ≤ (Quota.create c).capacity

def expectedReserveAccepted : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.reserve s n).accepted = true ↔ s.used ≤ s.capacity ∧ n ≤ s.capacity - s.used

def expectedReservePreserves : Prop := ∀ (s : Quota.State) (n : UInt64),
  s.used ≤ s.capacity → (Quota.reserve s n).state.used ≤ (Quota.reserve s n).state.capacity

def expectedReserveRejected : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.reserve s n).accepted = false → (Quota.reserve s n).state = s

def expectedReserveExact : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.reserve s n).accepted = true →
  (Quota.reserve s n).state.capacity = s.capacity ∧
    (Quota.reserve s n).state.used.toNat = s.used.toNat + n.toNat

def expectedReleaseAccepted : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.release s n).accepted = true ↔ s.used ≤ s.capacity ∧ n ≤ s.used

def expectedReleasePreserves : Prop := ∀ (s : Quota.State) (n : UInt64),
  s.used ≤ s.capacity → (Quota.release s n).state.used ≤ (Quota.release s n).state.capacity

def expectedReleaseRejected : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.release s n).accepted = false → (Quota.release s n).state = s

def expectedReleaseExact : Prop := ∀ (s : Quota.State) (n : UInt64),
  (Quota.release s n).accepted = true →
  (Quota.release s n).state.capacity = s.capacity ∧
    (Quota.release s n).state.used.toNat + n.toNat = s.used.toNat

def expectedRemaining : Prop := ∀ s : Quota.State, s.used ≤ s.capacity →
  (Quota.remaining s).toNat + s.used.toNat = s.capacity.toNat

end QuotaContract
