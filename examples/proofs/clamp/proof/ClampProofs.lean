import Clamp
import Init.Data.UInt.Lemmas

namespace ClampProofs

theorem bounded (value limit : UInt64) : Clamp.clamp value limit ≤ limit := by
  by_cases within : value ≤ limit
  · simp [Clamp.clamp, Clamp.Bounds.cap, within]
  · simp [Clamp.clamp, Clamp.Bounds.cap, within]

theorem preserves (value limit : UInt64) (within : value ≤ limit) :
    Clamp.clamp value limit = value := by
  simp [Clamp.clamp, Clamp.Bounds.cap, within]

theorem idempotent (value limit : UInt64) :
    Clamp.clamp (Clamp.clamp value limit) limit = Clamp.clamp value limit := by
  exact preserves (Clamp.clamp value limit) limit (bounded value limit)

end ClampProofs
