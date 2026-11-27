module
public meta import Lean
public import LeanGo.Validate.Spec
public import LeanGo.Validate.Steps
public meta section

/-!
# Proof automation

* `validate_obl [extra lemmas]` proves a per-function obligation
  `Callable.Rel Eq (evalBody (Spec.env S) fn) f`, i.e.
  `∀ a, evalBody ρ fn (encode a) = some (encode (f a))`, by symbolic
  evaluation:
  1. introduce the source arguments;
  2. `simp only` with the `ir_simp` set (evaluator, primitives, per-type
     `encode`/`decode` equations, `Callable.call`), the IR function, the spec
     list, the source function `f` (unfolded once, so recursive calls stay
     folded and meet the spec environment's answers), the hypotheses and any
     extra lemmas. The generic steps whose core lemmas are `rfl` and have the
     same head on both sides (`Option.bind_some`, `List.foldl_cons`,
     `ite_true`, `ite_false`) are taken from `LeanGo.Validate.Steps`, which
     restates them with non-`rfl` proofs: `simp` then records each step with
     an explicit proof instead of leaving the kernel to rediscover it by
     unfolding, which on fixed-width arithmetic can be exponentially costly;
  3. if evaluation is stuck on a `cases` whose scrutinee is `encode t`, case
     split on `t` (an argument variable is destructed; any other term is
     generalized first, and a split on `decide p` is turned into `p`/`¬p`);
  4. otherwise split the first remaining `if`/`match` (source-side control);
  5. repeat until every goal closes, or fail and print the stuck goal.
* `validate_dom` discharges `Callable.Dom φ P.types (fn.params.map (·.ty))`
  from `IRType` instances (`Subtable` by `decide`).
* `validate_prefixed [o₁, …, oₙ]` proves `PreFixed P (Spec.env S)` from the
  per-function obligations.

Nothing here is trusted: every step produces an ordinary kernel-checked proof.
-/

open Lean Elab Tactic Meta

namespace LeanGo.Validate.Meta

def VV (n : Name) : Ident := mkCIdent (`LeanGo.Validate ++ n)

/-- Constants to unfold: the IR function, the spec list and the source function. -/
def obligationConsts (e : Expr) : MetaM (Array Name) := do
  let e ← instantiateMVars e
  let mut out := #[]
  if let some ev := e.find? (fun x => x.isAppOfArity ``LeanGo.Validate.evalBody 3) then
    let args := ev.getAppArgs
    if let .const n _ := args[1]!.getAppFn then out := out.push n
    if let some sp := args[0]!.find? (fun x => x.isAppOfArity ``LeanGo.Validate.Spec.env 1) then
      if let .const n _ := sp.appArg!.getAppFn then out := out.push n
  -- right-hand side: some (encode (f a …))
  if let some (_, _, rhs) := e.eq? then
    if let some enc := rhs.find? (fun x => x.isAppOfArity ``LeanGo.Validate.Encode.encode 3) then
      if let .const n _ := enc.appArg!.getAppFn then out := out.push n
  return out

/-- First `Value.view (encode t)` in the goal. -/
def findScrutinee (e : Expr) : Option Expr :=
  (e.find? fun x =>
    x.isAppOfArity ``LeanGo.Validate.Value.view 1 &&
      x.appArg!.isAppOfArity ``LeanGo.Validate.Encode.encode 3).map fun x => x.appArg!.appArg!

/-! Literal normal forms (see `UIntW.shiftLeft_lit`, `UIntW.ofNat_lit`):
a scalar literal outside its width's range (the source may write
`4294967297 : UInt32`, the IR records `1`) is reduced, and a shift by a literal
amount becomes a multiplication by the power of two (the compiler turns
`x * 256` into `x <<< 8`). Each step is an instance of a proved lemma whose
side condition is `rfl` on closed natural numbers. -/
simproc_decl reduceUInt8Lit ((OfNat.ofNat _ : UInt8)) := fun e => do
  let_expr OfNat.ofNat _ n _ ← e | return .continue
  let some v ← Nat.fromExpr? n | return .continue
  if v < 256 then return .continue
  let m := v % 256
  let pf := mkApp3 (mkConst ``LeanGo.Validate.UInt8.ofNat_lit) (mkRawNatLit v) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := toExpr (UInt8.ofNat m), proof? := some pf }
simproc_decl shiftLitUInt8 ((_ <<< _ : UInt8)) := fun e => do
  let_expr HShiftLeft.hShiftLeft _ _ _ _ x y ← e | return .continue
  let_expr OfNat.ofNat _ n _ ← y | return .continue
  let some k ← Nat.fromExpr? n | return .continue
  let m := 2 ^ (k % 8)
  let rhs ← mkAppM ``HMul.hMul #[x, toExpr (UInt8.ofNat m)]
  let pf := mkApp4 (mkConst ``LeanGo.Validate.UInt8.shiftLeft_lit) x (mkRawNatLit k) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := rhs, proof? := some pf }
simproc_decl reduceUInt16Lit ((OfNat.ofNat _ : UInt16)) := fun e => do
  let_expr OfNat.ofNat _ n _ ← e | return .continue
  let some v ← Nat.fromExpr? n | return .continue
  if v < 65536 then return .continue
  let m := v % 65536
  let pf := mkApp3 (mkConst ``LeanGo.Validate.UInt16.ofNat_lit) (mkRawNatLit v) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := toExpr (UInt16.ofNat m), proof? := some pf }
simproc_decl shiftLitUInt16 ((_ <<< _ : UInt16)) := fun e => do
  let_expr HShiftLeft.hShiftLeft _ _ _ _ x y ← e | return .continue
  let_expr OfNat.ofNat _ n _ ← y | return .continue
  let some k ← Nat.fromExpr? n | return .continue
  let m := 2 ^ (k % 16)
  let rhs ← mkAppM ``HMul.hMul #[x, toExpr (UInt16.ofNat m)]
  let pf := mkApp4 (mkConst ``LeanGo.Validate.UInt16.shiftLeft_lit) x (mkRawNatLit k) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := rhs, proof? := some pf }
simproc_decl reduceUInt32Lit ((OfNat.ofNat _ : UInt32)) := fun e => do
  let_expr OfNat.ofNat _ n _ ← e | return .continue
  let some v ← Nat.fromExpr? n | return .continue
  if v < 4294967296 then return .continue
  let m := v % 4294967296
  let pf := mkApp3 (mkConst ``LeanGo.Validate.UInt32.ofNat_lit) (mkRawNatLit v) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := toExpr (UInt32.ofNat m), proof? := some pf }
simproc_decl shiftLitUInt32 ((_ <<< _ : UInt32)) := fun e => do
  let_expr HShiftLeft.hShiftLeft _ _ _ _ x y ← e | return .continue
  let_expr OfNat.ofNat _ n _ ← y | return .continue
  let some k ← Nat.fromExpr? n | return .continue
  let m := 2 ^ (k % 32)
  let rhs ← mkAppM ``HMul.hMul #[x, toExpr (UInt32.ofNat m)]
  let pf := mkApp4 (mkConst ``LeanGo.Validate.UInt32.shiftLeft_lit) x (mkRawNatLit k) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := rhs, proof? := some pf }
simproc_decl reduceUInt64Lit ((OfNat.ofNat _ : UInt64)) := fun e => do
  let_expr OfNat.ofNat _ n _ ← e | return .continue
  let some v ← Nat.fromExpr? n | return .continue
  if v < 18446744073709551616 then return .continue
  let m := v % 18446744073709551616
  let pf := mkApp3 (mkConst ``LeanGo.Validate.UInt64.ofNat_lit) (mkRawNatLit v) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := toExpr (UInt64.ofNat m), proof? := some pf }
simproc_decl shiftLitUInt64 ((_ <<< _ : UInt64)) := fun e => do
  let_expr HShiftLeft.hShiftLeft _ _ _ _ x y ← e | return .continue
  let_expr OfNat.ofNat _ n _ ← y | return .continue
  let some k ← Nat.fromExpr? n | return .continue
  let m := 2 ^ (k % 64)
  let rhs ← mkAppM ``HMul.hMul #[x, toExpr (UInt64.ofNat m)]
  let pf := mkApp4 (mkConst ``LeanGo.Validate.UInt64.shiftLeft_lit) x (mkRawNatLit k) (mkRawNatLit m)
    (mkApp2 (mkConst ``Eq.refl [1]) (mkConst ``Nat) (mkRawNatLit m))
  return .done { expr := rhs, proof? := some pf }

/-- One symbolic-evaluation pass. Join declarations are *not* unfolded here
(`Code.eval_join` is not in `ir_simp`): `generalizeJoin` names each
join's continuation first, so its body is evaluated only at jumps, with the
jump's arguments, instead of being copied into (and simplified under) every
enclosing closure. `withJoins := true` is the fallback for a join that sits
under a binder and therefore cannot be generalized. -/
def runSimp (consts : Array Name) (extra : Array Term) (withJoins := false) : TacticM Unit := do
  let ids : Array Term := consts.map fun n => mkCIdent n
  let lemmas ← (ids ++ extra).mapM fun t => `(Lean.Parser.Tactic.simpLemma| $t:term)
  if withJoins then
    evalTactic (← `(tactic|
      simp only [ir_simp, $(VV `Code.eval_join):ident, $(VV `Steps.bind_some):ident, Option.bind_none, $(VV `Steps.ite_true):ident, $(VV `Steps.ite_false):ident, ↓reduceIte, Nat.reduceEqDiff, String.reduceEq, reduceCtorEq,
        Bool.false_eq_true, Bool.true_eq_false, decide_true, decide_false, Bool.not_true, Bool.not_false,
        $(VV `Steps.foldl_cons):ident, List.foldl_nil, Option.some.injEq, Nat.reducePow, Nat.reduceMod,
        $(mkCIdent `LeanGo.Validate.Meta.reduceUInt8Lit):ident, $(mkCIdent `LeanGo.Validate.Meta.reduceUInt16Lit):ident,
        $(mkCIdent `LeanGo.Validate.Meta.reduceUInt32Lit):ident, $(mkCIdent `LeanGo.Validate.Meta.reduceUInt64Lit):ident,
        $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt8):ident, $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt16):ident,
        $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt32):ident, $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt64):ident, $lemmas,*, *]))
  else
    evalTactic (← `(tactic|
      simp only [ir_simp, $(VV `Steps.bind_some):ident, Option.bind_none, $(VV `Steps.ite_true):ident, $(VV `Steps.ite_false):ident, ↓reduceIte, Nat.reduceEqDiff, String.reduceEq, reduceCtorEq,
        Bool.false_eq_true, Bool.true_eq_false, decide_true, decide_false, Bool.not_true, Bool.not_false,
        $(VV `Steps.foldl_cons):ident, List.foldl_nil, Option.some.injEq, Nat.reducePow, Nat.reduceMod,
        $(mkCIdent `LeanGo.Validate.Meta.reduceUInt8Lit):ident, $(mkCIdent `LeanGo.Validate.Meta.reduceUInt16Lit):ident,
        $(mkCIdent `LeanGo.Validate.Meta.reduceUInt32Lit):ident, $(mkCIdent `LeanGo.Validate.Meta.reduceUInt64Lit):ident,
        $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt8):ident, $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt16):ident,
        $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt32):ident, $(mkCIdent `LeanGo.Validate.Meta.shiftLitUInt64):ident, $lemmas,*, *]))

