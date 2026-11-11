namespace Joins

structure Count where
  value : UInt32

structure Nested where
  count : Count

structure Enabled where
  value : Bool

structure Total where
  value : UInt32

structure State where
  count : Count
  nested : Nested
  enabled : Enabled
  raw : UInt32

-- Keep calls visible in pure mono so the compiler retains shared continuations.
@[noinline] def add (left right : UInt32) : UInt32 := left + right

def update (state : State) (delta : Count) (flag : Bool) : State :=
  let selected := if flag then delta.value else state.count.value
  let nested := match state.enabled.value with
    | true =>
      let branch := if decide (selected < state.raw) then selected else state.raw
      let first := add branch state.nested.count.value
      let second := add first delta.value
      let third := add second state.count.value
      add third state.raw
    | false => add state.nested.count.value selected
  { state with
    count := ⟨add selected state.count.value⟩
    nested := ⟨⟨nested⟩⟩
    enabled := ⟨!state.enabled.value⟩
    raw := add state.raw selected }

-- The continuation returns an erased scalar wrapper at the source boundary.
def total (state : State) (delta : Count) (flag : Bool) : Total :=
  let selected := if flag then delta.value else state.count.value
  let first := add selected state.raw
  let second := add first state.nested.count.value
  let third := add second state.count.value
  ⟨add third delta.value⟩

end Joins
