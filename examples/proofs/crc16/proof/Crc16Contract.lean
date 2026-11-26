import Crc16

namespace Crc16Contract

/-- Feed one message bit into the non-reflected XMODEM register. Feedback at
    x^16 adds x^12 + x^5 + 1 over GF(2). -/
def bitStep (state : BitVec 16) (input : Bool) : BitVec 16 :=
  (state <<< 1) ^^^ (if state.msb ^^ input then 0x1021 else 0)

/-- Consume leading message bits one at a time, starting at bit 7. -/
def consumeBits : Nat → BitVec 16 → BitVec 8 → BitVec 16
  | 0, state, _ => state
  | n + 1, state, byte => consumeBits n (bitStep state byte.msb) (byte <<< 1)

def referenceByte (state : BitVec 16) (byte : UInt8) : BitVec 16 :=
  consumeBits 8 state byte.toBitVec

def update_byte_spec : Prop := ∀ (state : UInt16) (byte : UInt8),
  (Crc16.updateByte state byte).toBitVec = referenceByte state.toBitVec byte

def chunks_spec : Prop := ∀ (state : UInt16) (chunks : List (List UInt8)),
  (chunks.foldl (fun acc chunk => chunk.foldl Crc16.updateByte acc) state).toBitVec =
    chunks.flatten.foldl referenceByte state.toBitVec

end Crc16Contract
