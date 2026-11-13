module
public meta import Lean.Compiler.LCNF
public meta section
open Lean Lean.Compiler.LCNF
namespace LeanGo.Module

-- Only intentional rejection sites create this exception. Arbitrary compiler
-- exceptions must never be reclassified as supported-profile rejections.
initialize admissionException : InternalExceptionId ← registerInternalExceptionId `leanGoAdmission

def reject (code hint : String) (message : MessageData) (form : Option String := none) : CoreM α := do
  let (_, message) ← AddErrorMessageContext.add (← getRef) message
  let extra := KVMap.empty |>.setString `code code |>.setString `hint hint
    |>.setString `message (← message.toString)
  let extra := match form with
    | some value => extra.setString `compiler_form value
    | none => extra
  throw <| Exception.internal admissionException extra

def rejectionMessage (error : Exception) : CoreM String := do
  if let .internal id extra := error then
    if id == admissionException then return extra.getString `message
  error.toMessageData.toString

def withDeclaration (name : Name) (action : CoreM α) : CoreM α := do
  try action catch error =>
    match error with
    | .internal id extra =>
      if id == admissionException && (extra.find `declaration).isNone then
        throw <| Exception.internal id (extra.setName `declaration name)
      throw error
    | _ => throw error

def valueForm : LetValue .pure → String
  | .lit _ => "LCNF.LetValue.lit"
  | .erased => "LCNF.LetValue.erased"
  | .proj .. => "LCNF.LetValue.proj"
  | .const .. => "LCNF.LetValue.const"
  | .fvar .. => "LCNF.LetValue.fvar"

def controlForm : Code .pure → String
  | .let .. => "LCNF.Code.let"
  | .fun .. => "LCNF.Code.fun"
  | .jp .. => "LCNF.Code.jp"
  | .jmp .. => "LCNF.Code.jmp"
  | .cases .. => "LCNF.Code.cases"
  | .return .. => "LCNF.Code.return"
  | .unreach .. => "LCNF.Code.unreach"

def declarationLocation (name : Name) (kind : String) : CoreM Json := do
  let env ← getEnv
  let some index := env.getModuleIdxFor? name | return .null
  let some ranges ← findDeclarationRanges? name | return .null
  let position (p : Position) := Json.mkObj [("line", toJson p.line), ("column", toJson p.column)]
  return Json.mkObj [("module", toJson env.allImportedModuleNames[index]!.toString),
    ("declaration", toJson name.toString), ("kind", toJson kind),
    ("start", position ranges.selectionRange.pos), ("end", position ranges.selectionRange.endPos)]

def diagnosticJson (extra : KVMap) (location : Json) : Json :=
  let optional (key : Name) := match extra.find key with
    | some (.ofString value) => toJson value
    | some (.ofName value) => toJson value.toString
    | _ => .null
  Json.mkObj [("schema", toJson "lean-go-diagnostics/v1"), ("diagnostic", Json.mkObj [
    ("code", toJson (extra.getString `code)), ("stage", toJson "admission"),
    ("message", toJson (extra.getString `message)), ("hint", toJson (extra.getString `hint)),
    ("declaration", optional `declaration), ("compiler_form", optional `compiler_form),
    ("location", location)])]
end LeanGo.Module
