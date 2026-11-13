namespace LeftShift

def notation32 (word count : UInt32) : UInt32 := word <<< count
def direct32 (word count : UInt32) : UInt32 := UInt32.shiftLeft word count
def notation64 (word count : UInt64) : UInt64 := word <<< count
def direct64 (word count : UInt64) : UInt64 := UInt64.shiftLeft word count

end LeftShift
