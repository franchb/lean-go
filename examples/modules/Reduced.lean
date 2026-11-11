namespace Reduced

structure Count where
  value : UInt32

inductive Chain where
  | nil
  | cons (next : Chain)

-- The retained arguments and result keep their source wrappers at the API.
@[noinline] def add (_before : Bool) (left : Count) (_middle : UInt32)
    (right : Count) (_after : Count) : Count :=
  ⟨left.value + right.value⟩

-- Keep the owner wrapper available as the compiler's reduction witness.
@[noinline] def run (flag : Bool) (_unused : Count) (left right bias : Count) : Count :=
  let selected := if flag then left else right
  let first := add false selected 0 bias ⟨0⟩
  let second := add false first 0 bias ⟨0⟩
  let third := add false second 0 bias ⟨0⟩
  add false third 0 bias ⟨0⟩

@[noinline] def allUnused (_unused : Count) (_flag : Bool) : Count :=
  let first := add false ⟨4⟩ 0 ⟨5⟩ ⟨0⟩
  let second := add false first 0 ⟨6⟩ ⟨0⟩
  let third := add false second 0 ⟨7⟩ ⟨0⟩
  add false third 0 ⟨8⟩ ⟨0⟩

@[noinline] def recursive (chain : Chain) (_unused : Count) (value : Count) : Count :=
  match chain with
  | .nil => value
  | .cons next => add false (recursive next _unused value) 0 value ⟨0⟩

end Reduced
