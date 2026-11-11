import Std

namespace LeanGo.Feasibility

inductive Tri where
  | f | u | t
  deriving Repr, DecidableEq, BEq

namespace Tri

def neg : Tri → Tri
  | .f => .t
  | .u => .u
  | .t => .f

def meet : Tri → Tri → Tri
  | .f, _ => .f
  | .u, .f => .f
  | .u, _ => .u
  | .t, b => b

def join : Tri → Tri → Tri
  | .t, _ => .t
  | .u, .t => .t
  | .u, _ => .u
  | .f, b => b

theorem neg_neg (a : Tri) : neg (neg a) = a := by
  cases a <;> rfl

theorem neg_meet (a b : Tri) :
    neg (meet a b) = join (neg a) (neg b) := by
  cases a <;> cases b <;> rfl

theorem neg_join (a b : Tri) :
    neg (join a b) = meet (neg a) (neg b) := by
  cases a <;> cases b <;> rfl

end Tri

inductive Expr where
  | lit : Tri → Expr
  | var : UInt32 → Expr
  | neg : Expr → Expr
  | conj : Expr → Expr → Expr
  | disj : Expr → Expr → Expr
  deriving Repr, DecidableEq, BEq

def eval (rho : UInt32 → Tri) : Expr → Tri
  | .lit value => value
  | .var id => rho id
  | .neg e => Tri.neg (eval rho e)
  | .conj a b => Tri.meet (eval rho a) (eval rho b)
  | .disj a b => Tri.join (eval rho a) (eval rho b)

def normalizeAux (p : Bool) : Expr → Expr
  | .lit value => .lit (if p then Tri.neg value else value)
  | .var id => if p then .neg (.var id) else .var id
  | .neg e => normalizeAux (!p) e
  | .conj a b =>
      if p then .disj (normalizeAux true a) (normalizeAux true b)
      else .conj (normalizeAux false a) (normalizeAux false b)
  | .disj a b =>
      if p then .conj (normalizeAux true a) (normalizeAux true b)
      else .disj (normalizeAux false a) (normalizeAux false b)

def normalize (e : Expr) : Expr :=
  normalizeAux false e

def IsNormalForm : Expr → Prop
  | .lit _ => True
  | .var _ => True
  | .neg (.var _) => True
  | .neg _ => False
  | .conj a b => IsNormalForm a ∧ IsNormalForm b
  | .disj a b => IsNormalForm a ∧ IsNormalForm b

theorem normalizeAux_preserves_eval (e : Expr) (rho : UInt32 → Tri) :
    ∀ p, eval rho (normalizeAux p e) =
      if p then Tri.neg (eval rho e) else eval rho e := by
  induction e with
  | lit value =>
      intro p
      cases p <;> rfl
  | var id =>
      intro p
      cases p <;> rfl
  | neg e ih =>
      intro p
      cases p <;>
        simp [normalizeAux, eval, ih, Tri.neg_neg]
  | conj a b ihA ihB =>
      intro p
      cases p <;>
        simp [normalizeAux, eval, ihA, ihB, Tri.neg_meet]
  | disj a b ihA ihB =>
      intro p
      cases p <;>
        simp [normalizeAux, eval, ihA, ihB, Tri.neg_join]

theorem normalize_preserves_eval (e : Expr) (rho : UInt32 → Tri) :
    eval rho (normalize e) = eval rho e := by
  simpa [normalize] using normalizeAux_preserves_eval e rho false

theorem normalizeAux_is_normal (e : Expr) :
    ∀ p, IsNormalForm (normalizeAux p e) := by
  induction e with
  | lit value =>
      intro p
      cases p <;> exact True.intro
  | var id =>
      intro p
      cases p <;> exact True.intro
  | neg e ih =>
      intro p
      exact ih (!p)
  | conj a b ihA ihB =>
      intro p
      cases p with
      | false => exact ⟨ihA false, ihB false⟩
      | true => exact ⟨ihA true, ihB true⟩
  | disj a b ihA ihB =>
      intro p
      cases p with
      | false => exact ⟨ihA false, ihB false⟩
      | true => exact ⟨ihA true, ihB true⟩

theorem normalize_is_normal (e : Expr) :
    IsNormalForm (normalize e) :=
  normalizeAux_is_normal e false

end LeanGo.Feasibility