/-- First `Code.eval ρ (Code.join …) E J` in the goal that is closed (not under
a binder), so that its continuation can be generalized. -/
def findJoin (e : Expr) : Option Expr :=
  e.find? fun x =>
    x.isAppOfArity ``LeanGo.Validate.Code.eval 4 &&
      (x.getArg! 1).isAppOfArity ``LeanGo.Validate.Code.join 4 && !x.hasLooseBVars

/-- Some join declaration remains, possibly under a binder. -/
def hasJoin (e : Expr) : Bool :=
  (e.find? fun x =>
    x.isAppOfArity ``LeanGo.Validate.Code.eval 4 &&
      (x.getArg! 1).isAppOfArity ``LeanGo.Validate.Code.join 4).isSome

/-- Replace a closed join declaration by its continuation with a fresh
closure variable `k` and the hypothesis `∀ args, k args = body…`
(`Code.eval_join_exists`). -/
def generalizeJoin (x : Expr) (counter : IO.Ref Nat) : TacticM Unit := do
  let args := x.getAppArgs
  let cargs := args[1]!.getAppArgs
  let pf ← mkAppM ``LeanGo.Validate.Code.eval_join_exists
    #[args[0]!, args[2]!, args[3]!, cargs[0]!, cargs[1]!, cargs[2]!, cargs[3]!]
  let n ← counter.modifyGet fun n => (n, n + 1)
  let k := mkIdent (Name.mkSimple s!"kir{n}")
  let hk := mkIdent (Name.mkSimple s!"hkir{n}")
  let heq := mkIdent (Name.mkSimple s!"heqir{n}")
  let stx ← Term.exprToSyntax pf
  evalTactic (← `(tactic| (obtain ⟨$k, $hk, $heq⟩ := $stx; rw [$heq:ident]; clear $heq)))

/-- Run `t`; on an exception or a logged error, restore the state and report failure. -/
def tryTac (t : TacticM Unit) : TacticM Bool := do
  let s ← saveState
  let errsBefore := (← Core.getMessageLog).toList.filter (·.severity == .error) |>.length
  try
    Term.withoutErrToSorry t
    let errsAfter := (← Core.getMessageLog).toList.filter (·.severity == .error) |>.length
    if errsAfter > errsBefore then
      s.restore (restoreInfo := true); return false
    return true
  catch _ =>
    s.restore (restoreInfo := true); return false

/-- Post-process a split hypothesis `decide p = b` into `p` or `¬p`. -/
def normalizeHyp (h : Name) : TacticM Unit := do
  let hId := mkIdent h
  discard <| tryTac <| evalTactic (← `(tactic|
    simp only [decide_eq_true_eq, decide_eq_false_iff_not, Bool.not_eq_true, Bool.not_eq_false] at $hId:ident))

