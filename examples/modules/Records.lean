namespace Records

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

@[noinline] def addCount (left right : Count) : Count :=
  ⟨left.value + right.value⟩

@[noinline] def flip (enabled : Enabled) : Enabled :=
  ⟨!enabled.value⟩

def update (state : State) (delta : Count) : State :=
  { state with
    count := addCount state.count delta
    nested := { state.nested with count := addCount state.nested.count state.count }
    enabled := flip state.enabled
    raw := state.raw + delta.value }

def total (state : State) (delta : Count) : Total :=
  let changed := update state delta
  ⟨changed.count.value + changed.nested.count.value + changed.raw⟩

def box (value : UInt32) : Nested := ⟨⟨value⟩⟩

end Records
