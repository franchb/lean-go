import LeanGo.Feasibility.Model
import Lean
import Lean.Compiler.LCNF
import Lean.Compiler.CSimpAttr
import Lean.Compiler.ExternAttr
import Lean.Compiler.ImplementedByAttr
import Lean.Util.CollectAxioms

open Lean Elab Command Meta
open Lean.Compiler.LCNF
namespace Pilot

def expectedPreserves : Prop := ∀ e : LeanGo.Feasibility.Expr,
  ∀ rho : UInt32 → LeanGo.Feasibility.Tri,
  LeanGo.Feasibility.eval rho (LeanGo.Feasibility.normalize e) = LeanGo.Feasibility.eval rho e

def expectedNormal : Prop := ∀ e : LeanGo.Feasibility.Expr,
  LeanGo.Feasibility.IsNormalForm (LeanGo.Feasibility.normalize e)

def expectedRewrite : Prop := @Bool.not = @Bool.Internal.not

def obj (xs : List (String × Json)) : Json := Json.mkObj xs
def str (s : String) : Json := toJson s

def typeKey (e : Lean.Expr) : CoreM String := do
  match e with
  | .const ``Bool [] => pure "bool"
  | .const ``LeanGo.Feasibility.Tri [] => pure "tri"
  | .const ``LeanGo.Feasibility.Expr [] => pure "expr"
  | .const ``UInt32 [] => pure "u32"
  | _ => throwError "Unsupported type {e}"

def typeName (n : Name) : CoreM String := typeKey (.const n [])

def signature (e : Lean.Expr) : CoreM (List String × String) := do
  match e with
  | .forallE _ domain body _ =>
    let domain ← typeKey domain
    let (domains, result) ← signature body
    pure (domain :: domains, result)
  | _ => pure ([], ← typeKey e)

def functions : List (Name × List String × String) := [
  (``LeanGo.Feasibility.Tri.neg, ["tri"], "tri"),
  (``LeanGo.Feasibility.normalize, ["expr"], "expr"),
  (``LeanGo.Feasibility.normalizeAux, ["bool", "expr"], "expr")]

def schemas : List (Name × List (Name × List String)) := [
  (``Bool, [(``Bool.false, []), (``Bool.true, [])]),
  (``LeanGo.Feasibility.Tri, [(``LeanGo.Feasibility.Tri.f, []),
    (``LeanGo.Feasibility.Tri.u, []), (``LeanGo.Feasibility.Tri.t, [])]),
  (``LeanGo.Feasibility.Expr, [(``LeanGo.Feasibility.Expr.lit, ["tri"]),
    (``LeanGo.Feasibility.Expr.var, ["u32"]), (``LeanGo.Feasibility.Expr.neg, ["expr"]),
    (``LeanGo.Feasibility.Expr.conj, ["expr", "expr"]),
    (``LeanGo.Feasibility.Expr.disj, ["expr", "expr"])])]

def constructor (name : Name) : CoreM (String × List String) := do
  for (ty, ctors) in schemas do
    for (n, fields) in ctors do
      if n == name then return (← typeName ty, fields)
  throwError "Unadmitted constructor {name}"

def exportTypes : CoreM Json := do
  let mut result := #[]
  for (name, ctors) in schemas do
    let info ← getConstInfoInduct name
    unless info.numParams == 0 && info.numIndices == 0 && info.levelParams.isEmpty &&
        info.type == .sort (.succ .zero) &&
        info.ctors == ctors.map Prod.fst do throwError "Inductive schema changed: {name}"
    let mut out := #[]
    for i in [:ctors.length] do
      let (n, fields) := ctors[i]!
      let ctor ← getConstInfoCtor n
      let actual ← signature ctor.type
      unless ctor.cidx == i && ctor.numParams == 0 && ctor.numFields == fields.length &&
          ctor.levelParams.isEmpty && actual == (fields, ← typeName name) do
        throwError "Constructor schema changed: {n}"
      out := out.push (obj [("name", str n.toString), ("tag", toJson i), ("fields", toJson fields)])
    result := result.push (obj [("name", str (← typeName name)), ("constructors", .arr out)])
  return .arr result

