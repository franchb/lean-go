module
public meta import Lean.Compiler.LCNF
public meta section
open Lean Lean.Compiler.LCNF
namespace LeanGo.Module

def typeKey (e : Expr) : CoreM String := do
  let .const n [] := e | throwError "Unsupported first-order monomorphic type: {e}"
  if n == ``UInt32 || n == ``UInt64 then return n.toString
  let info ← getConstInfoInduct n
  unless info.numParams == 0 && info.numIndices == 0 && info.levelParams.isEmpty &&
      info.type == .sort (.succ .zero) do
    throwError "Unsupported ADT type: {n}"
  return n.toString

def signature (e : Expr) : CoreM (List String × String) := do
  match e with
  | .forallE _ domain body _ =>
    let domain ← typeKey domain
    let (domains, result) ← signature body
    return (domain :: domains, result)
  | _ => return ([], ← typeKey e)

def constructor (name : Name) : CoreM (String × List String) := do
  let ctor ← getConstInfoCtor name
  unless ctor.numParams == 0 && ctor.levelParams.isEmpty do
    throwError "Unsupported constructor: {name}"
  let (fields, result) ← signature ctor.type
  unless ctor.numFields == fields.length do throwError "Dependent constructor: {name}"
  return (result, fields)

-- These fields retain the original source schema, in outer-to-inner order.
structure Wrapper where
  source : String
  ctor : Name
  field : String
  deriving Inhabited

structure Representation where
  source : String
  mono : String
  wrappers : List Wrapper
  deriving Inhabited

-- Follow only single-field wrappers. Recursive sums remain named types; their
-- complete source fields are validated separately by exportTypes.
private def wrapperChain (source : String) (seen : List String) (fuel : Nat) :
    CoreM (String × List Wrapper) := do
  let name := source.toName
  discard <| typeKey (.const name [])
  if source == "UInt32" || source == "UInt64" then return (source, [])
  if seen.contains source then throwError "Cyclic wrapper representation: {name}"
  let info ← getConstInfoInduct name
  if info.isUnsafe then throwError "Unsafe ADT: {name}"
  let descriptor ← hasTrivialStructure? name
  if let [ctorName] := info.ctors then
    -- Validate all SOURCE fields, even when Lean discards proof fields and calls
    -- the remaining runtime representation trivial.
    let (target, fields) ← constructor ctorName
    unless target == source do throwError "Constructor schema mismatch: {ctorName}"
    if let [field] := fields then
      unless info.isRec do
        let fuel + 1 := fuel | throwError "Wrapper normalization limit"
        let some descriptor := descriptor
          | throwError "Missing compiler wrapper representation: {name}"
        unless descriptor.ctorName == ctorName && descriptor.numParams == 0 && descriptor.fieldIdx == 0 do
          throwError "Compiler wrapper representation mismatch: {name}"
        let (mono, wrappers) ← wrapperChain field (source :: seen) fuel
        return (mono, { source, ctor := ctorName, field } :: wrappers)
  if descriptor.isSome then throwError "Unsupported compiler-trivial source schema: {name}"
  return (source, [])
termination_by fuel

def representation (source : String) : CoreM Representation := do
  let (mono, wrappers) ← wrapperChain source [] 64
  unless (← toMonoType (.const source.toName [])) == .const mono.toName [] do
    throwError "Source/mono representation mismatch: {source}"
  return { source, mono, wrappers }

end LeanGo.Module
