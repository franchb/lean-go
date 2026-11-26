# Translation validation for the module IR

Status: research library, not wired into the producer. Code: [`validation/`](../../validation).

Today extraction is *tested and trusted*: the producer pattern-matches the
compiler's mono LCNF, audits it, and emits JSON IR that the Go printer turns
into Go. This note describes a Lean library that gives the IR a semantics
inside Lean and proves, per extraction, that the IR program agrees with the
source Lean functions. A wrong lowering then fails to prove, and the compiler
adapter (LCNF → IR) leaves the trusted base.

## 1. Layout

```
validation/
  lakefile.toml, lean-toolchain        Lean 4.36.0, no dependencies beyond core
  LeanGo/Validate.lean                 object level (import this from user files)
  LeanGo/Validate/Attr.lean            the `ir_simp` simp set (meta, attribute only)
  LeanGo/Validate/Value.lean           values, IR type table, typing, Encode, IRType
  LeanGo/Validate/Prim.lean            primitive matrix and its semantics
  LeanGo/Validate/Syntax.lean          deep embedding of the IR
  LeanGo/Validate/Semantics.lean       evaluator and program semantics
  LeanGo/Validate/Soundness.lean       Park induction, partial correctness
  LeanGo/Validate/Spec.lean            spec environments, obligations, glue lemmas
  LeanGo/Validate/Meta.lean            meta level: imports the three below
  LeanGo/Validate/Meta/Derive.lean     `derive_ir_encoding`
  LeanGo/Validate/Meta/Load.lean       `validate_ir_program` (JSON bridge)
  LeanGo/Validate/Meta/Tactic.lean     `validate_obl`, `validate_dom`, `validate_lookup`, `validate_prefixed`
  LeanGo/Validate/Meta/Axioms.lean     `#assert_standard_axioms` (build gate)
  Examples/                            evidence on tracked IR (default build target)
  Open/                                stated but unproved obligations (non-default target)
  Examples/IR/*.ir.json, Examples/Src/*.lean   copies of tracked IR and sources
  check_ir_copies.py                   checks the copies against the tracked files
```

All library files use the module system (`module`, `@[expose] public section`
for object-level definitions, `public meta section` for elaborators and
tactics), so they can move unchanged to `producer/LeanGo/Validate/` under the
namespace `LeanGo.Validate`. Object-level files import only object-level files
and the attribute file; ordinary (non-module) user files import
`LeanGo.Validate` and, for automation, `LeanGo.Validate.Meta`.

Build: `cd validation && lake build` (both default targets; the example
target includes the axiom gate). A clean build of the library and the ten
validated example programs takes about 46 s of wall time (40 s CPU) on the
shared 4-core machine. `lake build Open` builds the one open statement.

## 2. Value domain and IR embedding

```lean
inductive Value where
  | u8 (x : UInt8) | u16 (x : UInt16) | u32 (x : UInt32) | u64 (x : UInt64)
  | ctor (tag : Nat) (fields : List Value)
```

Scalars carry Lean's own fixed-width integers; a constructor value has a tag
(the Lean constructor index, as in the IR type table) and its fields. The IR
type table is `List TypeDecl`, `TypeDecl = {name, ctors : List {name, tag, fields : List String}}`.

The embedding has exactly the IR's operations:

| JSON `op` | Lean |
|---|---|
| `return` | `Code.ret x` |
| `let` (`uint8`…`uint64`, `primitive`, `constructor`, `call`) | `Code.bind p (LetVal.lit l \| .prim p xs \| .ctor name tag xs \| .call f xs) next` |
| `cases` | `Code.cases x ty result alts` with `Alts.cons ctor tag params body rest` |
| `join` | `Code.join j params body next` |
| `jump` | `Code.jump j args` |

`Function = {name, params : List Param, result, body}` and
`Program = {types, functions}`. Constructor tags are resolved from the type
table by the loader and stored next to the names; the semantics uses tags only.

## 3. Semantics

Primitives (`Prim.eval`) are the complete unsigned matrix, defined with Lean's
`UInt8/16/32/64` operations: for each width `add sub mul div mod land lor xor
complement shiftLeft shiftRight decEq decLt decLe`, plus all twelve conversions
`UIntA.toUIntB` with `A ≠ B`. Names follow `Primitives.lean`
(`Prim.ofName? "UInt32.add" = some (.bin .add .w32)`). Shift counts reduce
modulo the width and `x / 0 = 0`, `x % 0 = x`, because that is what Lean does.
Comparisons return the IR `Bool` (`encode (decide (a < b))`). Several of these
(`div`, `mod`, `UInt8.toUInt64`, …) are not admitted by the extractor yet.

`Code.eval ρ c E J` evaluates a body with a callee environment
`ρ : Callees := String → List Value → Option Value`, a variable environment
`E : List (Nat × Value)` and a join environment `J` mapping join ids to closed
continuations. It is total (structural recursion on the code); a join's body
is evaluated in the scope of its declaration plus its parameters and cannot
jump to itself, as in LCNF. Any failure — unbound variable, ill-typed
primitive, no alternative for the tag, arity mismatch, failing callee — is
`none`. `evalBody ρ fn args` binds the parameters and runs the body.

```lean
def Program.run (P : Program) : Nat → Callees
  | 0 => fun _ _ => none
  | n + 1 => fun f args => match P.find? f with
    | some fn => if P.typed fn args then evalBody (P.run n) fn args else none
    | none => none
```

`P.typed fn args` checks the argument list against the parameter types with
the structural judgement `Value.hasType P.types` (scalars by width, table
types by tag and recursively by fields). Ill-typed arguments at a function
boundary are stuck. `let` binders and `cases` result types are not checked
inside a body; they carry no meaning for the value computed.

## 4. The meta-theorem

`x ⊑ y` on `Option α` is `x = none ∨ x = y` (`Approx`), lifted pointwise to
callee environments (`EnvLE ρ₁ ρ₂ := ∀ f args, ρ₁ f args ⊑ ρ₂ f args`).

```lean
theorem evalBody_mono {ρ₁ ρ₂ : Callees} (h : EnvLE ρ₁ ρ₂) (fn : Function) (args : List Value) :
    evalBody ρ₁ fn args ⊑ evalBody ρ₂ fn args

def PreFixed (P : Program) (ρ : Callees) : Prop :=
  ∀ fn ∈ P.functions, ∀ args, P.typed fn args = true → evalBody ρ fn args ⊑ ρ fn.name args

theorem run_le {P : Program} {ρ : Callees} (h : PreFixed P ρ) : ∀ n, EnvLE (P.run n) ρ

theorem run_sound {P : Program} {ρ : Callees} (h : PreFixed P ρ) {n : Nat} {f : String}
    {args : List Value} {v : Value} (hr : P.run n f args = some v) : ρ f args = some v
```

`run_le` is Park induction: by induction on fuel, using monotonicity for the
recursive calls and the pre-fixed-point property for one unfolding.
`run_sound` is the corollary: **whenever the IR program returns a value, it is
the value the specification environment gives** (partial correctness).

For a specification environment built from source functions, `Spec.lean`
packages this per entry point:

```lean
theorem run_rel {P : Program} {ρ : Callees} (hpre : PreFixed P ρ) {name : String} {φ : Type}
    [Callable φ] (f : φ) (hρ : ∀ args, ρ name args = Callable.call f args) (n : Nat) :
    Callable.Rel Approx (P.run n name) f
```

which unfolds to `∀ a₁ … aₖ, P.run n name [encode a₁, …, encode aₖ] ⊑ some (encode (f a₁ … aₖ))`.

What this covers: for every fuel, every entry point and every encoded (or,
via `Realizes`, every well-typed) argument list, a defined IR result equals the
encoded source result. What it does **not** cover: termination or
non-stuckness. A program that always returns `none` satisfies the theorem.
Totality of the IR (that `run` returns `some` for enough fuel, given the
resource limits) needs a separate proof, e.g. an evaluator-side
progress/ranking argument or an equality-based obligation plus a termination
measure from the source's well-founded recursion; this is not done.

## 5. Source encodings and the typed boundary

```lean
class Encode (α : Type) where
  encode : α → Value
  decode : Value → Option α
  decode_encode : ∀ a, decode (encode a) = some a
```

Instances: `UInt8/16/32/64` (scalars), `Bool` (`false` ↦ `ctor 0 []`,
`true` ↦ `ctor 1 []`), `List α` (`List.nil` = 0, `List.cons` = 1, as the IR's
`List UInt8`).

`derive_ir_encoding T₁ T₂ …` generates instances for monomorphic,
non-parametric inductives (enums, structures, self-recursive sums), with tags
equal to constructor indices, together with the law, `@[ir_simp]`
encode/decode equations per constructor, and the IR declaration `T.irDecl`.

The boundary checks IR types, obligations quantify over source values. The
bridge is `Realizes tys T α := ∀ v, Value.hasType tys T v = true → ∃ a : α, encode a = v`.
The class `IRType α` records the IR name, the declarations the type needs and
a proof of `Realizes` whenever those declarations are in the table
(`Subtable decls tys`, decided by `decide` per program). It is proved for the
scalars, `Bool` and `List α`, and *generated with its proof* by
`derive_ir_encoding` (strong induction on values, `Realizes.of_ctors`). So the
typed boundary is proved, not assumed, for every type the examples use.

## 6. Obligations and automation

`Callable φ` turns a curried source function into a partial function on
argument lists (`call`: decode each argument, apply, encode) and gives the
relational statement `Callable.Rel R g f`. The per-function obligation is

```lean
Callable.Rel Eq (evalBody (Spec.env spec) prog.«F») f
-- unfolds to ∀ a₁ … aₖ, evalBody ρ_spec fn [encode a₁, …, encode aₖ] = some (encode (f a₁ … aₖ))
```

with `spec : List SpecEntry` (name ↦ source function) and
`Spec.env spec` the specification environment. Callees are interpreted by the
specification, so each obligation is modular: recursive and helper calls are
answered by `encode (g …)`, never unfolded. `fnValid_of_obl` turns an
obligation plus the argument-domain fact `Callable.Dom` (from `Realizes`) into
the pre-fixed-point condition for that function.

Automation (all of it produces ordinary kernel-checked proofs):

* `validate_ir_program prog from "file.ir.json"` defines `prog.types`, one
  `prog.«F» : Function` per IR function and `prog : Program`, and emits a
  `decide` check that each embedded primitive has its IR name.
* `validate_obl [extra]` symbolic evaluation: destruct structure-typed
  arguments, then `simp only` with `ir_simp`,
  the IR function, the spec list and the source function (unfolded once);
  when evaluation is stuck on `cases` of `encode t`, it case-splits `t`
  (destructs an argument variable, or generalizes a computed value such as
  `decide (a ≤ b)` and turns the hypothesis into `a ≤ b` / `¬a ≤ b`); remaining
  source-side `if`/`match` are split; repeat.
* `validate_dom`, `validate_lookup`, `validate_prefixed [o₁, …]` discharge
  the side conditions and assemble `PreFixed prog (Spec.env spec)`.

A typical example file is about 30 lines (see `validation/Examples/Adler32.lean`).

## 7. What remains trusted

1. The Lean kernel (and the three standard axioms `propext`,
   `Classical.choice`, `Quot.sound`; the build fails on any other axiom).
2. The semantics definition itself (`Value`, `Prim.eval`, `Code.eval`,
   `Program.run`, `Value.hasType`): it must describe what the Go printer
   implements. It is short and reviewable, but it is a specification.
3. The JSON bridge (`validate_ir_program`): that the `Program` term is the
   JSON file (it reads only `types` and `functions`, resolves tags from the
   table, range-checks literals, and rejects unknown primitives).
4. The IR-to-Go printer and the Go runtime (limits, import/export of values,
   typed wrappers).
5. The Go compiler and runtime.

The LCNF → IR lowering (`Export.lean`, `Records.lean`, `Reduced.lean`, …) and
the compiler passes that produced the mono LCNF are no longer trusted for
functional behaviour of validated functions: a wrong lowering makes the
obligation false. They remain trusted for the *selection* of what is exported
(which roots, the declared parameter types) insofar as the spec list is built
from the same names.

## 8. Limits found

* Partial correctness only (§4).
* Spec environment by name: the specification of each IR function is a source
  function chosen by the integration. Compiler helpers without a source owner
  (`._redArg`) need a specification derived from the owner and the
  retained-argument map (`Examples/Reduced.lean` shows the shape).
* `Encode` covers non-parametric inductives; nested and mutual inductives are
  rejected by the deriving command (the extractor does not admit them either).
* Typed boundary: `Value.hasType` looks types up by name in the program's
  table; `Subtable` must hold between the derived declarations and the table
  (checked by `decide`). A table that diverges from the source types makes the
  side condition fail, which is the desired behaviour.
* Symbolic evaluation is `simp`-based: cost grows with code size after join
  inlining and with the number of case splits (each scrutinee doubles the
  goals). `Joins.update` (four mutually referencing joins, five Boolean
  splits) is the stress case; see the coverage table.
* Unfolding source definitions relies on `simp` equation lemmas. Source
  functions with `match` compile to equations that `simp` can use;
  well-founded recursion would need `unfold`/`decreasing_by`-aware handling.
* Defeq pitfalls: comparing obligations against goals by unification can make
  Lean evaluate source functions; the tactics therefore match by constant names
  and close spec lookups by `simp`, never by `rfl` on unknown terms.

## 9. Coverage

Every function of every listed program, with IR copied from the tracked file
and source copied verbatim (Clamp: two files merged). "Auto" means the
obligation is proved by `validate_obl` alone (Reduced: with the specs of
`._redArg` helpers given as extra unfolding lemmas), the program-level
`PreFixed` by `validate_prefixed`, and the whole-program statement by
`run_rel`.

| Program | IR | Functions | Obligations | Whole program |
|---|---|---|---|---|
| clamp | examples/proofs/clamp | `Clamp.Bounds.cap`, `Clamp.clamp` | auto | `clamp_sound` |
| flags | examples/proofs/flags | `set`, `clear`, `toggle`, `contains` | auto | 4 × `_sound` |
| words | examples/proofs/words | `widen`, `narrow`, `pack`, `high`, `low` | auto | 5 × `_sound` |
| crc16 | examples/proofs/crc16 | `step`, `updateByte`, `update` (fold) | auto | 3 × `_sound` |
| adler32 | examples/proofs/adler32 | `initial`, `reduce`, `step`, `update` (fold), `checksum` | auto | `initial/update/checksum_sound` |
| quota | testdata/quota | `create`, `reserve`, `release`, `remaining` | auto | 4 × `_sound` |
| trees | testdata/trees | `recolor`, `mirror` (recursive ADT) | auto | `mirror_sound` |
| counters | testdata/counters | `step`, `initial`, `advance`, `read`, `total` | auto | `advance_sound` |
| records | testdata/records | `addCount`, `flip`, `update` | auto | `update_sound` |
| reduced | testdata/reduced | `add._redArg`, `run._redArg`, `run` | auto (helper specs supplied) | `run_sound` |
| joins | testdata/joins | `add`, `update` (4 joins) | `add` auto; `update` **OPEN** | not derived (depends on the open obligation) |

**Open: `Joins.update`** (`validation/Open/Joins.lean`, non-default target
`lake build Open`, the only file that uses `sorry`). The body declares four
join points whose continuations jump to each other; the evaluator inlines a
join body at every jump site and the three Boolean splits multiply the goals.
With the default heartbeat limit `simp` times out after the first split; with
no limit the Lean process ran out of memory after about 12 minutes. This is a
cost failure of the tactic; no stuck (false) goal was observed. Fix
candidates: evaluate each join body once, symbolically in its parameters, and
reuse that equation at the jumps; or split the Boolean arguments before
unfolding joins. `Records.update`, `Reduced.run._redArg` and
`Counters.advance` (one join each, or none, but with record wrappers and
calls) are proved.

Other tracked programs (`bytes`, `header`, `wire16`, `checkedframe`,
`numbers`, `wide`, `local`, `choice`, `normalizer`) were not attempted.

The axiom gate (`Examples/AxiomCheck.lean`) checks every declaration of the
library and of the examples (about 2100 declarations, including generated
equation lemmas and instances) and reports only `propext`,
`Classical.choice` and `Quot.sound`.

## 10. Guidance for the integration task

Per extraction the producer should emit, next to the IR, one Lean file:

1. `import LeanGo.Validate` and `LeanGo.Validate.Meta`, plus the user source.
2. `derive_ir_encoding` for the ADT closure in dependency order — exactly the
   types it already enumerates in `exportTypes` (skipping `Bool`, scalars and
   `List UInt8`, which have library instances).
3. `validate_ir_program prog from "<ir>.json"` on the file it just wrote (or,
   better, generate the `Program` term directly from the same in-memory JSON so
   the bridge is a pretty-printer of one data structure).
4. `def spec : List SpecEntry` with one entry per IR function:
   * a source declaration: `.mk "<name>" <name>`;
   * a reduced helper: `.mk "<owner>._redArg" (fun retained… => owner (default for removed)…)`
     from the retained-argument map in `Reduced.lean`;
   * a lifted local helper: the lifted lambda with the capture order from
     `LocalSource.lean`;
   * a byte fold: the root itself (its IR is the recursion over `List UInt8`).
5. One theorem per function, `theorem <f>_obl : Callable.Rel Eq (evalBody (Spec.env spec) prog.«F») f := by validate_obl`,
   then `theorem prefixed : PreFixed prog (Spec.env spec) := by validate_prefixed [...]`
   and `run_rel` per root.
6. Build with `lake env lean` under the pinned toolchain and run
   `#assert_standard_axioms` on the generated module.

Manifest. Record per function: `validated: true|false`, the theorem name,
the Lean/toolchain commit, the hash of the generated validation file and the
axioms reported. When a proof fails, do not fail the extraction by default;
record `validated: false` with the obligation statement, the first stuck goal
printed by `validate_obl` (it reports `stuck at …`) or the elaboration error,
and the reason class (`stuck_simp`, `timeout`, `missing_spec`,
`realizes_failed`, `loader_rejected`). A root is validated only when every
function in its call closure is. Profiles that require validation can then
turn `validated: false` into an admission failure.

Determinism: the generated file depends only on the IR and the source names,
so it can be hashed alongside the Go output; keep `maxHeartbeats` explicit in
the generated file so that a slower machine does not change the verdict.
