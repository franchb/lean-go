structure ByteQuad where
  b0 : UInt8
  b1 : UInt8
  b2 : UInt8
  b3 : UInt8

namespace ByteCodec

def encodeLE (w : UInt32) : ByteQuad :=
  ⟨w.toUInt8, (w >>> (8 : UInt32)).toUInt8,
    (w >>> (16 : UInt32)).toUInt8, (w >>> (24 : UInt32)).toUInt8⟩
def encodeBE (w : UInt32) : ByteQuad :=
  ⟨(w >>> (24 : UInt32)).toUInt8, (w >>> (16 : UInt32)).toUInt8,
    (w >>> (8 : UInt32)).toUInt8, w.toUInt8⟩
def decodeLE (q : ByteQuad) : UInt32 :=
  (q.b3.toUInt32 <<< (24 : UInt32)) ||| (q.b2.toUInt32 <<< (16 : UInt32)) |||
    (q.b1.toUInt32 <<< (8 : UInt32)) ||| q.b0.toUInt32
def decodeBE (q : ByteQuad) : UInt32 :=
  (q.b0.toUInt32 <<< (24 : UInt32)) ||| (q.b1.toUInt32 <<< (16 : UInt32)) |||
    (q.b2.toUInt32 <<< (8 : UInt32)) ||| q.b3.toUInt32
end ByteCodec
