namespace Unsigned

-- Every operation of one unsigned width, in ordinary notation.
structure Ops8 where
  add : UInt8
  sub : UInt8
  mul : UInt8
  div : UInt8
  mod : UInt8
  and : UInt8
  or : UInt8
  xor : UInt8
  not : UInt8
  shl : UInt8
  shr : UInt8
  eq : Bool
  lt : Bool
  le : Bool
  to16 : UInt16
  to32 : UInt32
  to64 : UInt64

structure Ops16 where
  add : UInt16
  sub : UInt16
  mul : UInt16
  div : UInt16
  mod : UInt16
  and : UInt16
  or : UInt16
  xor : UInt16
  not : UInt16
  shl : UInt16
  shr : UInt16
  eq : Bool
  lt : Bool
  le : Bool
  to8 : UInt8
  to32 : UInt32
  to64 : UInt64

structure Ops32 where
  add : UInt32
  sub : UInt32
  mul : UInt32
  div : UInt32
  mod : UInt32
  and : UInt32
  or : UInt32
  xor : UInt32
  not : UInt32
  shl : UInt32
  shr : UInt32
  eq : Bool
  lt : Bool
  le : Bool
  to8 : UInt8
  to16 : UInt16
  to64 : UInt64

structure Ops64 where
  add : UInt64
  sub : UInt64
  mul : UInt64
  div : UInt64
  mod : UInt64
  and : UInt64
  or : UInt64
  xor : UInt64
  not : UInt64
  shl : UInt64
  shr : UInt64
  eq : Bool
  lt : Bool
  le : Bool
  to8 : UInt8
  to16 : UInt16
  to32 : UInt32

def ops8 (a b : UInt8) : Ops8 :=
  ⟨a + b, a - b, a * b, a / b, a % b, a &&& b, a ||| b, a ^^^ b, ~~~a, a <<< b, a >>> b,
   a == b, decide (a < b), decide (a ≤ b), a.toUInt16, a.toUInt32, a.toUInt64⟩

def ops16 (a b : UInt16) : Ops16 :=
  ⟨a + b, a - b, a * b, a / b, a % b, a &&& b, a ||| b, a ^^^ b, ~~~a, a <<< b, a >>> b,
   a == b, decide (a < b), decide (a ≤ b), a.toUInt8, a.toUInt32, a.toUInt64⟩

def ops32 (a b : UInt32) : Ops32 :=
  ⟨a + b, a - b, a * b, a / b, a % b, a &&& b, a ||| b, a ^^^ b, ~~~a, a <<< b, a >>> b,
   a == b, decide (a < b), decide (a ≤ b), a.toUInt8, a.toUInt16, a.toUInt64⟩

def ops64 (a b : UInt64) : Ops64 :=
  ⟨a + b, a - b, a * b, a / b, a % b, a &&& b, a ||| b, a ^^^ b, ~~~a, a <<< b, a >>> b,
   a == b, decide (a < b), decide (a ≤ b), a.toUInt8, a.toUInt16, a.toUInt32⟩

-- Forms the pinned compiler rewrites or keeps: `a / 16` becomes a right
-- shift, `a * 8` a left shift, `a % 0` and `a / 1` fold to `a`, while
-- `a % 16` and `a / 0` remain remainder and division leaves.
structure Folded where
  quotient : UInt16
  remainder : UInt16
  scaled : UInt16
  byZero : UInt16
  modZero : UInt16
  byOne : UInt16

def folded (a : UInt16) : Folded :=
  ⟨a / 16, a % 16, a * 8, a / 0, a % 0, a / 1⟩

end Unsigned
