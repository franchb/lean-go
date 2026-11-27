import LeanGo.Validate
import LeanGo.Validate.Meta
import Examples.Src.Trees

/-! Translation validation of `testdata/trees.ir.json` against
`examples/modules/Trees.lean`: a recursive sum with a self-recursive root. -/

namespace Examples.Trees
open LeanGo.Validate

derive_ir_encoding _root_.Trees.Color _root_.Trees.Tree

validate_ir_program prog from "IR/trees.ir.json"

def spec : List SpecEntry :=
  [.mk "Trees.recolor" _root_.Trees.recolor, .mk "Trees.mirror" _root_.Trees.mirror]

theorem recolor_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Trees.recolor») _root_.Trees.recolor := by
  validate_obl
theorem mirror_obl :
    Callable.Rel Eq (evalBody (Spec.env spec) prog.«Trees.mirror») _root_.Trees.mirror := by
  validate_obl

theorem prefixed : PreFixed prog (Spec.env spec) := by
  validate_prefixed [recolor_obl, mirror_obl]

theorem mirror_sound (n : Nat) : Callable.Rel Approx (prog.run n "Trees.mirror") _root_.Trees.mirror :=
  run_rel prefixed (name := "Trees.mirror") _ (by validate_lookup) n

/-- Spelled out: any tree the IR returns for `mirror t` is `encode (mirror t)`. -/
example (n : Nat) (t : _root_.Trees.Tree) (v : Value)
    (h : prog.run n "Trees.mirror" [Encode.encode t] = some v) :
    v = Encode.encode (_root_.Trees.mirror t) :=
  Option.some.inj ((((mirror_sound n) t).eq_of_some h).symm)

end Examples.Trees
