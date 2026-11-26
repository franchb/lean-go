namespace Normalizer

/-- Strong-Kleene truth values, ordered false < unknown < true. -/
inductive Tri where
  | f | u | t

namespace Tri

def neg : Tri → Tri
  | .f => .t
  | .u => .u
  | .t => .f

end Tri

/-- Three-valued logic expressions over `UInt32` variable identifiers. -/
inductive Expr where
  | lit : Tri → Expr
  | var : UInt32 → Expr
  | neg : Expr → Expr
  | conj : Expr → Expr → Expr
  | disj : Expr → Expr → Expr

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

/-- Push negations onto variables and evaluate negated literals. -/
def normalize (e : Expr) : Expr :=
  normalizeAux false e

end Normalizer
