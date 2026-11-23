structure ByteQuad where
  b0 : UInt8
  b1 : UInt8
  b2 : UInt8
  b3 : UInt8

structure Header where
  flags : UInt8
  length : UInt32

inductive ParseResult where
  | parsed (header : Header)
  | unsupportedVersion
  | reservedFlags

inductive EncodeResult where
  | encoded (wire : ByteQuad)
  | reservedFlags
  | lengthOverflow

namespace HeaderCodec

def parse (q : ByteQuad) : ParseResult :=
  if q.b0 = 1 then
    if q.b1 < 4 then
      .parsed ⟨q.b1, q.b2.toUInt32 * 256 + q.b3.toUInt32⟩
    else .reservedFlags
  else .unsupportedVersion

def encode (h : Header) : EncodeResult :=
  if h.flags < 4 then
    if h.length < 65536 then
      .encoded ⟨1, h.flags, (h.length >>> (8 : UInt32)).toUInt8, h.length.toUInt8⟩
    else .lengthOverflow
  else .reservedFlags

end HeaderCodec
