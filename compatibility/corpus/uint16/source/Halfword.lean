namespace Halfword

def joinBytes (hi lo : UInt8) : UInt16 := (hi.toUInt16 <<< 8) ||| lo.toUInt16
def high (word : UInt16) : UInt8 := (word >>> 8).toUInt8
def low (word : UInt16) : UInt8 := word.toUInt8
def left (word count : UInt16) : UInt16 := word <<< count
def right (word count : UInt16) : UInt16 := word >>> count
def combine (a b : UInt16) : UInt16 := a ||| b

end Halfword
