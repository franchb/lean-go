module
public meta import LeanGo.Module.Primitives
public meta import LeanGo.Module.Reduced
public meta import LeanGo.Module.LocalObserve
public meta import Lean.Compiler.LCNF.Main
public meta import Lean.Compiler.Options
public meta import Lean.Compiler.CSimpAttr
public meta import Lean.Compiler.ExternAttr
public meta import Lean.Compiler.ImplementedByAttr
public meta import Lean.Util.CollectAxioms
meta import all Lean
meta import all Init.Data.UInt.Package
public meta section
open Lean Lean.Compiler
namespace LeanGo.Module

-- Persist the clean toolchain evidence before this module installs the hook and
-- before the fresh build imports any user source.
run_cmd do
  Elab.Command.liftCoreM do
    for n in numericPrimitives.map (·.name) ++ numericSourceSupport do
      validateNumericDefinition n
      let boundary ← captureNumericBoundary n
      modifyEnv (numericTrustExt.addEntry · boundary)

structure Rewrite where
  source : Name
  target : Name
  theoremName : Name
  deriving Inhabited, BEq

structure Provenance where
  name : Name
  dependencies : Array Name
  rewrites : Array Rewrite
  issues : Array String
  issueOrigin : Option Name := none
  compiled : Bool := false
  sourceModule : Name
  safeDefinition : Bool
  deriving Inhabited

initialize provenanceExt : SimplePersistentEnvExtension Provenance (NameMap Provenance) ←
  registerSimplePersistentEnvExtension {
    addImportedFn := mkStateFromImportedEntries (fun s r => s.insert r.name r) {}
    addEntryFn := fun s r => s.insert r.name r
    asyncMode := .sync
    replay? := some <| SimplePersistentEnvExtension.replayOfFilter
      (fun s r => !s.contains r.name) (fun s r => s.insert r.name r)
  }

-- These are the only precompiled computational definitions admitted at the boundary.
def isPrimitiveDefinition (n : Name) : Bool :=
  n == ``Bool.not || n == ``Bool.Internal.not

-- Unit and its constructor alias appear in generated enum eliminators. They are
-- admitted only as source support; the runtime type checker still rejects aliases.
-- Bool.casesOn is the pinned recursor wrapper with body
-- `fun {motive} t false true => Bool.rec false true t`. It is not a runtime primitive;
-- exportFunction still requires fresh compilation provenance for any emitted call.
def isCompilerSupport (n : Name) : Bool :=
  n == ``Bool || n == ``UInt8 || n == ``UInt32 || n == ``UInt64 || n == ``Unit || n == ``Unit.unit || n == ``Bool.casesOn

-- In a `module` import, an ordinary public definition can be represented as an
-- axiom because its implementation is hidden. Only a safe definition captured by
-- this hook in its actual defining module can authorize that representation.
def importedCompilation? (env : Environment) (records : NameMap Provenance) (n : Name) : Option Provenance := do
  let moduleIdx ← env.getModuleIdxFor? n
  let record ← records.find? n
  guard (record.compiled && record.safeDefinition && record.issues.isEmpty &&
    record.sourceModule == env.allImportedModuleNames[moduleIdx]!)
  return record

def reducedSource? (name : Name) : CoreM (Option ReducedEvidence) := do
  let some evidence ← reducedEvidence? name | return none
  let env ← getEnv
  let records := provenanceExt.getState env
  let some source := records.find? evidence.owner
    | throwError "Reduced helper lacks original source provenance: {name}"
  unless source.compiled && source.safeDefinition && source.issues.isEmpty &&
      source.sourceModule == evidence.sourceModule do
    throwError "Reduced helper source owner rejected: {evidence.owner}: {source.issues}"
  match env.find? evidence.owner with
  | some (.defnInfo { safety := .safe, .. }) => pure ()
  | some (.axiomInfo _) =>
    unless (importedCompilation? env records evidence.owner).isSome do
      throwError "Reduced helper lacks safe imported source owner: {evidence.owner}"
  | _ => throwError "Reduced helper lacks safe source owner: {evidence.owner}"
  return some evidence

