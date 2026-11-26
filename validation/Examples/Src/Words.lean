namespace Words

def widen (word : UInt32) : UInt64 := word.toUInt64
def narrow (word : UInt64) : UInt32 := word.toUInt32
def pack (high low : UInt32) : UInt64 :=
  (high.toUInt64 <<< (32 : UInt64)) ||| low.toUInt64
def high (word : UInt64) : UInt32 := (word >>> (32 : UInt64)).toUInt32
def low (word : UInt64) : UInt32 := word.toUInt32

end Words
