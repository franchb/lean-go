import Reduced
import Lean

open Lean

namespace ReducedReference

-- Reject oversized tokens and nesting before the general JSON parser runs.
def checkWire (text : String) : Except String Unit := do
  let mut depth := 0
  let mut digits := 0
  for c in text.toList do
    if c == '[' then
      depth := depth + 1
      if depth > 2 then throw "input nesting exceeds two arrays"
    else if c == ']' then
      if depth == 0 then throw "unmatched closing array"
      depth := depth - 1
    else if !(c.isDigit || c == ',' || c == ' ' || c == '\n' || c == '\r' || c == '\t') then
      throw "invalid reduced wire character"
    digits := if c.isDigit then digits + 1 else 0
    if digits > 10 then throw "numeric token exceeds ten digits"
  if depth != 0 then throw "unclosed array"

def decodeUInt32 (j : Json) : Except String UInt32 := do
  let value ← j.getNat?
  if value > 4294967295 then throw "input exceeds UInt32"
  return value.toUInt32

def decodeBool (j : Json) : Except String Bool := do
  match ← j.getNat? with
  | 0 => return false
  | 1 => return true
  | _ => throw "expected Boolean bit"

-- Test-only rows are [flagBit,unused,left,right,bias,chainDepth].
-- Results are [run,allUnused,recursive], retaining UInt32 values on the wire.
def chain : Nat → Reduced.Chain
  | 0 => .nil
  | n + 1 => .cons (chain n)

def batch (j : Json) : Except String Json := do
  let values ← j.getArr?
  if values.size > 8192 then throw "batch limit"
  return .arr (← values.mapM fun j => do
    let row ← j.getArr?
    if row.size != 6 then throw "expected six reduced fields"
    let flag ← decodeBool row[0]!
    let unused : Reduced.Count := ⟨← decodeUInt32 row[1]!⟩
    let left : Reduced.Count := ⟨← decodeUInt32 row[2]!⟩
    let right : Reduced.Count := ⟨← decodeUInt32 row[3]!⟩
    let bias : Reduced.Count := ⟨← decodeUInt32 row[4]!⟩
    let depth ← row[5]!.getNat?
    if depth > 64 then throw "chain depth exceeds 64"
    return .arr #[toJson (Reduced.run flag unused left right bias).value.toNat,
      toJson (Reduced.allUnused unused flag).value.toNat,
      toJson (Reduced.recursive (chain depth) unused left).value.toNat])

end ReducedReference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 4194304 then throw (IO.userError "input exceeds 4 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  match ReducedReference.checkWire text >>= fun _ => Json.parse text >>= ReducedReference.batch with
  | .error err => throw (IO.userError err)
  | .ok result => IO.FS.writeFile output (result.compress ++ "\n")