def localSource? (name : Name) : CoreM (Option LocalEvidence) := do
  let some evidence ← localEvidence? name | return none
  let env ← getEnv
  let records := provenanceExt.getState env
  let some source := records.find? evidence.sourceOwner
    | throwError "Local helper lacks original source provenance: {name}"
  unless source.compiled && source.safeDefinition && source.issues.isEmpty &&
      source.sourceModule == evidence.sourceModule do
    throwError "Local helper source owner rejected: {evidence.sourceOwner}: {source.issues}"
  match env.find? evidence.sourceOwner with
  | some (.defnInfo { safety := .safe, .. }) => pure ()
  | some (.axiomInfo _) =>
    unless (importedCompilation? env records evidence.sourceOwner).isSome do
      throwError "Local helper lacks safe imported source owner: {evidence.sourceOwner}"
  | _ => throwError "Local helper lacks safe source owner: {evidence.sourceOwner}"
  return some evidence

-- This is source evidence, not reconstructed compiler IR. Preserve the dependency
-- graph even when imported module visibility removes the original bodies.
def recordedClosure (records : NameMap Provenance) (root : Provenance) : CoreM (Array Name) := do
  let mut pending := root.dependencies
  let mut seen : NameSet := {}
  let mut result := #[]
  let mut fuel := 20000
  while !pending.isEmpty do
    if fuel == 0 then throwError "recorded source closure limit"
    fuel := fuel - 1
    let n := pending.back!
    pending := pending.pop
    unless seen.contains n do
      seen := seen.insert n
      result := result.push n
      if let some r := records.find? n then pending := pending ++ r.dependencies
  return result

def customPasses (env : Environment) : Array Name := Id.run do
  let mut ns := (LCNF.passManagerExt.getState env).1.toArray
  for entries in (LCNF.passManagerExt.toEnvExtension.getState env).importedEntries do
    ns := ns ++ entries
  return ns

-- Capture the compiler's actual environment before ToLCNF applies csimp. The closure
-- intentionally includes source helpers that later optimization may inline or erase.
def capture (n : Name) : CoreM Provenance := do
  let env ← getEnv
  let prior := provenanceExt.getState env
  let info ← getConstInfo n
  let mut dependencies := info.value?.map (·.getUsedConstants) |>.getD #[]
  let rawName := mkUnsafeRecName n
  let mut variantIssue := false
  if let some rawInfo := env.find? rawName then
    -- Lean's generated direct recursive implementation has `partial` safety,
    -- while its logical parent must be a safe definition. User-authored unsafe
    -- or safe declarations with the reserved spelling are not accepted.
    match rawInfo with
    | .defnInfo raw =>
      unless raw.safety == .partial && (info matches .defnInfo { safety := .safe, .. }) do
        variantIssue := true
      dependencies := dependencies ++ raw.value.getUsedConstants.filter (· != rawName)
    | _ => variantIssue := true
  let mut pending := #[n] ++ dependencies
  let mut seen : NameSet := {}
  let mut rewrites := #[]
  let mut issues := if variantIssue then #[s!"unverified recursive implementation: {rawName}"] else #[]
  let mut issueOrigin := if variantIssue then some rawName else none
  let passes := customPasses env
  unless passes.isEmpty do issues := issues.push s!"custom compiler passes: {passes}"
  let mut fuel := 20000
  while !pending.isEmpty do
    if fuel == 0 then
      issues := issues.push "source provenance closure limit"
      break
    fuel := fuel - 1
    let current := pending.back!
    pending := pending.pop
    unless seen.contains current do
      seen := seen.insert current
      let issueCount := issues.size
      if let some e := (CSimp.ext.getState env).map.find? current then
        let rewrite := { source := current, target := e.toDeclName, theoremName := e.thmName : Rewrite }
        unless rewrites.contains rewrite do rewrites := rewrites.push rewrite
        pending := pending.push e.toDeclName
      if isNumericSourceBoundary current then
        issues := issues ++ numericBoundaryIssues env current
      else if (getExternAttrData? env current).isSome then
        issues := issues.push s!"extern source dependency: {current}"
      if (getImplementedBy? env current).isSome then
        issues := issues.push s!"implemented_by source dependency: {current}"
      if hasInitAttr env current then
        issues := issues.push s!"initialized source dependency: {current}"
      unless isCompilerSupport current || isPrimitiveDefinition current || isNumericSourceBoundary current do
        match env.find? current with
        | some (.defnInfo d) =>
          unless d.safety == .safe do issues := issues.push s!"unsafe source dependency: {current}"
          pending := pending ++ d.value.getUsedConstants
        | some (.opaqueInfo _) =>
          -- Module visibility may hide a definition's body. Its defining module's
          -- captured source dependencies remain available through this extension.
          if let some r := prior.find? current then
            pending := pending ++ r.dependencies
          else
            issues := issues.push s!"opaque or unaudited source dependency: {current}"
        | some (.axiomInfo _) =>
          if let some record := importedCompilation? env prior current then
            -- Its original source graph is audited again at export. Scan that
            -- graph here for context-sensitive replacements as well, including
            -- dependencies whose imported bodies are intentionally unavailable.
            for dep in ← recordedClosure prior record do
              let dependencyIssueCount := issues.size
              if let some e := (CSimp.ext.getState env).map.find? dep then
                let rewrite := { source := dep, target := e.toDeclName, theoremName := e.thmName : Rewrite }
                unless rewrites.contains rewrite do rewrites := rewrites.push rewrite
                pending := pending.push e.toDeclName
              if isNumericSourceBoundary dep then
                issues := issues ++ numericBoundaryIssues env dep
              else if (getExternAttrData? env dep).isSome then
                issues := issues.push s!"extern source dependency: {dep}"
              if (getImplementedBy? env dep).isSome then
                issues := issues.push s!"implemented_by source dependency: {dep}"
              if hasInitAttr env dep then
                issues := issues.push s!"initialized source dependency: {dep}"
              if issueOrigin.isNone && issues.size > dependencyIssueCount then
                issueOrigin := some dep
          else issues := issues.push s!"axiom source dependency: {current}"
        | some (.thmInfo _) =>
          let axioms ← collectAxioms current
          unless axioms.isEmpty do issues := issues.push s!"axiomatic source proof: {current}: {axioms}"
        | some _ => pure ()
        | none =>
          if let some r := prior.find? current then
            pending := pending ++ r.dependencies
          else issues := issues.push s!"missing source dependency: {current}"
      if issueOrigin.isNone && issues.size > issueCount then issueOrigin := some current
  let safeDefinition := info matches .defnInfo { safety := .safe, .. }
  return { name := n, dependencies, rewrites, issues, issueOrigin, sourceModule := env.mainModule, safeDefinition }

