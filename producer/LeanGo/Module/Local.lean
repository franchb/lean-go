module
public meta import LeanGo.Module.LocalSource
public meta import LeanGo.Module.Reduced
public meta import Lean.Compiler.LCNF.Internalize
public meta section
open Lean Lean.Compiler.LCNF
namespace LeanGo.Module

-- Compiler derivation evidence is separate from original source authority.
structure LocalEvidence where
  sourceOwner : Name
  container : Name
  helper : Name
  sourceModule : Name
  sourceDecl : Decl .pure
  sourceWitnesses : Array LocalSourceWitness
  sourceLocal : FunDecl .pure
  sourceCaptures : Array (Param .pure)
  typedDecl : Decl .pure
  typedLocal : FunDecl .pure
  typedCaptures : Array (Param .pure)
  localDecl : FunDecl .pure
  captures : Array (Param .pure)
  beforeDecl : Decl .pure
  afterDecl : Decl .pure
  liftedDecl : Decl .pure
  containerDecl : Decl .pure
  helperDecl : Decl .pure
  domains : List String
  result : String
  deriving Inhabited

structure LocalEntry where
  helper : Name
  evidence? : Option LocalEvidence := none
  issue : String := ""
  deriving Inhabited

initialize localExt : SimplePersistentEnvExtension LocalEntry (NameMap LocalEntry) ←
  registerSimplePersistentEnvExtension {
    addImportedFn := mkStateFromImportedEntries (fun s r => s.insert r.helper r) {}
    addEntryFn := fun s r => s.insert r.helper r
    asyncMode := .sync
    replay? := some <| SimplePersistentEnvExtension.replayOfFilter
      (fun s r => !s.contains r.helper) (fun s r => s.insert r.helper r)
  }

structure LocalOwnerEdge where
  owner : Name
  helper : Name
  deriving Inhabited

