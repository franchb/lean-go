module
public meta import Lean
public import LeanGo.Validate.Spec
public meta section

/-!
# `derive_ir_encoding`

`derive_ir_encoding T₁ T₂ …` generates, for each monomorphic, non-parametric
inductive type `Tᵢ` (enums, structures, self-recursive sums), in order:

* `Tᵢ.irEncode : Tᵢ → Value` – constructor `k` with fields `x₁ … xₘ` becomes
  `Value.ctor k [encode x₁, …, encode xₘ]` (tags are Lean constructor indices);
* `Tᵢ.irDecode : Value → Option Tᵢ` and the law `Tᵢ.irDecode_irEncode`;
* `instance : Encode Tᵢ`;
* `@[ir_simp]` equations `Tᵢ.encode_<ctor>` / `Tᵢ.decode_<ctor>`;
* `Tᵢ.irDecl : TypeDecl` – the IR type declaration (as the extractor exports it);
* `Tᵢ.irRealizes` and `instance : IRType Tᵢ` – IR-typed values are encodings.

Field types must already have `Encode`/`IRType` instances (scalars, `Bool`,
`List α`, or earlier derived types); a field of type `Tᵢ` itself is handled by
recursion. Mutual and nested inductives are rejected.
-/

open Lean Elab Command Meta Term

namespace LeanGo.Validate.Meta

/-- The IR type name of a field type, as the extractor's `typeKey` spells it. -/
partial def irTypeName (e : Expr) : MetaM String := do
  let e ← whnfR e
  match e.getAppFn, e.getAppArgs with
  | .const ``List [_], #[elem] => return "List " ++ (← irTypeName elem)
  | .const n [], #[] => return n.toString
  | _, _ => throwError "derive_ir_encoding: unsupported field type {e}"

structure FieldInfo where
  ty : Expr
  tySyntax : Term
  irName : String
  isSelf : Bool
  deriving Inhabited

structure CtorInfo where
  name : Name
  idx : Nat
  fields : Array FieldInfo

def collectCtors (T : Name) : TermElabM (Array CtorInfo) := do
  let info ← getConstInfoInduct T
  unless info.numParams == 0 && info.numIndices == 0 && info.levelParams.isEmpty do
    throwError "derive_ir_encoding: {T} must be monomorphic and non-parametric"
  unless info.all.length == 1 do throwError "derive_ir_encoding: mutual inductive {T} unsupported"
  if info.isNested then throwError "derive_ir_encoding: nested inductive {T} unsupported"
  let mut out := #[]
  for h : i in [:info.ctors.length] do
    let c := info.ctors[i]
    let cinfo ← getConstInfoCtor c
    let fields ← forallTelescopeReducing cinfo.type fun xs _ => do
      xs.mapM fun x => do
        let ty ← inferType x
        let isSelf := ty.isConstOf T
        let tySyntax ← PrettyPrinter.delab ty
        let irName ← if isSelf then pure T.toString else irTypeName ty
        pure { ty, tySyntax, irName, isSelf : FieldInfo }
    out := out.push { name := c, idx := i, fields }
  if out.isEmpty then throwError "derive_ir_encoding: empty inductive {T} unsupported"
  return out

