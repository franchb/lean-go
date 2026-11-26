import CheckedFrameContract
import Wire16Proofs
import Crc16Proofs

namespace CheckedFrameProofs
open CheckedFrame

private theorem decodeParts (q : ByteQuad) (h : Header) (payload suffix : List UInt8)
    (tag : BytePair) (parsed : Wire16.parse q = .parsed h) (size : h.length.toNat = payload.length) :
    decode (headerBytes q ++ payload ++ trailerBytes tag ++ suffix) =
      if Wire16.decodeBE tag = checksum (headerBytes q ++ payload)
      then some (h.flags, payload, suffix) else none := by
  by_cases same : Wire16.decodeBE tag = checksum (headerBytes q ++ payload)
  all_goals
    cases q
    cases tag
    simp [headerBytes, trailerBytes, List.append_assoc, decode, parsed, size] at same ⊢
    simp [same]

theorem roundtrip (flags : UInt8) (payload suffix : List UInt8)
    (valid : flags.toNat < 4) (fits : payload.length < 65536) :
    ∃ bytes, encode flags payload = some bytes ∧ decode (bytes ++ suffix) = some (flags, payload, suffix) := by
  let h : Header := ⟨flags, UInt16.ofNat payload.length⟩
  have size : h.length.toNat = payload.length := by simp [h, Nat.mod_eq_of_lt fits]
  obtain ⟨q, enc, parsed⟩ := Wire16Proofs.parse_encode_roundtrip h valid
  let tag := Wire16.encodeBE (checksum (headerBytes q ++ payload))
  refine ⟨(headerBytes q ++ payload) ++ trailerBytes tag, ?_, ?_⟩
  · have encoded : Wire16.encode ⟨flags, UInt16.ofNat payload.length⟩ = .encoded q := enc
    simp [encode, fits, encoded, tag]
  · rw [decodeParts q h payload suffix tag parsed size]
    simp [tag, Wire16Proofs.decode_encode_be, h]

theorem accepts_reference_crc (q : ByteQuad) (h : Header) (payload suffix : List UInt8)
    (tag : BytePair) (parsed : Wire16.parse q = .parsed h) (size : h.length.toNat = payload.length) :
    (decode (headerBytes q ++ payload ++ trailerBytes tag ++ suffix) = some (h.flags, payload, suffix) ↔
      (Wire16.decodeBE tag).toBitVec = (headerBytes q ++ payload).foldl Crc16Contract.referenceByte 0) := by
  rw [decodeParts q h payload suffix tag parsed size]
  have ref : (checksum (headerBytes q ++ payload)).toBitVec =
      (headerBytes q ++ payload).foldl Crc16Contract.referenceByte 0 := by
    simpa [checksum] using Crc16Proofs.chunks_spec 0 [headerBytes q ++ payload]
  rw [← ref]
  by_cases same : Wire16.decodeBE tag = checksum (headerBytes q ++ payload)
  · simp [same]
  · simp [same, UInt16.toBitVec_inj]

end CheckedFrameProofs
