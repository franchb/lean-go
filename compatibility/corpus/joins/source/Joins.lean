namespace Joins
@[noinline] def add (x y : UInt32) : UInt32 := x + y
def run (a b : Bool) (x y z bias : UInt32) : UInt32 :=
  let first := if a then x else y
  let second := if b then first else z
  let one := add second bias
  let two := add one bias
  let three := add two bias
  add three bias
end Joins
