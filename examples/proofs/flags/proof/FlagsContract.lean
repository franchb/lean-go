import Flags

namespace FlagsContract

-- Independent closed requirements: no proof module or theorem is reused here.
def set_idempotent : Prop := ∀ state mask : UInt32,
  Flags.set (Flags.set state mask) mask = Flags.set state mask

def clear_idempotent : Prop := ∀ state mask : UInt32,
  Flags.clear (Flags.clear state mask) mask = Flags.clear state mask

def cleared_bits_absent : Prop := ∀ state mask : UInt32,
  Flags.clear state mask &&& mask = 0

def toggle_involution : Prop := ∀ state mask : UInt32,
  Flags.toggle (Flags.toggle state mask) mask = state

def set_untouched : Prop := ∀ state mask : UInt32,
  Flags.set state mask &&& ~~~mask = state &&& ~~~mask

def clear_untouched : Prop := ∀ state mask : UInt32,
  Flags.clear state mask &&& ~~~mask = state &&& ~~~mask

def toggle_untouched : Prop := ∀ state mask : UInt32,
  Flags.toggle state mask &&& ~~~mask = state &&& ~~~mask

def contains_set : Prop := ∀ state mask : UInt32,
  Flags.contains (Flags.set state mask) mask = true

end FlagsContract
