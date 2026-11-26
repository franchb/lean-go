import Normalizer

namespace NormalizerContract

-- Independent requirements: these definitions do not import or reuse the proofs,
-- and the truth tables below are restated rather than taken from the runtime.

/-- Strong-Kleene negation keeps unknown and swaps false and true. -/
def neg : Normalizer.Tri → Normalizer.Tri
  | .f => .t
  | .u => .u
  | .t => .f

/-- Conjunction is the minimum in the order false < unknown < true. -/
def meet : Normalizer.Tri → Normalizer.Tri → Normalizer.Tri
  | .f, _ => .f
  | .u, .f => .f
  | .u, _ => .u
  | .t, b => b

/-- Disjunction is the maximum in the order false < unknown < true. -/
def join : Normalizer.Tri → Normalizer.Tri → Normalizer.Tri
  | .t, _ => .t
  | .u, .t => .t
  | .u, _ => .u
  | .f, b => b

/-- The meaning of an expression under a valuation of its variables. -/
def eval (rho : UInt32 → Normalizer.Tri) : Normalizer.Expr → Normalizer.Tri
  | .lit value => value
  | .var id => rho id
  | .neg e => neg (eval rho e)
  | .conj a b => meet (eval rho a) (eval rho b)
  | .disj a b => join (eval rho a) (eval rho b)

/-- Negation appears only directly on variables. -/
def IsNormalForm : Normalizer.Expr → Prop
  | .lit _ => True
  | .var _ => True
  | .neg (.var _) => True
  | .neg _ => False
  | .conj a b => IsNormalForm a ∧ IsNormalForm b
  | .disj a b => IsNormalForm a ∧ IsNormalForm b

def normalize_preserves_eval : Prop := ∀ (e : Normalizer.Expr) (rho : UInt32 → Normalizer.Tri),
  eval rho (Normalizer.normalize e) = eval rho e

def normalize_is_normal : Prop := ∀ e : Normalizer.Expr,
  IsNormalForm (Normalizer.normalize e)

end NormalizerContract
