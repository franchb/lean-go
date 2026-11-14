namespace Bytes
structure Result where
  add : UInt8
  sub : UInt8
  mul : UInt8
  left : UInt8
  and : UInt8
  or : UInt8
  xor : UInt8
  not : UInt8
  right : UInt8
  eq : Bool
  lt : Bool
  le : Bool
  wide : UInt32
  narrow : UInt8
  maximum : UInt8
  wrapped : UInt8

def calculate (a b : UInt8) (word : UInt32) : Result :=
  ⟨a + b, a - b, a * b, a <<< b, a &&& b, a ||| b, a ^^^ b, ~~~a, a >>> b,
   decide (a = b), decide (a < b), decide (a ≤ b), a.toUInt32, word.toUInt8, 255, 257⟩
def widen (a : UInt8) : UInt32 := a.toUInt32
def narrow (a : UInt32) : UInt8 := a.toUInt8
@[noinline] def helper (a b : UInt8) : UInt8 := a + b
def choose (flag : Bool) (a b : UInt8) : UInt8 :=
  if flag then helper a b else helper b a
end Bytes