def V (n : Name) : Ident := mkCIdent (`LeanGo.Validate ++ n)

def deriveOne (T : Name) : CommandElabM Unit := do
  let ctors ← liftTermElabM (collectCtors T)
  let Tid := mkCIdent T
  let encId := mkIdent (T ++ `irEncode)
  let decId := mkIdent (T ++ `irDecode)
  let lawId := mkIdent (T ++ `irDecode_irEncode)
  let declId := mkIdent (T ++ `irDecl)
  let realId := mkIdent (T ++ `irRealizes)
  let valueT : Term := V `Value
  -- irEncode
  let mut encAlts : Array (TSyntax ``Lean.Parser.Term.matchAlt) := #[]
  for c in ctors do
    let xs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"x{i+1}")
    let encs ← c.fields.mapIdxM fun i f => do
      let x := xs[i]!
      if f.isSelf then `($encId $x) else `($(V `Encode.encode) $x)
    let pat ← `(@$(mkCIdent c.name) $xs*)
    encAlts := encAlts.push (← `(Lean.Parser.Term.matchAltExpr| | $pat => $(V `Value.ctor) $(quote c.idx) [$encs,*]))
  elabCommand (← `(command| def $encId : $Tid → $valueT $encAlts:matchAlt*))
  -- irDecode
  let mut decAlts : Array (TSyntax ``Lean.Parser.Term.matchAlt) := #[]
  for c in ctors do
    let vs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"v{i+1}")
    let xs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"x{i+1}")
    let mut body ← `(some (@$(mkCIdent c.name) $xs*))
    for i' in [:c.fields.size] do
      let i := c.fields.size - 1 - i'
      let f := c.fields[i]!
      let dec ← if f.isSelf then `($decId $(vs[i]!))
        else `(($(V `Encode.decode) $(vs[i]!) : Option $(f.tySyntax)))
      body ← `(Option.bind $dec fun $(xs[i]!) => $body)
    let pat ← `($(V `Value.ctor) $(quote c.idx) [$vs,*])
    decAlts := decAlts.push (← `(Lean.Parser.Term.matchAltExpr| | $pat => $body))
  decAlts := decAlts.push (← `(Lean.Parser.Term.matchAltExpr| | _ => none))
  elabCommand (← `(command| def $decId : $valueT → Option $Tid $decAlts:matchAlt*))
  -- law and instance
  elabCommand (← `(command|
    theorem $lawId (a : $Tid) : $decId ($encId a) = some a := by
      induction a <;> simp_all [$encId:ident, $decId:ident, $(V `Encode.decode_encode):ident]))
  elabCommand (← `(command|
    instance : $(V `Encode) $Tid := ⟨$encId, $decId, $lawId⟩))
  -- simp equations
  for c in ctors do
    let xs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"x{i+1}")
    let vs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"v{i+1}")
    let binders ← c.fields.mapIdxM fun i f =>
      `(Lean.Parser.Term.bracketedBinderF| ($(xs[i]!) : $(f.tySyntax)))
    let vbinders ← c.fields.mapIdxM fun i _ =>
      `(Lean.Parser.Term.bracketedBinderF| ($(vs[i]!) : $valueT))
    let encs ← xs.mapM fun x => `($(V `Encode.encode) $x)
    let encEq := mkIdent (T ++ Name.mkSimple s!"encode_{c.name.componentsRev.head!}")
    let decEq := mkIdent (T ++ Name.mkSimple s!"decode_{c.name.componentsRev.head!}")
    elabCommand (← `(command|
      @[ir_simp] theorem $encEq $binders* :
          $(V `Encode.encode) (@$(mkCIdent c.name) $xs*) = $(V `Value.ctor) $(quote c.idx) [$encs,*] := rfl))
    let mut body ← `(some (@$(mkCIdent c.name) $xs*))
    for i' in [:c.fields.size] do
      let i := c.fields.size - 1 - i'
      let f := c.fields[i]!
      body ← `(Option.bind ($(V `Encode.decode) $(vs[i]!) : Option $(f.tySyntax)) fun $(xs[i]!) => $body)
    elabCommand (← `(command|
      @[ir_simp] theorem $decEq $vbinders* :
          ($(V `Encode.decode) ($(V `Value.ctor) $(quote c.idx) [$vs,*]) : Option $Tid) = $body := by
        first | rfl | simp only [$(V `Encode.decode):ident, $decId:ident]))
  -- IR declaration
  let ctorDecls ← ctors.mapM fun c => do
    let fs := c.fields.map fun f => Syntax.mkStrLit f.irName
    `({ name := $(Syntax.mkStrLit c.name.toString), tag := $(quote c.idx), fields := [$fs,*] : $(V `CtorDecl)})
  elabCommand (← `(command|
    def $declId : $(V `TypeDecl) := { name := $(Syntax.mkStrLit T.toString), ctors := [$ctorDecls,*] }))
  -- decls closure: own declaration followed by the field types' declarations
  let mut fieldTys : Array Term := #[]
  let mut seen : Array Expr := #[]
  for c in ctors do
    for f in c.fields do
      unless f.isSelf || seen.contains f.ty do
        seen := seen.push f.ty
        fieldTys := fieldTys.push f.tySyntax
  let mut declsTerm ← `(([] : $(V `TypeTable)))
  for ty in fieldTys.reverse do
    declsTerm ← `($(V `IRType.decls) $ty ++ $declsTerm)
  declsTerm ← `($declId :: $declsTerm)
  -- realizability proof, one constructor at a time
  let hId := mkIdent `h
  let rflId := mkIdent `rfl
  let mut caseTacs : Array (TSyntax `tactic) := #[]
  for c in ctors do
    let vs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"v{i+1}")
    let hs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"h{i+1}")
    let xs := c.fields.mapIdx fun i _ => mkIdent (Name.mkSimple s!"x{i+1}")
    let mut tacs : Array (TSyntax `tactic) := #[]
    tacs := tacs.push (← `(tactic| cases hTs))
    tacs := tacs.push (← `(tactic| subst tag))
    tacs := tacs.push (← `(tactic|
      simp only [$(V `Value.hasTypes_cons_iff):ident, $(V `Value.hasTypes_nil_iff):ident] at hfs))
    for i in [:c.fields.size] do
      tacs := tacs.push (← `(tactic| obtain ⟨$(vs[i]!), _, $rflId:ident, $(hs[i]!), hfs⟩ := hfs))
    tacs := tacs.push (← `(tactic| obtain $rflId:ident := hfs))
    for i in [:c.fields.size] do
      let f := c.fields[i]!
      let realizer ← if f.isSelf then `(ih $(vs[i]!) (by simp) $(hs[i]!))
        else `($(V `IRType.realizes_of) (α := $(f.tySyntax)) (tys := tys)
                (fun d hd => $hId d (by simp [$declId:ident, hd])) rfl $(vs[i]!) $(hs[i]!))
      tacs := tacs.push (← `(tactic| obtain ⟨$(xs[i]!), $rflId:ident⟩ := $realizer))
    tacs := tacs.push (← `(tactic| exact ⟨@$(mkCIdent c.name) $xs*, rfl⟩))
    caseTacs := caseTacs.push (← `(tactic| ($tacs;*)))
  -- nest: split at hTs; · case_i; · (split at hTs ...)
  let mut proof ← `(tactic| cases hTs)
  for t in caseTacs.reverse do
    proof ← `(tactic| (split at hTs; (next => $t:tactic); (next => $proof:tactic)))
  let Tstr := Syntax.mkStrLit T.toString
  elabCommand (← `(command|
    theorem $realId (tys : $(V `TypeTable)) ($hId : $(V `Subtable) ($declsTerm) tys) :
        $(V `Realizes) tys $Tstr $Tid := by
      have hT : $(V `TypeTable.lookup) tys $Tstr = some ($declId).ctors := $hId $declId (by simp)
      apply $(V `Realizes.of_ctors) tys $Tstr ($declId).ctors hT rfl
      intro tag Ts fs hTs ih hfs
      simp only [$declId:ident, $(V `ctorsFields):ident] at hTs
      ($proof:tactic)))
  elabCommand (← `(command|
    instance : $(V `IRType) $Tid where
      irName := $Tstr
      decls := $declsTerm
      realizes := $realId))

syntax (name := deriveIREncoding) "derive_ir_encoding " ident+ : command

@[command_elab deriveIREncoding] def elabDeriveIREncoding : CommandElab := fun stx => do
  for id in stx[1].getArgs do
    let T ← liftCoreM <| realizeGlobalConstNoOverloadWithInfo id
    deriveOne T

end LeanGo.Validate.Meta