partial def loop (consts : Array Name) (extra : Array Term) (fuel : Nat) (counter : IO.Ref Nat) :
    TacticM Unit := do
  if (← getGoals).isEmpty then return
  if fuel == 0 then throwError "validate_obl: fuel exhausted"
  discard <| tryTac (runSimp consts extra)
  if (← getGoals).isEmpty then return
  let g ← getMainGoal
  let tgt ← instantiateMVars (← g.getType)
  trace[debug] m!"validate_obl step:{indentD g}"
  if let some x := findJoin tgt then
    trace[debug] m!"join: generalize"
    generalizeJoin x counter
    return ← loop consts extra (fuel - 1) counter
  if let some t := findScrutinee tgt then
    trace[debug] m!"scrutinee: {t}"
    let subgoals ← g.withContext do
      if t.isFVar then
        let gs ← g.cases t.fvarId!
        return gs.toList.map (·.mvarId)
      else
        let k ← counter.modifyGet fun k => (k, k + 1)
        let hName := Name.mkSimple s!"hir{k}"
        let (fvars, g') ← g.generalize #[{ expr := t, xName? := Name.mkSimple s!"sir{k}", hName? := hName }]
        let gs ← g'.cases fvars[0]!
        -- name the generalization hypothesis accessibly and normalize it
        let mut out := []
        for sg in gs do
          setGoals [sg.mvarId]
          normalizeHyp hName
          out := out ++ (← getGoals)
        return out
    for sg in subgoals do
      setGoals [sg]
      loop consts extra (fuel - 1) counter
    return
  -- a join under a binder: unfold it the old way
  setGoals [g]
  if hasJoin tgt then
    if ← tryTac (runSimp consts extra (withJoins := true)) then
      return ← loop consts extra (fuel - 1) counter
  -- source-side control flow
  setGoals [g]
  trace[debug] m!"split"
  if ← tryTac (evalTactic (← `(tactic| split))) then
    let gs ← getGoals
    for sg in gs do
      setGoals [sg]
      if ← tryTac (evalTactic (← `(tactic| contradiction))) then continue
      loop consts extra (fuel - 1) counter
    return
  -- Only reducible unfolding: a default-transparency `rfl` can produce proofs
  -- that the kernel checks by evaluating fixed-width arithmetic on symbolic
  -- operands (the same kernel cost as the definitional steps that
  -- `LeanGo.Validate.Steps` avoids).
  if ← tryTac (evalTactic (← `(tactic| with_reducible rfl))) then return
  throwError "validate_obl: stuck at{indentExpr tgt}"

/-- Destruct every local of structure type (single constructor, no recursion),
recursively through structure-typed fields. This is always safe (no case
explosion, no infinite unfolding) and lets both the IR side and the source
side compute through record fields before `simp` runs. -/
partial def destructStructures (g : MVarId) : MetaM (List MVarId) := g.withContext do
  for d in (← getLCtx) do
    if d.isImplementationDetail then continue
    let ty ← whnfR (← instantiateMVars d.type)
    let .const n _ := ty.getAppFn | continue
    unless ty.getAppNumArgs == 0 && isStructure (← getEnv) n do continue
    -- scalars are structures too, but their encoding is the scalar itself
    if n == ``UInt8 || n == ``UInt16 || n == ``UInt32 || n == ``UInt64 then continue
    let gs ← g.cases d.fvarId
    let mut out := []
    for sg in gs do
      out := out ++ (← destructStructures sg.mvarId)
    return out
  return [g]

syntax (name := validateObl) "validate_obl" (" [" term,* "]")? : tactic

@[tactic validateObl] def evalValidateObl : Tactic := fun stx => do
  let extra : Array Term := if stx[1].isNone then #[] else
    (stx[1][1].getSepArgs.map fun s => ⟨s⟩)
  evalTactic (← `(tactic| simp only [$(VV `Callable.rel_arrow):ident, $(VV `Callable.rel_base):ident]))
  evalTactic (← `(tactic| intros))
  let g ← getMainGoal
  let consts ← g.withContext do obligationConsts (← g.getType)
  let counter ← IO.mkRef 0
  for sg in ← destructStructures g do
    setGoals [sg]
    loop consts extra 200 counter

/-- `∀ args, Spec.env S name args = Callable.call f args`, by unfolding the
specification list `S` (no definitional unfolding of source functions). -/
syntax (name := validateLookup) "validate_lookup" : tactic

@[tactic validateLookup] def evalValidateLookup : Tactic := fun _ => do
  let g ← getMainGoal
  let ty ← instantiateMVars (← g.getType)
  let some sp := ty.find? (fun x => x.isAppOf ``LeanGo.Validate.Spec.env && x.getAppNumArgs ≥ 1)
    | throwError "validate_lookup: no specification environment in the goal"
  let .const S _ := sp.getAppArgs[0]!.getAppFn
    | throwError "validate_lookup: the specification list must be a named constant"
  let mut ids : Array Ident := #[mkCIdent S]
  if let some nm := ty.find? (fun x => x.isAppOfArity ``LeanGo.Validate.Function.name 1) then
    if let .const c _ := nm.appArg!.getAppFn then ids := ids.push (mkCIdent c)
  let lemmas ← ids.mapM fun t => `(Lean.Parser.Tactic.simpLemma| $t:ident)
  evalTactic (← `(tactic|
    (intro _
     simp only [$(VV `Spec.env):ident, $lemmas,*, String.reduceEq, ↓reduceIte, ite_true, ite_false])))

