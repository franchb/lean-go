module
public meta import LeanGo.Module.Audit
public meta import Lean.Compiler.LCNF
public meta section
open Lean Elab Command Meta
open Lean.Compiler.LCNF
namespace LeanGo.Module

def obj (xs : List (String × Json)) : Json := Json.mkObj xs
def str (s : String) : Json := toJson s

def sortByName (xs : Array Json) : Array Json :=
  xs.qsort fun a b =>
    (a.getObjValAs? String "name").toOption.getD "" < (b.getObjValAs? String "name").toOption.getD ""

def sortedDependencies (ns : Array Name) : Array String :=
  (ns.foldl (fun (s : NameSet) n => s.insert n) {}).toArray.map Name.toString |>.qsort (· < ·)

def sortRewrites (rs : Array Rewrite) : Array Rewrite :=
  rs.qsort fun a b =>
    if a.source != b.source then a.source.toString < b.source.toString
    else if a.target != b.target then a.target.toString < b.target.toString
    else a.theoremName.toString < b.theoremName.toString

def typeKey (e : Expr) : CoreM String := do
  let .const n [] := e | throwError "Unsupported first-order monomorphic type: {e}"
  if n == ``UInt32 then return n.toString
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

structure EncodingState where
  next : Nat := 1
  seen : List FVarId := []
  nodes : Nat := 0
  calls : List Name := []
  types : List String := []
abbrev EncodeM := StateRefT EncodingState CoreM
abbrev Scope := List (FVarId × Nat × String)

def lookup (scope : Scope) (f : FVarId) : EncodeM (Nat × String) := do
  for (id, n, ty) in scope do
    if id == f then return (n, ty)
  throwError "Unbound variable {f.name}"

def bind (scope : Scope) (f : FVarId) (ty : String) : EncodeM (Scope × Json) := do
  let s ← get
  if s.seen.contains f then throwError "Duplicate variable {f.name}"
  set { s with next := s.next + 1, seen := f :: s.seen, types := ty :: s.types }
  return ((f, s.next, ty) :: scope, obj [("id", toJson s.next), ("type", str ty)])

partial def encode (code : Code .pure) (scope : Scope) (result : String) (depth : Nat := 1) : EncodeM Json := do
  let s ← get
  if depth > 256 || s.nodes >= 10000 then throwError "IR traversal limit"
  modify fun s => { s with nodes := s.nodes + 1 }
  match code with
  | .return f =>
    let (id, ty) ← lookup scope f
    unless ty == result do throwError "Return type mismatch"
    return obj [("op", str "return"), ("var", toJson id)]
  | .let decl next =>
    let ty ← typeKey decl.type
    let .const name levels args _ := decl.value | throwError "Unsupported let value (only calls and constructors are admitted)"
    unless levels.isEmpty do throwError "Universe arguments unsupported"
    let info ← getConstInfo name
    let (kind, expected, target) ← match info with
      | .ctorInfo _ => do
        let (target, fields) ← constructor name
        pure ("constructor", fields, target)
      | _ => do
        let (domains, target) ← signature info.type
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
    return obj [("op", str "let"), ("bind", param), ("value", value),
      ("next", ← encode next scope result (depth + 1))]
  | .cases (.mk name resultType discr alts) =>
    let ty ← typeKey (.const name [])
    let info ← getConstInfoInduct name
    let ctors := info.ctors
    let (id, actual) ← lookup scope discr
    unless actual == ty && (← typeKey resultType) == result do throwError "Cases type mismatch"
    unless alts.size == ctors.length do throwError "Nonexhaustive alternatives"
    let mut seen : List Name := []
    let mut branches := #[]
    for alt in alts do
      let .alt ctor params body _ := alt | throwError "Unsupported alternative"
      let (target, fields) ← constructor ctor
      unless target == ty && ctors.contains ctor && !seen.contains ctor && params.size == fields.length do
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
    return obj [("op", str "cases"), ("var", toJson id), ("type", str ty),
      ("result", str result), ("alts", .arr branches)]
  | _ => throwError "Unsupported control flow (only return, let and exhaustive cases are admitted)"

