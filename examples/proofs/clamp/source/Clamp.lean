import Clamp.Bounds

namespace Clamp

/-- Return the input when it fits, otherwise return the upper limit. -/
def clamp (value limit : UInt64) : UInt64 :=
  Bounds.cap value limit

end Clamp
