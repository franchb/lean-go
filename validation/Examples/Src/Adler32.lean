namespace Adler32

structure State where
  low : UInt32
  high : UInt32
  deriving DecidableEq

def initial : State := ⟨1, 0⟩

-- Both addends are already reduced, so one subtraction is enough.
def reduce (sum : UInt32) : UInt32 := if sum < 65521 then sum else sum - 65521

def step (state : State) (byte : UInt8) : State :=
  let low := reduce (state.low + byte.toUInt32)
  ⟨low, reduce (state.high + low)⟩

def update (state : State) (bytes : List UInt8) : State := bytes.foldl step state

def checksum (state : State) : UInt32 := (state.high <<< 16) ||| state.low

end Adler32
