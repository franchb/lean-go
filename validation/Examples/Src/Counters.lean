namespace Counters

structure Count where
  value : UInt32

structure State where
  count : Count
  total : UInt64
  enabled : Bool

@[noinline] def step (state : State) (delta : Count) : State :=
  { state with count := ⟨state.count.value + delta.value⟩ }

def initial (value : UInt32) (total : UInt64) (enabled : Bool) : State :=
  { count := ⟨value⟩, total := total, enabled := enabled }

def advance (state : State) (delta : Count) : State :=
  if state.enabled then step state delta else state

def read (state : State) : UInt32 := state.count.value

def total (state : State) : UInt64 := state.total

end Counters
