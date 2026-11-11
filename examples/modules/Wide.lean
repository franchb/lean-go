namespace Wide

structure Results where
  sum : UInt64
  difference : UInt64
  product : UInt64
  shifted : UInt64
  equal : Bool
  less : Bool
  lessEq : Bool
  twice : UInt64
  zero : UInt64
  maximum : UInt64
  wrapped : UInt64
  above53 : UInt64
  mixed : UInt64

@[noinline] def scalar (a b : UInt64) : UInt64 := (a + 1) * (b - 1)

def compute (a b : UInt64) : Results :=
  ⟨a + b, a - b, a * b, UInt64.shiftLeft a b,
    a == b, decide (a < b), decide (a ≤ b),
    a * 2, 0, 18446744073709551615, 18446744073709551617,
    9007199254740993, scalar a b⟩

end Wide
