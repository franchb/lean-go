namespace Bitwise
structure Result32 where
  masked : UInt32
  merged : UInt32
  toggled : UInt32
  inverted : UInt32
  shifted : UInt32
def calc32 (a b : UInt32) : Result32 :=
  ⟨a &&& b, a ||| b, a ^^^ b, ~~~a, a >>> b⟩
structure Result64 where
  masked : UInt64
  merged : UInt64
  toggled : UInt64
  inverted : UInt64
  shifted : UInt64
def calc64 (a b : UInt64) : Result64 :=
  ⟨a &&& b, a ||| b, a ^^^ b, ~~~a, a >>> b⟩
end Bitwise
