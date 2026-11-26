module
public meta import LeanGo.Module.Export
public meta import LeanGo.Validate.Meta
public meta import LeanGo.Validate.Meta.Axioms
public import LeanGo.Validate
public meta section

/-!
# `#lean_go_validate`: translation validation of an export, in the producer

```
#lean_go_export_roots "ir.json" [R₁, …] "Module"
#lean_go_validate "ir.json" [R₁, …] "Module" report "validation.json"
  heartbeats 200000 timeout_ms 120000 strict
```

run in the environment where the producer exported the roots. The command

1. recomputes the producer's JSON value (`exportProgramRoots`, the function
   the export command writes) and checks that the IR file is byte-identical
   to it, then builds the `Program` term from that value
   (`LeanGo.Validate.Meta.elabProgramJson`);
2. derives `Encode`/`IRType` instances for every table type
   (`derive_ir_encoding`), in field-dependency order;
3. pairs every IR function with a specification, using only the producer's
   own validated evidence (`reducedEvidence?`, `localEvidence?`, the byte-fold
   provenance record) and never a name pattern:
   * source declaration or byte-fold root `F`: the constant `F`;
   * reduced helper `O._redArg` with retained positions `r`: the owner `O`
     applied to the retained parameters, removed parameters fixed to an
     arbitrary inhabitant;
   * lifted local helper: the source lambda of the owner's `let`, with the
     captured variables (in the producer's capture order) as leading
     parameters;
   * mono-typed boundaries (a helper parameter of type `UInt32` where the
     source has a single-field wrapper `Count`): adapted through the
     producer's own `representation` wrapper chain;
4. states `Callable.Rel Eq (evalBody (Spec.env spec) prog.«F») spec_F` for
   each function and proves it with `validate_obl` under a fixed heartbeat and
   wall-clock budget; a proof is added with `addDecl` (kernel-checked) only if
   it is complete, has no `sorry`, and uses only the standard axioms;
5. for each root whose call closure `S` is entirely proved, proves
   `PreFixedOn prog (Spec.env spec) S`, the whole-program theorem
   `∀ n, Callable.Rel Approx (prog.run n "R") R` (`run_rel_on`) and its
   explicit form
   `∀ n a₁ … aₖ v, prog.run n "R" [encode a₁, …] = some v → v = encode (R a₁ … aₖ)`;
6. writes a JSON report. Nothing here fails the command unless `strict` is
   given and some function is not proved.

Generated declarations live under `LeanGo.Validated` (and the derived
instances under the source types' namespaces). The specifications of
non-root functions are never trusted: the whole-program theorem is about the
root's own source constant, and the pre-fixed-point argument is checked by
the kernel for whatever helper specifications were chosen.
-/

open Lean Elab Command Meta Term

namespace LeanGo.Validate.Command

/-- Default namespace of the generated declarations. -/
def defaultNamespace : Name := `LeanGo.Validated

def V (n : Name) : Name := `LeanGo.Validate ++ n

/-! ## Budgets and exception capture -/

structure Budget where
  /-- Heartbeats in thousands, as `maxHeartbeats`. -/
  heartbeats : Nat
  timeoutMs : Nat

/-- Catch every exception, including heartbeat exhaustion and interrupts. -/
def catchAll {α : Type} (x : CoreM α) : CoreM (Except Exception α) :=
  fun ctx s => do
    try
      let a ← x ctx s
      pure (Except.ok a)
    catch e => pure (Except.error e)

/-- Run `x` with the budget: heartbeats from now, and a cancellation token
set by a timer thread. Returns the result, the wall-clock time in
milliseconds and the heartbeats used (in thousands). -/
def withBudget {α : Type} (b : Budget) (x : CoreM α) : CoreM (Except Exception α × Nat × Nat) := do
  let tk ← IO.CancelToken.new
  let timer ← IO.asTask (prio := .dedicated) do
    IO.sleep b.timeoutMs.toUInt32
    tk.set
  let start ← IO.monoMsNow
  let hb ← IO.getNumHeartbeats
  let r ← catchAll <| withReader (fun ctx => { ctx with maxHeartbeats := b.heartbeats * 1000, initHeartbeats := hb, cancelTk? := some tk }) x
  IO.cancel timer
  let stop ← IO.monoMsNow
  let hb' ← IO.getNumHeartbeats
  return (r, stop - start, (hb' - hb) / 1000)

def runTerm {α : Type} (x : TermElabM α) : CoreM α :=
  (Term.withoutErrToSorry x).run' {} {} |>.run' {} {}

def truncate (s : String) (n : Nat := 4000) : String :=
  if s.length ≤ n then s else (s.take n).toString ++ " …"

def exceptionText (e : Exception) : CoreM String := do
  let msg ← e.toMessageData.toString
  if e.isInterrupt then return "wall-clock budget exhausted"
  if e.isMaxHeartbeat then return "heartbeat budget exhausted"
  return truncate msg

/-- Run a command step and report the error messages it logged or threw;
the command's own message log is left unchanged. -/
def capture {α : Type} (x : CommandElabM α) : CommandElabM (Except String α) := do
  let saved := (← get).messages
  modify fun s => { s with messages := {} }
  let r ← try (Except.ok <$> x) catch e => pure (Except.error (← e.toMessageData.toString))
  let msgs := (← get).messages
  modify fun s => { s with messages := saved }
  let errs ← (msgs.toList.filter (·.severity == .error)).mapM (·.data.toString)
  match r with
  | .ok a => if errs.isEmpty then return .ok a else return .error (truncate (" ".intercalate errs))
  | .error e => return .error (truncate (" ".intercalate (e :: errs)))

/-- The axioms a declaration depends on; only the standard three are accepted. -/
def axiomsOf (n : Name) : CoreM (Array Name) := collectAxioms n

def standardAxioms (axs : Array Name) : Bool :=
  axs.all (LeanGo.Validate.Meta.allowedAxioms.contains ·)

/-- Prove `type` by `tac` within the budget and add it as theorem `name`.
The declaration is added only if the proof is complete, closed, free of
`sorry`, accepted by the kernel and uses only the standard axioms. -/
def proveTheorem (name : Name) (type : Expr) (tac : Syntax) (b : Budget) :
    CoreM (Except String (Array Name) × Nat × Nat) := do
  let saved := (← get).messages
  let (r, ms, hb) ← withBudget b <| runTerm do
    let mvar ← mkFreshExprMVar type (kind := .syntheticOpaque)
    let (rest, _) ← Lean.Elab.runTactic mvar.mvarId! tac
    unless rest.isEmpty do throwError "unsolved goals remain"
    let pf ← instantiateMVars mvar
    if pf.hasMVar then throwError "the proof has metavariables"
    if pf.hasSorry then throwError "the proof contains sorry"
    addDecl (.thmDecl { name, levelParams := [], type, value := pf })
    enableRealizationsForConst name
    let axs ← axiomsOf name
    return axs
  modify fun s => { s with messages := saved }
  match r with
  | .ok axs =>
    if standardAxioms axs then return (.ok axs, ms, hb)
    else return (.error s!"non-standard axioms {axs}", ms, hb)
  | .error e => return (.error (← exceptionText e), ms, hb)

/-! ## The IR value -/

structure IRFunction where
  name : String
  params : Array String
  result : String
  calls : Array String
  deriving Inhabited

partial def jsonCalls (j : Json) : Array String :=
  let op := (j.getObjValAs? String "op").toOption.getD ""
  let get (k : String) := (j.getObjVal? k).toOption.getD .null
  match op with
  | "let" =>
    let v := get "value"
    let here := if (v.getObjValAs? String "kind").toOption == some "call" then
      #[(v.getObjValAs? String "name").toOption.getD ""] else #[]
    here ++ jsonCalls (get "next")
  | "join" => jsonCalls ((get "join").getObjVal? "body" |>.toOption.getD .null) ++ jsonCalls (get "next")
  | "cases" =>
    match get "alts" with
    | .arr alts => alts.foldl (fun acc a => acc ++ jsonCalls ((a.getObjVal? "body").toOption.getD .null)) #[]
    | _ => #[]
  | _ => #[]

def irFunctions (json : Json) : Except String (Array IRFunction) := do
  let .arr fns ← json.getObjVal? "functions" | throw "functions is not an array"
  fns.mapM fun f => do
    let name ← f.getObjValAs? String "name"
    let .arr ps ← f.getObjVal? "params" | throw "params is not an array"
    let params ← ps.mapM (·.getObjValAs? String "type")
    let result ← f.getObjValAs? String "result"
    let body ← f.getObjVal? "body"
    return { name, params, result, calls := jsonCalls body }

/-- Table types and, per type, the table types its constructor fields use. -/
def irTypes (json : Json) : Except String (Array (String × Array String)) := do
  let .arr tys ← json.getObjVal? "types" | throw "types is not an array"
  tys.mapM fun t => do
    let name ← t.getObjValAs? String "name"
    let .arr cs ← t.getObjVal? "constructors" | throw "constructors is not an array"
    let mut deps := #[]
    for c in cs do
      for f in ← c.getObjValAs? (Array String) "fields" do
        unless deps.contains f || f == name do deps := deps.push f
    return (name, deps)

/-- Call closure of a root, from the IR bodies. -/
def closure (fns : Array IRFunction) (root : String) : Array String := Id.run do
  let mut seen := #[]
  let mut pending := [root]
  for _ in [:10000] do
    match pending with
    | [] => break
    | f :: rest =>
      pending := rest
      unless seen.contains f do
        seen := seen.push f
        if let some fn := fns.find? (·.name == f) then pending := fn.calls.toList ++ pending
  return seen

/-! ## Encodings -/

def libraryType (n : String) : Bool :=
  n == "Bool" || n == "List UInt8" || n == "UInt8" || n == "UInt16" || n == "UInt32" || n == "UInt64"

/-- Derive encodings in dependency order. Returns, per table type, `none`
(available) or the reason it is not. -/
partial def deriveEncodings (tys : Array (String × Array String)) :
    CommandElabM (Array (String × Option String)) := do
  let done ← IO.mkRef (#[] : Array (String × Option String))
  let rec visit (n : String) (stack : List String) : CommandElabM (Option String) := do
    if let some (_, r) := (← done.get).find? (·.1 == n) then return r
    if libraryType n then return none
    if stack.contains n then return some s!"mutually recursive types through {n}"
    let some (_, deps) := tys.find? (·.1 == n) | return some s!"type {n} is not in the table"
    let mut failed : Option String := none
    for d in deps do
      if let some r ← visit d (n :: stack) then
        failed := some s!"field type {d}: {r}"
        break
    let result ← match failed with
      | some r => pure (some r)
      | none =>
        let T := n.toName
        match (← getEnv).find? T with
        | some (.inductInfo _) =>
          match ← capture (LeanGo.Validate.Meta.deriveOne T) with
          | .ok () => pure none
          | .error e => pure (some s!"derive_ir_encoding failed: {e}")
        | _ => pure (some s!"no source inductive named {n}")
    done.modify (·.push (n, result))
    return result
  for (n, _) in tys do discard <| visit n []
  return (← done.get)

/-! ## Specifications -/

/-- IR name of a source type (as the extractor spells it). -/
def irName (e : Expr) : MetaM (Option String) := do
  try return some (← LeanGo.Validate.Meta.irTypeName e) catch _ => return none

/-- An arbitrary closed inhabitant (only used for arguments the compiler
proved unused). -/
partial def inhabitant (ty : Expr) (depth : Nat := 6) : MetaM (Option Expr) := do
  if depth == 0 then return none
  let inst? ← try some <$> synthInstance (mkApp (mkConst ``Inhabited [1]) ty) catch _ => pure none
  if let some inst := inst? then
    return some (mkApp2 (mkConst ``Inhabited.default [1]) ty inst)
  let .const n _ := (← whnf ty).getAppFn | return none
  let some (.inductInfo info) := (← getEnv).find? n | return none
  for c in info.ctors do
    let r ← forallTelescope (← inferType (mkConst c)) fun fs _ => do
      let mut args := #[]
      for f in fs do
        match ← inhabitant (← inferType f) (depth - 1) with
        | some a => args := args.push a
        | none => return none
      return some (mkAppN (mkConst c) args)
    if r.isSome then return r
  return none

/-- Wrapper chain taking the IR (mono) type `ir` to the source type `src`. -/
def wrappersFor (src : Expr) (ir : String) : MetaM (Option (List LeanGo.Module.Wrapper)) := do
  let some s ← irName src | return none
  if s == ir then return some []
  let rep ← try LeanGo.Module.representation s catch _ => return none
  if rep.mono == ir then return some rep.wrappers else return none

def projOf (structName : Name) : MetaM Name := do
  let some info := getStructureInfo? (← getEnv) structName
    | throwError "not a structure: {structName}"
  let some f := info.fieldNames[0]? | throwError "structure without fields: {structName}"
  return structName ++ f

/-- `fun (y₁ : IR₁) … => unwrap (f (wrap y₁) …)`, where each `wrap`/`unwrap`
follows the producer's representation of the source parameter/result type. -/
def adapt (f : Expr) (irParams : Array String) (irResult : String) : MetaM (Except String Expr) := do
  forallTelescope (← inferType f) fun xs res => do
    unless xs.size == irParams.size do
      return .error s!"arity mismatch: specification has {xs.size} parameters, IR has {irParams.size}"
    let rec go (i : Nat) (ys args : Array Expr) : MetaM (Except String Expr) := do
      if h : i < xs.size then
        let ty ← inferType xs[i]
        let some ws ← wrappersFor ty irParams[i]!
          | return .error s!"parameter {i}: source type {← ppExpr ty} does not represent IR type {irParams[i]!}"
        let monoTy := if ws.isEmpty then ty else mkConst irParams[i]!.toName
        withLocalDeclD ((← xs[i].fvarId!.getUserName)) monoTy fun y => do
          let arg := ws.foldr (fun w acc => mkApp (mkConst w.ctor) acc) y
          go (i + 1) (ys.push y) (args.push arg)
      else
        let some ws ← wrappersFor res irResult
          | return .error s!"result: source type {← ppExpr res} does not represent IR type {irResult}"
        let mut body := mkAppN f args
        for w in ws do
          body := mkApp (mkConst (← projOf w.source.toName)) body
        return .ok (← mkLambdaFVars ys body)
    go 0 #[] #[]

structure SpecInfo where
  kind : String
  /-- The specification term (closed). -/
  term : Expr
  /-- Source owner (the function itself for sources and byte folds). -/
  owner : Name
  /-- Constants `validate_obl` should unfold besides the obligation's own. -/
  unfold : Array Name := #[]
  note : String := ""

/-- Reduced helper: owner applied to the retained parameters. -/
def reducedSpec (ev : LeanGo.Module.ReducedEvidence) : MetaM (Except String Expr) := do
  let ownerTy ← inferType (mkConst ev.owner)
  forallTelescope ownerTy fun xs _ => do
    let mut args := #[]
    for h : i in [:xs.size] do
      if ev.retained.contains i then args := args.push xs[i]
      else
        let ty ← inferType xs[i]
        let some a ← inhabitant ty | return .error s!"no inhabitant for removed parameter {i} : {← ppExpr ty}"
        args := args.push a
    let keep := if ev.allUnused then #[] else ev.retained.map (xs[·]!)
    unless ev.allUnused || ev.retained.all (· < xs.size) do return .error "retained position out of range"
    return .ok (← mkLambdaFVars keep (mkAppN (mkConst ev.owner) args))

/-- Abstract `fvs` (lambda- or let-bound) in `e` as leading lambda parameters. -/
def abstractParams (fvs : Array Expr) (e : Expr) : MetaM Expr := do
  let mut body := e.abstract fvs
  for i' in [:fvs.size] do
    let i := fvs.size - 1 - i'
    let d ← fvs[i]!.fvarId!.getDecl
    body := .lam d.userName (d.type.abstractRange i fvs) body .default
  return body

def localLambda (captureNames : Array Name) (v : Expr) : MetaM (Except String Expr) := do
  -- inline let-bound variables that are not captures
  let mut v ← instantiateMVars v
  for _ in [:64] do
    let fvs := (collectFVars {} v).fvarIds
    let mut changed := false
    for fv in fvs do
      let d ← fv.getDecl
      if let some val := d.value? then
        unless captureNames.contains d.userName do
          v := v.replaceFVarId fv val
          changed := true
    unless changed do break
  let fvs := (collectFVars {} v).fvarIds
  let mut caps := #[]
  for c in captureNames do
    let ms ← fvs.filterM fun fv => return (← fv.getUserName) == c
    match ms with
    | #[fv] => caps := caps.push (mkFVar fv)
    | #[] => return .error s!"capture {c} is not a free variable of the source lambda"
    | _ => return .error s!"capture {c} is ambiguous in the source lambda"
  unless caps.size == fvs.size do
    return .error s!"source lambda has {fvs.size} free variables, the producer recorded {caps.size} captures"
  return .ok (← abstractParams caps v)

/-- The source `let` name of the local function: the producer accepted the
local either because its own binder is a source witness or because a source
witness aliases it (`LeanGo.Module.sourceAlias`); find that name the same way. -/
partial def aliasName (code : Lean.Compiler.LCNF.Code .pure) (fv : FVarId)
    (witnesses : Array LeanGo.Module.LocalSourceWitness) : Option Name :=
  match code with
  | .let d k =>
    if d.value == .fvar fv #[] && witnesses.any (·.binderName == d.binderName) then some d.binderName
    else aliasName k fv witnesses
  | .fun d k | .jp d k => (aliasName d.value fv witnesses).orElse fun _ => aliasName k fv witnesses
  | .cases c => c.alts.findSome? fun a => aliasName a.getCode fv witnesses
  | _ => none

def sourceBinder (ev : LeanGo.Module.LocalEvidence) : Option Name :=
  let n := ev.sourceLocal.binderName
  if ev.sourceWitnesses.any (·.binderName == n) then some n
  else match ev.sourceDecl.value with
    | .code c => aliasName c ev.sourceLocal.fvarId ev.sourceWitnesses
    | _ => none

/-- Lifted local helper: the source lambda bound by `let <binder> := …` in the
owner, with the producer's captures as leading parameters. -/
partial def localSpec (ev : LeanGo.Module.LocalEvidence) : MetaM (Except String Expr) := do
  let info ← getConstInfo ev.sourceOwner
  let some value := info.value? | return .error s!"source owner {ev.sourceOwner} has no definition"
  let some target := sourceBinder ev
    | return .error s!"no source binder recorded for local {ev.sourceLocal.binderName}"
  let captureNames := ev.captures.map (·.binderName)
  let found ← IO.mkRef (#[] : Array (Except String Expr))
  let rec walk (e : Expr) : MetaM Unit := do
    match e with
    | .lam n t b bi => withLocalDecl n bi t fun x => walk (b.instantiate1 x)
    | .letE n t v b nd =>
      if n == target && (← whnf t).isForall then
        found.modify (·.push (← localLambda captureNames v))
      walk v
      withLetDecl n t v (nondep := nd) fun x => walk (b.instantiate1 x)
    | .app f a => do walk f; walk a
    | .mdata _ e | .proj _ _ e => walk e
    | _ => pure ()
  walk value
  match ← found.get with
  | #[r] => return r
  | #[] => return .error s!"no let-bound function {target} in {ev.sourceOwner}"
  | _ => return .error s!"several let-bound functions named {target} in {ev.sourceOwner}"

/-- Determine the specification of one IR function from the producer's
evidence. `none` reason means not attempted. -/
def specFor (fn : IRFunction) (roots : Array Name) : CoreM (Except String SpecInfo) := do
  let env ← getEnv
  let name := fn.name.toName
  let isRoot := roots.contains name
  -- byte-fold roots carry their (accumulator, step) in the provenance record
  let fold? := ((LeanGo.Module.provenanceExt.getState env).find? name).bind (·.fold?)
  let local? ← try LeanGo.Module.localEvidence? name catch e => return .error s!"local helper evidence rejected: {← e.toMessageData.toString}"
  let reduced? ← try LeanGo.Module.reducedEvidence? name catch e => return .error s!"reduced helper evidence rejected: {← e.toMessageData.toString}"
  let kind := if fold?.isSome then "byte_fold" else if local?.isSome then "local_helper"
    else if reduced?.isSome then "reduced_helper" else "source"
  if isRoot && kind != "source" && kind != "byte_fold" then
    return .error s!"root {name} is a compiler-generated helper"
  Meta.MetaM.run' do
    match kind with
    | "local_helper" =>
      let ev := local?.get!
      match ← localSpec ev with
      | .error e => return .error e
      | .ok t =>
        match ← adapt t fn.params fn.result with
        | .error e => return .error e
        | .ok t => return .ok { kind := kind, term := t, owner := ev.sourceOwner, note := s!"lambda {ev.sourceLocal.binderName} of {ev.sourceOwner}, captures {ev.captures.map (·.binderName)}" }
    | "reduced_helper" =>
      let ev := reduced?.get!
      match ← reducedSpec ev with
      | .error e => return .error e
      | .ok t =>
        match ← adapt t fn.params fn.result with
        | .error e => return .error e
        | .ok t =>
          let note := s!"{ev.owner} with retained parameters {ev.retained}" ++
            (if ev.allUnused then " (all unused)" else "")
          return .ok { kind := kind, term := t, owner := ev.owner, unfold := #[ev.owner], note := note }
    | _ =>
      unless env.contains name do return .error s!"no source declaration named {name}"
      -- sources are paired with themselves: no adaptation, exact signature
      let ty ← inferType (mkConst name)
      let ok ← forallTelescope ty fun xs res => do
        let ps ← xs.mapM fun x => do irName (← inferType x)
        let r ← irName res
        return ps.toList == fn.params.toList.map some && r == some fn.result
      unless ok do return .error s!"source signature of {name} does not match the IR signature {fn.params} → {fn.result}"
      let note := match fold? with
        | some (acc, step) => s!"List.foldl {step} over {acc}"
        | none => ""
      return .ok { kind := kind, term := mkConst name, owner := name, note := note }

/-! ## Statements -/

def mkOblType (specList : Expr) (fnConst : Name) (spec : Expr) : MetaM Expr := do
  let g := mkApp2 (mkConst (V `evalBody)) (mkApp (mkConst (V `Spec.env)) specList) (mkConst fnConst)
  let eq := mkApp (mkConst ``Eq [1]) (mkApp (mkConst ``Option [0]) (mkConst (V `Value)))
  mkAppM (V `Callable.Rel) #[eq, g, spec]

def mkStrList (xs : Array String) : MetaM Expr :=
  mkListLit (mkConst ``String) (xs.toList.map mkStrLit)

def pp (e : Expr) : MetaM String := do
  return toString (← ppExpr e)

/-- `∀ n, Callable.Rel Approx (prog.run n "R") R`. -/
def mkSoundType (progName root : Name) : MetaM Expr := do
  withLocalDeclD `n (mkConst ``Nat) fun n => do
    let run := mkApp3 (mkConst (V `Program.run)) (mkConst progName) n (mkStrLit root.toString)
    let approx := mkApp (mkConst (V `Approx)) (mkConst (V `Value))
    mkForallFVars #[n] (← mkAppM (V `Callable.Rel) #[approx, run, mkConst root])

/-- `∀ n a₁ … aₖ v, prog.run n "R" [encode a₁, …] = some v → v = encode (R a₁ … aₖ)`. -/
def mkExplicitType (progName root : Name) : MetaM Expr := do
  withLocalDeclD `n (mkConst ``Nat) fun n => do
    forallTelescope (← inferType (mkConst root)) fun xs _ => do
      let encs ← xs.mapM fun x => mkAppM (V `Encode.encode) #[x]
      let args ← mkListLit (mkConst (V `Value)) encs.toList
      let run := mkApp4 (mkConst (V `Program.run)) (mkConst progName) n (mkStrLit root.toString) args
      withLocalDeclD `v (mkConst (V `Value)) fun v => do
        let hyp ← mkEq run (← mkAppM ``Option.some #[v])
        let concl ← mkEq v (← mkAppM (V `Encode.encode) #[mkAppN (mkConst root) xs])
        mkForallFVars (#[n] ++ xs ++ #[v]) (← mkArrow hyp concl)

/-! ## `validate_prefixed_on` -/

partial def splitAllValidOn (g : MVarId) : MetaM (List MVarId) := do
  let t ← whnfD (← g.getType)
  if t.isConstOf ``True then
    g.assign (mkConst ``True.intro); return []
  if t.isAppOfArity ``And 2 then
    let a := t.appFn!.appArg!
    let b := t.appArg!
    let g1 ← mkFreshExprSyntheticOpaqueMVar a
    let g2 ← mkFreshExprSyntheticOpaqueMVar b
    g.assign (mkApp4 (mkConst ``And.intro) a b g1 g2)
    return g1.mvarId! :: (← splitAllValidOn g2.mvarId!)
  throwError "validate_prefixed_on: cannot unfold the function list{indentExpr t}"

/-- `PreFixedOn P (Spec.env S) names` from obligations for (at least) the
functions named in `names`; every other function is discharged by deciding
that its name is not in `names`. -/
syntax (name := validatePrefixedOn) "validate_prefixed_on" " [" term,* "]" : tactic

open Lean.Elab.Tactic in
@[tactic validatePrefixedOn] def evalValidatePrefixedOn : Tactic := fun stx => do
  let obls : Array Term := stx[2].getSepArgs.map fun s => ⟨s⟩
  evalTactic (← `(tactic| apply $(mkCIdent (V `PreFixedOn.of_allValidOn)):ident))
  let gs ← splitAllValidOn (← getMainGoal)
  let mut table : Array (Name × Term) := #[]
  for o in obls do
    let ty ← Term.withoutErrToSorry do
      let e ← Term.elabTerm o none
      instantiateMVars (← inferType e)
    let some ev := ty.find? (fun x => x.isAppOf (V `evalBody) && x.getAppNumArgs ≥ 2)
      | throwError "validate_prefixed_on: not an obligation{indentExpr ty}"
    let .const c _ := ev.getAppArgs[1]!.getAppFn
      | throwError "validate_prefixed_on: obligation is not about a named IR function"
    table := table.push (c, o)
  for g in gs do
    setGoals [g]
    let gty ← instantiateMVars (← g.getType)
    let some nm := gty.find? (fun x => x.isAppOfArity (V `Function.name) 1)
      | throwError "validate_prefixed_on: unexpected goal{indentExpr gty}"
    let some c := nm.appArg!.getAppFn.constName?
      | throwError "validate_prefixed_on: unexpected function{indentExpr gty}"
    let fnId := mkCIdent c
    match table.find? (·.1 == c) with
    | some (_, o) =>
      evalTactic (← `(tactic|
        (intro _
         refine ⟨?_, $(mkCIdent (V `fnValid_of_obl)) _ ?_ ?_ $o⟩
         · first
           | decide
           | simp [$(mkCIdent (V `Code.calls)):ident, $(mkCIdent (V `Alts.calls)):ident, $fnId:ident]
         · validate_lookup
         · validate_dom)))
    | none =>
      evalTactic (← `(tactic| (intro h; exact absurd h (by decide))))
  setGoals []

/-! ## The command -/

structure FnReport where
  ir : IRFunction
  kind : String := ""
  owner : String := ""
  note : String := ""
  status : String := "not_attempted"
  reason : String := ""
  timeMs : Nat := 0
  heartbeats : Nat := 0
  theoremName : Name := .anonymous
  statement : String := ""
  axioms : Array Name := #[]
  deriving Inhabited

def FnReport.json (r : FnReport) : Json := Json.mkObj [
  ("name", r.ir.name), ("params", toJson r.ir.params), ("result", r.ir.result),
  ("kind", r.kind), ("source_owner", r.owner), ("specification", r.note),
  ("status", r.status), ("reason", r.reason), ("time_ms", r.timeMs),
  ("heartbeats_used", r.heartbeats),
  ("theorem", if r.theoremName.isAnonymous then Json.null else toJson r.theoremName.toString),
  ("statement", r.statement), ("axioms", toJson (r.axioms.map Name.toString))]

syntax (name := leanGoValidate) "#lean_go_validate " str " [" ident,* "] " str
  &"report" str (&"heartbeats" num)? (&"timeout_ms" num)? (&"strict")? (&"namespace" ident)?
  (&"skip" str)? : command

/-- The export command writes `json.pretty ++ "\n"`; `none` when no file is named. -/
def irFileMatches (path : String) (json : Json) : IO (Option Bool) := do
  if path.isEmpty then return none
  try return some ((← IO.FS.readFile path) == json.pretty ++ "\n") catch _ => return some false

@[command_elab leanGoValidate] def elabLeanGoValidate : CommandElab := fun stx => do
  let irPath := stx[1].isStrLit?.getD ""
  let moduleName := stx[5].isStrLit?.getD ""
  let reportPath := stx[7].isStrLit?.getD ""
  let heartbeats := if stx[8].isNone then 200000 else stx[8][1].isNatLit?.getD 200000
  let timeoutMs := if stx[9].isNone then 120000 else stx[9][1].isNatLit?.getD 120000
  let strict := !stx[10].isNone
  let ns := if stx[11].isNone then defaultNamespace else stx[11][1].getId
  -- `skip "F G …"`: IR functions the caller does not want attempted (for
  -- example one whose proof attempt exhausted the machine's memory in an
  -- earlier run); they are reported `not_attempted` with the caller's reason.
  let skipped : List String := if stx[12].isNone then [] else
    ((stx[12][1].isStrLit?.getD "").splitOn " ").filter (!·.isEmpty)
  let progName := ns ++ `prog
  let specListName := ns ++ `spec
  let budget : Budget := { heartbeats, timeoutMs }
  let mut roots := #[]
  for r in stx[3].getSepArgs do
    roots := roots.push (← liftCoreM <| realizeGlobalConstNoOverloadWithInfo r)
  roots := roots.qsort (·.toString < ·.toString)
  let started ← IO.monoMsNow
  -- Progress lines go to `<report>.progress` (the frontend captures stdout and
  -- stderr of elaborators), so a caller can follow a long run or see where a
  -- process was killed.
  let progress (msg : String) : CommandElabM Unit := do
    unless reportPath.isEmpty do
      let h ← IO.FS.Handle.mk (reportPath ++ ".progress") .append
      h.putStrLn s!"{(← IO.monoMsNow) - started} ms {msg}"
      h.flush
  unless reportPath.isEmpty do IO.FS.writeFile (reportPath ++ ".progress") ""
  progress "export value"
  let writeReport (fields : List (String × Json)) : CommandElabM Unit := do
    let base := [("module", toJson moduleName), ("roots", toJson (roots.map Name.toString)),
      ("budget", Json.mkObj [("heartbeats", toJson heartbeats), ("timeout_ms", toJson timeoutMs)]),
      ("strict", toJson strict)]
    unless reportPath.isEmpty do
      IO.FS.writeFile reportPath ((Json.mkObj (base ++ fields)).pretty ++ "\n")
  -- 1. the producer's value, and the file it wrote
  let json ← match ← capture (liftCoreM (LeanGo.Module.exportProgramRoots moduleName roots)) with
    | .ok j => pure j
    | .error e =>
      writeReport [("status", toJson "export_failed"), ("reason", toJson e)]
      if strict then throwError "lean_go_validate: export failed: {e}"
      return
  let fileMatches ← irFileMatches irPath json
  let fns ← match irFunctions json with
    | .ok f => pure f
    | .error e => throwError "lean_go_validate: unexpected IR: {e}"
  let tys ← match irTypes json with
    | .ok t => pure t
    | .error e => throwError "lean_go_validate: unexpected IR: {e}"
  let mut reports : Array FnReport := fns.map fun ir => { ir }
  -- 2. encodings
  progress "encodings"
  let encodings ← deriveEncodings tys
  -- 3. program term
  progress "program term"
  let loaded ← capture (LeanGo.Validate.Meta.elabProgramJson progName json)
  let fnConsts : Array (String × Name) ← match loaded with
    | .ok cs => pure cs
    | .error e =>
      let failedReports := reports.map fun r => { r with reason := s!"IR not loaded: {e}" }
      writeReport [("status", toJson "load_failed"), ("reason", toJson e), ("ir_file_matches", toJson fileMatches),
        ("functions", toJson (failedReports.map FnReport.json))]
      if strict then throwError "lean_go_validate: IR not loaded: {e}"
      return
  -- 4. specifications
  progress "specifications"
  let mut specs : Array (Option SpecInfo) := #[]
  let mut specConsts : Array Name := #[]
  for i in [:fns.size] do
    let fn := fns[i]!
    let missing := (fn.params.push fn.result).filterMap fun t =>
      match encodings.find? (·.1 == t) with
      | some (_, some r) => some s!"{t}: {r}"
      | _ => none
    if !missing.isEmpty then
      reports := reports.modify i ({ · with reason := s!"no encoding for {", ".intercalate missing.toList}" })
      specs := specs.push none
      continue
    match ← liftCoreM (specFor fn roots) with
    | .error e =>
      reports := reports.modify i ({ · with reason := e })
      specs := specs.push none
    | .ok s =>
      -- name the specification so that callers can unfold it
      let c ← if s.kind == "source" || s.kind == "byte_fold" then pure s.owner else do
        let c := ns ++ Name.mkSimple s!"spec_{i}"
        let r ← capture <| liftTermElabM do
          let ty ← inferType s.term
          addDecl (.defnDecl { name := c, levelParams := [], type := ty, value := s.term, hints := .regular 0, safety := .safe })
          enableRealizationsForConst c
        match r with
        | .ok () => pure c
        | .error e =>
          reports := reports.modify i ({ · with reason := s!"specification not definable: {e}" })
          specs := specs.push none
          continue
      -- the entry must elaborate (it needs `Callable`, i.e. encodings of its types)
      let r ← capture <| liftTermElabM do
        let e ← mkAppM (V `SpecEntry.mk) #[mkStrLit fn.name, mkConst c]
        discard <| instantiateMVars e
      match r with
      | .error e =>
        reports := reports.modify i ({ · with reason := s!"specification has no IR encoding: {e}" })
        specs := specs.push none
      | .ok () =>
        let note := if s.note.isEmpty then c.toString else s!"{c}: {s.note}"
        reports := reports.modify i ({ · with kind := s.kind, owner := s.owner.toString, note := note })
        specs := specs.push (some { s with term := mkConst c })
        specConsts := specConsts.push c
  -- the specification list
  let specList ← liftTermElabM do
    let mut entries := #[]
    for i in [:fns.size] do
      if let some s := specs[i]! then
        entries := entries.push (← mkAppM (V `SpecEntry.mk) #[mkStrLit fns[i]!.name, s.term])
    let lst ← mkListLit (mkConst (V `SpecEntry)) entries.toList
    let ty := mkApp (mkConst ``List [1]) (mkConst (V `SpecEntry))
    addDecl (.defnDecl { name := specListName, levelParams := [], type := ty, value := lst, hints := .regular 0, safety := .safe })
    enableRealizationsForConst specListName
    return Lean.mkConst specListName
  -- 5. obligations
  progress "obligations"
  -- Generated specifications are unfolded everywhere. So are the owners of
  -- reduced helpers: their specification fixes the removed arguments to an
  -- arbitrary inhabitant, which differs syntactically from the source call
  -- sites until the (argument-ignoring) owner body is exposed.
  let helperSpecs := specConsts.filter (ns.isPrefixOf ·)
  let owners := specs.foldl (init := #[]) fun acc s? => match s? with
    | some s => if s.kind == "reduced_helper" && !acc.contains s.owner then acc.push s.owner else acc
    | none => acc
  for i in [:fns.size] do
    let some s := specs[i]! | continue
    let fn := fns[i]!
    let some (_, fnConst) := fnConsts.find? (·.1 == fn.name) | continue
    let thm := ns ++ Name.mkSimple s!"obl_{i}"
    if skipped.contains fn.name then
      reports := reports.modify i ({ · with reason := "skipped by the caller" })
      continue
    let type ← liftTermElabM (mkOblType specList fnConst s.term)
    let statement ← liftTermElabM (pp type)
    let extra := (helperSpecs ++ owners ++ s.unfold.filter (!owners.contains ·)).map fun n => (mkCIdent n : Term)
    let tac ← `(tactic| validate_obl [$extra,*])
    progress s!"proving {fn.name}"
    let (r, ms, hb) ← liftCoreM (proveTheorem thm type tac budget)
    progress s!"{fn.name}: {if r matches .ok _ then "proved" else "failed"} in {ms} ms"
    let (status, reason, axioms, thmName) := match r with
      | .ok a => ("proved", "", a, thm)
      | .error e => ("failed", e, #[], Name.anonymous)
    reports := reports.modify i fun rep =>
      { rep with timeMs := ms, heartbeats := hb, statement := statement, theoremName := thmName, status := status, reason := reason, axioms := axioms }
  -- 6. whole-program theorems
  progress "whole-program theorems"
  let mut programs : Array Json := #[]
  for h : k in [:roots.size] do
    let root := roots[k]
    let cl := closure fns root.toString
    let missing := cl.filter fun f => !(reports.any fun r => r.ir.name == f && r.status == "proved")
    let base := [("root", toJson root.toString), ("closure", toJson cl)]
    if !missing.isEmpty then
      programs := programs.push (Json.mkObj (base ++ [("status", toJson "not_established"),
        ("missing", toJson missing)]))
      continue
    let obls := cl.filterMap fun f => (reports.find? (·.ir.name == f)).map (·.theoremName)
    let preName := ns ++ Name.mkSimple s!"prefixed_{k}"
    let soundName := ns ++ Name.mkSimple s!"sound_{k}"
    let explicitName := ns ++ Name.mkSimple s!"explicit_{k}"
    let preType ← liftTermElabM do
      mkAppM (V `PreFixedOn) #[mkConst progName, mkApp (mkConst (V `Spec.env)) specList, ← mkStrList cl]
    let oblIds := obls.map fun n => (mkCIdent n : Term)
    let (r1, ms1, _) ← liftCoreM (proveTheorem preName preType (← `(tactic| validate_prefixed_on [$oblIds,*])) budget)
    let fail (what : String) (e : String) : Json :=
      Json.mkObj (base ++ [("status", toJson "failed"), ("reason", toJson s!"{what}: {e}")])
    let .ok _ := r1 | programs := programs.push (fail "PreFixedOn" (match r1 with | .error e => e | _ => "")); continue
    let soundType ← liftTermElabM (mkSoundType progName root)
    let runRelOn := mkCIdent (V `run_rel_on)
    let preId := mkCIdent preName
    let rootId := mkCIdent root
    let soundTac ← `(tactic| (intro n; exact $runRelOn $preId (by decide) $rootId (by validate_lookup) n))
    let (r2, ms2, _) ← liftCoreM (proveTheorem soundName soundType soundTac budget)
    let .ok axs := r2 | programs := programs.push (fail "run_rel_on" (match r2 with | .error e => e | _ => "")); continue
    let explicitType ← liftTermElabM (mkExplicitType progName root)
    let arity ← liftTermElabM do forallTelescope (← inferType (mkConst root)) fun xs _ => pure xs.size
    let as := (Array.range arity).map fun j => mkIdent (Name.mkSimple s!"a{j}")
    let eqOfSome := mkCIdent (V `Approx.eq_of_some)
    let soundId := mkCIdent soundName
    let explicitTac ← `(tactic| (intro n $as* v h; exact Option.some.inj (($eqOfSome ($soundId n $as*) h).symm)))
    let (r3, ms3, _) ← liftCoreM (proveTheorem explicitName explicitType explicitTac budget)
    let .ok axs3 := r3 | programs := programs.push (fail "explicit form" (match r3 with | .error e => e | _ => "")); continue
    programs := programs.push (Json.mkObj (base ++ [("status", toJson "proved"),
      ("prefixed_theorem", toJson preName.toString),
      ("theorem", toJson soundName.toString), ("statement", toJson (← liftTermElabM (pp soundType))),
      ("explicit_theorem", toJson explicitName.toString),
      ("explicit_statement", toJson (← liftTermElabM (pp explicitType))),
      ("axioms", toJson ((axs ++ axs3).toList.eraseDups.map Name.toString)),
      ("time_ms", toJson (ms1 + ms2 + ms3))]))
  let count (s : String) := (reports.filter (·.status == s)).size
  let rootsProved := (programs.filter fun p => (p.getObjValAs? String "status").toOption == some "proved").size
  let stopped ← IO.monoMsNow
  writeReport [("status", toJson "completed"), ("ir_file_matches", toJson fileMatches),
    ("program", toJson progName.toString), ("specification_list", toJson specListName.toString),
    ("encodings", toJson (encodings.map fun (t, r) => Json.mkObj
      [("type", toJson t), ("status", toJson (if r.isNone then "available" else "failed")),
       ("reason", toJson (r.getD ""))])),
    ("functions", toJson (reports.map FnReport.json)),
    ("programs", toJson programs),
    ("summary", Json.mkObj [("functions", toJson fns.size), ("proved", toJson (count "proved")),
      ("failed", toJson (count "failed")), ("not_attempted", toJson (count "not_attempted")),
      ("roots", toJson roots.size), ("roots_proved", toJson rootsProved),
      ("time_ms", toJson (stopped - started))])]
  if fileMatches == some false then
    logWarning m!"lean_go_validate: {irPath} is not the producer's JSON value for these roots"
  if strict then
    let bad := reports.filter (·.status != "proved")
    unless bad.isEmpty && fileMatches != some false do
      throwError "lean_go_validate: unproved obligations: {bad.map (·.ir.name)}"

end LeanGo.Validate.Command
