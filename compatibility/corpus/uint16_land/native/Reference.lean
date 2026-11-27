import Example

def main (args : List String) : IO Unit := do
  let [path] := args | throw <| IO.userError "expected output path"
  let mut bytes := ByteArray.empty
  for n in [:65536] do
    let a := n.toUInt16
    for b in [0, 65535, (40503 * n + 7919).toUInt16] do
      let result := (run a b).toNat
      bytes := bytes.push (result / 256).toUInt8 |>.push result.toUInt8
  IO.FS.writeBinFile path bytes
