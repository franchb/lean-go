namespace Quota

/-- Raw quota state. Operations check `used ≤ capacity` before changing it. -/
structure State where
  capacity : UInt64
  used : UInt64

structure Outcome where
  state : State
  accepted : Bool

/-- Create an empty quota with the requested capacity. -/
def create (capacity : UInt64) : State :=
  { capacity := capacity, used := 0 }

/-- Reserve capacity when the raw state is valid and the amount fits. -/
def reserve (state : State) (amount : UInt64) : Outcome :=
  if state.used ≤ state.capacity then
    if amount ≤ state.capacity - state.used then
      { state := { state with used := state.used + amount }, accepted := true }
    else { state := state, accepted := false }
  else { state := state, accepted := false }

/-- Release usage when the raw state is valid and the amount is already used. -/
def release (state : State) (amount : UInt64) : Outcome :=
  if state.used ≤ state.capacity then
    if amount ≤ state.used then
      { state := { state with used := state.used - amount }, accepted := true }
    else { state := state, accepted := false }
  else { state := state, accepted := false }

/-- Return available capacity, or zero for an invalid raw state. -/
def remaining (state : State) : UInt64 :=
  if state.used ≤ state.capacity then state.capacity - state.used else 0

end Quota
