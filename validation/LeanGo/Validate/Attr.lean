module
public meta import Lean.Meta.Tactic.Simp.RegisterCommand
public meta section

/-!
# Simp set for IR symbolic evaluation

`ir_simp` collects the rewrite rules that symbolic evaluation of IR bodies
needs: the evaluator equations, primitive semantics, and the per-type
`encode`/`decode` equations produced by `derive_ir_encoding`. It is the only
meta-level declaration the object-level files import (to tag lemmas).
-/

register_simp_attr ir_simp
