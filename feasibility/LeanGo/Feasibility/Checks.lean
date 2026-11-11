import LeanGo.Feasibility.Model

open LeanGo.Feasibility

example :
    [Tri.neg .f, Tri.neg .u, Tri.neg .t] = [.t, .u, .f] := rfl

example :
    [Tri.meet .f .f, Tri.meet .f .u, Tri.meet .f .t,
     Tri.meet .u .f, Tri.meet .u .u, Tri.meet .u .t,
     Tri.meet .t .f, Tri.meet .t .u, Tri.meet .t .t] =
    [.f, .f, .f, .f, .u, .u, .f, .u, .t] := rfl

example :
    [Tri.join .f .f, Tri.join .f .u, Tri.join .f .t,
     Tri.join .u .f, Tri.join .u .u, Tri.join .u .t,
     Tri.join .t .f, Tri.join .t .u, Tri.join .t .t] =
    [.f, .u, .t, .u, .u, .t, .t, .t, .t] := rfl

def sample : Expr :=
  .neg (.conj (.var 7) (.disj (.lit .u) (.neg (.var 9))))

def expected : Expr :=
  .disj (.neg (.var 7)) (.conj (.lit .u) (.var 9))

example : normalize sample = expected := rfl
example : normalize (.neg (.neg (.neg (.var 7)))) = .neg (.var 7) := rfl
example : normalize (.neg (.lit .u)) = .lit .u := rfl
example : normalize (.var (0 : UInt32)) = .var (0 : UInt32) := rfl
example : normalize (.var (4294967295 : UInt32)) =
    .var (4294967295 : UInt32) := rfl

example (e : Expr) (rho : UInt32 → Tri) :
    eval rho (normalize e) = eval rho e := normalize_preserves_eval e rho
example (e : Expr) : IsNormalForm (normalize e) := normalize_is_normal e

#print axioms LeanGo.Feasibility.normalize_preserves_eval
#print axioms LeanGo.Feasibility.normalize_is_normal

def main : IO Unit := do
  unless normalize sample == expected do
    throw (IO.userError "unexpected normal form or operand order")
  let rho : UInt32 → Tri := fun _ => .u
  unless eval rho (normalize sample) == eval rho sample do
    throw (IO.userError "evaluation changed")
  IO.println "normalizer smoke: OK"
