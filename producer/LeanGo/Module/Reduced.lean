module
public meta import Lean.Compiler.LCNF
public meta section
open Lean Lean.Compiler.LCNF
namespace LeanGo.Module

-- Compiler derivations are deliberately separate from original source evidence.
structure ReducedEvidence where
  owner : Name
  helper : Name
  sourceModule : Name
  ownerDecl : Decl .pure
  helperDecl : Decl .pure
  retained : Array Nat
  allUnused : Bool
  deriving Inhabited

structure ReducedEntry where
  helper : Name
  evidence? : Option ReducedEvidence := none
  issue : String := ""
  deriving Inhabited

initialize reducedExt : SimplePersistentEnvExtension ReducedEntry (NameMap ReducedEntry) ←
  registerSimplePersistentEnvExtension {
    addImportedFn := mkStateFromImportedEntries (fun s r => s.insert r.helper r) {}
    addEntryFn := fun s r => s.insert r.helper r
    asyncMode := .sync
    replay? := some <| SimplePersistentEnvExtension.replayOfFilter
      (fun s r => !s.contains r.helper) (fun s r => s.insert r.helper r)
  }

structure ReducedObservation where
  owner : Name
  helper : Name
  sourceModule : Name
  kernelPresent : Bool
  mono? : Option (Decl .pure)

def observeReduced (owner : Name) : CoreM ReducedObservation := do
  let env ← getEnv
  let helper := owner ++ `_redArg
  return {
    owner, helper, sourceModule := env.mainModule, kernelPresent := env.contains helper
    mono? := (← getMonoDecl? helper) }

private def closedType (type : Expr) : Bool :=
  !type.hasFVar && !type.hasMVar && !type.hasLooseBVars

-- Only nondependent, closed mono parameter types can provide position evidence.
private def monoShape? (decl : Decl .pure) : Option Expr := do
  guard (decl.safe && decl.levelParams.isEmpty)
  let .code _ := decl.value | none
  let mut type := decl.type
  let mut seen : List FVarId := []
  for param in decl.params do
    let .forallE _ domain body _ := type | none
    guard (closedType domain && domain == param.type && !seen.contains param.fvarId)
    seen := param.fvarId :: seen
    type := body
  guard (closedType type && !(type matches .forallE ..))
  return type

-- Recover positions only from owner FVarIds. Helper FVarIds have been normalized
-- independently and cannot witness which original arguments survived.
def reducedMapping? (owner helper : Decl .pure) : Option (Array Nat × Bool) := do
  guard (helper.name == owner.name ++ `_redArg && !owner.params.isEmpty)
  let ownerResult ← monoShape? owner
  let helperResult ← monoShape? helper
  guard (ownerResult == helperResult)
  let .code (.let call (.return result)) := owner.value | none
  let .const callee levels args _ := call.value | none
  guard (callee == helper.name && levels.isEmpty && result == call.fvarId &&
    call.type == ownerResult && !(owner.params.any (·.fvarId == result)))
  if args == #[.erased] then
    guard (helper.params.size == 1 && helper.params[0]!.type == .const ``lcVoid [])
    return (#[], true)
  guard (!args.isEmpty && args.size == helper.params.size && args.size < owner.params.size)
  let mut retained := #[]
  for i in [:args.size] do
    let .fvar f := args[i]! | none
    let some position := owner.params.findIdx? (·.fvarId == f) | none
    guard (retained.isEmpty || retained.back! < position)
    guard (helper.params[i]!.type == owner.params[position]!.type)
    retained := retained.push position
  return (retained, false)

def captureReduced (before : ReducedObservation) : CoreM Unit := do
  let some helper ← getMonoDecl? before.helper | return
  let owner? ← getMonoDecl? before.owner
  let mapping? := owner?.bind (reducedMapping? · helper)
  let env ← getEnv
  let prior := (reducedExt.getState env).contains before.helper
  -- An authored helper may legitimately coexist with an owner that never reduces.
  -- A changed mono snapshot or a forwarding wrapper proves an actual collision,
  -- even when simplification subsequently erased the wrapper.
  if before.kernelPresent || before.mono?.isSome then
    if mapping?.isSome || before.mono? != some helper then
      modifyEnv (reducedExt.addEntry ·
        { helper := before.helper, issue := s!"Reduced helper name collision: {before.helper}" })
    return
  let issue := if prior then s!"Repeated reduced helper compilation: {before.helper}"
    else if env.contains before.helper then s!"Reduced helper kernel collision: {before.helper}"
    else ""
  unless issue.isEmpty do
    modifyEnv (reducedExt.addEntry · { helper := before.helper, issue })
    return
  let some owner := owner? | return
  let some (retained, allUnused) := mapping? | return
  let evidence : ReducedEvidence := {
    owner := before.owner, helper := before.helper, sourceModule := before.sourceModule
    ownerDecl := owner, helperDecl := helper, retained, allUnused }
  modifyEnv (reducedExt.addEntry · { helper := before.helper, evidence? := some evidence })

private def definingModule (env : Environment) (name : Name) : Name :=
  if let some idx := env.getModuleIdxFor? name then env.allImportedModuleNames[idx]!
  else env.mainModule

-- Repeat every structural check against live declarations. Persistent snapshots
-- authenticate an observed compiler event; they are never a substitute for bodies.
def reducedEvidence? (name : Name) : CoreM (Option ReducedEvidence) := do
  let env ← getEnv
  let some entry := (reducedExt.getState env).find? name | return none
  unless entry.issue.isEmpty do throwError "{entry.issue}"
  let some evidence := entry.evidence? | throwError "Missing reduced helper evidence: {name}"
  unless entry.helper == name && evidence.helper == name &&
      evidence.ownerDecl.name == evidence.owner && evidence.helperDecl.name == name &&
      !(env.contains name) do
    throwError "Reduced helper identity or kernel collision: {name}"
  unless definingModule env evidence.owner == evidence.sourceModule &&
      definingModule env name == evidence.sourceModule do
    throwError "Reduced helper module mismatch: {name}"
  let some owner ← getMonoDecl? evidence.owner | throwError "Missing reduced owner declaration: {name}"
  let some helper ← getMonoDecl? name | throwError "Missing reduced helper declaration: {name}"
  unless owner == evidence.ownerDecl && helper == evidence.helperDecl do
    throwError "Reduced helper declaration snapshot mismatch: {name}"
  unless reducedMapping? owner helper == some (evidence.retained, evidence.allUnused) do
    throwError "Reduced helper forwarding mapping mismatch: {name}"
  return some evidence

-- Check an observed derivation while auditing its source owner as well. Otherwise
-- a later collision or changed owner body could erase every runtime helper call
-- and prevent the helper-indexed checks from ever running.
def validateReducedOwner (owner : Name) : CoreM Unit := do
  if let some evidence ← reducedEvidence? (owner ++ `_redArg) then
    unless evidence.owner == owner do throwError "Reduced helper owner mismatch: {owner}"

end LeanGo.Module
