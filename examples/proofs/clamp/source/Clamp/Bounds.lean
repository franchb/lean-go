namespace Clamp.Bounds

/-- Limit a value without arithmetic or machine-integer overflow. -/
def cap (value limit : UInt64) : UInt64 :=
  if value ≤ limit then value else limit

end Clamp.Bounds
