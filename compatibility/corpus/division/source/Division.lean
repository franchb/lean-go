namespace Division

structure Q8 where
  quotient : UInt8
  remainder : UInt8

structure Q16 where
  quotient : UInt16
  remainder : UInt16

structure Q32 where
  quotient : UInt32
  remainder : UInt32

structure Q64 where
  quotient : UInt64
  remainder : UInt64

def div8 (a b : UInt8) : Q8 := ⟨a / b, a % b⟩
def div16 (a b : UInt16) : Q16 := ⟨a / b, a % b⟩
def div32 (a b : UInt32) : Q32 := ⟨a / b, a % b⟩
def div64 (a b : UInt64) : Q64 := ⟨a / b, a % b⟩
-- The pinned compiler turns this quotient into a right shift and keeps the remainder.
def halves (a : UInt32) : Q32 := ⟨a / 65536, a % 65536⟩

end Division
