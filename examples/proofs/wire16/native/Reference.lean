import Wire16

-- Emit the native encoding, decoding and UInt32 widening for each UInt16.
def main (args : List String) : IO Unit := do
  let [path] := args | throw <| IO.userError "expected an output path"
  let mut bytes := ByteArray.empty
  for n in [:65536] do
    let pair := Wire16.encodeBE n.toUInt16
    let input : BytePair := ⟨(n / 256).toUInt8, n.toUInt8⟩
    let decoded := (Wire16.decodeBE input).toNat
    bytes := bytes.push pair.hi |>.push pair.lo
    bytes := bytes.push (decoded / 256).toUInt8 |>.push decoded.toUInt8
    let wide := n.toUInt16.toUInt32.toNat
    bytes := bytes.push (wide / 16777216).toUInt8 |>.push (wide / 65536).toUInt8
    bytes := bytes.push (wide / 256).toUInt8 |>.push wide.toUInt8
  IO.FS.writeBinFile path bytes
