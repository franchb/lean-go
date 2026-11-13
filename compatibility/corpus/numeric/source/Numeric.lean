namespace Numeric
structure Result32 where
  arithmetic : UInt32
  eq : Bool
  lt : Bool
  le : Bool
structure Result64 where
  arithmetic : UInt64
  eq : Bool
  lt : Bool
  le : Bool
def calc32 (a b : UInt32) : Result32 :=
  ⟨((a + 1) - b) * 3, decide (a = b), decide (a < b), decide (a ≤ b)⟩
def calc64 (a b : UInt64) : Result64 :=
  ⟨((a + 1) - b) * 3, decide (a = b), decide (a < b), decide (a ≤ b)⟩
end Numeric
