import NormalizerContract

namespace NormalizerProofs
open Normalizer

private theorem neg_agrees (a : Tri) : Tri.neg a = NormalizerContract.neg a := by
  cases a <;> rfl

private theorem neg_neg (a : Tri) : NormalizerContract.neg (NormalizerContract.neg a) = a := by
  cases a <;> rfl

private theorem neg_meet (a b : Tri) :
    NormalizerContract.neg (NormalizerContract.meet a b) =
      NormalizerContract.join (NormalizerContract.neg a) (NormalizerContract.neg b) := by
  cases a <;> cases b <;> rfl

private theorem neg_join (a b : Tri) :
    NormalizerContract.neg (NormalizerContract.join a b) =
      NormalizerContract.meet (NormalizerContract.neg a) (NormalizerContract.neg b) := by
  cases a <;> cases b <;> rfl

private theorem aux_preserves_eval (e : Expr) (rho : UInt32 → Tri) :
    ∀ p, NormalizerContract.eval rho (normalizeAux p e) =
      if p then NormalizerContract.neg (NormalizerContract.eval rho e)
      else NormalizerContract.eval rho e := by
  induction e with
  | lit value =>
      intro p
      cases p <;> simp [normalizeAux, NormalizerContract.eval, neg_agrees]
  | var id =>
      intro p
      cases p <;> rfl
  | neg e ih =>
      intro p
      cases p <;> simp [normalizeAux, NormalizerContract.eval, ih, neg_neg]
  | conj a b ihA ihB =>
      intro p
      cases p <;> simp [normalizeAux, NormalizerContract.eval, ihA, ihB, neg_meet]
  | disj a b ihA ihB =>
      intro p
      cases p <;> simp [normalizeAux, NormalizerContract.eval, ihA, ihB, neg_join]

theorem normalize_preserves_eval : NormalizerContract.normalize_preserves_eval := by
  intro e rho
  simpa [normalize] using aux_preserves_eval e rho false

private theorem aux_is_normal (e : Expr) :
    ∀ p, NormalizerContract.IsNormalForm (normalizeAux p e) := by
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

theorem normalize_is_normal : NormalizerContract.normalize_is_normal :=
  fun e => aux_is_normal e false

end NormalizerProofs