/-- Discharge the argument-domain side condition from `IRType` instances. -/
macro "validate_dom" : tactic => `(tactic|
  repeat' (first
    | exact ($(VV `Callable.dom_base) _ _).mpr rfl
    | refine ($(VV `Callable.dom_arrow) _ _ _).mpr ⟨?_, ?_⟩
    | exact $(VV `IRType.realizes_of) (by decide) rfl))

/-- Split `AllValid P ρ fns` (for a concrete function list) into one goal per function. -/
partial def splitAllValid (g : MVarId) : MetaM (List MVarId) := do
  let t ← whnfD (← g.getType)
  if t.isConstOf ``True then
    g.assign (mkConst ``True.intro); return []
  if t.isAppOfArity ``And 2 then
    let a := t.appFn!.appArg!
    let b := t.appArg!
    let g1 ← mkFreshExprSyntheticOpaqueMVar a
    let g2 ← mkFreshExprSyntheticOpaqueMVar b
    g.assign (mkApp4 (mkConst ``And.intro) a b g1 g2)
    return g1.mvarId! :: (← splitAllValid g2.mvarId!)
  throwError "validate_prefixed: cannot unfold the function list{indentExpr t}"

/-- `PreFixed P (Spec.env S)` from one obligation per function of `P`. -/
syntax (name := validatePrefixed) "validate_prefixed" " [" term,* "]" : tactic

