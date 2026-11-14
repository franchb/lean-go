import ByteCodec

namespace ByteCodecContract

-- Expected statements describe the two inverse maps and arithmetic byte positions.
-- They depend only on the runtime module, never on the proof implementation.
def decodeLE_encodeLE : Prop :=
  ∀ word : UInt32, ByteCodec.decodeLE (ByteCodec.encodeLE word) = word
def decodeBE_encodeBE : Prop :=
  ∀ word : UInt32, ByteCodec.decodeBE (ByteCodec.encodeBE word) = word
def encodeLE_decodeLE : Prop :=
  ∀ bytes : ByteQuad, ByteCodec.encodeLE (ByteCodec.decodeLE bytes) = bytes
def encodeBE_decodeBE : Prop :=
  ∀ bytes : ByteQuad, ByteCodec.encodeBE (ByteCodec.decodeBE bytes) = bytes

def encodeLE_b0 : Prop := ∀ word : UInt32, (ByteCodec.encodeLE word).b0.toNat = word.toNat % 256
def encodeLE_b1 : Prop := ∀ word : UInt32, (ByteCodec.encodeLE word).b1.toNat = word.toNat / 256 % 256
def encodeLE_b2 : Prop := ∀ word : UInt32, (ByteCodec.encodeLE word).b2.toNat = word.toNat / 65536 % 256
def encodeLE_b3 : Prop := ∀ word : UInt32, (ByteCodec.encodeLE word).b3.toNat = word.toNat / 16777216 % 256
def encodeBE_b0 : Prop := ∀ word : UInt32, (ByteCodec.encodeBE word).b0.toNat = word.toNat / 16777216 % 256
def encodeBE_b1 : Prop := ∀ word : UInt32, (ByteCodec.encodeBE word).b1.toNat = word.toNat / 65536 % 256
def encodeBE_b2 : Prop := ∀ word : UInt32, (ByteCodec.encodeBE word).b2.toNat = word.toNat / 256 % 256
def encodeBE_b3 : Prop := ∀ word : UInt32, (ByteCodec.encodeBE word).b3.toNat = word.toNat % 256

end ByteCodecContract
