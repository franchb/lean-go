module

/-!
# Propositional evaluation steps

`validate_obl` evaluates by `simp only`. A lemma whose proof is `rfl` is used
by `simp` as a *definitional* step: it rewrites the goal and records no proof,
so the kernel has to rediscover the step by checking that the term before and
the term after are definitionally equal. That is harmless when the two sides
have different head symbols, but it is costly when they share one, because the
kernel first compares the arguments of the two applications pairwise and only
unfolds after that fails. For the core lemma

    Option.bind_some : (some a).bind f = f a      -- proved by rfl

the right-hand side of a symbolic evaluation step is again an `Option.bind`
(the rest of the body), so the kernel compares `some a` with the next
computation and `f` with *its* continuation. Those comparisons fail, but only
after the kernel has reduced both sides, which includes fixed-width arithmetic
on symbolic operands: `x * 32768 : UInt32` reduces through `BitVec` to
`Nat.mul`, whose literal argument the kernel unfolds one successor at a time.
The cost is linear in the literal and multiplies with every nested step, which
is how a 7-term sum of shifted bytes (`DnsHeader.word1`) needed more than
12 GB of kernel memory.

The lemmas below restate those core `rfl` lemmas with a proof that is not
`rfl`, so that `simp` applies them as ordinary rewrites with an explicit proof
term, and the kernel only checks syntactic instances. The statements are
identical to the core ones; only how `simp` uses them changes.
-/

@[expose] public section

namespace LeanGo.Validate.Steps

theorem bind_some {α β : Type _} (a : α) (f : α → Option β) : (some a).bind f = f a :=
  Eq.mpr (congrArg (· = f a) (Option.bind_some a f)) (Eq.refl (f a))

theorem foldl_cons {α β : Type _} (f : α → β → α) (init : α) (b : β) (l : List β) :
    (b :: l).foldl f init = l.foldl f (f init b) :=
  Eq.mpr (congrArg (· = l.foldl f (f init b)) List.foldl_cons) (Eq.refl _)

theorem ite_true {α : Sort _} (a b : α) : (if True then a else b) = a :=
  Eq.mpr (congrArg (· = a) (ite_eq_left trivial : (if True then a else b) = a)) (Eq.refl a)

theorem ite_false {α : Sort _} (a b : α) : (if False then a else b) = b :=
  Eq.mpr (congrArg (· = b) (ite_eq_right not_false : (if False then a else b) = b)) (Eq.refl b)

end LeanGo.Validate.Steps
