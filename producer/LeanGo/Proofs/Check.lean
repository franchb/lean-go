import Lean

/-!
# The lean-go proof checker

This program decides a proof contract. `lean-go check-proofs` runs it as
`lean --run Check.lean LIB MODULE... -- THEOREM EXPECTED...` from a directory
outside the proof workspace, after the workspace has been built.

The program never elaborates or imports a user module and never runs user
code. It reads the workspace `.olean` files under `LIB` as data, builds a
kernel environment from the pinned toolchain's own `.olean` files without
loading environment extensions (so no initializer, attribute handler,
delaborator or unification hint of any module runs or is consulted), and
replays every workspace declaration through the kernel
(`Lean.Kernel.Environment.replay`), module by module in import order. Each
claim is then decided by the kernel: a new theorem whose type is the
expected proposition and whose value is the claimed theorem must be accepted.
Axioms are collected from the replayed kernel environment.

The result is one JSON array on standard output; any failure is reported on
standard error with exit code 1.
-/

open Lean

namespace LeanGo.Proofs

/-- The axioms a checked theorem may depend on. -/
def approvedAxioms : List Name := [``propext, ``Classical.choice, ``Quot.sound]

/-- Rendered statements larger than this are refused rather than recorded. -/
def maxStatementBytes : Nat := 256 * 1024

/-! ## Statement rendering

The recorded statement is rendered by this function alone, from the kernel
term of the expected proposition. It does not use Lean's pretty printer, so no
notation, unexpander, delaborator or option declared by any module affects it.
Every argument is shown, implicit and instance arguments included; universe
levels of constants are omitted. Binder names are renamed when they would
shadow another binder or read as a constant. -/

private def constantSpellings (e : Expr) : NameSet :=
  e.getUsedConstants.foldl (init := {}) fun s n =>
    (n.components.foldl (init := (s.insert n, Name.anonymous)) fun (s, p) c =>
      let p := p ++ c
      (s.insert p, p)).1

private def displayName (taken : NameSet) (scope : Array String) (n : Name) : String := Id.run do
  let base := match n.eraseMacroScopes with
    | .str .anonymous s => if n.hasMacroScopes || s.isEmpty || s == "_" then "x" else s
    | _ => "x"
  let spelled (s : String) : Bool :=
    scope.contains s || taken.contains (.str .anonymous s) || s == "fun" || s == "let"
  let first := (Name.str .anonymous base).toString
  if !spelled first then return first
  let mut i := 1
  while spelled ((Name.str .anonymous s!"{base}_{i}").toString) do
    i := i + 1
  return (Name.str .anonymous s!"{base}_{i}").toString

private def isAtom : Expr → Bool
  | .bvar .. | .const .. | .sort .zero | .lit .. => true
  | .mdata _ e => isAtom e
  | .proj .. => true
  | _ => false

private def checkSize (s : String) : Except String String :=
  if s.utf8ByteSize > maxStatementBytes then .error "Expected statement is too large to record" else .ok s

