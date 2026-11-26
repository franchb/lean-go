import Wire16
import Crc16

namespace CheckedFrame

def headerBytes (q : ByteQuad) : List UInt8 := [q.b0, q.b1, q.b2, q.b3]

def trailerBytes (p : BytePair) : List UInt8 := [p.hi, p.lo]

def checksum (body : List UInt8) : UInt16 := Crc16.update 0 body

/-- The declared length counts payload bytes. The checksum covers header and payload. -/
def encode (flags : UInt8) (payload : List UInt8) : Option (List UInt8) :=
  if payload.length < 65536 then
    match Wire16.encode ⟨flags, UInt16.ofNat payload.length⟩ with
    | .reservedFlags => none
    | .encoded q =>
      let body := headerBytes q ++ payload
      some (body ++ trailerBytes (Wire16.encodeBE (checksum body)))
  else none

/-- Decode the first frame and preserve the following bytes. -/
def decode : List UInt8 → Option (UInt8 × List UInt8 × List UInt8)
  | a :: b :: c :: d :: rest =>
    let q : ByteQuad := ⟨a, b, c, d⟩
    match Wire16.parse q with
    | .parsed h =>
      let payload := rest.take h.length.toNat
      match rest.drop h.length.toNat with
      | hi :: lo :: suffix =>
        if Wire16.decodeBE ⟨hi, lo⟩ = checksum (headerBytes q ++ payload)
        then some (h.flags, payload, suffix) else none
      | _ => none
    | _ => none
  | _ => none

end CheckedFrame
