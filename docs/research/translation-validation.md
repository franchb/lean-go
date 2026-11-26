# Translation validation for the module IR

Status: library and producer-side command (`#lean_go_validate`) in
[`producer/LeanGo/Validate/`](../../producer/LeanGo/Validate); evidence in
[`validation/`](../../validation); runner `scripts/validate_module.py`, coverage
`scripts/validation_coverage.py`. Not yet wired into the `lean-go` CLI (§11
says how).

Today extraction is *tested and trusted*: the producer pattern-matches the
compiler's mono LCNF, audits it, and emits JSON IR that the Go printer turns
into Go. This library gives the IR a semantics inside Lean and proves, per
extraction, that the IR program agrees with the source Lean functions. A
wrong lowering then fails to prove, and the compiler adapter (LCNF → IR) leaves
the trusted base for every function that validates.

## 1. Layout

```
producer/LeanGo/Validate.lean               object level (import from ordinary files)
producer/LeanGo/Validate/Attr.lean          the `ir_simp` simp set (meta, attribute only)
producer/LeanGo/Validate/Value.lean         values, IR type table, typing, Encode, IRType
producer/LeanGo/Validate/Prim.lean          primitive matrix, semantics, arithmetic normal forms
producer/LeanGo/Validate/Syntax.lean        deep embedding of the IR
producer/LeanGo/Validate/Semantics.lean     evaluator, program semantics, `Code.calls`
producer/LeanGo/Validate/Soundness.lean     Park induction (whole program and call-closed subsets)
producer/LeanGo/Validate/Spec.lean          spec environments, obligations, `run_rel`, `run_rel_on`
producer/LeanGo/Validate/Meta.lean          meta level: imports the three below
producer/LeanGo/Validate/Meta/Derive.lean   `derive_ir_encoding`
producer/LeanGo/Validate/Meta/Load.lean     JSON bridge (`elabProgramJson`, `validate_ir_program`)
producer/LeanGo/Validate/Meta/Tactic.lean   `validate_obl`, `validate_dom`, `validate_lookup`, `validate_prefixed`
producer/LeanGo/Validate/Meta/Axioms.lean   `#assert_standard_axioms` (build gate)
producer/LeanGo/Validate/Command.lean       `#lean_go_validate` (uses LeanGo.Module.Export)
validation/                                  Lake package requiring ../producer; evidence only
  Examples/*.lean, Examples/IR, Examples/Src   hand-written examples on copies of tracked IR
  Examples/Command.lean, Examples/Producer/    the command run inside the build (Local)
  Examples/AxiomCheck.lean                     axiom gate over LeanGo.Validate.* and Examples.*
scripts/validate_module.py                   export + validation in one fresh workspace
scripts/validation_coverage.py               the command over every tracked program
```

There is one copy of the library. Object-level files are `module` files with
`@[expose] public section` and import only object-level files and `Attr`
(`public meta import`); meta-level files are `public meta section`. Ordinary
(non-module) files, such as the export harness or user-facing proofs, import
`LeanGo.Validate` and, for automation, `LeanGo.Validate.Meta` or
`LeanGo.Validate.Command`.

Build: `cd validation && lake build` (the `Examples` target includes the axiom
gate and an in-build run of the command). Clean build of library, producer
modules the command needs, examples and gate: about 3 minutes of wall time on
the shared machine; incremental rebuilds of the examples take under a minute.

The producer workspace is assembled from an explicit list of producer files
(`export_module.PRODUCER_INPUTS`; in Go, the `producer/embed.go` glob
`LeanGo/Module/*.lean` plus `LeanGo/Proofs/Check.lean`). The validation files
are **not** in that list and not matched by that glob, so the inputs hashed
into every existing manifest are unchanged; a run that requests validation
copies them in additionally (`validate_module.VALIDATION_INPUTS`) and hashes
them into the validation report, never into the IR's `inputs`.

## 2. Value domain and IR embedding

```lean
inductive Value where
  | u8 (x : UInt8) | u16 (x : UInt16) | u32 (x : UInt32) | u64 (x : UInt64)
  | ctor (tag : Nat) (fields : List Value)
```

Scalars carry Lean's own fixed-width integers; a constructor value has a tag
(the Lean constructor index, as in the IR type table) and its fields. The IR
type table is `List TypeDecl`, `TypeDecl = {name, ctors : List {name, tag, fields : List String}}`.

| JSON `op` | Lean |
|---|---|
| `return` | `Code.ret x` |
| `let` (`uint8`…`uint64`, `primitive`, `constructor`, `call`) | `Code.bind p (LetVal.lit l \| .prim p xs \| .ctor name tag xs \| .call f xs) next` |
| `cases` | `Code.cases x ty result alts` with `Alts.cons ctor tag params body rest` |
| `join` | `Code.join j params body next` |
| `jump` | `Code.jump j args` |

`Function = {name, params : List Param, result, body}` and
`Program = {types, functions}`. Constructor tags are resolved from the type
table by the loader; the semantics uses tags only.

## 3. Semantics

Primitives (`Prim.eval`) are the complete unsigned matrix, defined with Lean's
`UInt8/16/32/64` operations: for each width `add sub mul div mod land lor xor
complement shiftLeft shiftRight decEq decLt decLe`, plus all twelve
conversions. Names follow `Primitives.lean` (`Prim.ofName? "UInt32.add" =
some (.bin .add .w32)`). Shift counts reduce modulo the width and `x / 0 = 0`,
`x % 0 = x`, as in Lean. The signed/integer primitives being added to
`Primitives.lean` are not in the matrix yet: the loader rejects an unknown
primitive name, which the command reports as `load_failed` (§11).

`Code.eval ρ c E J` evaluates a body under a callee environment
`ρ : Callees := String → List Value → Option Value`, a variable environment
and a join environment mapping join ids to closed continuations. It is total
(structural recursion on code); a join body sees its declaration scope plus
its parameters. Any failure is `none`. `evalBody ρ fn args` binds the
parameters and runs the body.

```lean
def Program.run (P : Program) : Nat → Callees
  | 0 => fun _ _ => none
  | n + 1 => fun f args => match P.find? f with
    | some fn => if P.typed fn args then evalBody (P.run n) fn args else none
    | none => none
```

`P.typed fn args` checks arguments against parameter types
(`Value.hasType P.types`). Ill-typed arguments at a function boundary are stuck.

## 4. The meta-theorem

`x ⊑ y` on `Option α` is `x = none ∨ x = y`, lifted pointwise to callee
environments.

```lean
def PreFixed (P : Program) (ρ : Callees) : Prop :=
  ∀ fn ∈ P.functions, ∀ args, P.typed fn args = true → evalBody ρ fn args ⊑ ρ fn.name args
theorem run_le {P ρ} (h : PreFixed P ρ) : ∀ n, EnvLE (P.run n) ρ
theorem run_sound … (hr : P.run n f args = some v) : ρ f args = some v
```

New for per-root statements: a call-closed subset `S` of function names.

```lean
def PreFixedOn (P : Program) (ρ : Callees) (S : List String) : Prop :=
  ∀ fn ∈ P.functions, fn.name ∈ S → (∀ f ∈ fn.body.calls, f ∈ S) ∧ FnValid P ρ fn
theorem run_le_on (h : PreFixedOn P ρ S) : ∀ n, ∀ f ∈ S, ∀ args, P.run n f args ⊑ ρ f args
theorem run_rel_on (hpre : PreFixedOn P ρ S) (hname : name ∈ S) (f : φ)
    (hρ : ∀ args, ρ name args = Callable.call f args) (n : Nat) :
    Callable.Rel Approx (P.run n name) f
```

`Code.calls` lists every callee of a body; the call-closedness side condition
is decided by the kernel on the concrete program. So a root gets its
whole-program theorem as soon as the functions it can reach are validated,
whatever happens to other functions of the same extraction.

What it covers: for every fuel, every validated entry point and every encoded
argument list, a defined IR result equals the encoded source result. What it
does **not** cover: termination or non-stuckness (a program that always
returns `none` satisfies it). That needs a separate progress argument.

## 5. Source encodings and the typed boundary

`class Encode α` (`encode`, `decode`, `decode (encode a) = some a`) with
instances for the scalars, `Bool` and `List α`. `derive_ir_encoding` generates
instances for monomorphic, non-parametric inductives with tags equal to
constructor indices, `@[ir_simp]` equations, the IR declaration `T.irDecl` and
`IRType T`, whose `realizes` proves that every IR-typed value is an encoding
whenever the declarations are in the program's table (`Subtable`, decided per
program). The typed boundary is therefore proved, not assumed.

## 6. Obligations and automation

The per-function obligation is

```lean
Callable.Rel Eq (evalBody (Spec.env spec) prog.«F») specF
-- ∀ a₁ … aₖ, evalBody ρ_spec fn [encode a₁, …, encode aₖ] = some (encode (specF a₁ … aₖ))
```

Callees are answered by the specification, so obligations are modular.
`fnValid_of_obl` turns an obligation plus `Callable.Dom` (from `IRType`) into
`FnValid`.

`validate_obl [extra]` is symbolic evaluation by `simp only` with `ir_simp`,
the IR function, the spec list and the source function unfolded once; stuck
`cases` on `encode t` split `t` (variables are destructed, computed terms
generalized, `decide p` turned into `p`/`¬p`); remaining source `if`/`match`
are split. Every step is an ordinary kernel-checked proof. Changes in this
round:

* **Joins are evaluated lazily (the `Joins.update` fix).** `Code.eval_join` is
  no longer in `ir_simp`. When evaluation reaches a closed join declaration,
  the tactic applies `Code.eval_join_exists`:
  `∃ k, (∀ args, k args = (E.bindParams ps args).bind (body.eval ρ · J)) ∧ (join j ps body next).eval ρ E J = next.eval ρ E ((j, k) :: J)`,
  obtains a fresh closure variable `k` with its defining equation, and
  continues. `simp` can no longer look inside the continuation, so the join
  body is neither copied into every enclosing closure nor simplified under its
  binder; it is evaluated only when a jump applies `k` to concrete argument
  values, and then only along the path the case splits select. A join under a
  binder (not observed in the tracked programs) falls back to the old
  unfolding. `Joins.update` (four mutually jumping joins, three Boolean splits)
  went from exhausting memory after about 12 minutes to 4–9 s, is proved by the
  general tactic and has moved to `validation/Examples/Joins.lean`; `Open/` is
  gone. All earlier examples still prove.
* **Arithmetic normal forms.** The compiler rewrites `x * 256` into `x <<< 8`
  (header, dns, udp, numbers, wide) and reduces out-of-range literals
  (`4294967297 : UInt32` becomes `1`). Both sides of an obligation are brought
  to one form: simprocs `shiftLitUIntN` rewrite a shift by a literal into a
  multiplication by the power of two (justified by the proved lemmas
  `UIntN.shiftLeft_lit : x <<< k = x * 2 ^ (k % w)`), `reduceUIntNLit`
  reduces literals modulo the width (a definitional step), and `UIntN.shiftLeft
  a b = a <<< b` etc. are `rfl` lemmas.
* Scalars are no longer destructed into their `BitVec` field (they are
  structures, too); this made kernel checking of large arithmetic bodies
  expensive.

## 7. `#lean_go_validate`

```
#lean_go_export_roots "ir.json" [R₁, …, Rₖ] "Module"      -- unchanged producer command
#lean_go_validate "ir.json" [R₁, …, Rₖ] "Module" report "validation.json"
  heartbeats 200000 timeout_ms 120000 [strict] [namespace N] [skip "F …"]
```

in the same environment, after the export command. Steps:

1. **Program term from the producer's value.** The command calls
   `exportProgramRoots module roots` (the function the export command writes)
   and records whether the file `ir.json` is byte-identical to
   `json.pretty ++ "\n"` of that value (`ir_file_matches`). The `Program` term
   is elaborated from the in-memory value by `elabProgramJson`, the same bridge
   `validate_ir_program` uses for files. Empty paths skip the file check / the
   report file.
2. **Encodings** for every table type except `Bool` and `List UInt8`
   (library instances), derived in field-dependency order; a type that cannot
   be derived marks every function using it `not_attempted`.
3. **Specifications** (§8) for every IR function, defined as
   `N.spec_i` when generated; the list `N.spec` holds one entry per function
   that has one.
4. **Obligations** `N.obl_i : Callable.Rel Eq (evalBody (Spec.env N.spec) N.prog.«F») spec_F`,
   each proved by `validate_obl [all generated specs, all reduced-helper owners]`
   under the budget: `maxHeartbeats` (thousands, as the option) counted from
   the start of the obligation, and a wall-clock limit enforced by a
   cancellation token set by a timer thread. The proof is added with
   `addDecl` (so the kernel checks it) only if no goals remain, it has no
   metavariables, no `sorry`, the kernel accepts it, and `collectAxioms` reports
   only `propext`, `Classical.choice`, `Quot.sound`. Messages from failed
   attempts are discarded; exceptions (including heartbeat exhaustion and the
   interrupt) are caught and reported. A goal reported stuck is retried once
   with the owner's non-recursive same-module source definitions also
   unfolded (the compiler inlines them). Nothing here can fail the extraction.
5. **Whole program per root** whose call closure (computed from the IR) is
   entirely proved: `N.prefixed_k : PreFixedOn N.prog (Spec.env N.spec) S`
   (by `validate_prefixed_on`), `N.sound_k : ∀ n, Callable.Rel Approx (N.prog.run n "R") R`
   (by `run_rel_on`) and the explicit form
   `N.explicit_k : ∀ n a₁ … aₖ v, N.prog.run n "R" [encode a₁, …, encode aₖ] = some v → v = encode (R a₁ … aₖ)`.
6. **Report** (below). With `strict`, any function not proved, any root
   without its theorem, or an IR file mismatch is an error after the report is
   written.

`N` defaults to `LeanGo.Validated` (users cannot declare `LeanGo.*`; their
sources cannot import the library either, so no user-supplied `Encode`
instance can interfere).

### Report (`validation.json`, schema `lean-go-validation/v1`)

```jsonc
{
  "schema": "lean-go-validation/v1",          // added by the runner
  "module": "Local", "roots": ["Local.run"],
  "status": "completed",                       // or "export_failed" / "load_failed" with "reason"
  "ir_file_matches": true,                     // null when no file was named
  "ir_sha256": "…",                            // runner: hash of the completed IR it published
  "budget": {"heartbeats": 200000, "timeout_ms": 120000}, "strict": false,
  "lean_version": "4.36.0", "lean_commit": "…", // runner
  "inputs": {"source/Local.lean": "…", "producer/LeanGo/Validate/Command.lean": "…", …},
  "program": "LeanGo.Validated.prog", "specification_list": "LeanGo.Validated.spec",
  "encodings": [{"type": "Local.Count", "status": "available", "reason": ""}],
  "functions": [{
    "name": "Local.run._lam_0", "params": ["Local.Config", "UInt32"], "result": "UInt32",
    "kind": "local_helper",                    // source | byte_fold | reduced_helper | local_helper
    "source_owner": "Local.run",
    "specification": "LeanGo.Validated.spec_2: lambda bump of Local.run, captures [config]",
    "status": "proved",                        // proved | failed | not_attempted
    "reason": "",                              // stuck goal, "heartbeat budget exhausted", "wall-clock budget exhausted", …
    "time_ms": 901, "heartbeats_used": 2105,   // thousands
    "theorem": "LeanGo.Validated.obl_2",       // null unless proved
    "statement": "LeanGo.Validate.Callable.Rel Eq (…) LeanGo.Validated.spec_2",
    "axioms": ["propext", "Classical.choice", "Quot.sound"]}],
  "programs": [{
    "root": "Local.run", "closure": ["Local.run", "Local.run._lam_0", "Local.add"],
    "status": "proved",                        // proved | not_established (+ "missing") | failed (+ "reason")
    "prefixed_theorem": "…prefixed_0", "theorem": "…sound_0", "statement": "∀ (n : Nat), …",
    "explicit_theorem": "…explicit_0", "explicit_statement": "∀ (n : Nat) (config : Local.Config) …",
    "axioms": [...], "time_ms": 412}],
  "summary": {"functions": 3, "proved": 3, "failed": 0, "not_attempted": 0,
              "roots": 1, "roots_proved": 1, "time_ms": 4438}
}
```

Statements are pretty-printed with full names; the theorem names identify the
kernel-checked declarations in the harness environment.

## 8. Pairing IR functions with specifications, and what is trusted in it

The kind of each IR function is taken only from the producer's own validated
evidence, the same calls `resolveFunction`/`exportFunction` make:

| Kind | Evidence | Specification |
|---|---|---|
| `byte_fold` | provenance record `fold? = (acc, step)` | the constant `F` itself (`List.foldl step` over the bytes) |
| `local_helper` | `localEvidence? F` (owner, capture list, source witness) | the source lambda of the owner's `let` (found through the witness, or the alias the producer accepted), captures in the producer's order as leading parameters, adapted to mono types |
| `reduced_helper` | `reducedEvidence? F` (owner, retained positions) | `fun retained… => owner a₁ … aₙ` with removed arguments fixed to an arbitrary inhabitant |
| `source` | none of the above | the constant `F`; its source signature must spell exactly the IR signature |

Mono boundaries (a helper parameter of IR type `UInt32` whose source type is
the single-field wrapper `Count`) are adapted through the producer's
`representation` wrapper chain: the parameter is re-wrapped with the wrapper
constructors, the result unwrapped with the projections. Sources and roots are
never adapted.

What is trusted, and how it is checked:

* **Roots.** The whole-program theorem names the root's *own source constant*
  (resolved from the command's identifiers, the same ones the export used) and
  the IR function of the same name, `prog.run n "R"`. The spec entry for `"R"`
  must be `.mk "R" R` (kernel-checked by `validate_lookup`); a root that the
  evidence classifies as a compiler helper is refused. Nothing else about the
  pairing enters the statement.
* **Helpers are not trusted at all.** The pre-fixed-point argument holds for
  any specification environment, so a helper paired with a wrong specification
  can only make its obligation (and the roots that call it) fail to prove; it
  cannot make a false root statement provable. The report's per-function
  `proved` means exactly the printed statement: agreement with the printed
  specification.
* **No weaker statements.** Obligations, `PreFixedOn`, `sound` and `explicit`
  statements are built by the command from fixed templates (Expr level, not
  syntax a source could influence), and the proof is checked against that
  type by `addDecl`.
* **Encodings.** One instance per type, derived from the source declaration;
  `Subtable` (decided per program) forces the IR table to agree with the
  derived declaration, so the encoding in the statement is the IR's.
* Remaining trusted pieces are those of §10: the semantics, the JSON bridge,
  the Go printer/runtime, and that `exportProgramRoots` recomputed in the same
  environment is the value the export command wrote (checked byte-for-byte
  when the file is named).

## 9. Coverage

`uv run --locked python scripts/validation_coverage.py --out-dir <dir>` runs the
command (default budget: 200000 thousand heartbeats, Lean's default `maxHeartbeats`, and 120 s per obligation)
on every tracked program, sequentially, checking that the IR exported in the
validation workspace is byte-identical to the tracked IR. Results of the run on
the shared 4-core machine (validation time is the command's own time, without
the workspace build):

| Program | IR | Functions proved / total | Roots with whole-program theorem | Validation time |
|---|---|---|---|---|
| choice | testdata/choice | 1 / 1 | 1 / 1 | 0.8 s |
| counters | testdata/counters | 5 / 5 | 4 / 4 | 4.2 s |
| joins | testdata/joins | 2 / 2 | 1 / 1 | 5.8 s |
| local | testdata/local | 3 / 3 (incl. lifted `_lam_0` with capture) | 1 / 1 | 3.3 s |
| numbers | testdata/numbers | 2 / 2 | 1 / 1 | 5.7 s |
| records | testdata/records | 3 / 3 | 1 / 1 | 4.4 s |
| reduced | testdata/reduced | 3 / 3 (incl. two `._redArg`) | 1 / 1 | 3.5 s |
| trees | testdata/trees | 2 / 2 | 1 / 1 | 2.7 s |
| wide | testdata/wide | 2 / 2 | 1 / 1 | 6.2 s |
| quota | libraries/quota | 4 / 4 | 4 / 4 | 5.8 s |
| clamp | examples/proofs/clamp | 2 / 2 | 1 / 1 | 0.9 s |
| flags | examples/proofs/flags | 4 / 4 | 4 / 4 | 2.3 s |
| words | examples/proofs/words | 5 / 5 | 5 / 5 | 3.2 s |
| bytes | examples/proofs/bytes | 4 / 4 | 4 / 4 | 6.0 s |
| header | examples/proofs/header | 2 / 2 | 2 / 2 | 5.0 s |
| wire16 | examples/proofs/wire16 | 5 / 5 | 5 / 5 | 9.2 s |
| crc16 | examples/proofs/crc16 | 3 / 3 (byte fold) | 2 / 2 | 2.6 s |
| adler32 | examples/proofs/adler32 | 5 / 5 (byte fold) | 3 / 3 | 4.0 s |
| dns | examples/formats/dns | 13 / 14 | 2 / 3 | 58.8 s |
| header (format) | examples/formats/header | 7 / 7 | 4 / 4 | 7.8 s |
| udp | examples/formats/udp | 7 / 7 | 4 / 4 | 8.2 s |
| **total** | 21 programs | **84 / 85** | **52 / 53** | |

In every run the IR exported in the validation workspace has the same
`module`, `roots`, `types`, `functions` and `api` as the tracked IR, and the
command's recomputed value matched the file the export wrote
(`ir_file_matches`). The tracked files are no longer byte-identical to the
Python runner's output only because the tracked artifacts are now produced by
the Go pipeline, whose `inputs` inventory differs from `export_module.py`'s.
No program needed more than 13.5 M heartbeats for one obligation (budget:
200 M); the slowest single obligation is `DnsHeader.serialize` (≈40 s on the
loaded machine).

`checkedframe` has no IR of its own: it is the composition of `wire16` and
`crc16`, both completely validated. The pilot `normalizer` fixture uses the
pilot IR, not the module IR, and is out of scope.

### Failure classes

Every failure observed during this work, with the fix or the open status. No
failure has been a counterexample (a false obligation from a wrong lowering).

1. **Join blowup** (`Joins.update`): evaluation cost, fixed by lazy join
   continuations (§6). Before: out of memory after ~12 min; now ~6 s.
2. **Compiler arithmetic rewrites** (header `HeaderCodec.parse`, dns/udp
   `be16w`, numbers/wide `compute`): the IR has `x <<< 8` where the source has
   `x * 256`, `UInt32.shiftLeft a b` where the source has `a <<< b`'s
   instance, `1` where the source has `4294967297 : UInt32`. The goal was stuck
   at an equality that is true but not syntactic, e.g.
   `Value.u32 (b2.toUInt32 <<< 8 + b3.toUInt32) = Value.u32 (b2.toUInt32 * 256 + b3.toUInt32)`.
   Fixed by the literal normal forms (§6); all five now prove.
3. **Compiler inlining of source callees** (format `DemoHeader.classify`,
   `serialize`, after the merged source change): the IR computes
   `payloadLength x` inline while the source side keeps the call, stuck at
   `ctor 1 [u32 x.toUInt32] = ctor 1 [u32 (DemoHeader.payloadLength x)]`.
   Fixed by one retry that also unfolds the owner's non-recursive same-module
   source definitions; only a stuck goal is retried, never an exhausted budget.
4. **Reduced-helper call sites** (`Reduced.run._redArg`, first command run):
   the helper's specification fixes removed arguments to an arbitrary
   inhabitant, the source call sites pass `⟨0⟩`; stuck until the owner is
   unfolded. Fixed: owners of reduced helpers are unfolded in every obligation.
5. **Kernel memory exhaustion, open** (`DnsHeader.word1`, a 7-term straight-line
   sum of shifted bytes): elaboration of the proof finishes quickly within the
   heartbeat budget, but the kernel check of the proof term grows past 12 GB and
   the process is killed (exit 137); `be16w`, the same pattern with one term,
   checks instantly, so it is some interaction of the six literal-shift steps
   with kernel defeq checking that has not been isolated. Heartbeats do not
   bound kernel memory. The runner therefore runs validation in its own `lean`
   process after the export (the IR is never affected), reads the progress file
   to see which obligation was in progress, reruns with it skipped
   (`skip "F"`), and reports it `failed` with
   `validation process exited (137) while proving this obligation (memory limit)`.
   `DnsHeader.serialize` calls `word1`, so its root has no whole-program
   theorem; `parse` and `validate` do.
6. **Kernel cost of a default-transparency `rfl` fallback**: the tactic's last
   resort was `rfl`, which can prove an arithmetic goal by evaluation that the
   kernel then re-does on symbolic operands. It is now `with_reducible rfl`
   (no tracked obligation depended on the stronger form). Scalars are also no
   longer destructed into `BitVec` fields.

Machine note: on the shared machine the container's memory cgroup also killed
`lean` processes of these runs for reasons unrelated to the proofs (several
agents compiling at once). The coverage script retries a program once after
exit 137 and records `killed_for_memory` distinctly; the final table above was
produced sequentially, one program at a time, with no such kill except
`DnsHeader.word1`, which reproduces when run alone.

## 10. What remains trusted

1. The Lean kernel and the three standard axioms; the axiom gate
   (`validation/Examples/AxiomCheck.lean`, `#assert_standard_axioms
   LeanGo.Validate Examples`) fails the build on any other axiom, `sorry`,
   `native_decide`/`bv_decide` (`Lean.ofReduceBool`, `Lean.trustCompiler`) or
   a declared axiom. It covers the library, the command's meta code, the
   examples and — through `Examples/Command.lean`, which runs the command with
   `strict` inside the build — everything the command adds (2741
   declarations). In an extraction, the command applies the same check to
   every theorem it adds before reporting it as proved.
2. The semantics (`Value`, `Prim.eval`, `Code.eval`, `Program.run`,
   `Value.hasType`): it must describe what the Go printer implements.
3. The JSON bridge `elabProgramJson`.
4. The IR-to-Go printer and the Go runtime; the Go compiler.

The LCNF → IR lowering and the compiler passes are no longer trusted for the
functional behaviour of validated roots. They remain trusted for the
*selection* of what is exported (which roots).

## 11. Integration with the CLI (`internal/pipeline/export.go`)

What to embed. `producer/LeanGo/Validate.lean`, `producer/LeanGo/Validate/*.lean`
and `producer/LeanGo/Validate/Meta/*.lean` (14 files, listed in
`validate_module.VALIDATION_INPUTS`). They depend only on Lean core and
`LeanGo.Module.*` (the command imports `LeanGo.Module.Export`), never on user
sources, so they belong in the **cached producer build**; key that cache entry
by their digests in addition to the module producer's. Embed them under a
*separate* glob/accessor (e.g. `ValidationFiles()`), not in `ModuleFiles()`,
so that exports without validation keep hashing exactly the current inputs and
the tracked manifests stay unchanged. What is compiled per extraction is only
the validation harness: derived encodings, the program term and all theorems live in the
harness environment and are not written to an olean.

The invocation, mirroring `ExportModule` and `scripts/validate_module.py`:

1. Stage sources and `ModuleFiles()` as today, plus `ValidationFiles()`.
2. `lake build LeanGo.Module.Export LeanGo.Validate LeanGo.Validate.Command <source modules…>`
   (the extra targets are cacheable).
3. Run the export harness exactly as today. Then write a second harness
   (`LeanGo/Module/Validate.lean`) and run it in its **own** `lake env lean`
   process, so that validation (including the process being killed for
   memory) can never affect the IR or the admission diagnostics:
   ```
   import LeanGo.Module.Export
   import LeanGo.Validate
   import LeanGo.Validate.Command
   import <Module>
   #lean_go_validate "ir.json" [<roots, sorted>] "<Module>" report "validation.json" heartbeats <H> timeout_ms <T>[ strict][ skip "F …"]
   ```
   The command recomputes the export value in this environment and compares
   it with the `ir.json` the first process wrote. It appends progress lines
   (`<ms> ms proving F`, `<ms> ms F: proved in … ms`, phase names) to
   `validation.json.progress`. If the process dies without a report, the last
   `proving F` line names the obligation; rerun with `skip "F"` (accumulating,
   at most a few times) and mark F `failed` with the exit status, as
   `validate_module.py` does. Allow for the extra time in the process timeout
   (dns takes about a minute of proving).
4. Complete the IR exactly as today; its `inputs` remain the export inputs.
   Read `validation.json` (bounded size), check `module`/`roots`, add
   `schema`, `lean_version`, `lean_commit`, `ir_sha256` (of the completed IR)
   and `inputs` (sources plus export and validation files), and publish it next
   to the IR. Fixed budget defaults: `heartbeats 200000`, `timeout_ms 120000`;
   record them (they are in the report). Heartbeats make the verdict
   deterministic; the wall-clock limit is a safety net, and a verdict reached
   only because of it is visible as `wall-clock budget exhausted`.
5. A failed or unattempted obligation is data. Only `strict` (for profiles that
   require validation) turns it into an error; then the harness exits non-zero
   *after* writing the report, so the report can still be shown.

For the manifest, record per root the `programs[].status`, theorem names and
`ir_sha256`/report digest; per function the status. Exit code 137 from `lean`
is the container's memory limit, not a verdict (the coverage script retries once
and reports `killed_for_memory`).

## 12. Limits and open items

* Partial correctness only (§4).
* Primitives outside the unsigned matrix (signed integers being added to
  `Primitives.lean`) are rejected by the loader until `Prim.lean` and the
  semantics are extended; the command then reports `load_failed`.
* `Encode` covers non-parametric inductives without nesting or mutual
  recursion (the extractor does not admit them either).
* Symbolic evaluation is `simp`-based; cost grows with the number of case
  splits and with arithmetic expression size, and the kernel re-checks every
  step (see the dns failure class). Source functions using well-founded
  recursion would need `unfold`-aware handling; none of the tracked ones do.
* Local helpers: the source lambda is identified through the producer's
  witness and capture names; a capture whose name is ambiguous among the
  lambda's free variables makes that helper `not_attempted` (not observed).
