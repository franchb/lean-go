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

attribute [ir_simp] Env.get Env.getAll Env.bindParams Joins.get LetVal.eval Lit.toValue
  Code.eval Alts.eval evalBody

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
