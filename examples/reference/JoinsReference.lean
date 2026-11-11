import Joins
import Lean

open Lean

namespace JoinsReference

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
      throw "invalid join wire character"
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

-- Test-only wire rows are [count,nestedCount,enabledBit,raw,delta,flagBit].
-- Results are [count,nestedCount,enabledBit,raw,total].
def batch (j : Json) : Except String Json := do
  let values ← j.getArr?
  if values.size > 8192 then throw "batch limit"
  return .arr (← values.mapM fun j => do
    let row ← j.getArr?
    if row.size != 6 then throw "expected six join fields"
    let state : Joins.State := {
      count := ⟨← decodeUInt32 row[0]!⟩
      nested := ⟨⟨← decodeUInt32 row[1]!⟩⟩
      enabled := ⟨← decodeBool row[2]!⟩
      raw := ← decodeUInt32 row[3]! }
    let delta : Joins.Count := ⟨← decodeUInt32 row[4]!⟩
    let flag ← decodeBool row[5]!
    let result := Joins.update state delta flag
    return .arr #[toJson result.count.value.toNat, toJson result.nested.count.value.toNat,
      toJson (if result.enabled.value then (1 : Nat) else 0), toJson result.raw.toNat,
      toJson (Joins.total state delta flag).value.toNat])

end JoinsReference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 4194304 then throw (IO.userError "input exceeds 4 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  match JoinsReference.checkWire text >>= fun _ => Json.parse text >>= JoinsReference.batch with
  | .error err => throw (IO.userError err)
  | .ok result => IO.FS.writeFile output (result.compress ++ "\n")
