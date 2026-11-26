namespace Crc16

/-- Multiply the register by x, reducing by the XMODEM polynomial on overflow. -/
def step (state : UInt16) : UInt16 :=
  let shifted := state <<< 1
  if state.toUInt32 < 0x8000 then shifted else shifted ^^^ 0x1021

/-- Update one byte, with its most significant bit first. -/
def updateByte (state : UInt16) (byte : UInt8) : UInt16 :=
  let loaded := state ^^^ (byte.toUInt16 <<< 8)
  let a := step (step loaded)
  let b := step (step a)
  let c := step (step b)
  step (step c)

end Crc16
