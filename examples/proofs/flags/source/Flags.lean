namespace Flags

/-- Set every bit selected by the mask. -/
def set (state mask : UInt32) : UInt32 := state ||| mask

/-- Clear every bit selected by the mask. -/
def clear (state mask : UInt32) : UInt32 := state &&& ~~~mask

/-- Toggle every bit selected by the mask. -/
def toggle (state mask : UInt32) : UInt32 := state ^^^ mask

/-- Test whether all masked bits are present; the zero mask is always contained. -/
def contains (state mask : UInt32) : Bool := (state &&& mask) == mask

end Flags
