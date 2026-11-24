structure BytePair where
  hi : UInt8
  lo : UInt8

structure ByteQuad where
  b0 : UInt8
  b1 : UInt8
  b2 : UInt8
  b3 : UInt8

structure Header where
  flags : UInt8
  length : UInt16

inductive ParseResult where
  | parsed (header : Header)
  | unsupportedVersion
  | reservedFlags

inductive EncodeResult where
  | encoded (wire : ByteQuad)
  | reservedFlags

inductive Availability where
  | needMore (missing : UInt32)
  | complete (consumed : UInt32)

namespace Wire16

def encodeBE (n : UInt16) : BytePair :=
  ⟨(n >>> 8).toUInt8, n.toUInt8⟩

def decodeBE (p : BytePair) : UInt16 :=
  (p.hi.toUInt16 <<< 8) ||| p.lo.toUInt16

def parse (q : ByteQuad) : ParseResult :=
  if q.b0 = 1 then
    if q.b1 < 4 then .parsed ⟨q.b1, decodeBE ⟨q.b2, q.b3⟩⟩
    else .reservedFlags
  else .unsupportedVersion

def encode (h : Header) : EncodeResult :=
  if h.flags < 4 then
    let p := encodeBE h.length
    .encoded ⟨1, h.flags, p.hi, p.lo⟩
  else .reservedFlags

/-- Classify the payload bytes available after the four-byte header. -/
def classify (header : Header) (available : UInt32) : Availability :=
  let length := header.length.toUInt32
  if available < length then .needMore (length - available)
  else .complete length

end Wire16
