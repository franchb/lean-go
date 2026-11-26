module
public import LeanGo.Validate.Syntax
public meta import LeanGo.Validate.Attr

/-!
# Semantics

* `Code.eval ρ c E J` evaluates a body under a callee environment
  `ρ : Callees`, a variable environment `E` and a join environment `J` that
  maps join ids to their (already closed) continuations. It is total: it is
  defined by structural recursion on the code, and anything that goes wrong
  (unbound variable, ill-typed primitive, no matching alternative, arity
  mismatch, failing callee) yields `none`.
* `evalBody ρ fn args` binds the parameters and runs the body.
* `Program.run P fuel` ties the knot through the program's own function
  table: a call consumes one unit of fuel; out of fuel, unknown function or an
  ill-typed argument list at the boundary is `none`.
-/

@[expose] public section

namespace LeanGo.Validate

/-- Callee environment: function name ↦ partial function on argument lists. -/
abbrev Callees := String → List Value → Option Value

/-- Variable environment, most recent binding first. -/
abbrev Env := List (Nat × Value)

/-- Join environment: join id ↦ continuation. -/
abbrev Joins := List (Nat × (List Value → Option Value))

def Env.get : Env → Nat → Option Value
  | [], _ => none
  | (y, v) :: E, x => if y = x then some v else Env.get E x

def Env.getAll (E : Env) : List Nat → Option (List Value)
  | [] => some []
  | x :: xs => (E.get x).bind fun v => (Env.getAll E xs).bind fun vs => some (v :: vs)

/-- Bind parameters left to right; arity mismatch is stuck. -/
def Env.bindParams : Env → List Param → List Value → Option Env
  | E, [], [] => some E
  | E, p :: ps, v :: vs => Env.bindParams ((p.id, v) :: E) ps vs
  | _, _, _ => none

def Joins.get : Joins → Nat → Option (List Value → Option Value)
  | [], _ => none
  | (k, f) :: J, j => if k = j then some f else Joins.get J j

def LetVal.eval (ρ : Callees) (E : Env) : LetVal → Option Value
  | .lit l => some l.toValue
  | .prim p xs => (E.getAll xs).bind p.eval
  | .ctor _ tag xs => (E.getAll xs).bind fun vs => some (.ctor tag vs)
  | .call f xs => (E.getAll xs).bind (ρ f)

mutual
def Code.eval (ρ : Callees) : Code → Env → Joins → Option Value
  | .ret x, E, _ => E.get x
  | .bind p v next, E, J => (v.eval ρ E).bind fun val => Code.eval ρ next ((p.id, val) :: E) J
  | .cases x _ _ alts, E, J => (E.get x).bind fun v => Alts.eval ρ alts v E J
  | .join j ps body next, E, J =>
    Code.eval ρ next E ((j, fun args => (E.bindParams ps args).bind fun E' => Code.eval ρ body E' J) :: J)
  | .jump j xs, E, J => (J.get j).bind fun k => (E.getAll xs).bind k

/-- Select the first alternative whose tag is the scrutinee's tag. -/
def Alts.eval (ρ : Callees) : Alts → Value → Env → Joins → Option Value
  | .nil, _, _, _ => none
  | .cons _ tag ps body rest, v, E, J =>
    match v.view with
    | some (t, fs) =>
      if tag = t then (E.bindParams ps fs).bind fun E' => Code.eval ρ body E' J
      else Alts.eval ρ rest v E J
    | none => none
end

/-- Run one function body with the given arguments. -/
def evalBody (ρ : Callees) (fn : Function) (args : List Value) : Option Value :=
  (Env.bindParams [] fn.params args).bind fun E => fn.body.eval ρ E []

/-! Evaluation equations for symbolic evaluation. They are proved by rewriting
(not by `rfl`) on purpose: `simp` then records them as propositional steps,
so the kernel instantiates lemmas instead of re-running the evaluator on large
terms when it checks a proof. -/
section
variable (ρ : Callees) (E : Env) (J : Joins)
@[ir_simp] theorem Env.get_nil (x : Nat) : Env.get [] x = none := by rw [Env.get]
@[ir_simp] theorem Env.get_cons (y : Nat) (v : Value) (x : Nat) :
    Env.get ((y, v) :: E) x = if y = x then some v else Env.get E x := by rw [Env.get]
@[ir_simp] theorem Env.getAll_nil : E.getAll [] = some [] := by rw [Env.getAll]
@[ir_simp] theorem Env.getAll_cons (x : Nat) (xs : List Nat) :
    E.getAll (x :: xs) = (E.get x).bind fun v => (E.getAll xs).bind fun vs => some (v :: vs) := by
  rw [Env.getAll]
@[ir_simp] theorem Env.bindParams_nil_nil : Env.bindParams E [] [] = some E := by rw [Env.bindParams]
@[ir_simp] theorem Env.bindParams_cons_cons (p : Param) (ps : List Param) (v : Value) (vs : List Value) :
    Env.bindParams E (p :: ps) (v :: vs) = Env.bindParams ((p.id, v) :: E) ps vs := by
  rw [Env.bindParams]
