module
public meta import LeanGo.Module.Local
public meta import Lean.Compiler.LCNF.Main
public meta section
open Lean Lean.Compiler Lean.Compiler.LCNF
namespace LeanGo.Module

structure LocalTyped where
  decl : Decl .pure
  localDecl : FunDecl .pure
  captures : Array (Param .pure)

structure LocalObservation where
  witnesses : Array (Name × Array LocalSourceWitness) := #[]
  sources : Array LocalSource := #[]
  typed : Array LocalTyped := #[]
  events : Array LocalEvidence := #[]
  issues : Array (Name × String) := #[]

private def addIssue (ref : IO.Ref LocalObservation) (owner : Name) (ex : Exception) : CoreM Unit := do
  let message ← rejectionMessage ex
  ref.modify fun s => { s with issues := s.issues.push (owner, message) }

private def observeInit (ref : IO.Ref LocalObservation) (decls : Array (Decl .pure)) : CompilerM Unit := do
  for decl in decls do
    let owner := (isUnsafeRecName? decl.name).getD decl.name
    let some (_, witnesses) := (← ref.get).witnesses.find? (·.1 == owner) | continue
    try
      let .code code := decl.value | continue
      let sources ← collectLocalSources owner code (decl.params.map (·.fvarId)) witnesses
      ref.modify fun s => { s with sources := s.sources ++ sources.map (fun source => { source with sourceDecl := decl, witnesses }) }
    catch ex => addIssue ref owner ex

private def observeTyped (ref : IO.Ref LocalObservation) (decls : Array (Decl .pure)) : CompilerM Unit := do
  for decl in decls do
    let .code body := decl.value | continue
    for source in (← ref.get).sources do
      if let some (localDecl, captures, _) := lexicalLocal body decl.params source.localDecl.fvarId then
        try
          validateLocalType localDecl.type
          for p in captures do validateSourceTypes [← typeKey p.type]
          ref.modify fun s => { s with typed := s.typed.push { decl, localDecl, captures } }
        catch ex => addIssue ref source.sourceOwner ex

structure LocalCandidate where
  source : LocalSource
  beforeDecl : Decl .pure
  localDecl : FunDecl .pure
  captures : Array (Param .pure)

private partial def containsLocalFunction (code : Code .pure) : Bool :=
  match code with
  | .fun .. => true
  | .let _ next => containsLocalFunction next
  | .jp decl next => containsLocalFunction decl.value || containsLocalFunction next
  | .cases c => c.alts.any (fun a => containsLocalFunction a.getCode)
  | _ => false

