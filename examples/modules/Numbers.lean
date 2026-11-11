namespace Numbers

inductive Results where
  | mk (sum difference product shifted : UInt32)
    (equal less lessEq : Bool)
    (twice zero maximum wrapped mixed : UInt32)

def scalar (a b : UInt32) : UInt32 := (a + 1) * (b - 1)

def compute (a b : UInt32) : Results :=
  .mk (a + b) (a - b) (a * b) (UInt32.shiftLeft a b)
    (a == b) (decide (a < b)) (decide (a ≤ b))
    (a * 2) 0 4294967295 4294967297 (scalar a b)

end Numbers
