module
public section
namespace Parts
structure Count where
  value : UInt32
structure Packet where
  count : Count
  enabled : Bool
@[noinline] def increment (packet : Packet) : Packet :=
  { packet with count := ⟨packet.count.value + 1⟩ }
@[noinline] def read (packet : Packet) : UInt32 := packet.count.value
end Parts
