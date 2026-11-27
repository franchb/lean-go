import Clock
import Init.Data.UInt.Lemmas

namespace ClockProofs
open Clock

-- Each proof moves to natural numbers, where `omega` decides the
-- division and remainder facts by the constants 3600 and 60.
theorem recombines (total : UInt32) : join (split total) = total := by
  apply UInt32.toNat_inj.mp
  have bound := total.toNat_lt
  simp only [join, split, UInt32.toNat_add, UInt32.toNat_mul, UInt32.toNat_div, UInt32.toNat_mod,
    UInt32.reduceToNat]
  omega

theorem minutesBelow60 (total : UInt32) : (split total).minutes < 60 := by
  simp only [split, UInt32.lt_iff_toNat_lt, UInt32.toNat_div, UInt32.toNat_mod, UInt32.reduceToNat]
  omega

theorem secondsBelow60 (total : UInt32) : (split total).seconds < 60 := by
  simp only [split, UInt32.lt_iff_toNat_lt, UInt32.toNat_mod, UInt32.reduceToNat]
  omega

end ClockProofs
