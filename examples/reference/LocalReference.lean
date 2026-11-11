import Local
import Lean

open Lean

namespace LocalReference

-- Bound nesting and numeric tokens before invoking the general JSON parser.
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
      throw "invalid local wire character"
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

-- Test-only rows are [enabledBit,bias,x,y]; results are Count.value scalars.
def batch (j : Json) : Except String Json := do
  let values ← j.getArr?
  if values.size > 8192 then throw "batch limit"
  return .arr (← values.mapM fun j => do
    let row ← j.getArr?
    if row.size != 4 then throw "expected four local fields"
    let enabled ← decodeBool row[0]!
    let bias : Local.Count := ⟨← decodeUInt32 row[1]!⟩
    let config : Local.Config := ⟨⟨bias⟩, enabled⟩
    let x : Local.Count := ⟨← decodeUInt32 row[2]!⟩
    let y : Local.Count := ⟨← decodeUInt32 row[3]!⟩
    return toJson (Local.run config x y).value.toNat)

end LocalReference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 4194304 then throw (IO.userError "input exceeds 4 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  match LocalReference.checkWire text >>= fun _ => Json.parse text >>= LocalReference.batch with
  | .error err => throw (IO.userError err)
  | .ok result => IO.FS.writeFile output (result.compress ++ "\n")
