namespace Helpers
@[noinline] def add (x y : UInt32) : UInt32 := x + y
@[noinline] def reduced (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=
  let selected := if flag then x else y
  let first := add selected bias
  let second := add first bias
  let third := add second bias
  add third bias
@[noinline] def captured (bias x y : UInt32) : UInt32 :=
  let bump := fun (a : UInt32) =>
    let value := add a bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    value
  add (bump x) (bump y)
end Helpers
