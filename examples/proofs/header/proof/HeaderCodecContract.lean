import HeaderCodec

namespace HeaderCodecContract

-- A successful parse identifies every semantic field from the wire bytes.
def parse_success_iff : Prop :=
  ∀ (q : ByteQuad) (h : Header), HeaderCodec.parse q = .parsed h ↔
    q.b0 = 1 ∧ q.b1.toNat < 4 ∧ h.flags = q.b1 ∧
      h.length.toNat = q.b2.toNat * 256 + q.b3.toNat

def parse_unsupported_version_iff : Prop :=
  ∀ q : ByteQuad, HeaderCodec.parse q = .unsupportedVersion ↔ q.b0 ≠ 1

def parse_reserved_flags_iff : Prop :=
  ∀ q : ByteQuad, HeaderCodec.parse q = .reservedFlags ↔ q.b0 = 1 ∧ 4 ≤ q.b1.toNat

def parsed_length_bound : Prop :=
  ∀ (q : ByteQuad) (h : Header), HeaderCodec.parse q = .parsed h → h.length.toNat < 65536

-- Successful encoding is canonical, with high byte first and no loss of bits.
def encode_success_iff : Prop :=
  ∀ (h : Header) (q : ByteQuad), HeaderCodec.encode h = .encoded q ↔
    h.flags.toNat < 4 ∧ h.length.toNat < 65536 ∧ q.b0 = 1 ∧ q.b1 = h.flags ∧
      q.b2.toNat = h.length.toNat / 256 ∧ q.b3.toNat = h.length.toNat % 256

def encode_reserved_flags_iff : Prop :=
  ∀ h : Header, HeaderCodec.encode h = .reservedFlags ↔ 4 ≤ h.flags.toNat

def encode_length_overflow_iff : Prop :=
  ∀ h : Header, HeaderCodec.encode h = .lengthOverflow ↔
    h.flags.toNat < 4 ∧ 65536 ≤ h.length.toNat

def parse_encode_roundtrip : Prop :=
  ∀ h : Header, h.flags.toNat < 4 → h.length.toNat < 65536 →
    ∃ q : ByteQuad, HeaderCodec.encode h = .encoded q ∧ HeaderCodec.parse q = .parsed h

def encode_parse_roundtrip : Prop :=
  ∀ q : ByteQuad, q.b0 = 1 → q.b1.toNat < 4 →
    ∃ h : Header, HeaderCodec.parse q = .parsed h ∧ HeaderCodec.encode h = .encoded q

end HeaderCodecContract