def exportFunction (name : Name) : CoreM (Json × List Name × List String) := do
  let env ← getEnv
  unless isPrimitiveDefinition name do
    let some record := (provenanceExt.getState env).find? name
      | throwError "Runtime function lacks compilation provenance: {name}"
    unless record.compiled do throwError "Runtime helper lacks its own compilation provenance: {name}"
  let info ← getConstInfo name
  let (domains, result) ← signature info.type
  let some decl ← getMonoDecl? name | throwError "Missing pure-mono declaration: {name}"
  unless decl.safe && decl.levelParams.isEmpty && decl.params.size == domains.length &&
      (← signature decl.type) == (domains, result) do
    throwError "Function signature/safety mismatch: {name}"
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
  return (obj [("name", str name.toString), ("params", .arr params),
    ("result", str result), ("body", body)], state.calls, result :: state.types)

def exportTypes (used : List String) : CoreM Json := do
  let mut pending := used
  let mut seen : List String := []
  let mut result := #[]
  let mut fuel := 1000
  while !pending.isEmpty do
    if fuel == 0 then throwError "ADT closure limit"
    fuel := fuel - 1
    let name := pending.head!
    pending := pending.tail!
    unless seen.contains name || name == "UInt32" do
      seen := name :: seen
      let n := name.toName
      discard <| typeKey (.const n [])
      let info ← getConstInfoInduct n
      if info.isUnsafe then throwError "Unsafe ADT: {n}"
      let mut ctors := #[]
      for i in [:info.ctors.length] do
        let ctorName := info.ctors[i]!
        let ctor ← getConstInfoCtor ctorName
        let (target, fields) ← constructor ctorName
        unless target == name && ctor.cidx == i do throwError "Constructor schema mismatch: {ctorName}"
        pending := fields ++ pending
        ctors := ctors.push <| obj [("name", str ctorName.toString), ("tag", toJson i), ("fields", toJson fields)]
      if ctors.isEmpty then throwError "Empty inductives unsupported: {n}"
      result := result.push <| obj [("name", str name), ("constructors", .arr ctors)]
  return .arr (sortByName result)

def rewriteJson (r : Rewrite) : CoreM Json := do
  unless r.source == ``Bool.not && r.target == ``Bool.Internal.not &&
      r.theoremName == ``Bool.not_eq_internalNot do
    throwError "Unsupported historical compiler rewrite: {r.source} -> {r.target} via {r.theoremName}"
  let info ← getConstInfo r.theoremName
  unless info matches .thmInfo _ do throwError "Compiler rewrite witness is not a theorem"
  let some (_, .const lhs [], .const rhs []) := info.type.eq? | throwError "Invalid compiler rewrite theorem"
  unless lhs == r.source && rhs == r.target do throwError "Compiler rewrite theorem does not match recorded replacement"
  let axioms ← collectAxioms r.theoremName
  unless axioms.toList == [``Quot.sound] do throwError "Unexpected compiler rewrite axioms: {axioms}"
  return obj [("source", str r.source.toString), ("target", str r.target.toString),
    ("theorem", str r.theoremName.toString), ("axioms", toJson (axioms.toList.map Name.toString))]

