@[noinline] def original (x : UInt32) : UInt32 := x + 1
@[noinline] def replacement (x : UInt32) : UInt32 := original x
theorem replacement_eq : original = replacement := rfl
attribute [csimp] replacement_eq
def run (x : UInt32) : UInt32 := original x
