import Division
open Division

def samples : List Nat :=
  [0, 1, 2, 3, 7, 8, 255, 256, 65535, 65536, 2147483648, 4294967295,
   9223372036854775808, 18446744073709551615]

def put (bytes : ByteArray) (n : Nat) : ByteArray := Id.run do
  let mut out := bytes
  for i in [:8] do
    out := out.push ((n / 2 ^ (8 * (7 - i))) % 256).toUInt8
  return out

def main (args : List String) : IO Unit := do
  let [path] := args | throw <| IO.userError "expected output path"
  let mut bytes := ByteArray.empty
  for a in samples do
    for b in samples do
      let r8 := div8 a.toUInt8 b.toUInt8
      let r16 := div16 a.toUInt16 b.toUInt16
      let r32 := div32 a.toUInt32 b.toUInt32
      let r64 := div64 a.toUInt64 b.toUInt64
      for n in [r8.quotient.toNat, r8.remainder.toNat, r16.quotient.toNat, r16.remainder.toNat,
                r32.quotient.toNat, r32.remainder.toNat, r64.quotient.toNat, r64.remainder.toNat] do
        bytes := put bytes n
    let h := halves a.toUInt32
    bytes := put (put bytes h.quotient.toNat) h.remainder.toNat
  IO.FS.writeBinFile path bytes