def auditSource (roots : List Name) : CoreM (Json × Json) := do
  let env ← getEnv
  let records := provenanceExt.getState env
  unless (customPasses env).isEmpty do throwError "custom compiler passes unsupported: {customPasses env}"
  let mut pending := roots
  let mut seen : NameSet := {}
  let mut provenance := #[]
  let mut rewrites : Array Rewrite := #[]
  let mut fuel := 20000
  while !pending.isEmpty do
    if fuel == 0 then throwError "Source closure limit"
    fuel := fuel - 1
    let n := pending.head!
    pending := pending.tail!
    unless seen.contains n do
      seen := seen.insert n
      if (getExternAttrData? env n).isSome then throwError "extern source dependency: {n}"
      if (Lean.Compiler.getImplementedBy? env n).isSome then throwError "implemented_by source dependency: {n}"
      if Lean.hasInitAttr env n then throwError "initialized source dependency: {n}"
      let activeRewrite? := ((Lean.Compiler.CSimp.ext.getState env).map.find? n).map fun e =>
        { source := n, target := e.toDeclName, theoremName := e.thmName : Rewrite }
      for rewrite in activeRewrite?.toList do
        discard <| rewriteJson rewrite
      if let some rawInfo := env.find? (Lean.Compiler.mkUnsafeRecName n) then
        unless (rawInfo matches .defnInfo { safety := .partial, .. }) &&
            (env.find? n matches some (.defnInfo { safety := .safe, .. })) do
          throwError "Unverified recursive implementation override: {n}"
      if let some record := records.find? n then
        unless record.issues.isEmpty do throwError "Source audit rejected {n}: {record.issues}"
        let mut evidence := #[]
        for rewrite in sortRewrites record.rewrites do
          evidence := evidence.push (← rewriteJson rewrite)
          unless rewrites.contains rewrite do rewrites := rewrites.push rewrite
          pending := rewrite.target :: pending
        provenance := provenance.push <| obj [("name", str n.toString),
          ("dependencies", toJson (sortedDependencies record.dependencies)), ("rewrites", .arr evidence)]
        pending := record.dependencies.toList ++ pending
      else if isPrimitiveDefinition n then
        provenance := provenance.push <| obj [("name", str n.toString),
          ("dependencies", .arr #[]), ("rewrites", .arr #[])]
      else if isCompilerSupport n then pure ()
      else
        match env.find? n with
        | some (.ctorInfo _) | some (.recInfo _) | some (.inductInfo _) => pure ()
        | some (.thmInfo _) =>
          let axioms ← collectAxioms n
          unless axioms.isEmpty do throwError "Axiomatic source proof: {n}: {axioms}"
        | _ => throwError "Source dependency lacks fresh compilation provenance: {n}"
  return (.arr (sortByName provenance), .arr (← (sortRewrites rewrites).mapM rewriteJson))

-- Self recursion is supported; a path through a different function back to the
-- starting function is a mutually recursive group and is outside this profile.
def rejectMutualRecursion (graph : List (Name × List Name)) : CoreM Unit := do
  for (start, calls) in graph do
    let mut pending := calls.filter (· != start)
    let mut seen : NameSet := {}
    let mut fuel := 10000
    while !pending.isEmpty do
      if fuel == 0 then throwError "Function graph limit"
      fuel := fuel - 1
      let n := pending.head!
      pending := pending.tail!
      if n == start then throwError "Mutually recursive functions unsupported: {start}"
      unless seen.contains n do
        seen := seen.insert n
        if let some (_, calls) := graph.find? (·.1 == n) then
          pending := calls.filter (· != n) ++ pending

def exportProgram (moduleName : String) (root : Name) : CoreM Json := do
  discard <| auditSource [root]
  let mut pending := [root]
  let mut seen : NameSet := {}
  let mut functions := #[]
  let mut graph := []
  let mut types := []
  let mut fuel := 1000
  while !pending.isEmpty do
    if fuel == 0 then throwError "Function closure limit"
    fuel := fuel - 1
    let n := pending.head!
    pending := pending.tail!
    unless seen.contains n do
      seen := seen.insert n
      let (f, calls, usedTypes) ← exportFunction n
      functions := functions.push f
      graph := (n, calls) :: graph
      types := usedTypes ++ types
      pending := calls ++ pending
  rejectMutualRecursion graph
  let (provenance, rewrites) ← auditSource (graph.map Prod.fst)
  return obj [("module", str moduleName), ("root", str root.toString),
    ("types", ← exportTypes types), ("functions", .arr (sortByName functions)),
    ("claims", .arr #[]), ("rewrites", rewrites), ("provenance", provenance)]

elab "#lean_go_export " output:str root:ident moduleName:str : command => do
  let root ← liftCoreM <| realizeGlobalConstNoOverloadWithInfo root
  let json ← liftCoreM <| exportProgram moduleName.getString root
  IO.FS.writeFile output.getString (json.pretty ++ "\n")
end LeanGo.Module
