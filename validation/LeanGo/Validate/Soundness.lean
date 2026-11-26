module
public import LeanGo.Validate.Semantics

/-!
# Soundness of modular validation (Park induction)

`x ⊑ y` on `Option α` means `x = none ∨ x = y`: `x` is "less defined" than
`y`. It is lifted pointwise to callee environments (`EnvLE`).

1. `evalBody_mono`: the body evaluator is monotone in the callee environment.
2. `run_le`: if `ρ` is a pre-fixed point of the program (`PreFixed P ρ`: for
   every function `fn` of the program and every argument list that is well
   typed for `fn`'s parameters, `evalBody ρ fn args ⊑ ρ fn.name args`), then
   for every fuel `n`, `P.run n ⊑ ρ`.
3. `run_sound` (partial correctness): whenever the IR program returns a value,
   it is the value `ρ` gives.

Nothing here says the program terminates or that `run` ever returns `some`;
see `docs/research/translation-validation.md`.
-/

@[expose] public section

namespace LeanGo.Validate

/-- `Approx x y` (`x ⊑ y`): `x` is undefined or equal to `y`. -/
def Approx {α : Type} (x y : Option α) : Prop := x = none ∨ x = y

@[inherit_doc] scoped infix:50 " ⊑ " => Approx

/-- Pointwise order on callee environments. -/
def EnvLE (ρ₁ ρ₂ : Callees) : Prop := ∀ f args, ρ₁ f args ⊑ ρ₂ f args

namespace Approx
variable {α β : Type}

theorem refl (x : Option α) : x ⊑ x := Or.inr rfl

theorem none_le (y : Option α) : (none : Option α) ⊑ y := Or.inl rfl

theorem of_eq {x y : Option α} (h : x = y) : x ⊑ y := Or.inr h

theorem trans {x y z : Option α} (h₁ : x ⊑ y) (h₂ : y ⊑ z) : x ⊑ z := by
  rcases h₁ with h | h
  · exact Or.inl h
  · subst h; exact h₂

/-- If `x ⊑ y` and `x` is defined, it agrees with `y`. -/
theorem eq_of_some {x y : Option α} {v : α} (h : x ⊑ y) (hx : x = some v) : y = some v := by
  rcases h with h | h
  · simp [h] at hx
  · rw [← h]; exact hx

theorem bind {x y : Option α} {f g : α → Option β} (hx : x ⊑ y) (hfg : ∀ a, f a ⊑ g a) :
    x.bind f ⊑ y.bind g := by
  rcases hx with h | h
  · subst h; exact Or.inl rfl
  · subst h
    cases x with
    | none => exact Or.inl rfl
    | some a => exact hfg a

end Approx

theorem EnvLE.refl (ρ : Callees) : EnvLE ρ ρ := fun _ _ => Approx.refl _

theorem EnvLE.trans {ρ₁ ρ₂ ρ₃ : Callees} (h₁ : EnvLE ρ₁ ρ₂) (h₂ : EnvLE ρ₂ ρ₃) : EnvLE ρ₁ ρ₃ :=
  fun f args => (h₁ f args).trans (h₂ f args)

/-- Join environments with the same ids and pointwise smaller continuations. -/
def JoinsLE : Joins → Joins → Prop
  | [], [] => True
  | (j₁, k₁) :: J₁, (j₂, k₂) :: J₂ => j₁ = j₂ ∧ (∀ args, k₁ args ⊑ k₂ args) ∧ JoinsLE J₁ J₂
  | _, _ => False

theorem JoinsLE.refl : ∀ J : Joins, JoinsLE J J
  | [] => trivial
  | (_, _) :: J => ⟨rfl, fun _ => Approx.refl _, JoinsLE.refl J⟩

theorem JoinsLE.get : ∀ {J₁ J₂ : Joins}, JoinsLE J₁ J₂ → ∀ j args,
    ((J₁.get j).bind fun k => k args) ⊑ ((J₂.get j).bind fun k => k args)
  | [], [], _, _, _ => Approx.refl _
  | (j₁, k₁) :: J₁, (j₂, k₂) :: J₂, ⟨hj, hk, hJ⟩, j, args => by
    subst hj
    simp only [Joins.get]
    split
    · exact hk args
    · exact JoinsLE.get hJ j args

theorem LetVal.eval_mono {ρ₁ ρ₂ : Callees} (h : EnvLE ρ₁ ρ₂) (E : Env) :
    ∀ v : LetVal, v.eval ρ₁ E ⊑ v.eval ρ₂ E
  | .lit _ => Approx.refl _
  | .prim _ _ => Approx.refl _
  | .ctor _ _ _ => Approx.refl _
  | .call f _ => Approx.bind (Approx.refl _) fun args => h f args

mutual
theorem Code.eval_mono {ρ₁ ρ₂ : Callees} (h : EnvLE ρ₁ ρ₂) :
    ∀ (c : Code) (E : Env) (J₁ J₂ : Joins), JoinsLE J₁ J₂ → c.eval ρ₁ E J₁ ⊑ c.eval ρ₂ E J₂
  | .ret x, E, J₁, J₂, _ => Approx.refl _
  | .bind p v next, E, J₁, J₂, hJ => by
    simp only [Code.eval]
    exact Approx.bind (LetVal.eval_mono h E v) fun val =>
      Code.eval_mono h next ((p.id, val) :: E) J₁ J₂ hJ
  | .cases x _ _ alts, E, J₁, J₂, hJ => by
    simp only [Code.eval]
    exact Approx.bind (Approx.refl _) fun v => Alts.eval_mono h alts v E J₁ J₂ hJ
  | .join j ps body next, E, J₁, J₂, hJ => by
    simp only [Code.eval]
    refine Code.eval_mono h next E _ _ ⟨rfl, ?_, hJ⟩
    intro args
    exact Approx.bind (Approx.refl _) fun E' => Code.eval_mono h body E' J₁ J₂ hJ
  | .jump j xs, E, J₁, J₂, hJ => by
    simp only [Code.eval]
    cases hx : E.getAll xs with
    | none =>
      have : ∀ J : Joins, ((J.get j).bind fun k => (none : Option (List Value)).bind k) = none := by
        intro J; cases J.get j <;> rfl
      rw [this, this]; exact Approx.refl _
    | some args =>
      have : ∀ J : Joins, ((J.get j).bind fun k => (some args).bind k) =
          ((J.get j).bind fun k => k args) := by
        intro J; rfl
      rw [this, this]; exact JoinsLE.get hJ j args

theorem Alts.eval_mono {ρ₁ ρ₂ : Callees} (h : EnvLE ρ₁ ρ₂) :
    ∀ (alts : Alts) (v : Value) (E : Env) (J₁ J₂ : Joins), JoinsLE J₁ J₂ →
      alts.eval ρ₁ v E J₁ ⊑ alts.eval ρ₂ v E J₂
  | .nil, _, _, _, _, _ => Approx.refl _
  | .cons _ tag ps body rest, v, E, J₁, J₂, hJ => by
    simp only [Alts.eval]
    split
    · split
      · exact Approx.bind (Approx.refl _) fun E' => Code.eval_mono h body E' J₁ J₂ hJ
      · exact Alts.eval_mono h rest v E J₁ J₂ hJ
    · exact Approx.refl _
end

/-- The body evaluator is monotone in the callee environment. -/
theorem evalBody_mono {ρ₁ ρ₂ : Callees} (h : EnvLE ρ₁ ρ₂) (fn : Function) (args : List Value) :
    evalBody ρ₁ fn args ⊑ evalBody ρ₂ fn args :=
  Approx.bind (Approx.refl _) fun E => Code.eval_mono h fn.body E [] [] trivial

/-- `ρ` is a pre-fixed point of the program's body functional on well-typed
arguments: one unfolding of any function, with callees interpreted by `ρ`,
is at most what `ρ` says about that function. -/
def PreFixed (P : Program) (ρ : Callees) : Prop :=
  ∀ fn ∈ P.functions, ∀ args, P.typed fn args = true → evalBody ρ fn args ⊑ ρ fn.name args

/-- **Park induction.** Every finite approximation of the program is below
any pre-fixed point. -/
theorem run_le {P : Program} {ρ : Callees} (h : PreFixed P ρ) : ∀ n, EnvLE (P.run n) ρ
  | 0 => fun _ _ => Approx.none_le _
  | n + 1 => by
    intro f args
    simp only [Program.run]
    split
    · rename_i fn hfn
      obtain ⟨hmem, hname⟩ := Program.find?_spec hfn
      split
      · rename_i htyped
        have h₁ := evalBody_mono (run_le h n) fn args
        have h₂ := h fn hmem args htyped
        rw [hname] at h₂
        exact h₁.trans h₂
      · exact Approx.none_le _
    · exact Approx.none_le _

/-- **Partial correctness.** Whenever the IR program returns a value, it is
the value the pre-fixed point (e.g. the specification environment) gives. -/
theorem run_sound {P : Program} {ρ : Callees} (h : PreFixed P ρ) {n : Nat} {f : String}
    {args : List Value} {v : Value} (hr : P.run n f args = some v) : ρ f args = some v :=
  (run_le h n f args).eq_of_some hr

/-- Per-function form of the pre-fixed-point condition. -/
def FnValid (P : Program) (ρ : Callees) (fn : Function) : Prop :=
  ∀ args, P.typed fn args = true → evalBody ρ fn args ⊑ ρ fn.name args

/-- Conjunction of `FnValid` over a list of functions, by recursion so that a
concrete program unfolds to a tuple of per-function facts. -/
def AllValid (P : Program) (ρ : Callees) : List Function → Prop
  | [] => True
  | fn :: fns => FnValid P ρ fn ∧ AllValid P ρ fns

theorem AllValid.mem {P : Program} {ρ : Callees} :
    ∀ {fns : List Function}, AllValid P ρ fns → ∀ fn ∈ fns, FnValid P ρ fn
  | [], _, _, hmem => by simp at hmem
  | g :: gs, h, fn, hmem => by
    simp only [List.mem_cons] at hmem
    rcases hmem with rfl | hmem
    · exact h.1
    · exact AllValid.mem h.2 fn hmem

theorem PreFixed.of_allValid {P : Program} {ρ : Callees} (h : AllValid P ρ P.functions) :
    PreFixed P ρ :=
  fun fn hmem => AllValid.mem h fn hmem

end LeanGo.Validate