initialize
  let original ← compileDeclsRef.get
  compileDeclsRef.set fun names opts => do
    if compiler.postponeCompile.get (← getOptions) then
      throwError "lean-go provenance requires compiler.postponeCompile=false"
    let moduleName := (← getEnv).mainModule
    let mut reducedObservations := #[]
    let mut localOwners := #[]
    unless moduleName == `LeanGo.Module.Export || moduleName == `LeanGo.Module.Audit do
      for rawName in names do
        let n := (isUnsafeRecName? rawName).getD rawName
        unless isMarkedMeta (← getEnv) n do
          let info ← getConstInfo n
          unless info matches .thmInfo _ do
            localOwners := localOwners.push n
            reducedObservations := reducedObservations.push (← observeReduced n)
            let mut entry ← capture n
            if let some old := (provenanceExt.getState (← getEnv)).find? n then
              if old.compiled then
                entry := { entry with issues := entry.issues.push s!"repeated compilation or recursive implementation override: {n}" }
            modifyEnv (provenanceExt.addEntry · { entry with compiled := true })
            -- Generated source auxiliaries (e.g. `brecOn`, `_f`, `casesOn`)
            -- need no machine code, hence have no compile callback of their own.
            -- Observe their bodies while the root's lexical context still exists.
            let mut pending := entry.dependencies
            let mut seen : NameSet := {}
            let mut fuel := 20000
            while !pending.isEmpty do
              if fuel == 0 then throwError "source witness closure limit"
              fuel := fuel - 1
              let dep := pending.back!
              pending := pending.pop
              unless seen.contains dep do
                seen := seen.insert dep
                let env ← getEnv
                unless (provenanceExt.getState env).contains dep || (env.getModuleIdxFor? dep).isSome do
                  if let some (.defnInfo _) := env.find? dep then
                    let source ← capture dep
                    modifyEnv (provenanceExt.addEntry · source)
                    pending := pending ++ source.dependencies
    let issues ← withLocalObservations localOwners do
      original names opts
      for before in reducedObservations do captureReduced before
    for (owner, issue) in issues do
      if let some entry := (provenanceExt.getState (← getEnv)).find? owner then
        modifyEnv (provenanceExt.addEntry · { entry with issues := entry.issues.push issue })
end LeanGo.Module
