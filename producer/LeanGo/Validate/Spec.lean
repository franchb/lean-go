module
public import LeanGo.Validate.Soundness
public meta import LeanGo.Validate.Attr

/-!
# Specification environments and per-function obligations

A source function `f : α₁ → … → αₙ → β` (all with `Encode` instances) is
turned into a partial function on IR argument lists by `Callable.call`:
decode each argument, apply `f`, encode the result; anything that does not
decode is `none`.

`Callable.Rel R g f` states `∀ a₁ … aₙ, R (g [encode a₁, …, encode aₙ]) (some (encode (f a₁ … aₙ)))`.
The per-function validation obligation is `Callable.Rel Eq (evalBody ρ fn) f`,
i.e. `∀ a, evalBody ρ fn (encode a) = some (encode (f a))`.

`fnValid_of_obl` turns an obligation into the pre-fixed-point condition for
that function: well-typed arguments are encodings (`Callable.Dom`, discharged
from `IRType.realizes`), so the obligation covers every argument list the
boundary admits.
-/

@[expose] public section

namespace LeanGo.Validate
open Encode

class Callable (φ : Type) where
  call : φ → List Value → Option Value
  /-- Relational agreement of an IR-level partial function with `f` on encoded arguments. -/
  Rel : (Option Value → Option Value → Prop) → (List Value → Option Value) → φ → Prop
  /-- The parameter type names are all realized by the source argument types. -/
  Dom : TypeTable → List String → Prop
  rel_congr : ∀ (R : Option Value → Option Value → Prop) (g g' : List Value → Option Value) (f : φ),
    (∀ args, g args = g' args) → Rel R g f → Rel R g' f
  call_rel : ∀ f : φ, Rel Eq (call f) f
  rel_le : ∀ (g g' : List Value → Option Value) (f : φ),
    (∀ args, g args ⊑ g' args) → Rel Eq g' f → Rel Approx g f
  sound : ∀ (tys : TypeTable) (Ts : List String) (g : List Value → Option Value) (f : φ),
    Dom tys Ts → Rel Eq g f → ∀ args, Value.hasTypes tys Ts args = true → g args ⊑ call f args

/-- A result value: no arguments left. -/
instance (priority := low) instCallableBase {β : Type} [Encode β] : Callable β where
  call b args := match args with
    | [] => some (encode b)
    | _ :: _ => none
  Rel R g b := R (g []) (some (encode b))
  Dom _ Ts := Ts = []
  rel_congr _ g g' _ h hr := by rw [← h]; exact hr
  call_rel _ := rfl
  rel_le g g' _ h hr := by
    show g [] ⊑ _
    have := h []; rw [hr] at this; exact this
  sound tys Ts g b hd hr args ht := by
    subst hd
    rw [Value.hasTypes_nil_iff] at ht
    subst ht
    show g [] ⊑ some (encode b)
    rw [show g [] = some (encode b) from hr]
    exact Approx.refl _

instance instCallableArrow {α φ : Type} [Encode α] [Callable φ] : Callable (α → φ) where
  call f args := match args with
    | [] => none
    | v :: vs => (decode v).bind fun a => Callable.call (f a) vs
  Rel R g f := ∀ a, Callable.Rel R (fun vs => g (encode a :: vs)) (f a)
  Dom tys Ts := match Ts with
    | [] => False
    | T :: Ts => Realizes tys T α ∧ Callable.Dom φ tys Ts
  rel_congr R g g' f h hr a := Callable.rel_congr R _ _ (f a) (fun vs => h _) (hr a)
  call_rel f a := by
    refine Callable.rel_congr Eq _ _ (f a) ?_ (Callable.call_rel (f a))
    intro vs; simp
  rel_le g g' f h hr a := Callable.rel_le _ _ (f a) (fun vs => h _) (hr a)
  sound tys Ts g f hd hr args ht := by
    match Ts, hd with
    | T :: Ts, ⟨hreal, hd⟩ =>
      rw [Value.hasTypes_cons_iff] at ht
      obtain ⟨v, vs, rfl, hv, hvs⟩ := ht
      obtain ⟨a, rfl⟩ := hreal v hv
      have := Callable.sound tys Ts (fun vs => g (encode a :: vs)) (f a) hd (hr a) vs hvs
      simpa using this

@[ir_simp] theorem Callable.call_base {β : Type} [Encode β] (b : β) :
    Callable.call b [] = some (encode b) := rfl
@[ir_simp] theorem Callable.call_nil {α φ : Type} [Encode α] [Callable φ] (f : α → φ) :
    Callable.call f [] = none := rfl
@[ir_simp] theorem Callable.call_cons {α φ : Type} [Encode α] [Callable φ] (f : α → φ)
    (v : Value) (vs : List Value) :
    Callable.call f (v :: vs) = (decode v).bind fun a => Callable.call (f a) vs := rfl

theorem Callable.rel_base {β : Type} [Encode β] (R : Option Value → Option Value → Prop)
    (g : List Value → Option Value) (b : β) :
    Callable.Rel R g b ↔ R (g []) (some (encode b)) := Iff.rfl
theorem Callable.rel_arrow {α φ : Type} [Encode α] [Callable φ] (R : Option Value → Option Value → Prop)
    (g : List Value → Option Value) (f : α → φ) :
    Callable.Rel R g f ↔ ∀ a, Callable.Rel R (fun vs => g (encode a :: vs)) (f a) := Iff.rfl
theorem Callable.dom_base {β : Type} [Encode β] (tys : TypeTable) (Ts : List String) :
    Callable.Dom β tys Ts ↔ Ts = [] := Iff.rfl
theorem Callable.dom_arrow {α φ : Type} [Encode α] [Callable φ] (tys : TypeTable) (T : String)
    (Ts : List String) :
    Callable.Dom (α → φ) tys (T :: Ts) ↔ Realizes tys T α ∧ Callable.Dom φ tys Ts := Iff.rfl

/-! ## Specification environments -/

/-- One source function, under its IR name. -/
structure SpecEntry where
  name : String
  {φ : Type}
  [inst : Callable φ]
  fn : φ

/-- The specification environment of a list of source functions. -/
def Spec.env : List SpecEntry → Callees
  | [], _, _ => none
  | e :: es, f, args => if e.name = f then e.inst.call e.fn args else Spec.env es f args

attribute [ir_simp] Spec.env

/-- From a per-function obligation to the pre-fixed-point condition. -/
theorem fnValid_of_obl {P : Program} {ρ : Callees} {fn : Function} {φ : Type} [Callable φ]
    (f : φ) (hρ : ∀ args, ρ fn.name args = Callable.call f args)
    (hdom : Callable.Dom φ P.types (fn.params.map (·.ty)))
    (hobl : Callable.Rel Eq (evalBody ρ fn) f) : FnValid P ρ fn := by
  intro args ht
  rw [hρ]
  exact Callable.sound _ _ _ f hdom hobl args ht

/-- Whole-program partial correctness for one entry point `name` implemented
by source function `f`: on encoded arguments, any value the IR program
returns (for any fuel) is the encoding of `f`'s result. -/
theorem run_rel {P : Program} {ρ : Callees} (hpre : PreFixed P ρ) {name : String} {φ : Type}
    [Callable φ] (f : φ) (hρ : ∀ args, ρ name args = Callable.call f args) (n : Nat) :
    Callable.Rel Approx (P.run n name) f :=
  Callable.rel_le _ _ f (fun args => (hρ args) ▸ run_le hpre n name args) (Callable.call_rel f)

/-- `run_rel` for an entry point whose call closure `S` is validated
(`PreFixedOn`); functions outside `S` are unconstrained. -/
theorem run_rel_on {P : Program} {ρ : Callees} {S : List String} (hpre : PreFixedOn P ρ S)
    {name : String} (hname : name ∈ S) {φ : Type} [Callable φ] (f : φ)
    (hρ : ∀ args, ρ name args = Callable.call f args) (n : Nat) :
    Callable.Rel Approx (P.run n name) f :=
  Callable.rel_le _ _ f (fun args => (hρ args) ▸ run_le_on hpre n name hname args)
    (Callable.call_rel f)

end LeanGo.Validate
