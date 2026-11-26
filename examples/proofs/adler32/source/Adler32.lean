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

/-- Replace a factor 2^16 by its residue 15 modulo 65521. -/
def fold16 (x : UInt32) : UInt32 := (x &&& (65535 : UInt32)) + (x >>> (16 : UInt32)) * (15 : UInt32)

def residue (x : UInt32) : UInt32 := reduce (fold16 (fold16 x))

/-- The high 32-bit limb has weight 225 modulo 65521. -/
def lengthResidue (n : UInt64) : UInt32 :=
  residue (residue n.toUInt32 + (225 : UInt32) * residue ((n >>> (32 : UInt64)).toUInt32))

def unpack (word : UInt32) : State := ⟨word &&& 65535, word >>> 16⟩

def combineState (left right : State) (rightLength : UInt64) : State :=
  let delta := residue (left.low + 65520)
  let low := residue (delta + right.low)
  let cross := residue (delta * lengthResidue rightLength)
  ⟨low, residue (left.high + right.high + cross)⟩

def combine (left right : UInt32) (rightLength : UInt64) : UInt32 :=
  checksum (combineState (unpack left) (unpack right) rightLength)

end Adler32
