import Wire16

namespace Wire16Contract

def encode_be_bytes : Prop :=
  ∀ n : UInt16, (Wire16.encodeBE n).hi.toNat = n.toNat / 256 ∧
    (Wire16.encodeBE n).lo.toNat = n.toNat % 256

def decode_be_value : Prop :=
  ∀ p : BytePair, (Wire16.decodeBE p).toNat = p.hi.toNat * 256 + p.lo.toNat

def decode_encode_be : Prop :=
  ∀ n : UInt16, Wire16.decodeBE (Wire16.encodeBE n) = n

def encode_decode_be : Prop :=
  ∀ p : BytePair, Wire16.encodeBE (Wire16.decodeBE p) = p

def parse_success_iff : Prop :=
  ∀ (q : ByteQuad) (h : Header), Wire16.parse q = .parsed h ↔
    q.b0 = 1 ∧ q.b1.toNat < 4 ∧ h.flags = q.b1 ∧
      h.length.toNat = q.b2.toNat * 256 + q.b3.toNat

def parse_unsupported_version_iff : Prop :=
  ∀ q : ByteQuad, Wire16.parse q = .unsupportedVersion ↔ q.b0 ≠ 1

def parse_reserved_flags_iff : Prop :=
  ∀ q : ByteQuad, Wire16.parse q = .reservedFlags ↔ q.b0 = 1 ∧ 4 ≤ q.b1.toNat

def encode_success_iff : Prop :=
  ∀ (h : Header) (q : ByteQuad), Wire16.encode h = .encoded q ↔
    h.flags.toNat < 4 ∧ q.b0 = 1 ∧ q.b1 = h.flags ∧
      q.b2.toNat = h.length.toNat / 256 ∧ q.b3.toNat = h.length.toNat % 256

def encode_reserved_flags_iff : Prop :=
  ∀ h : Header, Wire16.encode h = .reservedFlags ↔ 4 ≤ h.flags.toNat

def parse_encode_roundtrip : Prop :=
  ∀ h : Header, h.flags.toNat < 4 →
    ∃ q : ByteQuad, Wire16.encode h = .encoded q ∧ Wire16.parse q = .parsed h

def encode_parse_roundtrip : Prop :=
  ∀ q : ByteQuad, q.b0 = 1 → q.b1.toNat < 4 →
    ∃ h : Header, Wire16.parse q = .parsed h ∧ Wire16.encode h = .encoded q

def need_more_iff : Prop :=
  ∀ (header : Header) (available missing : UInt32),
    Wire16.classify header available = .needMore missing ↔
      available.toNat < header.length.toNat ∧ missing.toNat = header.length.toNat - available.toNat

def complete_iff : Prop :=
  ∀ (header : Header) (available consumed : UInt32),
    Wire16.classify header available = .complete consumed ↔
      header.length.toNat ≤ available.toNat ∧ consumed.toNat = header.length.toNat

end Wire16Contract
