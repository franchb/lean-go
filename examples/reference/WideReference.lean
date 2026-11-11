import Wide
import Lean

open Lean

namespace WideReference

-- Bound nesting and decimal tokens before the general JSON parser runs.
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
    if digits > 20 then throw "numeric token exceeds twenty digits"

def decodeUInt64 (j : Json) : Except String UInt64 := do
  let value ← j.getNat?
  if value > 18446744073709551615 then throw "input exceeds UInt64"
  return value.toUInt64

-- Every value is a JSON integer; booleans use the exact flags zero and one.
def encode (result : Wide.Results) : Json :=
  .arr #[toJson result.sum.toNat, toJson result.difference.toNat,
    toJson result.product.toNat, toJson result.shifted.toNat,
    toJson (if result.equal then (1 : Nat) else 0),
    toJson (if result.less then (1 : Nat) else 0),
    toJson (if result.lessEq then (1 : Nat) else 0),
    toJson result.twice.toNat, toJson result.zero.toNat,
    toJson result.maximum.toNat, toJson result.wrapped.toNat,
    toJson result.above53.toNat, toJson result.mixed.toNat]

def batch (j : Json) : Except String Json := do
  let values ← j.getArr?
  if values.size > 8192 then throw "batch limit"
  return .arr (← values.mapM fun j => do
    let pair ← j.getArr?
    if pair.size != 2 then throw "expected UInt64 pair"
    return encode (Wide.compute (← decodeUInt64 pair[0]!) (← decodeUInt64 pair[1]!)))

end WideReference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 4194304 then throw (IO.userError "input exceeds 4 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  match WideReference.checkWire text >>= fun _ => Json.parse text >>= WideReference.batch with
  | .error err => throw (IO.userError err)
  | .ok result => IO.FS.writeFile output (result.compress ++ "\n")
