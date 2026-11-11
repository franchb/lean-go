import LeanGo.Feasibility.Model
import Lean

open Lean LeanGo.Feasibility
namespace Reference

-- Lean's general JSON object parser retains the last duplicate member. Detect
-- duplicates first; the admitted tree codec has string tokens only as keys.
def uniqueKeys (text : String) : Except String Unit := do
  let mut stack : List (List String) := []
  let mut quoted := false
  let mut escaped := false
  let mut token : Array Char := #[]
  for char in text.toList do
    if quoted then
      if escaped then
        token := token.push char
        escaped := false
      else if char == '\\' then
        token := token.push char
        escaped := true
      else if char == '"' then
        let key ← (← Json.parse ("\"" ++ String.ofList token.toList ++ "\"")).getStr?
        if let fields :: rest := stack then
          if fields.contains key then throw "duplicate JSON object key"
          stack := (key :: fields) :: rest
        quoted := false
      else
        token := token.push char
    else if char == '"' then
      quoted := true
      token := #[]
    else if char == '{' then
      if stack.length >= 64 then throw "tree depth exceeds 64"
      stack := [] :: stack
    else if char == '}' then
      match stack with
      | [] => throw "unmatched JSON object delimiter"
      | _ :: rest => stack := rest

def exactFields (j : Json) (keys : List String) : Except String Unit := do
  let fields ← j.getObj?
  unless (fields.toList.map Prod.fst).mergeSort (· ≤ ·) == keys.mergeSort (· ≤ ·) do
    throw "invalid tree fields"

def natural (j : Json) (key : String) : Except String Nat := do
  (← j.getObjVal? key).getNat?

partial def tree (j : Json) (depth : Nat := 1) : Except String LeanGo.Feasibility.Expr := do
  if depth > 64 then throw "tree depth exceeds 64"
  match ← natural j "tag" with
  | 0 =>
    exactFields j ["tag", "tri"]
    let value ← natural j "tri"
    match value with
    | 0 => pure (.lit .f)
    | 1 => pure (.lit .u)
    | 2 => pure (.lit .t)
    | _ => throw "invalid Tri"
  | 1 =>
    exactFields j ["tag", "id"]
    let id ← natural j "id"
    if id > 4294967295 then throw "UInt32 out of range"
    pure (.var id.toUInt32)
  | 2 =>
    exactFields j ["tag", "left"]
    pure (.neg (← tree (← j.getObjVal? "left") (depth + 1)))
  | 3 =>
    exactFields j ["tag", "left", "right"]
    pure (.conj (← tree (← j.getObjVal? "left") (depth + 1))
      (← tree (← j.getObjVal? "right") (depth + 1)))
  | 4 =>
    exactFields j ["tag", "left", "right"]
    pure (.disj (← tree (← j.getObjVal? "left") (depth + 1))
      (← tree (← j.getObjVal? "right") (depth + 1)))
  | _ => throw "invalid tree tag"

def triNumber : Tri → Nat
  | .f => 0 | .u => 1 | .t => 2

def encode : LeanGo.Feasibility.Expr → Json
  | .lit value => Json.mkObj [("tag", toJson (0 : Nat)), ("tri", toJson (triNumber value))]
  | .var id => Json.mkObj [("tag", toJson (1 : Nat)), ("id", toJson id.toNat)]
  | .neg e => Json.mkObj [("tag", toJson (2 : Nat)), ("left", encode e)]
  | .conj a b => Json.mkObj [("tag", toJson (3 : Nat)), ("left", encode a), ("right", encode b)]
  | .disj a b => Json.mkObj [("tag", toJson (4 : Nat)), ("left", encode a), ("right", encode b)]

def batch (input : Json) : Except String Json := do
  let inputs ← input.getArr?
  if inputs.size > 1024 then throw "batch exceeds 1024 expressions"
  let mut output := #[]
  for item in inputs do
    let e ← tree item
    let mut values := #[]
    for a in [Tri.f, .u, .t] do
      for b in [Tri.f, .u, .t] do
        let rho := fun id : UInt32 => if id == 0 then a else if id == 1 then b else .u
        values := values.push (toJson (triNumber (eval rho e)))
    output := output.push (Json.mkObj [("normal", encode (normalize e)), ("values", .arr values)])
  pure (.arr output)
end Reference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output paths")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 1048576 then throw (IO.userError "input exceeds 1 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  let result := do
    Reference.uniqueKeys text
    let json ← Json.parse text
    Reference.batch json
  match result with
  | .error err => throw (IO.userError err)
  | .ok json => IO.FS.writeFile output (json.compress ++ "\n")
