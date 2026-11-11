namespace Local

structure Count where
  value : UInt32

structure Nested where
  count : Count

structure Config where
  bias : Nested
  enabled : Bool

@[noinline] def add (left right : UInt32) : UInt32 := left + right

-- The repeated calls keep bump as a genuine compiler-lifted local helper.
@[noinline] def run (config : Config) (x y : Count) : Count :=
  let bump := fun (a : Count) =>
    let bias := if config.enabled then config.bias.count.value else a.value
    let value := add a.value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    let value := add value bias
    Count.mk value
  ⟨add (bump x).value (bump y).value⟩

end Local