private partial def render (taken : NameSet) (scope : Array String) (e : Expr) : Except String String := do
  let atom (e : Expr) : Except String String := do
    let s ← render taken scope e
    return if isAtom e then s else "(" ++ s ++ ")"
  checkSize <| ← match e with
  | .bvar i =>
    if h : i < scope.size then pure scope[scope.size - 1 - i] else .error "Expected statement has a loose bound variable"
  | .fvar .. | .mvar .. => .error "Expected statement is not closed"
  | .sort .zero => pure "Prop"
  | .sort u => pure s!"Sort ({u})"
  | .const n _ => pure n.toString
  | .lit (.natVal n) => pure (toString n)
  | .lit (.strVal s) => pure s.quote
  | .mdata _ b => render taken scope b
  | .proj _ i b => do return (← atom b) ++ "." ++ toString (i + 1)
  | .app .. => do
    let mut out ← atom e.getAppFn
    for arg in e.getAppArgs do
      out := out ++ " " ++ (← atom arg)
      discard <| checkSize out
    return out
  | .lam n t b _ => do
    let x := displayName taken scope n
    return s!"fun ({x} : {← render taken scope t}) => {← render taken (scope.push x) b}"
  | .forallE n t b _ =>
    if b.hasLooseBVar 0 then do
      -- Consecutive dependent binders share one `∀`.
      let mut binders := ""
      let mut scope := scope
      let mut e := e
      while true do
        match e with
        | .forallE n t b _ =>
          if !b.hasLooseBVar 0 then break
          let x := displayName taken scope n
          binders := binders ++ s!" ({x} : {← render taken scope t})"
          discard <| checkSize binders
          scope := scope.push x
          e := b
        | _ => break
      return s!"∀{binders}, {← render taken scope e}"
    else do
      let domain ← render taken scope t
      let domain := if t.isForall || t.isLambda || t.isLet then "(" ++ domain ++ ")" else domain
      return s!"{domain} → {← render taken (scope.push "_") b}"
  | .letE n t v b _ => do
    let x := displayName taken scope n
    return s!"let {x} : {← render taken scope t} := {← render taken scope v}; {← render taken (scope.push x) b}"

/-- Render a closed proposition with the trusted renderer above. -/
def renderStatement (e : Expr) : Except String String :=
  render (constantSpellings e) #[] e

/-! ## Loading workspace modules as data -/

structure WorkspaceModule where
  name : Name
  data : ModuleData

private def fail (message : String) : IO α := throw (IO.userError message)

/-- Read one workspace module's `.olean` as data, refusing the module system. -/
def readWorkspaceModule (lib : System.FilePath) (name : Name) : IO WorkspaceModule := do
  let file := modToFilePath lib name "olean"
  unless ← file.pathExists do
    fail s!"Workspace module was not built: {name}"
  let parts ← readModuleDataParts #[file]
  let some (data, _) := parts[0]? | fail s!"Workspace module could not be read: {name}"
  if data.isModule then
    fail s!"Workspace module uses the Lean module system, which check-proofs does not support; remove `module` from {name}"
  return { name, data }

