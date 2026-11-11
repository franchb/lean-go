import Numbers
import Lean

open Lean

namespace NumbersReference

-- Limit nesting before the general JSON parser sees the test-only wire format.
def checkWire (text : String) : Except String Unit := do
  let mut depth := 0
  let mut digits := 0
  for c in text.toList do
    if c == '[' then
      depth := depth + 1
      if depth > 2 then throw "input nesting exceeds two arrays"
    else if c == ']' then
      depth := depth - 1
    else if !(c.isDigit || c == ',' || c == ' ' || c == '\n' || c == '\r' || c == '\t') then
      throw "invalid numeric wire character"
    digits := if c.isDigit then digits + 1 else 0
    if digits > 10 then throw "numeric token exceeds ten digits"

-- Test-only codec: each pair and its twelve result fields are JSON arrays.
def decodeUInt32 (j : Json) : Except String UInt32 := do
  let value ← j.getNat?
  if value > 4294967295 then throw "input exceeds UInt32"
  return value.toUInt32

def encode : Numbers.Results → Json
  | .mk sum difference product shifted equal less lessEq twice zero maximum wrapped mixed =>
    .arr #[toJson sum.toNat, toJson difference.toNat, toJson product.toNat,
      toJson shifted.toNat, toJson (if equal then (1 : Nat) else 0),
      toJson (if less then (1 : Nat) else 0), toJson (if lessEq then (1 : Nat) else 0),
      toJson twice.toNat, toJson zero.toNat, toJson maximum.toNat,
      toJson wrapped.toNat, toJson mixed.toNat]

def batch (j : Json) : Except String Json := do
  let values ← j.getArr?
  if values.size > 8192 then throw "batch limit"
  return .arr (← values.mapM fun j => do
    let pair ← j.getArr?
    if pair.size != 2 then throw "expected UInt32 pair"
    return encode (Numbers.compute (← decodeUInt32 pair[0]!) (← decodeUInt32 pair[1]!)))

end NumbersReference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 4194304 then throw (IO.userError "input exceeds 4 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  match NumbersReference.checkWire text >>= fun _ => Json.parse text >>= NumbersReference.batch with
  | .error err => throw (IO.userError err)
  | .ok result => IO.FS.writeFile output (result.compress ++ "\n")