@[tactic validatePrefixed] def evalValidatePrefixed : Tactic := fun stx => do
  let obls : Array Term := stx[2].getSepArgs.map fun s => ⟨s⟩
  evalTactic (← `(tactic| apply $(VV `PreFixed.of_allValid):ident))
  let gs ← splitAllValid (← getMainGoal)
  -- index the obligations by the IR function they are about
  let mut table : Array (Name × Term) := #[]
  for o in obls do
    let ty ← withoutRecover <| Term.withoutErrToSorry do
      let e ← Term.elabTerm o none
      instantiateMVars (← inferType e)
    let some ev := ty.find? (fun x => x.isAppOf ``LeanGo.Validate.evalBody && x.getAppNumArgs ≥ 2)
      | throwError "validate_prefixed: not an obligation{indentExpr ty}"
    let .const c _ := ev.getAppArgs[1]!.getAppFn
      | throwError "validate_prefixed: obligation is not about a named IR function{indentExpr ty}"
    table := table.push (c, o)
  for g in gs do
    setGoals [g]
    let gty ← instantiateMVars (← g.getType)
    let fnC := match gty.getAppFn, gty.getAppArgs with
      | .const ``LeanGo.Validate.FnValid _, #[_, _, fn] => fn.getAppFn.constName?
      | _, _ => none
    let some c := fnC | throwError "validate_prefixed: unexpected goal{indentExpr gty}"
    let some (_, o) := table.find? (·.1 == c)
      | throwError "validate_prefixed: no obligation for {c}"
    evalTactic (← `(tactic|
      (refine $(VV `fnValid_of_obl) _ ?_ ?_ $o
       · validate_lookup
       · validate_dom)))
  setGoals []

end LeanGo.Validate.Meta
