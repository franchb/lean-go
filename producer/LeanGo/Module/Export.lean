module
public meta import LeanGo.Module.Audit
public meta import LeanGo.Module.Records
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
    unless seen.contains name || name == "UInt32" || name == "UInt64" do
      seen := name :: seen
      let n := name.toName
      discard <| withDeclaration n <| typeKey (.const n [])
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

-- Direct structure fields follow constructor order, including embedded parents.
-- Environment lookup does not run MetaM or consume the compiler name generator.
def exportAPI (types : Json) : CoreM Json := do
  let .arr declarations := types | throwError "Invalid exported type closure"
  let env ← getEnv
  let mut records := []
  for declaration in declarations do
    let .ok name := declaration.getObjValAs? String "name"
      | throwError "Invalid exported type name"
    if let some info := getStructureInfo? env name.toName then
      let .ok ctors := declaration.getObjValAs? (Array Json) "constructors"
        | throwError "Invalid exported constructors: {name}"
      unless ctors.size == 1 do throwError "Structure constructor count mismatch: {name}"
      let .ok fields := ctors[0]!.getObjValAs? (Array String) "fields"
        | throwError "Invalid exported constructor fields: {name}"
      unless info.fieldNames.size == fields.size do
        throwError "Structure field count mismatch: {name}"
      records := records ++ [(name, toJson (info.fieldNames.map Name.toString))]
  return obj [("records", obj records)]

structure FunctionShape where
  domains : List String
  result : String
  allUnused : Bool := false

-- Source-shaped signatures are the public and internal runtime contract. Validate
-- every original owner domain and ADT field before discarding unused parameters.
def resolveFunction (name : Name) : CoreM FunctionShape := do
  validateReducedOwner name
  validateLocalOwner name
  if let some evidence ← localSource? name then
    return { domains := evidence.domains, result := evidence.result }
  if let some evidence ← reducedSource? name then
    let info ← getConstInfo evidence.owner
    let (domains, result) ← signature info.type
    discard <| exportTypes (domains ++ [result])
    let monoDomains ← domains.mapM fun domain => return (← representation domain).mono
    let monoResult := (← representation result).mono
    unless evidence.ownerDecl.params.size == domains.length &&
        (← signature evidence.ownerDecl.type) == (monoDomains, monoResult) do
      throwError "Reduced helper source owner signature mismatch: {name}"
    return {
      domains := evidence.retained.toList.map (domains[·]!), result
      allUnused := evidence.allUnused }
  unless isPrimitiveDefinition name do
    let some record := (provenanceExt.getState (← getEnv)).find? name
      | throwError "Runtime function lacks compilation provenance: {name}"
    unless record.compiled do throwError "Runtime helper lacks its own compilation provenance: {name}"
  let info ← getConstInfo name
  let (domains, result) ← signature info.type
  return { domains, result }

structure EncodingState where
  next : Nat := 1
  seen : List FVarId := []
  nodes : Nat := 0
  calls : List Name := []
  types : List String := []
abbrev EncodeM := StateRefT EncodingState CoreM
abbrev Scope := List (FVarId × Nat × String)
abbrev JoinScope := List (FVarId × Nat × List String)
abbrev Prefix := Array (Json → Json)

def prepend (steps : Prefix) (body : Json) : Json :=
  steps.foldr (fun step body => step body) body

def charge (count : Nat := 1) : EncodeM Unit := do
  let s ← get
  if s.nodes + count > 10000 then throwError "IR traversal limit"
  modify fun s => { s with nodes := s.nodes + count }

def instruction (depth : Nat) : EncodeM Unit := do
  if depth > 256 then throwError "IR traversal limit"
  charge

def lookup (scope : Scope) (f : FVarId) : EncodeM (Nat × String) := do
  for (id, n, ty) in scope do
    if id == f then return (n, ty)
  throwError "Unbound variable {f.name}"

def lookupJoin (joins : JoinScope) (f : FVarId) : EncodeM (Nat × List String) := do
  for (target, id, params) in joins do
    if target == f then return (id, params)
  throwError "Out of scope join point {f.name}"

-- Join IDs, synthetic binders and compiler binders share one allocator.
def freshId : EncodeM Nat := do
  charge
  let s ← get
  set { s with next := s.next + 1 }
  return s.next

def fresh (ty : String) : EncodeM (Nat × Json) := do
  let id ← freshId
  modify fun s => { s with types := ty :: s.types }
  return (id, obj [("id", toJson id), ("type", str ty)])

def reserve (f : FVarId) : EncodeM Unit := do
  let s ← get
  if s.seen.contains f then throwError "Duplicate variable {f.name}"
  set { s with seen := f :: s.seen }