def checkTheorem (n expected : Name) (axioms : List Name) : MetaM Unit := do
  let info ← getConstInfo n
  unless info matches .thmInfo _ do throwError "Claim is not a theorem: {n}"
  let witness ← getConstInfo expected
  let some body := witness.value? | throwError "Missing expected statement"
  unless ← isDefEq info.type body do throwError "Theorem type changed: {n}"
  let actual ← collectAxioms n
  unless actual.toList == axioms do throwError "Unexpected theorem axioms: {n}: {actual}"

-- This audit is deliberately scoped to the pinned original model and toolchain.
-- Current-environment attributes alone do not establish general rewrite history.
def auditSource : CoreM Unit := do
  let env ← getEnv
  let mut pending := [``LeanGo.Feasibility.normalize]
  let mut seen : List Name := []
  let mut rewrites : List Name := []
  let mut fuel := 10000
  while !pending.isEmpty do
    if fuel == 0 then throwError "Source closure limit"
    fuel := fuel - 1
    let n := pending.head!
    pending := pending.tail!
    unless seen.contains n do
      seen := n :: seen
      let info ← getConstInfo n
      if (getExternAttrData? env n).isSome then throwError "Unexpected source extern: {n}"
      if (Compiler.getImplementedBy? env n).isSome then throwError "Unexpected source replacement: {n}"
      if let some entry := (Compiler.CSimp.ext.getState env).map.find? n then
        unless n == ``Bool.not && entry.toDeclName == ``Bool.Internal.not &&
            entry.thmName == ``Bool.not_eq_internalNot do throwError "Unexpected compiler rewrite: {n}"
        rewrites := n :: rewrites
      match info with
      | .defnInfo d =>
        unless d.safety == .safe do throwError "Unsafe source declaration: {n}"
        pending := d.value.getUsedConstants.toList ++ pending
      | .opaqueInfo _ => throwError "Opaque source dependency: {n}"
      | _ => pure ()
  unless rewrites == [``Bool.not] do throwError "Reviewed compiler rewrite missing"

structure EncodingState where
  next : Nat := 1
  seen : List FVarId := []
  nodes : Nat := 0
  calls : List Name := []
abbrev EncodeM := StateRefT EncodingState CoreM
abbrev Scope := List (FVarId × Nat × String)

def lookup (scope : Scope) (f : FVarId) : EncodeM (Nat × String) := do
  for (id, n, ty) in scope do
    if id == f then return (n, ty)
  throwError "Unbound variable {f.name}"

def bind (scope : Scope) (f : FVarId) (ty : String) : EncodeM (Scope × Json) := do
  let s ← get
  if s.seen.contains f then throwError "Duplicate variable {f.name}"
  set { s with next := s.next + 1, seen := f :: s.seen }
  pure ((f, s.next, ty) :: scope, obj [("id", toJson s.next), ("type", str ty)])

partial def encode (code : Code .pure) (scope : Scope) (result : String) (depth : Nat := 1) : EncodeM Json := do
  let s ← get
  if depth > 256 || s.nodes >= 10000 then throwError "IR traversal limit"
  modify fun s => { s with nodes := s.nodes + 1 }
  match code with
  | .return f =>
    let (id, ty) ← lookup scope f
    unless ty == result do throwError "Return type mismatch"
    pure (obj [("op", str "return"), ("var", toJson id)])
  | .let decl next =>
    let ty ← typeKey decl.type
    let .const name levels args _ := decl.value | throwError "Unsupported let value"
    unless levels.isEmpty do throwError "Universe arguments unsupported"
    let info ← getConstInfo name
    let (kind, expected, target) ← match info with
      | .ctorInfo _ => do
        let (target, fields) ← constructor name
        pure ("constructor", fields, target)
      | _ => do
        let some (_, domains, target) := functions.find? (fun f => f.1 == name)
          | throwError "Unadmitted call {name}"
        modify fun s => { s with calls := name :: s.calls }
        pure ("call", domains, target)
    unless ty == target && args.size == expected.length do throwError "Application signature mismatch: {name}"
    let mut ids := #[]
    for i in [:args.size] do
      let .fvar f := args[i]! | throwError "Unsupported argument"
      let (id, ty) ← lookup scope f
      unless ty == expected[i]! do throwError "Argument type mismatch"
      ids := ids.push id
    let (scope, param) ← bind scope decl.fvarId ty
    let value := obj [("kind", str kind), ("name", str name.toString), ("args", toJson ids)]
    pure (obj [("op", str "let"), ("bind", param), ("value", value),
      ("next", ← encode next scope result (depth + 1))])
  | .cases (.mk name resultType discr alts) =>
    let ty ← typeName name
    let (id, actual) ← lookup scope discr
    unless actual == ty && (← typeKey resultType) == result do throwError "Cases type mismatch"
    let some (_, ctors) := schemas.find? (fun s => s.1 == name) | throwError "Unadmitted cases type"
    unless alts.size == ctors.length do throwError "Nonexhaustive alternatives"
    let mut seen : List Name := []
    let mut branches := #[]
    for alt in alts do
      let .alt ctor params body _ := alt | throwError "Unsupported alternative"
      let (target, fields) ← constructor ctor
      unless target == ty && !seen.contains ctor && params.size == fields.length do
        throwError "Invalid case constructor"
      seen := ctor :: seen
      let mut branchScope := scope
      let mut binders := #[]
      for i in [:params.size] do
        let p := params[i]!
        unless (← typeKey p.type) == fields[i]! do throwError "Case field type mismatch"
        let (updated, binder) ← bind branchScope p.fvarId fields[i]!
        branchScope := updated
        binders := binders.push binder
      branches := branches.push (obj [("constructor", str ctor.toString), ("params", .arr binders),
        ("body", ← encode body branchScope result (depth + 1))])
    pure (obj [("op", str "cases"), ("var", toJson id), ("type", str ty),
      ("result", str result), ("alts", .arr branches)])
  | _ => throwError "Unsupported control flow"

