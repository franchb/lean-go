namespace RecordFold

structure Totals where
  count : UInt32
  sum : UInt32

def addByte (s : Totals) (b : UInt8) : Totals := ⟨s.count + 1, s.sum + b.toUInt32⟩

def run (s : Totals) (bytes : List UInt8) := bytes.foldl addByte s

end RecordFold
