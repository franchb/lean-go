-- Copy of examples/proofs/clamp/source/Clamp/Bounds.lean and Clamp.lean (merged).
namespace Clamp.Bounds

/-- Limit a value without arithmetic or machine-integer overflow. -/
def cap (value limit : UInt64) : UInt64 :=
  if value ≤ limit then value else limit

end Clamp.Bounds

namespace Clamp

/-- Return the input when it fits, otherwise return the upper limit. -/
def clamp (value limit : UInt64) : UInt64 :=
  Bounds.cap value limit

end Clamp