/-- Workspace modules in import order; every other import must be a toolchain module. -/
def importOrder (mods : Array WorkspaceModule) (isToolchain : Name → IO Bool) :
    IO (Array WorkspaceModule) := do
  let byName : Std.HashMap Name WorkspaceModule := mods.foldl (init := {}) fun m w => m.insert w.name w
  for w in mods do
    for i in w.data.imports do
      unless byName.contains i.module || (← isToolchain i.module) do
        fail s!"Workspace module {w.name} imports {i.module}, which is neither a workspace nor a toolchain module"
  let rec visit (fuel : Nat) (stack : List Name) (done : Std.HashSet Name) (out : Array WorkspaceModule)
      (w : WorkspaceModule) : IO (Std.HashSet Name × Array WorkspaceModule) := do
    if done.contains w.name then return (done, out)
    if stack.contains w.name then fail s!"Workspace import cycle through {w.name}"
    match fuel with
    | 0 => fail "Workspace import graph is too deep"
    | fuel + 1 =>
      let mut state := (done, out)
      for i in w.data.imports do
        if let some dep := byName[i.module]? then
          state ← visit fuel (w.name :: stack) state.1 state.2 dep
      return (state.1.insert w.name, state.2.push w)
  let mut state : Std.HashSet Name × Array WorkspaceModule := ({}, #[])
  for w in mods do
    state ← visit (mods.size + 1) [] state.1 state.2 w
  return state.2

/-! ## Replay and claims -/

/-- Toolchain modules transitively imported by `roots`, from the base environment header. -/
private partial def toolchainClosure (imports : Std.HashMap Name (Array Name)) (roots : Array Name) : NameSet := Id.run do
  let mut seen : NameSet := {}
  let mut todo := roots.toList
  while true do
    match todo with
    | [] => break
    | m :: rest =>
      todo := rest
      unless seen.contains m do
        seen := seen.insert m
        todo := (imports.getD m #[]).toList ++ todo
  return seen

/-- Collect the axioms a constant transitively depends on in the replayed environment. -/
partial def collectAxioms (env : Kernel.Environment) (roots : List Name) : IO NameSet := do
  let mut seen : NameSet := {}
  let mut axioms : NameSet := {}
  let mut todo := roots
  while true do
    match todo with
    | [] => break
    | c :: rest =>
      todo := rest
      if seen.contains c then continue
      seen := seen.insert c
      let some info := env.find? c | fail s!"Unknown constant while collecting axioms: {c}"
      if info matches .axiomInfo _ then axioms := axioms.insert c
      todo := info.getUsedConstantsAsSet.toList ++ todo
      if let .inductInfo v := info then todo := v.ctors ++ todo
  return axioms

private def claimJson (name expected : Name) (statement : String) (axioms : NameSet) : Json :=
  let ordered := (axioms.toList.map Name.toString).toArray.qsort (· < ·)
  Json.mkObj [("name", toJson name.toString), ("expected", toJson expected.toString),
    ("statement", toJson statement), ("axioms", toJson ordered)]

/-- Decide one claim in the replayed kernel environment. -/
def checkClaim (env : Kernel.Environment) (index : Nat) (name expected : Name) : IO Json := do
  let some (.thmInfo claim) := env.find? name
    | fail s!"Claim is not a theorem: {name}"
  unless claim.levelParams.isEmpty do
    fail s!"Polymorphic theorem is unsupported: {name}"
  let some (.defnInfo specification) := env.find? expected
    | fail s!"Expected statement is not a definition: {expected}"
  unless specification.levelParams.isEmpty && specification.type == .sort .zero &&
      !specification.value.hasFVar && !specification.value.hasMVar &&
      !specification.value.hasLooseBVars && !specification.value.hasLevelParam do
    fail s!"Expected statement must be a closed nonpolymorphic Prop definition: {expected}"
  let checkName := Name.mkNum `_lean_go_proof_check index
  if (env.find? checkName).isSome then
    fail s!"Reserved checker name is already declared: {checkName}"
  -- The kernel decides that the claimed theorem proves the expected proposition.
  let decl := Declaration.thmDecl
    { name := checkName, levelParams := [], type := specification.value, value := .const name [] }
  let env ← match env.addDeclCore 0 0 decl none with
    | .ok env => pure env
    | .error _ => fail s!"Theorem type changed: {name}"
  let axioms ← collectAxioms env [checkName]
  for axiomName in axioms do
    unless approvedAxioms.contains axiomName do
      fail s!"Unapproved theorem axiom: {name}: {axiomName}"
  let statement ← match renderStatement specification.value with
    | .ok s => pure s
    | .error e => fail s!"{e}: {expected}"
  return claimJson name expected statement axioms

/-- Replay all workspace modules and decide the claims. -/
def check (lib : System.FilePath) (moduleNames : Array Name) (claims : Array (Name × Name)) : IO Json := do
  let toolchainLib ← getLibDir (← getBuildDir)
  -- Toolchain modules resolve in the toolchain alone; workspace files are read by explicit path.
  searchPathRef.set [toolchainLib]
  let isToolchainRoot (n : Name) : IO Bool := do
    let root := n.getRoot.toString (escape := false)
    return (← (toolchainLib / root).isDir) || (← (toolchainLib / (root ++ ".olean")).pathExists)
  let isToolchain (n : Name) : IO Bool := do
    return (← isToolchainRoot n) && (← (modToFilePath toolchainLib n "olean").pathExists)
  for m in moduleNames do
    if ← isToolchainRoot m then
      fail s!"Workspace module name collides with the toolchain: {m}"
  let mods ← moduleNames.mapM (readWorkspaceModule lib)
  let order ← importOrder mods isToolchain
  let workspace : NameSet := moduleNames.foldl (init := {}) NameSet.insert
  let mut baseImports : Array Import := #[{ module := `Init }]
  for w in order do
    for i in w.data.imports do
      unless workspace.contains i.module || baseImports.any (·.module == i.module) do
        baseImports := baseImports.push { module := i.module }
  -- `loadExts := false`: no extension state is loaded and no initializer runs.
  let base ← importModules baseImports {} (loadExts := false)
  let header := base.header
  let mut toolchainImports : Std.HashMap Name (Array Name) := {}
  for h : i in [:header.moduleNames.size] do
    let imports := (header.moduleData[i]?.map (·.imports.map (·.module))).getD #[]
    toolchainImports := toolchainImports.insert header.moduleNames[i] imports
  let mut env := base.toKernelEnv
  -- Where each replayed workspace constant came from, and each module's workspace closure.
  let mut owner : Std.HashMap Name Name := {}
  let mut closure : Std.HashMap Name NameSet := {}
  for w in order do
    let mut visible : NameSet := ({} : NameSet).insert w.name
    let mut toolchainRoots : Array Name := #[]
    for i in w.data.imports do
      if let some c := closure[i.module]? then
        visible := c.foldl (init := visible) NameSet.insert
      else
        toolchainRoots := toolchainRoots.push i.module
    for m in visible.toList do
      if let some wm := order.find? (·.name == m) then
        for i in wm.data.imports do
          unless workspace.contains i.module do
            toolchainRoots := toolchainRoots.push i.module
    let toolchainVisible := toolchainClosure toolchainImports toolchainRoots
    closure := closure.insert w.name visible
    let mut newConstants : Std.HashMap Name ConstantInfo := {}
    for n in w.data.constNames, ci in w.data.constants do
      if newConstants.contains n || ci.name != n then
        fail s!"Workspace module data is malformed at {n}: {w.name}"
      newConstants := newConstants.insert n ci
    -- A module may only refer to its own constants and those of modules it imports.
    for (n, ci) in newConstants.toList do
      for used in ci.getUsedConstantsAsSet.toList do
        let ok := newConstants.contains used ||
          (match owner[used]? with
            | some m => visible.contains m
            | none => match base.getModuleIdxFor? used with
              | some idx => (header.moduleNames[idx.toNat]?.map toolchainVisible.contains).getD false
              | none => false)
        unless ok do
          fail s!"Declaration {n} of {w.name} refers to {used}, which {w.name} does not import"
    try
      env ← env.replay newConstants
    catch e =>
      fail s!"Kernel rejected a declaration of workspace module {w.name}: {e}"
    for n in newConstants.keys do
      owner := owner.insert n w.name
  let mut results := #[]
  for h : i in [:claims.size] do
    let (name, expected) := claims[i]
    results := results.push (← checkClaim env i name expected)
  return toJson results

end LeanGo.Proofs

/-- `lean --run Check.lean LIB MODULE... -- THEOREM EXPECTED ...` -/
def main (args : List String) : IO UInt32 := do
  try
    let lib :: rest := args | throw (IO.userError "usage: Check.lean LIB MODULE... -- THEOREM EXPECTED...")
    let (modules, claimArgs) := rest.span (· != "--")
    let claimArgs := claimArgs.drop 1
    if modules.isEmpty || claimArgs.isEmpty || claimArgs.length % 2 != 0 then
      throw (IO.userError "usage: Check.lean LIB MODULE... -- THEOREM EXPECTED...")
    let rec pairs : List String → List (Name × Name)
      | a :: b :: rest => (a.toName, b.toName) :: pairs rest
      | _ => []
    let result ← LeanGo.Proofs.check lib (modules.map String.toName).toArray (pairs claimArgs).toArray
    IO.println result.pretty
    return 0
  catch e =>
    IO.eprintln s!"{e}"
    return 1