private partial def candidates (sources : Array LocalSource) (container : Decl .pure)
    (code : Code .pure) (scope : Array FVarId) : CompilerM (Array LocalCandidate) := do
  match code with
  | .let decl next => candidates sources container next (scope.push decl.fvarId)
  | .fun decl next =>
    let some source := sources.find? (·.localDecl.fvarId == decl.fvarId) | do
      -- Lifting an outer function refreshes its body's IDs. A nested unknown
      -- replacement is no longer observable in the original container and may
      -- become visible after outer inlining, so this derivation fails closed.
      if containsLocalFunction decl.value then
        throwError "Nested local helper lacks original source identity: {container.name}"
      -- Dictionary and other compiler lambdas can survive in source dependencies
      -- without entering the exported runtime graph. Grant them no evidence;
      -- uncached targets fail runtime provenance, and reuse is checked below.
      let inside ← candidates sources container decl.value (scope ++ decl.params.map (·.fvarId))
      return inside ++ (← candidates sources container next (scope.push decl.fvarId))
    unless container.name == source.sourceOwner || container.name == source.sourceOwner ++ `_redArg do
      throwError "Unsupported local helper container: {container.name}"
    unless directLocalUses decl.value decl.fvarId 0 do
      throwError "Recursive local helper unsupported: {container.name}.{decl.binderName}"
    let .code whole := container.value | throwError "Missing local helper container body"
    unless directLocalUses whole decl.fvarId decl.params.size do
      throwError "Local helper requires saturated direct calls: {container.name}.{decl.binderName}"
    let (_, captures, _) ← Closure.run (Closure.collectFunDecl decl) scope.contains
    let nested ← candidates sources container decl.value (scope ++ decl.params.map (·.fvarId))
    unless nested.isEmpty do throwError "Nested local helpers unsupported: {container.name}"
    let rest ← candidates sources container next (scope.push decl.fvarId)
    return #[{ source, beforeDecl := container, localDecl := decl, captures }] ++ rest
  | .jp decl next =>
    let inside ← candidates sources container decl.value (scope ++ decl.params.map (·.fvarId))
    return inside ++ (← candidates sources container next (scope.push decl.fvarId))
  | .cases c =>
    c.alts.foldlM (init := #[]) fun found alt => do
      let scope := match alt with
        | .alt _ ps _ _ => scope ++ ps.map (·.fvarId)
        | _ => scope
      return found ++ (← candidates sources container alt.getCode scope)
  | _ => return #[]

-- Preserve the builtin pass's visitation order for every local, including ones
-- without source authority. Auxiliary-cache reuse must not turn an unwitnessed
-- function into an admitted one by borrowing another event's generated name.
private partial def localBinders (code : Code .pure) : Array FVarId :=
  match code with
  | .fun decl next => (localBinders decl.value).push decl.fvarId ++ localBinders next
  | .jp decl next => localBinders decl.value ++ localBinders next
  | .let _ next => localBinders next
  | .cases c => c.alts.foldl (init := #[]) fun ids a => ids ++ localBinders a.getCode
  | _ => #[]

private structure LiftedBinding where
  container : Name
  localId : FVarId
  helper : Name

private def liftingBindings (before after : Array (Decl .pure)) : Array LiftedBinding := Id.run do
  let mut result := #[]
  for decl in before do
    let .code code := decl.value | continue
    let some final := after.find? (·.name == decl.name) | continue
    let .code body := final.value | continue
    for localId in localBinders code do
      let some replacement := localBinding? body localId | continue
      let .const helper [] _ _ := replacement.value | continue
      result := result.push { container := decl.name, localId, helper }
  return result

private def observationOwner (state : LocalObservation) (container : Name) : Name :=
  if let some (owner, _) := state.witnesses.find? (fun (owner, _) =>
      container == owner || container == owner ++ `_redArg) then owner else container

private def observeLifting (ref : IO.Ref LocalObservation)
    (run : Array (Decl .pure) → CompilerM (Array (Decl .pure)))
    (decls : Array (Decl .pure)) : CompilerM (Array (Decl .pure)) := do
  let mut pending := #[]
  let sources := (← ref.get).sources
  for decl in decls do
    let owner := observationOwner (← ref.get) decl.name
    try
      let .code code := decl.value | continue
      pending := pending ++ (← candidates sources decl code (decl.params.map (·.fvarId)))
    catch ex => addIssue ref owner ex
  let beforeEnv ← getEnv
  -- The compiler cache may reuse an existing helper from another source owner.
  -- Capture the complete prior environment, then ask it about the actual result.
  let after ← run decls
  let bindings := liftingBindings decls after
  for candidate in pending do
    let owner := candidate.source.sourceOwner
    try
      let some container := after.find? (·.name == candidate.beforeDecl.name)
        | throwError "Local helper post-lifting container missing: {owner}"
      let .code body := container.value | throwError "Local helper post-lifting body missing: {owner}"
      let some binding := localBinding? body candidate.localDecl.fvarId
        | throwError "Local helper replacement missing: {owner}"
      let .const helper [] _ _ := binding.value
        | throwError "Local helper replacement unsupported: {owner}"
      let some first := bindings.find? (·.helper == helper)
        | throwError "Local helper creation mapping missing: {helper}"
      unless first.container == candidate.beforeDecl.name && first.localId == candidate.localDecl.fvarId do
        throwError "Local helper ambiguous cache reuse: {helper}"
      let priorCompiler ← withEnv beforeEnv (getDecl? helper)
      unless !beforeEnv.contains helper && priorCompiler.isNone &&
          !(localExt.getState beforeEnv).contains helper &&
          !((← ref.get).events.any (·.helper == helper)) do
        throwError "Local helper name collision or ambiguous cache reuse: {helper}"
      let some lifted := after.find? (·.name == helper)
        | throwError "Local helper missing new lifted declaration: {helper}"
      let some typed := (← ref.get).typed.find? (·.localDecl.fvarId == candidate.localDecl.fvarId)
        | throwError "Local helper lacks live pre-mono source identity: {helper}"
      let (domains, result) ← signature lifted.type
      let e : LocalEvidence := {
        sourceOwner := owner, container := container.name, helper,
        sourceModule := beforeEnv.mainModule
        sourceDecl := candidate.source.sourceDecl, sourceWitnesses := candidate.source.witnesses
        sourceLocal := candidate.source.localDecl, sourceCaptures := candidate.source.captures
        typedDecl := typed.decl, typedLocal := typed.localDecl, typedCaptures := typed.captures
        localDecl := candidate.localDecl, captures := candidate.captures
        beforeDecl := candidate.beforeDecl, afterDecl := container, liftedDecl := lifted
        containerDecl := container, helperDecl := lifted, domains, result }
      validateLocalEvent e
      ref.modify fun s => { s with events := s.events.push e }
    catch ex => addIssue ref owner ex
  for binding in bindings do
    unless sources.any (·.localDecl.fvarId == binding.localId) do
      if (localExt.getState beforeEnv).contains binding.helper ||
          (← ref.get).events.any (·.helper == binding.helper) then
        let owner := observationOwner (← ref.get) binding.container
        let issue := s!"Unwitnessed local helper ambiguous cache reuse: {binding.helper}"
        ref.modify fun s => { s with issues := s.issues.push (owner, issue) }
  return after