@[ir_simp] theorem Joins.get_nil (j : Nat) : Joins.get [] j = none := by rw [Joins.get]
@[ir_simp] theorem Joins.get_cons (k : Nat) (f : List Value → Option Value) (j : Nat) :
    Joins.get ((k, f) :: J) j = if k = j then some f else Joins.get J j := by rw [Joins.get]
@[ir_simp] theorem Lit.toValue_u8 (x : UInt8) : (Lit.u8 x).toValue = .u8 x := by rw [Lit.toValue]
@[ir_simp] theorem Lit.toValue_u16 (x : UInt16) : (Lit.u16 x).toValue = .u16 x := by rw [Lit.toValue]
@[ir_simp] theorem Lit.toValue_u32 (x : UInt32) : (Lit.u32 x).toValue = .u32 x := by rw [Lit.toValue]
@[ir_simp] theorem Lit.toValue_u64 (x : UInt64) : (Lit.u64 x).toValue = .u64 x := by rw [Lit.toValue]
@[ir_simp] theorem LetVal.eval_lit (l : Lit) : (LetVal.lit l).eval ρ E = some l.toValue := by
  rw [LetVal.eval]
@[ir_simp] theorem LetVal.eval_prim (p : Prim) (xs : List Nat) :
    (LetVal.prim p xs).eval ρ E = (E.getAll xs).bind p.eval := by rw [LetVal.eval]
@[ir_simp] theorem LetVal.eval_ctor (n : String) (tag : Nat) (xs : List Nat) :
    (LetVal.ctor n tag xs).eval ρ E = (E.getAll xs).bind fun vs => some (.ctor tag vs) := by
  rw [LetVal.eval]
@[ir_simp] theorem LetVal.eval_call (f : String) (xs : List Nat) :
    (LetVal.call f xs).eval ρ E = (E.getAll xs).bind (ρ f) := by rw [LetVal.eval]
@[ir_simp] theorem Code.eval_ret (x : Nat) : (Code.ret x).eval ρ E J = E.get x := by rw [Code.eval]
@[ir_simp] theorem Code.eval_bind (p : Param) (v : LetVal) (next : Code) :
    (Code.bind p v next).eval ρ E J =
      (v.eval ρ E).bind fun val => next.eval ρ ((p.id, val) :: E) J := by rw [Code.eval]
@[ir_simp] theorem Code.eval_cases (x : Nat) (ty r : String) (alts : Alts) :
    (Code.cases x ty r alts).eval ρ E J = (E.get x).bind fun v => alts.eval ρ v E J := by
  rw [Code.eval]
@[ir_simp] theorem Code.eval_join (j : Nat) (ps : List Param) (body next : Code) :
    (Code.join j ps body next).eval ρ E J =
      next.eval ρ E ((j, fun args => (E.bindParams ps args).bind fun E' => body.eval ρ E' J) :: J) := by
  rw [Code.eval]
@[ir_simp] theorem Code.eval_jump (j : Nat) (xs : List Nat) :
    (Code.jump j xs).eval ρ E J = (J.get j).bind fun k => (E.getAll xs).bind k := by rw [Code.eval]
@[ir_simp] theorem Alts.eval_nil (v : Value) : Alts.nil.eval ρ v E J = none := by rw [Alts.eval]
@[ir_simp] theorem Alts.eval_cons (c : String) (tag : Nat) (ps : List Param) (body : Code)
    (rest : Alts) (v : Value) :
    (Alts.cons c tag ps body rest).eval ρ v E J =
      match v.view with
      | some (t, fs) =>
        if tag = t then (E.bindParams ps fs).bind fun E' => body.eval ρ E' J
        else rest.eval ρ v E J
      | none => none := by rw [Alts.eval]
@[ir_simp] theorem evalBody_eq (fn : Function) (args : List Value) :
    evalBody ρ fn args = (Env.bindParams [] fn.params args).bind fun E => fn.body.eval ρ E [] := by
  rw [evalBody]
end

/-- First function of the program with the given name. -/
def Program.find? (P : Program) (f : String) : Option Function :=
  P.functions.find? (·.name == f)

/-- Arguments are well typed for the function's parameter types. -/
def Program.typed (P : Program) (fn : Function) (args : List Value) : Bool :=
  Value.hasTypes P.types (fn.params.map (·.ty)) args

/-- Fuel-indexed whole-program semantics. -/
def Program.run (P : Program) : Nat → Callees
  | 0 => fun _ _ => none
  | n + 1 => fun f args =>
    match P.find? f with
    | some fn => if P.typed fn args then evalBody (P.run n) fn args else none
    | none => none

theorem Program.find?_spec {P : Program} {f : String} {fn : Function}
    (h : P.find? f = some fn) : fn ∈ P.functions ∧ fn.name = f := by
  unfold Program.find? at h
  have hm := List.mem_of_find?_eq_some h
  have hn := List.find?_some h
  simp at hn
  exact ⟨hm, hn⟩

end LeanGo.Validate