initialize localOwnerExt : SimplePersistentEnvExtension LocalOwnerEdge (NameMap (Array Name)) ←
  registerSimplePersistentEnvExtension {
    addImportedFn := mkStateFromImportedEntries
      (fun s e => s.insert e.owner ((s.find? e.owner).getD #[] |>.push e.helper)) {}
    addEntryFn := fun s e => s.insert e.owner ((s.find? e.owner).getD #[] |>.push e.helper)
    asyncMode := .sync
    replay? := some <| SimplePersistentEnvExtension.replayOfFilter
      (fun s e => !((s.find? e.owner).getD #[]).contains e.helper)
      (fun s e => s.insert e.owner ((s.find? e.owner).getD #[] |>.push e.helper))
  }

partial def localBinding? (code : Code .pure) (id : FVarId) : Option (LetDecl .pure) :=
  match code with
  | .let d k => if d.fvarId == id then some d else localBinding? k id
  | .fun d k | .jp d k => (localBinding? d.value id).orElse fun _ => localBinding? k id
  | .cases c => c.alts.findSome? fun a => localBinding? a.getCode id
  | _ => none

-- Recompute captures from a saved lexical scope. All admitted types are closed,
-- so the pinned collector's operand visitation order completely determines them.
private def captureOperand (scope seen : Array (Param .pure)) (id : FVarId) : Array (Param .pure) :=
  if seen.any (·.fvarId == id) then seen
  else if let some p := scope.find? (·.fvarId == id) then seen.push p else seen

private def captureArgs (scope seen : Array (Param .pure)) (args : Array (Arg .pure)) :=
  args.foldl (init := seen) fun seen arg =>
    if let .fvar id := arg then captureOperand scope seen id else seen

private partial def captureBody (scope seen : Array (Param .pure)) (code : Code .pure) :
    Array (Param .pure) :=
  match code with
  | .let d k =>
    let seen := match d.value with
      | .fvar id args => captureArgs scope (captureOperand scope seen id) args
      | .const _ _ args _ => captureArgs scope seen args
      | .proj _ _ id _ => captureOperand scope seen id
      | _ => seen
    captureBody scope seen k
  | .fun d k | .jp d k => captureBody scope (captureBody scope seen d.value) k
  | .cases c => c.alts.foldl (init := captureOperand scope seen c.discr) fun seen a =>
      captureBody scope seen a.getCode
  | .return id => captureOperand scope seen id
  | .jmp _ args => captureArgs scope seen args
  | .unreach _ => seen

partial def lexicalLocal (code : Code .pure) (scope : Array (Param .pure)) (id : FVarId) :
    Option (FunDecl .pure × Array (Param .pure) × Code .pure) :=
  match code with
  | .let d k => lexicalLocal k (scope.push { d with borrow := false }) id
  | .fun d k =>
    if d.fvarId == id then some (d, captureBody scope #[] d.value, k)
    else (lexicalLocal d.value (scope ++ d.params) id).orElse fun _ =>
      lexicalLocal k (scope.push (d.toParam false)) id
  | .jp d k =>
    (lexicalLocal d.value (scope ++ d.params) id).orElse fun _ =>
      lexicalLocal k (scope.push (d.toParam false)) id
  | .cases c => c.alts.findSome? fun a =>
    let scope := match a with
      | .alt _ ps _ _ => scope ++ ps
      | _ => scope
    lexicalLocal a.getCode scope id
  | _ => none

-- Lambda lifting changes only function bindings in the container. Check the
-- unchanged surroundings, including case alternatives and join declarations.
private partial def liftingShape (before after : Code .pure) : Bool :=
  match before, after with
  | .let a x, .let b y => a == b && liftingShape x y
  | .fun a x, .let b y =>
    a.fvarId == b.fvarId && a.binderName == b.binderName && a.type == b.type &&
    (b.value matches .const _ [] _ _) && liftingShape x y
  | .jp a x, .jp b y =>
    a.fvarId == b.fvarId && a.binderName == b.binderName && a.type == b.type &&
    a.params == b.params && liftingShape a.value b.value && liftingShape x y
  | .cases a, .cases b =>
    a.typeName == b.typeName && a.resultType == b.resultType && a.discr == b.discr &&
    a.alts.size == b.alts.size && (a.alts.zip b.alts).all (fun (a, b) =>
      match a, b with
      | .alt n ps x _, .alt m qs y _ => n == m && ps == qs && liftingShape x y
      | .default x, .default y => liftingShape x y
      | _, _ => false)
  | _, _ => before == after

-- Check occurrences in every operand position; only saturated direct calls count.
partial def directLocalUses (code : Code .pure) (id : FVarId) (arity : Nat) : Bool :=
  let safeArgs := fun (args : Array (Arg .pure)) => !args.any (· == .fvar id)
  match code with
  | .let d k =>
    let valid := match d.value with
      | .fvar f args => safeArgs args && (f != id || args.size == arity)
      | .const _ _ args _ => safeArgs args
      | .proj _ _ f _ => f != id
      | _ => true
    valid && !d.type.containsFVar id && directLocalUses k id arity
  | .fun d k | .jp d k =>
    !d.type.containsFVar id && directLocalUses d.value id arity && directLocalUses k id arity
  | .return f => f != id
  | .jmp f args => f != id && safeArgs args
  | .cases c => c.discr != id && c.alts.all (fun a => directLocalUses a.getCode id arity)
  | .unreach _ => true

private def prependTypes (params : Array (Param .pure)) (result : Expr) : Expr :=
  params.foldr (fun p body => .forallE p.binderName p.type body .default) result

def localSynthetic (helper : Name) (safe : Bool) (captures : Array (Param .pure))
    (decl : FunDecl .pure) : CoreM (Decl .pure) := do
  let (_, result) ← signature decl.type
  let params := captures ++ decl.params
  return {
    name := helper, safe := safe, levelParams := [], params := params
    type := prependTypes params (.const result.toName []), value := .code decl.value,
    recursive := false, inlineAttr? := none }

def localLiftMatches (e : LocalEvidence) : CoreM Bool := do
  let expected ← normalizeFVarIds (← localSynthetic e.helper e.beforeDecl.safe e.captures e.localDecl)
  let actual ← normalizeFVarIds e.liftedDecl
  return expected == actual

def validateLocalEvent (e : LocalEvidence) : CoreM Unit := do
  unless e.beforeDecl.name == e.container && e.afterDecl.name == e.container &&
      e.liftedDecl.name == e.helper && e.sourceLocal.fvarId == e.localDecl.fvarId &&
      e.typedLocal.fvarId == e.localDecl.fvarId &&
      e.beforeDecl.safe && e.afterDecl.safe && e.liftedDecl.safe &&
      e.beforeDecl.levelParams.isEmpty && e.liftedDecl.levelParams.isEmpty do
    throwError "Local helper event identity mismatch: {e.helper}"
  unless e.sourceDecl.name == e.sourceOwner && e.sourceDecl.safe &&
      e.sourceDecl.levelParams.isEmpty && e.sourceDecl.size <= 10000 &&
      e.beforeDecl.size <= 10000 &&
      { e.beforeDecl with value := e.afterDecl.value } == e.afterDecl do
    throwError "Local helper source or container snapshot identity mismatch: {e.helper}"
  let .code sourceBody := e.sourceDecl.value | throwError "Local helper missing source snapshot"
  let some (sourceLocal, sourceCaptures, sourceNext) :=
      lexicalLocal sourceBody e.sourceDecl.params e.sourceLocal.fvarId
    | throwError "Local helper source local missing: {e.helper}"
  unless sourceLocal == e.sourceLocal && sourceCaptures == e.sourceCaptures &&
      (e.sourceWitnesses.any (fun w => w.binderName == sourceLocal.binderName && w.type == sourceLocal.type) ||
        sourceAlias sourceNext sourceLocal e.sourceWitnesses) do
    throwError "Local helper source snapshot mapping mismatch: {e.helper}"
  unless e.typedDecl.name == e.sourceOwner && e.typedDecl.safe && e.typedDecl.levelParams.isEmpty do
    throwError "Local helper typed source snapshot identity mismatch: {e.helper}"
  let .code typedBody := e.typedDecl.value | throwError "Local helper missing typed source snapshot"
  let some (typedLocal, typedCaptures, _) := lexicalLocal typedBody e.typedDecl.params e.typedLocal.fvarId
    | throwError "Local helper typed source local missing: {e.helper}"
  unless typedLocal == e.typedLocal && typedCaptures == e.typedCaptures do
    throwError "Local helper typed source mapping mismatch: {e.helper}"
  validateLocalType e.typedLocal.type
  for p in e.typedCaptures do validateSourceTypes [← typeKey p.type]
  let .code before := e.beforeDecl.value | throwError "Local helper missing pre-lifting body"
  let .code after := e.afterDecl.value | throwError "Local helper missing post-lifting body"
  let some (localDecl, captures, _) := lexicalLocal before e.beforeDecl.params e.localDecl.fvarId
    | throwError "Local helper pre-lifting local missing: {e.helper}"
  unless localDecl == e.localDecl && captures == e.captures && liftingShape before after &&
      directLocalUses before e.localDecl.fvarId e.localDecl.params.size do
    throwError "Local helper declaration or direct call mapping mismatch: {e.helper}"
  let some binding := localBinding? after e.localDecl.fvarId
    | throwError "Local helper replacement missing: {e.helper}"
  unless binding.type == e.localDecl.type &&
      binding.value == .const e.helper [] (e.captures.map (.fvar ·.fvarId)) do
    throwError "Local helper capture mapping mismatch: {e.helper}"
  unless ← localLiftMatches e do throwError "Local helper lifted body mismatch: {e.helper}"
  validateLocalType e.sourceLocal.type
  for p in e.sourceCaptures do validateSourceTypes [← typeKey p.type]
  for p in e.localDecl.params do
    let some source := e.typedLocal.params.find? (·.fvarId == p.fvarId)
      | throwError "Local helper parameter lacks source identity: {e.helper}"
    unless (← toMonoType source.type) == p.type do
      throwError "Local helper source parameter type mismatch: {e.helper}"
  for p in e.captures do
    let some source := e.typedCaptures.find? (·.fvarId == p.fvarId)
      | throwError "Local helper capture lacks source identity: {e.helper}"
    unless (← toMonoType source.type) == p.type do
      throwError "Local helper source capture type mismatch: {e.helper}"
  let (_, sourceResult) ← signature e.sourceLocal.type
  let (_, monoResult) ← signature e.localDecl.type
  unless (← representation sourceResult).mono == monoResult &&
      (← signature e.liftedDecl.type) == (e.domains, e.result) do
    throwError "Local helper checked signature mismatch: {e.helper}"

private def definingModule (env : Environment) (name : Name) : Name :=
  if let some idx := env.getModuleIdxFor? name then env.allImportedModuleNames[idx]!
  else env.mainModule

def localEvidence? (name : Name) : CoreM (Option LocalEvidence) := do
  let env ← getEnv
  let some entry := (localExt.getState env).find? name | return none
  unless entry.issue.isEmpty do throwError "{entry.issue}"
  let some e := entry.evidence? | throwError "Missing local helper evidence: {name}"
  unless entry.helper == name && e.helper == name && !env.contains name do
    throwError "Local helper identity or kernel collision: {name}"
  unless ((localOwnerExt.getState env).find? e.sourceOwner |>.getD #[]).contains name do
    throwError "Local helper source owner edge missing: {name}"
  for n in #[e.sourceOwner, e.container, name] do
    unless definingModule env n == e.sourceModule do throwError "Local helper module mismatch: {name}"
  unless e.container == e.sourceOwner do
    let some reduced ← reducedEvidence? e.container
      | throwError "Unsupported local helper owner derivation: {e.container}"
    unless reduced.owner == e.sourceOwner && reduced.sourceModule == e.sourceModule do
      throwError "Local helper reduced owner mismatch: {name}"
  let some container ← getMonoDecl? e.container | throwError "Missing local helper container: {name}"
  let some helper ← getMonoDecl? name | throwError "Missing local helper declaration: {name}"
  unless container == e.containerDecl && helper == e.helperDecl do
    throwError "Local helper declaration snapshot mismatch: {name}"
  validateLocalEvent e
  unless helper.safe && helper.levelParams.isEmpty &&
      (← signature helper.type) == (e.domains, e.result) do
    throwError "Local helper final signature mismatch: {name}"
  return some e

def validateLocalOwner (owner : Name) : CoreM Unit := do
  let helpers := (localOwnerExt.getState (← getEnv)).find? owner |>.getD #[]
  for helper in helpers do
    let some evidence ← localEvidence? helper
      | throwError "Local source owner edge lacks helper evidence: {owner} -> {helper}"
    unless evidence.sourceOwner == owner do throwError "Local helper source owner edge mismatch: {helper}"
  for (_, entry) in (localExt.getState (← getEnv)).toList do
    if let some evidence := entry.evidence? then
      if evidence.sourceOwner == owner || evidence.container == owner then
        discard <| localEvidence? entry.helper

end LeanGo.Module
