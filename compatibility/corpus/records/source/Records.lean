module
public import Parts
public section
namespace Records
def increment (packet : Parts.Packet) : Parts.Packet := Parts.increment packet
def read (packet : Parts.Packet) : UInt32 := Parts.read packet
end Records