-- Nested compile callbacks inherit the active wrapper; they must not install it
-- again. Their unrelated declaration names are ignored by the owning observation.
initialize localObserverActive : EnvExtension Bool ← registerEnvExtension (pure false) (asyncMode := .local)

def withLocalObservations (names : Array Name) (action : CoreM Unit) : CoreM (Array (Name × String)) := do
  if names.isEmpty || localObserverActive.getState (← getEnv) then
    action
    return #[]
  let ref ← IO.mkRef ({} : LocalObservation)
  for owner in names do
    try
      let witnesses ← localSourceWitnesses owner
      ref.modify fun s => { s with witnesses := s.witnesses.push (owner, witnesses) }
    catch ex => addIssue ref owner ex
  let old ← getPassManager
  let base := old.basePasses.map fun pass =>
    if pass.name == `init then
      { pass with run := fun decls => pass.phase.withPurityCheck .pure fun h => do
          let decls := h ▸ decls
          observeInit ref decls
          pass.run (h ▸ decls) }
    else if pass.name == `toMono then
      { pass with run := fun decls => pass.phase.withPurityCheck .pure fun h => do
          observeTyped ref (h ▸ decls)
          pass.run decls }
    else pass
  let mono := old.monoPasses.map fun pass =>
    if pass.name == `lambdaLifting then
      { pass with run := fun decls => pass.phase.withPurityCheck .pure fun h =>
          pass.phaseOut.withPurityCheck .pure fun out => do
            let result ← observeLifting ref (fun ds => do return out ▸ (← pass.run (h ▸ ds))) (h ▸ decls)
            return out ▸ result }
    else pass
  unless old.basePasses.countP (·.name == `init) == 1 &&
      old.basePasses.countP (·.name == `toMono) == 1 &&
      old.monoPasses.countP (·.name == `lambdaLifting) == 1 do
    throwError "Local helper observation requires the pinned builtin passes"
  modifyEnv fun env => localObserverActive.setState env true
  modifyEnv fun env => passManagerExt.modifyState (asyncMode := .local) env fun (ns, _) =>
    (ns, { old with basePasses := base, monoPasses := mono })
  try
    action
  finally
    modifyEnv fun env => passManagerExt.modifyState (asyncMode := .local) env fun (ns, _) => (ns, old)
    modifyEnv fun env => localObserverActive.setState env false
  for e in (← ref.get).events do
    try
      let some container ← getMonoDecl? e.container | throwError "Missing final local helper container: {e.helper}"
      let some helper ← getMonoDecl? e.helper | throwError "Missing final local helper declaration: {e.helper}"
      let evidence := { e with containerDecl := container, helperDecl := helper }
      unless evidence.container == evidence.sourceOwner do
        let some reduced ← reducedEvidence? evidence.container
          | throwError "Unsupported local helper reduced owner: {evidence.container}"
        unless reduced.owner == evidence.sourceOwner do throwError "Local helper reduced owner mismatch"
      modifyEnv (localExt.addEntry · { helper := e.helper, evidence? := some evidence })
      modifyEnv (localOwnerExt.addEntry · { owner := e.sourceOwner, helper := e.helper })
    catch ex => addIssue ref e.sourceOwner ex
  return (← ref.get).issues

end LeanGo.Module