-- An unwrapped compiler parameter maps only to its final mono value.
def associate (scope : Scope) (f : FVarId) (id : Nat) (ty : String) : EncodeM Scope := do
  reserve f
  return (f, id, ty) :: scope

def bind (scope : Scope) (f : FVarId) (ty : String) : EncodeM (Scope × Json) := do
  let (id, binder) ← fresh ty
  return (← associate scope f id ty, binder)

def wrapValue (rep : Representation) (id : Nat) (depth : Nat) : EncodeM (Nat × Prefix) := do
  let mut id := id
  let mut steps := #[]
  for wrapper in rep.wrappers.reverse do
    instruction (depth + steps.size)
    let (nextId, binder) ← fresh wrapper.source
    charge -- Constructor value object, in addition to its instruction and binder.
    let value := obj [("kind", str "constructor"), ("name", str wrapper.ctor.toString),
      ("args", toJson #[id])]
    steps := steps.push fun body => obj [("op", str "let"), ("bind", binder),
      ("value", value), ("next", body)]
    id := nextId
  return (id, steps)

def unwrapValue (rep : Representation) (id : Nat) (result : String) (depth : Nat) :
    EncodeM (Nat × Prefix) := do
  let mut id := id
  let mut steps := #[]
  for wrapper in rep.wrappers do
    instruction (depth + steps.size)
    charge -- The generated case alternative is a structural node too.
    let (nextId, binder) ← fresh wrapper.field
    let discr := id
    steps := steps.push fun body =>
      let alt := obj [("constructor", str wrapper.ctor.toString), ("params", toJson #[binder]),
        ("body", body)]
      obj [("op", str "cases"), ("var", toJson discr), ("type", str wrapper.source),
        ("result", str result), ("alts", toJson #[alt])]
    id := nextId
  return (id, steps)

def sourceBinder (scope : Scope) (f : FVarId) (source : String) (monoType : Expr)
    (result : String) (depth : Nat) : EncodeM (Scope × Json × Prefix) := do
  let rep ← representation source
  unless (← typeKey monoType) == rep.mono do throwError "Source binder type mismatch"
  let (id, binder) ← fresh source
  let (monoId, steps) ← unwrapValue rep id result depth
  return (← associate scope f monoId rep.mono, binder, steps)

partial def encode (code : Code .pure) (scope : Scope) (joins : JoinScope)
    (result : String) (depth : Nat := 1) : EncodeM Json := do
  match code with
  | .return f =>
    let (id, ty) ← lookup scope f
    let rep ← representation result
    unless ty == rep.mono do throwError "Return type mismatch"
    let (id, steps) ← wrapValue rep id depth
    instruction (depth + steps.size)
    return prepend steps <| obj [("op", str "return"), ("var", toJson id)]
  | .let decl next =>
    let ty ← typeKey decl.type
    let literal? := match decl.value with
      | .lit (.uint32 value) => some ("UInt32", "uint32", value.toNat)
      | .lit (.uint64 value) => some ("UInt64", "uint64", value.toNat)
      | _ => none
    if let some (expected, kind, literal) := literal? then
      instruction depth
      unless ty == expected do throwError "Numeric literal type mismatch"
      let (scope, param) ← bind scope decl.fvarId ty
      charge
      let value := obj [("kind", str kind), ("name", str (toString literal)), ("args", .arr #[])]
      return obj [("op", str "let"), ("bind", param), ("value", value),
        ("next", ← encode next scope joins result (depth + 1))]
    let .const name levels args _ := decl.value
      | reject "unsupported_lcnf_value" "Use UInt32/UInt64 literals, admitted primitive calls, first-order functions or constructors."
          m!"Unsupported let value (only UInt32/UInt64 literals, primitive calls, functions and constructors are admitted)"
          (some (valueForm decl.value))
    unless levels.isEmpty do
      reject "unsupported_signature" "Use a monomorphic function without universe arguments."
        m!"Universe arguments unsupported" (some "LCNF.LetValue.const")
    -- A later kernel collision must fail before it can be mistaken for a
    -- constructor or another independently admitted constant kind.
    discard <| reducedEvidence? name
    discard <| localEvidence? name
    let (kind, expected, target, allUnused) ← if let some primitive := numericPrimitive? name then do
      validateNumericPrimitive primitive
      pure ("primitive", (List.replicate primitive.arity primitive.operand.toString), primitive.result.toString, false)
    else do
      if (← getEnv).find? name matches some (.ctorInfo _) then
        let (target, fields) ← constructor name
        pure ("constructor", fields, target, false)
      else
        let shape ← resolveFunction name
        modify fun s => { s with calls := name :: s.calls }
        pure ("call", shape.domains, shape.result, shape.allUnused)
    let args ← if allUnused then do
      unless args == #[.erased] do throwError "Invalid reduced helper erased argument: {name}"
      pure #[]
    else pure args
    let targetRep ← representation target
    unless ty == targetRep.mono && args.size == expected.length do
      throwError "Application signature mismatch: {name}"
    let mut steps := #[]
    let mut ids := #[]
    for i in [:args.size] do
      let .fvar f := args[i]! | throwError "Unsupported argument"
      let (id, actual) ← lookup scope f
      let rep ← representation expected[i]!
      unless actual == rep.mono do throwError "Argument type mismatch"
      let (id, wrapping) ← wrapValue rep id (depth + steps.size)
      steps := steps ++ wrapping
      ids := ids.push id
    let applicationDepth := depth + steps.size
    instruction applicationDepth
    let (id, param) ← fresh target
    let (monoId, suffix) ← unwrapValue targetRep id result (applicationDepth + 1)
    let scope ← associate scope decl.fvarId monoId targetRep.mono
    charge
    let value := obj [("kind", str kind), ("name", str name.toString), ("args", toJson ids)]
    let body ← encode next scope joins result (applicationDepth + 1 + suffix.size)
    return prepend steps <| obj [("op", str "let"), ("bind", param), ("value", value),
      ("next", prepend suffix body)]
  | .jp decl next =>
    instruction depth
    let (expected, target) ← signature decl.type
    let resultRep ← representation result
    unless expected.length == decl.params.size && target == resultRep.mono do
      throwError "Join point signature mismatch"
    if decl.params.size > 64 then throwError "Join point arity limit"
    let id ← freshId -- The join object is a structural node too.
    reserve decl.fvarId
    let mut bodyScope := scope
    let mut params := #[]
    for i in [:decl.params.size] do
      let param := decl.params[i]!
      let ty ← typeKey param.type
      unless ty == expected[i]! do throwError "Join point parameter type mismatch"
      -- These are internal mono values, without source-boundary wrappers.
      let (updated, binder) ← bind bodyScope param.fvarId ty
      bodyScope := updated
      params := params.push binder
    -- The declaration cannot see itself; only the continuation gains its target.
    let body ← encode decl.value bodyScope joins result (depth + 1)
    let next ← encode next scope ((decl.fvarId, id, expected) :: joins) result (depth + 1)
    return obj [("op", str "join"),
      ("join", obj [("id", toJson id), ("params", .arr params), ("body", body)]), ("next", next)]
  | .jmp target args =>
    instruction depth
    let (id, expected) ← lookupJoin joins target
    unless args.size == expected.length do throwError "Jump arity mismatch"
    let mut ids := #[]
    for i in [:args.size] do
      let .fvar f := args[i]! | throwError "Unsupported jump argument"
      let (id, actual) ← lookup scope f
      unless actual == expected[i]! do throwError "Jump argument type mismatch"
      ids := ids.push id
    return obj [("op", str "jump"), ("target", toJson id), ("args", toJson ids)]
  | .cases (.mk name resultType discr alts) =>
    instruction depth
    let ty ← typeKey (.const name [])
    let info ← getConstInfoInduct name
    let ctors := info.ctors
    let (id, actual) ← lookup scope discr
    let resultRep ← representation result
    unless actual == ty && (← typeKey resultType) == resultRep.mono do throwError "Cases type mismatch"
    unless alts.size == ctors.length do throwError "Nonexhaustive alternatives"
    let mut seen : List Name := []
    let mut branches := #[]
    for alt in alts do
      charge
      let .alt ctor params body _ := alt | throwError "Unsupported alternative"
      let (target, fields) ← constructor ctor
      unless target == ty && ctors.contains ctor && !seen.contains ctor && params.size == fields.length do
        throwError "Invalid case constructor"
      seen := ctor :: seen
      let mut branchScope := scope
      let mut binders := #[]
      let mut steps := #[]
      for i in [:params.size] do
        let p := params[i]!
        let (updated, binder, unwrapping) ← sourceBinder branchScope p.fvarId fields[i]! p.type result
          (depth + 1 + steps.size)
        branchScope := updated
        binders := binders.push binder
        steps := steps ++ unwrapping
      let branchBody ← encode body branchScope joins result (depth + 1 + steps.size)
      branches := branches.push (obj [("constructor", str ctor.toString), ("params", .arr binders),
        ("body", prepend steps branchBody)])
    return obj [("op", str "cases"), ("var", toJson id), ("type", str ty),
      ("result", str result), ("alts", .arr branches)]
  | _ =>
    reject "unsupported_lcnf_control" "Use first-order calls, exhaustive matches and local joins; retained closures are outside this profile."
      m!"Unsupported control flow (only return, let, exhaustive cases and local joins are admitted)"
      (some (controlForm code))

def exportFunction (name : Name) : CoreM (Json × List Name × List String) := withDeclaration name do
  let shape ← resolveFunction name
  let domains := shape.domains
  let result := shape.result
  let monoDomains ← domains.mapM fun domain => return (← representation domain).mono
  let monoResult := (← representation result).mono
  let some decl ← getMonoDecl? name | throwError "Missing pure-mono declaration: {name}"
  unless decl.safe && decl.levelParams.isEmpty do
    throwError "Function signature/safety mismatch: {name}"
  if shape.allUnused then
    let .forallE _ domain resultType _ := decl.type
      | throwError "Invalid reduced helper dummy signature: {name}"
    unless domains.isEmpty && decl.params.size == 1 && domain == .const ``lcVoid [] &&
        decl.params[0]!.type == domain && (← typeKey resultType) == monoResult do
      throwError "Invalid reduced helper dummy signature: {name}"
  else
    unless decl.params.size == domains.length &&
        (← signature decl.type) == (monoDomains, monoResult) do
      throwError "Function signature/safety mismatch: {name}"
  let .code body := decl.value | throwError "External function body: {name}"
  let ((params, body), state) ← (do
    let mut scope := []
    let mut params := #[]
    let mut steps := #[]
    if shape.allUnused then
      -- Reserve the compiler dummy without giving it a runtime value. Any use in
      -- the body is an unbound-variable error, including self-recursive operands.
      reserve decl.params[0]!.fvarId
    else
      for i in [:decl.params.size] do
        let p := decl.params[i]!
        let (updated, param, unwrapping) ← sourceBinder scope p.fvarId domains[i]! p.type result
          (1 + steps.size)
        scope := updated
        params := params.push param
        steps := steps ++ unwrapping
    let body ← encode body scope [] result (1 + steps.size)
    pure (params, prepend steps body) : EncodeM (Array Json × Json)).run {}
  return (obj [("name", str name.toString), ("params", .arr params),
    ("result", str result), ("body", body)], state.calls, result :: state.types)

def rewriteJson (r : Rewrite) : CoreM Json := do
  unless r.source == ``Bool.not && r.target == ``Bool.Internal.not &&
      r.theoremName == ``Bool.not_eq_internalNot do
    withDeclaration r.source do
      reject "source_audit" "Remove the custom csimp rewrite from the selected root's dependency closure."
        m!"Unsupported historical compiler rewrite: {r.source} -> {r.target} via {r.theoremName}"
        (some "csimp")
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
  unless (customPasses env).isEmpty do
    reject "source_audit" "Remove custom compiler passes from the source environment."
      m!"custom compiler passes unsupported: {customPasses env}"
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
      let (newPending, newProvenance, newRewrites) ← withDeclaration n do
        let mut pending := pending
        let mut provenance := provenance
        let mut rewrites := rewrites
        validateReducedOwner n
        validateLocalOwner n
        if isNumericSourceBoundary n then
          let issues := numericBoundaryIssues env n
          unless issues.isEmpty do reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"Numeric source audit rejected: {issues}"
        else if (getExternAttrData? env n).isSome then reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"extern source dependency: {n}"
        if (Lean.Compiler.getImplementedBy? env n).isSome then reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"implemented_by source dependency: {n}"
        if Lean.hasInitAttr env n then reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"initialized source dependency: {n}"
        let activeRewrite? := ((Lean.Compiler.CSimp.ext.getState env).map.find? n).map fun e =>
          { source := n, target := e.toDeclName, theoremName := e.thmName : Rewrite }
        for rewrite in activeRewrite?.toList do
          discard <| rewriteJson rewrite
        if let some rawInfo := env.find? (Lean.Compiler.mkUnsafeRecName n) then
          unless (rawInfo matches .defnInfo { safety := .partial, .. }) &&
              (env.find? n matches some (.defnInfo { safety := .safe, .. })) do
            reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"Unverified recursive implementation override: {n}"
        if let some evidence ← localSource? n then
          provenance := provenance.push <| obj [("name", str n.toString),
            ("dependencies", toJson #[evidence.sourceOwner.toString]), ("rewrites", .arr #[])]
          pending := evidence.sourceOwner :: pending
        else if let some evidence ← reducedSource? n then
          provenance := provenance.push <| obj [("name", str n.toString),
            ("dependencies", toJson #[evidence.owner.toString]), ("rewrites", .arr #[])]
          pending := evidence.owner :: pending
        else if let some record := records.find? n then
          unless record.issues.isEmpty do withDeclaration (record.issueOrigin.getD n) do reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"Source audit rejected {n}: {record.issues}"
          let mut evidence := #[]
          for rewrite in sortRewrites record.rewrites do
            evidence := evidence.push (← rewriteJson rewrite)
            unless rewrites.contains rewrite do rewrites := rewrites.push rewrite
            pending := rewrite.target :: pending
          provenance := provenance.push <| obj [("name", str n.toString),
            ("dependencies", toJson (sortedDependencies record.dependencies)), ("rewrites", .arr evidence)]
          pending := record.dependencies.toList ++ pending
        else if isPrimitiveDefinition n || isNumericSourceBoundary n then
          provenance := provenance.push <| obj [("name", str n.toString),
            ("dependencies", .arr #[]), ("rewrites", .arr #[])]
        else if isCompilerSupport n then pure ()
        else
          match env.find? n with
          | some (.ctorInfo _) | some (.recInfo _) | some (.inductInfo _) => pure ()
          | some (.thmInfo _) =>
            let axioms ← collectAxioms n
            unless axioms.isEmpty do reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"Axiomatic source proof: {n}: {axioms}"
          | _ => reject "source_audit" "Remove the unsupported source dependency or select a root whose dependency closure stays within the admitted pure profile." m!"Source dependency lacks fresh compilation provenance: {n}"
        return (pending, provenance, rewrites)
      pending := newPending
      provenance := newProvenance
      rewrites := newRewrites
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

def exportProgramRoots (moduleName : String) (requested : Array Name) : CoreM Json := do
  unless requested.size > 0 && requested.size <= 64 do
    throwError "Expected between 1 and 64 roots"
  let roots := requested.qsort (fun a b => a.toString < b.toString)
  let mut selected : NameSet := {}
  for root in roots do
    if selected.contains root then throwError "Duplicate root: {root}"
    selected := selected.insert root
  discard <| auditSource roots.toList
  let mut pending := roots.toList
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
  let exportedTypes ← exportTypes types
  let api ← exportAPI exportedTypes
  let selection := if roots.size == 1 then ("root", str roots[0]!.toString)
    else ("roots", toJson (roots.map Name.toString))
  return obj [("module", str moduleName), selection,
    ("types", exportedTypes), ("api", api), ("functions", .arr (sortByName functions)),
    ("claims", .arr #[]), ("rewrites", rewrites), ("provenance", provenance)]

def exportProgram (moduleName : String) (root : Name) : CoreM Json :=
  exportProgramRoots moduleName #[root]

-- Ranges are available after imports. Generated helper ownership comes only
-- from validated compiler evidence; a suffix is never a source-location guess.
def failureLocation (extra : KVMap) : CoreM Json := do
  let name := extra.getName `declaration
  if name.isAnonymous then return .null
  let location ← declarationLocation name "declaration"
  if location != .null then return location
  try
    if let some evidence ← localSource? name then
      return ← declarationLocation evidence.sourceOwner "source_owner"
    if let some evidence ← reducedSource? name then
      return ← declarationLocation evidence.owner "source_owner"
  catch _ => pure ()
  return .null

def reportAdmission (action : CoreM Json) : CoreM Json := do
  try action catch error =>
    match error with
    | .internal id extra =>
      if id == admissionException then
        let json := diagnosticJson extra (← failureLocation extra)
        IO.FS.writeFile "lean-go-diagnostic.json" (json.compress ++ "\n")
        throwError "{extra.getString `message}"
      throw error
    | _ => throw error

elab "#lean_go_export " output:str root:ident moduleName:str : command => do
  let root ← liftCoreM <| realizeGlobalConstNoOverloadWithInfo root
  let json ← liftCoreM <| reportAdmission (exportProgram moduleName.getString root)
  IO.FS.writeFile output.getString (json.pretty ++ "\n")

elab "#lean_go_export_roots " output:str "[" roots:ident,* "]" moduleName:str : command => do
  let mut names := #[]
  for root in roots.getElems do
    names := names.push (← liftCoreM <| realizeGlobalConstNoOverloadWithInfo root)
  let json ← liftCoreM <| reportAdmission (exportProgramRoots moduleName.getString names)
  IO.FS.writeFile output.getString (json.pretty ++ "\n")
end LeanGo.Module
