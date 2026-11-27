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
levels of constants are omitted.

Names are spelled here and not by `Name.toString`, which leaves some names
unescaped (inaccessible names, names with macro scopes, and names that begin
with `#` or `?`). A component of a constant's name is written as is only when
it is a plain ASCII identifier other than the renderer's own words; any other
component is wrapped in `«»`, with every character outside printable ASCII
written as `\u{…}`. A constant therefore cannot be named so that it reads as
other syntax, as another constant, or as a look-alike of an ASCII name. Binder
names carry no meaning, so an unusual one is replaced by a plain one instead,
and a binder is renamed when it would shadow another binder or read as a
constant. String literals are escaped the same way. -/

/-- Words the renderer writes itself; a name component equal to one is escaped. -/
private def rendererWords : List String := ["Prop", "Sort", "fun", "let"]

private def isPlainChar (c : Char) : Bool :=
  c.isAlphanum || c == '_' || c == '\'' || c == '!' || c == '?'

/-- A plain ASCII identifier: the only kind of name component written as is. -/
private def isPlainComponent (s : String) : Bool :=
  !s.isEmpty && (s.front.isAlpha || s.front == '_') && s.all isPlainChar &&
    !rendererWords.contains s

/-- Printable ASCII is kept (`\` and `"` with a backslash); anything else is `\u{hex}`. -/
private def escaped (s : String) : String :=
  s.foldl (init := "") fun out c =>
    if c == '\\' || c == '"' then (out.push '\\').push c
    else if 32 ≤ c.toNat && c.toNat < 127 then out.push c
    else out ++ "\\u{" ++ String.ofList (Nat.toDigits 16 c.toNat) ++ "}"

private def spellComponent (s : String) : String :=
  if isPlainComponent s then s else "«" ++ escaped s ++ "»"

/-- Spell a constant's name. Numeric components are decimal, which no string
component can be, and the anonymous name is `[anonymous]`. -/
private def spellName : Name → String
  | .anonymous => "[anonymous]"
  | .str .anonymous s => spellComponent s
  | .str p s => spellName p ++ "." ++ spellComponent s
  | .num p i => spellName p ++ "." ++ toString i

/-- The plain name a binder is shown with: its ASCII identifier characters,
with subscript digits as digits, or `x` when that is not a plain identifier. -/
private def binderBase : Name → String
  | .str .anonymous s =>
    let ascii := s.foldl (init := "") fun out c =>
      if isPlainChar c then out.push c
      else if 0x2080 ≤ c.toNat && c.toNat ≤ 0x2089 then out.push (Char.ofNat (c.toNat - 0x2080 + 48))
      else out
    if isPlainComponent ascii && ascii != "_" then ascii else "x"
  | _ => "x"

/-- The first component of every constant used. A binder is never shown with
one of these, so a bound variable cannot read as a constant or its prefix. -/
private def constantRoots (e : Expr) : Std.HashSet String :=
  e.getUsedConstants.foldl (init := {}) fun roots n =>
    match n.getRoot with
    | .str _ root => roots.insert root
    | _ => roots

private def displayName (taken : Std.HashSet String) (scope : Array String) (n : Name) : String := Id.run do
  let base := binderBase n
  let used (s : String) : Bool := scope.contains s || taken.contains s
  if !used base then return base
  let mut i := 1
  while used s!"{base}_{i}" do
    i := i + 1
  return s!"{base}_{i}"

private def isAtom : Expr → Bool
  | .bvar .. | .const .. | .sort .zero | .lit .. => true
  | .mdata _ e => isAtom e
  | .proj .. => true
  | _ => false

private def checkSize (s : String) : Except String String :=
  if s.utf8ByteSize > maxStatementBytes then .error "Expected statement is too large to record" else .ok s

private partial def render (taken : Std.HashSet String) (scope : Array String) (e : Expr) : Except String String := do
  let atom (e : Expr) : Except String String := do
    let s ← render taken scope e
    return if isAtom e then s else "(" ++ s ++ ")"
  checkSize <| ← match e with
  | .bvar i =>
    if h : i < scope.size then pure scope[scope.size - 1 - i] else .error "Expected statement has a loose bound variable"
  | .fvar .. | .mvar .. => .error "Expected statement is not closed"
  | .sort .zero => pure "Prop"
  | .sort u => pure s!"Sort ({u})"
  | .const n _ => pure (spellName n)
  | .lit (.natVal n) => pure (toString n)
  | .lit (.strVal s) => pure ("\"" ++ escaped s ++ "\"")
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
  render (constantRoots e) #[] e

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
  let mut env := base.toKernelEnv
  -- Every module is replayed on top of all earlier ones. The kernel refuses a
  -- name that is already declared, so a later module cannot replace an
  -- earlier declaration; `replay` leaves out a theorem whose name and type
  -- equal those of one already checked (Lean generates the same equation
  -- lemmas for an imported function in every module that needs them), and the
  -- checked one is kept.
  for w in order do
    let mut newConstants : Std.HashMap Name ConstantInfo := {}
    for n in w.data.constNames, ci in w.data.constants do
      if newConstants.contains n || ci.name != n then
        fail s!"Workspace module data is malformed at {n}: {w.name}"
      newConstants := newConstants.insert n ci
    try
      env ← env.replay newConstants
    catch e =>
      fail s!"Kernel rejected a declaration of workspace module {w.name}: {e}"
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
