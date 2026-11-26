namespace ByteFold

def step (state : UInt32) (byte : UInt8) : UInt32 := state + byte.toUInt32

def run (state : UInt32) (bytes : List UInt8) : UInt32 := bytes.foldl step state

end ByteFold
