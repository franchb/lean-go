import Clamp

namespace ClampContract

-- Independent requirements: these definitions do not import or reuse the proofs.
def bounded : Prop := ∀ value limit : UInt64, Clamp.clamp value limit ≤ limit

def preserves : Prop := ∀ value limit : UInt64,
  value ≤ limit → Clamp.clamp value limit = value

def idempotent : Prop := ∀ value limit : UInt64,
  Clamp.clamp (Clamp.clamp value limit) limit = Clamp.clamp value limit

end ClampContract
