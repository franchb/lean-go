import Trees
import Lean

open Lean

namespace TreesReference

-- Test-only codec: a leaf is [color]; a branch is [left,right].
partial def decode (j : Json) (depth : Nat := 1) : Except String Trees.Tree := do
  if depth > 64 then throw "tree depth exceeds 64"
  let values ← j.getArr?
  if values.size == 1 then
    match ← values[0]!.getNat? with
    | 0 => pure (.leaf .red)
    | 1 => pure (.leaf .green)
    | 2 => pure (.leaf .blue)
    | _ => throw "invalid color"
  else if values.size == 2 then
    pure (.branch (← decode values[0]! (depth + 1))
      (← decode values[1]! (depth + 1)))
  else throw "invalid tree arity"

def encode : Trees.Tree → Json
  | .leaf c => .arr #[toJson (match c with | .red => (0 : Nat) | .green => 1 | .blue => 2)]
  | .branch a b => .arr #[encode a, encode b]

def batch (j : Json) : Except String Json := do
  let values ← j.getArr?
  if values.size > 2048 then throw "batch limit"
  return .arr (← values.mapM fun j => do
    return encode (Trees.mirror (← decode j)))

end TreesReference

def main (args : List String) : IO Unit := do
  let [input, output] := args | throw (IO.userError "expected input and output")
  let bytes ← IO.FS.readBinFile input
  if bytes.size > 1048576 then throw (IO.userError "input exceeds 1 MiB")
  let some text := String.fromUTF8? bytes | throw (IO.userError "invalid UTF-8")
  match Json.parse text >>= TreesReference.batch with
  | .error err => throw (IO.userError err)
  | .ok result => IO.FS.writeFile output (result.compress ++ "\n")