def exportFunction (name : Name) (domains : List String) (result : String) : CoreM (Json × List Name) := do
  let some decl ← getMonoDecl? name | throwError "Missing pure-mono declaration: {name}"
  unless decl.safe && decl.levelParams.isEmpty && decl.params.size == domains.length &&
      (← signature decl.type) == (domains, result) &&
      decl.recursive == (name == ``LeanGo.Feasibility.normalizeAux) do
    throwError "Function signature/safety changed: {name}"
  let .code body := decl.value | throwError "External function body: {name}"
  let ((params, body), state) ← (do
    let mut scope := []
    let mut params := #[]
    for i in [:decl.params.size] do
      let p := decl.params[i]!
      unless (← typeKey p.type) == domains[i]! do throwError "Parameter type mismatch"
      let (updated, param) ← bind scope p.fvarId domains[i]!
      scope := updated
      params := params.push param
    pure (params, ← encode body scope result) : EncodeM (Array Json × Json)).run {}
  for call in state.calls do
    if call == ``LeanGo.Feasibility.normalize ||
        (name == ``LeanGo.Feasibility.Tri.neg) then
      throwError "Unexpected recursion or function dependency"
  pure (obj [("name", str name.toString), ("params", .arr params),
    ("result", str result), ("body", body)], state.calls)

def exportProgram : MetaM Json := do
  checkTheorem ``LeanGo.Feasibility.normalize_preserves_eval ``expectedPreserves [``propext]
  checkTheorem ``LeanGo.Feasibility.normalize_is_normal ``expectedNormal [``propext]
  checkTheorem ``Bool.not_eq_internalNot ``expectedRewrite [``Quot.sound]
  auditSource
  let types ← exportTypes
  let mut emitted := #[]
  let mut graph := []
  for (name, args, result) in functions do
    let (decl, calls) ← exportFunction name args result
    emitted := emitted.push decl
    graph := (name, calls) :: graph
  let mut reachable := [``LeanGo.Feasibility.normalize]
  for _ in [:3] do
    for (name, calls) in graph do
      if reachable.contains name then
        for call in calls do
          unless reachable.contains call do reachable := call :: reachable
  unless reachable.length == 3 do throwError "Code closure incomplete"
  let claims := [
    (``LeanGo.Feasibility.normalize_preserves_eval,
      "forall e : Expr, forall rho : UInt32 -> Tri, eval rho (normalize e) = eval rho e"),
    (``LeanGo.Feasibility.normalize_is_normal, "forall e : Expr, IsNormalForm (normalize e)")]
  pure (obj [("types", types), ("functions", .arr emitted),
    ("claims", toJson (claims.map fun (n, statement) => obj [
      ("name", str n.toString), ("statement", str statement), ("axioms", toJson ["propext"])])),
    ("rewrites", toJson [obj [("source", str "Bool.not"), ("target", str "Bool.Internal.not"),
      ("theorem", str "Bool.not_eq_internalNot"), ("axioms", toJson ["Quot.sound"]) ]])])
end Pilot
