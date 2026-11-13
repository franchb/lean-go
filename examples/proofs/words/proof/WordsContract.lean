import Words
namespace WordsContract

def widen_preserves : Prop := ∀ word : UInt32, (Words.widen word).toNat = word.toNat
def narrow_truncates : Prop := ∀ word : UInt64, (Words.narrow word).toNat = word.toNat % 2^32
def narrow_widen : Prop := ∀ word : UInt32, Words.narrow (Words.widen word) = word
def widen_narrow : Prop := ∀ word : UInt64, Words.widen (Words.narrow word) = word % 4294967296
def low_pack : Prop := ∀ hi lo : UInt32, Words.low (Words.pack hi lo) = lo
def high_pack : Prop := ∀ hi lo : UInt32, Words.high (Words.pack hi lo) = hi
def pack_unpack : Prop := ∀ word : UInt64, Words.pack (Words.high word) (Words.low word) = word
end WordsContract
