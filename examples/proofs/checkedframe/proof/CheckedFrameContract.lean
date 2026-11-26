import CheckedFrame
import Crc16Contract

namespace CheckedFrameContract

def roundtrip : Prop :=
  ∀ (flags : UInt8) (payload suffix : List UInt8), flags.toNat < 4 → payload.length < 65536 →
    ∃ bytes, CheckedFrame.encode flags payload = some bytes ∧
      CheckedFrame.decode (bytes ++ suffix) = some (flags, payload, suffix)

def accepts_reference_crc : Prop :=
  ∀ (q : ByteQuad) (h : Header) (payload suffix : List UInt8) (tag : BytePair),
    Wire16.parse q = .parsed h → h.length.toNat = payload.length →
    (CheckedFrame.decode (CheckedFrame.headerBytes q ++ payload ++ CheckedFrame.trailerBytes tag ++ suffix) =
        some (h.flags, payload, suffix) ↔
      (Wire16.decodeBE tag).toBitVec =
        (CheckedFrame.headerBytes q ++ payload).foldl Crc16Contract.referenceByte 0)

end CheckedFrameContract
