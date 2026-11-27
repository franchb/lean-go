# Translation validation for the module IR

Status: library and producer-side command (`#lean_go_validate`) in
[`producer/LeanGo/Validate/`](../../producer/LeanGo/Validate); evidence in
[`validation/`](../../validation). Wired into the Go CLI:
`lean-go generate --validate report|require` and `lean-go validate`
([cli.md](../cli.md#translation-validation)), runner
`internal/pipeline/validate_*.go` (§11). Every tracked module program has a
tracked, deterministic report next to its IR (§9), written by
`lean-go regenerate`. The Python runner and coverage script are gone.

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
producer/LeanGo/Validate/Steps.lean         evaluation steps with explicit proofs (used by `validate_obl`)
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
internal/pipeline/validate_*.go              Go runner: harness, limits, skip-and-rerun, report split
internal/artifacts/validation.go             tracked reports of every program, coverage table
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

The producer workspace is assembled from the `producer/embed.go` glob
`LeanGo/Module/*.lean` (plus `LeanGo/Proofs/Check.lean` for the proof
checker). The validation files are embedded separately
(`producer.ValidationFiles()`), so the inputs hashed into every manifest are
unchanged; a run that requests validation stages them additionally and hashes
them into the validation report (`library`), never into the IR's `inputs`.

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
* **Evaluation steps carry explicit proofs (`LeanGo.Validate.Steps`).** The
  core lemmas `Option.bind_some`, `List.foldl_cons`, `ite_true` and
  `ite_false` are proved by `rfl`, so `simp` applies them *definitionally*:
  it rewrites the goal and records no proof, and the kernel has to recheck
  the step as a definitional equality between the term before and the term
  after. `Steps` restates the four lemmas with proofs that are not `rfl`, and
  `validate_obl` uses those, so each step is an ordinary rewrite whose proof
  the kernel checks by instantiation. The statements are the core ones and
  the obligations are unchanged; only the shape of the proof term changes.
  This is what made `DnsHeader.word1` and `DnsHeader.serialize` prove
  (failure class 5).

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

### Report

The command writes its raw report (`validation.json` in the workspace, with
timings and heartbeat counts). The Go runner splits it into two files:

```jsonc
// X.validation.json, schema lean-go-validation/v2: deterministic, tracked
{
  "schema": "lean-go-validation/v2", "module": "Local", "roots": ["Local.run"],
  "status": "completed",                        // or export_failed / load_failed / process_failed (+ "reason_class")
  "ir_file_matches": true, "ir_sha256": "…",     // the complete IR that was validated
  "lean_version": "4.36.0", "lean_commit": "…",
  "budget": {"heartbeats": 200000, "timeout_ms": 120000, "memory_mb": 4096, "process_timeout_ms": 3600000},
  "library": {"producer/LeanGo/Validate/Command.lean": "…", …, "tool/lean-go/validate": "…"},
  "program": "LeanGo.Validated.prog", "specification_list": "LeanGo.Validated.spec",
  "encodings": [{"type": "Local.Count", "status": "available"}],
  "functions": [{
    "name": "Local.run._lam_0", "params": ["Local.Config", "UInt32"], "result": "UInt32",
    "kind": "local_helper",                     // source | byte_fold | reduced_helper | local_helper
    "source_owner": "Local.run",
    "specification": "LeanGo.Validated.spec_2: lambda bump of Local.run, captures [config]",
    "status": "proved",                         // proved | failed | not_attempted
    "reason_class": "…",                        // when not proved: stuck, heartbeat_limit, obligation_time_limit,
                                                // memory_limit, process_time_limit, no_encoding, no_specification, …
    "theorem": "LeanGo.Validated.obl_2",        // only when proved
    "statement": "LeanGo.Validate.Callable.Rel Eq (…) LeanGo.Validated.spec_2",
    "axioms": ["Classical.choice", "Quot.sound", "propext"]}],
  "programs": [{
    "root": "Local.run", "closure": ["Local.run", "Local.run._lam_0", "Local.add"],
    "status": "proved",                         // proved | not_established (+ "missing") | failed (+ "reason_class")
    "prefixed_theorem": "…prefixed_0", "theorem": "…sound_0", "statement": "∀ (n : Nat), …",
    "explicit_theorem": "…explicit_0", "explicit_statement": "∀ (n : Nat) (config : Local.Config) …",
    "axioms": [...]}],
  "summary": {"functions": 3, "proved": 3, "failed": 0, "not_attempted": 0, "roots": 1, "roots_proved": 1}
}
// --validation-diagnostics FILE, schema lean-go-validation-diagnostics/v1: untracked
{"schema": "…", "module": "…", "roots": […], "ir_sha256": "…", "time_ms": 4438,
 "attempts": [{"skip": [], "exit_code": 0, "timed_out": false, "wall_ms": 5120}],
 "functions": [{"name": "…", "status": "…", "reason": "full message", "time_ms": 901, "heartbeats_used": 2105}],
 "programs": [{"root": "…", "reason": "…", "time_ms": 412}], "encodings": […]}
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

Coverage is a checked-in fact: `lean-go regenerate` writes the report of every
tracked module program next to its IR (`testdata/X.validation.json`,
`examples/proofs/X/X.validation.json`, `examples/formats/X/X.validation.json`)
with the default budget (200000 thousand heartbeats and 120 s per obligation,
4096 MiB for the process), and a regression in the proof automation shows up as
a diff. The table below is the output of `lean-go regenerate --coverage`; an
ungated test (`internal/artifacts/validation_test.go`) checks it against the
tracked reports, and that every report belongs to the tracked IR and the
current validation library.

<!-- validation-coverage:begin (lean-go regenerate --coverage) -->
| Program | Report | Functions proved / total | Roots established / total | Not proved |
|---|---|---|---|---|
| choice | `testdata/choice.validation.json` | 1 / 1 | 1 / 1 | – |
| counters | `testdata/counters.validation.json` | 5 / 5 | 4 / 4 | – |
| joins | `testdata/joins.validation.json` | 2 / 2 | 1 / 1 | – |
| local | `testdata/local.validation.json` | 3 / 3 | 1 / 1 | – |
| numbers | `testdata/numbers.validation.json` | 2 / 2 | 1 / 1 | – |
| records | `testdata/records.validation.json` | 3 / 3 | 1 / 1 | – |
| reduced | `testdata/reduced.validation.json` | 3 / 3 | 1 / 1 | – |
| trees | `testdata/trees.validation.json` | 2 / 2 | 1 / 1 | – |
| wide | `testdata/wide.validation.json` | 2 / 2 | 1 / 1 | – |
| quota | `testdata/quota.validation.json` | 4 / 4 | 4 / 4 | – |
| clamp | `examples/proofs/clamp/clamp.validation.json` | 2 / 2 | 1 / 1 | – |
| flags | `examples/proofs/flags/flags.validation.json` | 4 / 4 | 4 / 4 | – |
| words | `examples/proofs/words/words.validation.json` | 5 / 5 | 5 / 5 | – |
| bytes | `examples/proofs/bytes/bytes.validation.json` | 4 / 4 | 4 / 4 | – |
| header | `examples/proofs/header/header.validation.json` | 2 / 2 | 2 / 2 | – |
| wire16 | `examples/proofs/wire16/wire16.validation.json` | 5 / 5 | 5 / 5 | – |
| crc16 | `examples/proofs/crc16/crc16.validation.json` | 3 / 3 | 2 / 2 | – |
| adler32 | `examples/proofs/adler32/adler32.validation.json` | 5 / 5 | 3 / 3 | – |
| normalizer | `examples/proofs/normalizer/normalizer.validation.json` | 3 / 3 | 1 / 1 | – |
| format dns | `examples/formats/dns/dns.validation.json` | 14 / 14 | 3 / 3 | – |
| format header | `examples/formats/header/header.validation.json` | 7 / 7 | 4 / 4 | – |
| format udp | `examples/formats/udp/udp.validation.json` | 7 / 7 | 4 / 4 | – |
| **total** | 22 programs | **88 / 88** | **54 / 54** | |
<!-- validation-coverage:end -->

Every tracked function and root is proved under the default budget. The
last two, `DnsHeader.word1` and `DnsHeader.serialize`, had exceeded the
memory cap until the evaluation steps got explicit proofs (failure class 5);
the whole DNS validation now takes about 13 s with a peak of about 1.7 GB,
most of it the imported environment.

`checkedframe` has no IR of its own: it is the composition of `wire16` and
`crc16`, both completely validated.

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
5. **Kernel memory exhaustion from definitional evaluation steps**
   (`DnsHeader.word1`, a 7-term straight-line sum of shifted bytes, and
   `DnsHeader.serialize`, which calls it): elaboration finished in well under a
   second, but the kernel check of the proof exhausted memory (more than 12 GB
   uncapped; under `-M 6144` it failed after 29 s), and `serialize` needed
   38.6 s and a peak of 6.0 GB. Fixed by the explicit evaluation steps (§6).
   How it was found, each step reproduced in isolation under `lean -M`:
   * The obligation is closed by a single `simp only` pass. Running that pass
     with the source side left open (`sorry`), and without the literal and
     shift normal forms, still exhausted the cap: the cost was in the proof of
     the *IR-side* evaluation, not in the arithmetic normal forms or the final
     comparison with the source.
   * A synthetic family `w_k` (k shifted bytes plus one byte, the shape of
     `word1`) gave kernel times of under 0.1 s, 0.37 s and 2.0 s for k = 1, 2,
     3, and k = 4 exceeded 4096 MiB: a factor of about five per term. With
     the shift replaced by a multiplication by a literal, one term took
     0.12 s, 1.1 s and 5.3 s for the literals 2^12, 2^15 and 2^17: linear in
     the literal's value, which is the signature of the kernel reducing
     `Nat` arithmetic on a literal one successor at a time.
   * A throwaway probe kernel-checked every proof subterm separately and
     descended into the most expensive one. The minimal expensive piece was an
     `Eq.trans` whose two halves checked in 6 ms and 3 ms; the whole second
     was spent checking that the right-hand side of the first half and the
     left-hand side of the second are the same term. They are not
     syntactically equal: `simp` had applied `Option.bind_some`
     (`(some a).bind f = f a`, proved by `rfl`) as a definitional step, so
     the kernel had to establish `(some E).bind (fun E => …) ≡ (the body
     with E substituted)` itself. Both sides are `Option.bind` applications,
     and the kernel compares same-head applications argument by argument
     before it unfolds; those comparisons fail, but only after reducing the
     evaluated values, `UInt32` arithmetic on symbolic operands, down to
     `Nat`. Each nested step repeats this, so the cost grows with the
     literals and multiplies with the length of the body.
   * Replacing `Option.bind_some` by a copy with a non-`rfl` proof brought
     the synthetic `w_1` … `w_5` together to 0.11 s of kernel time,
     `word1` to 38 ms and `serialize` to 67 ms (from 38.6 s); in the
     command, the whole DNS program now validates in about 13 s with a peak
     of about 1.7 GB, most of which is the imported environment.
   `List.foldl_cons`, `ite_true` and `ite_false` are the other `rfl` lemmas
   of the evaluation set whose two sides can share a head symbol, so `Steps`
   restates them too. The rule the automation follows is now: a generic
   evaluation step whose left- and right-hand sides can have the same head
   must not be a definitional step.

   The memory cap stays: heartbeats do not bound kernel memory; Lean's `-M`
   limit does. The runner starts the validation process with `-M <cap>` and
   `Elab.async` off, so the kernel's `excessive memory consumption` error
   fails that obligation inside the command, which reports it; the runner
   then reruns with it skipped (`skip "F"`), because the process can stay
   near the cap and fail later obligations spuriously, and reports it
   `failed` with reason class `memory_limit`. A process that dies outright
   (Lean's uncaught memory exception, the system's OOM killer, the process
   timeout) is handled the same way through the progress file. No tracked
   obligation reaches the cap any more (the largest whole-program run peaks
   at about 1.7 GB, close to what the imported environment alone needs), so
   the rerun of an obligation that hits the cap is tested with a fake
   validator, and the real process only with a cap it cannot meet.
6. **Kernel cost of a default-transparency `rfl` fallback**: the tactic's last
   resort was `rfl`, which can prove an arithmetic goal by evaluation that the
   kernel then re-does on symbolic operands. It is now `with_reducible rfl`
   (no tracked obligation depended on the stronger form). Scalars are also no
   longer destructed into `BitVec` fields.

Machine note: on the shared machine the container's memory cgroup can also
kill `lean` processes for reasons unrelated to the proofs (several agents
compiling at once). An exit 137 during an obligation is reported as
`memory_limit`, with the exit status in the diagnostics; rerun such a program
alone before reading it as a verdict.

## 10. What remains trusted

1. The Lean kernel and the three standard axioms; the axiom gate
   (`validation/Examples/AxiomCheck.lean`, `#assert_standard_axioms
   LeanGo.Validate Examples`) fails the build on any other axiom, `sorry`,
   `native_decide`/`bv_decide` (`Lean.ofReduceBool`, `Lean.trustCompiler`) or
   a declared axiom. It covers the library, the command's meta code, the
   examples and — through `Examples/Command.lean`, which runs the command with
   `strict` inside the build — everything the command adds (2758
   declarations). In an extraction, the command applies the same check to
   every theorem it adds before reporting it as proved.
2. The semantics (`Value`, `Prim.eval`, `Code.eval`, `Program.run`,
   `Value.hasType`): it must describe what the Go printer implements.
3. The JSON bridge `elabProgramJson`.
4. The IR-to-Go printer and the Go runtime; the Go compiler.

The LCNF → IR lowering and the compiler passes are no longer trusted for the
functional behaviour of validated roots. They remain trusted for the
*selection* of what is exported (which roots).

## 11. Integration with the CLI (`internal/pipeline/validate_*.go`)

Implemented as follows.

* **Embedding and caching.** `producer/embed.go` embeds
  `LeanGo/Validate.lean`, `LeanGo/Validate/*.lean` and
  `LeanGo/Validate/Meta/*.lean` (15 files) as `ValidationFiles()`, separate from
  `ModuleFiles()`. A validating run stages them, builds
  `LeanGo.Module.Export LeanGo.Validate LeanGo.Validate.Command` and uses a
  producer-cache entry keyed by the module producer *and* the validation files;
  the key and the entry of a run without validation are unchanged. Per
  extraction only the harness runs; derived encodings, the program term and all
  theorems live in the harness environment and are never written to an olean.
* **Separate process.** `ExportModule` runs the export harness exactly as
  before and computes the complete IR. Then it writes
  `LeanGo/Module/Validate.lean`
  (`#lean_go_validate "ir.json" [<sorted roots>] "<Module>" report "validation.json" heartbeats H timeout_ms T [skip "F …"]`)
  and runs it as `lean -M <MiB> -DElab.async=false`, directly, with Lake's
  `LEAN_PATH` (so a timeout kills the process that does the work, on every
  OS). The command's `strict` flag is not used: the runner decides `require`.
* **Limits and reruns.** If the process dies, the last `proving F` line of
  `validation.json.progress` names the obligation; a report in which the
  command recorded a memory failure names it too. The runner reruns with the
  accumulated `skip` list (at most four reruns) and marks each such F `failed`
  with the limit's reason class. A process that ends without a report and
  without an obligation in progress gives `status: process_failed`.
* **Report.** Read with a 16 MiB bound, `module` and `roots` checked, split
  into the deterministic report and the diagnostics (§7), with `ir_sha256` of
  the complete IR and the `library` digests of the validation files and of the
  runner (`tool/lean-go/validate`: the `validate_*.go` sources, which are
  excluded from the pipeline digest because they cannot change the IR).
  `report` mode never fails on an unproved obligation; `require` fails after
  writing the report.

## 12. Limits and open items

* Partial correctness only (§4).
* Primitives outside the unsigned matrix (signed integers being added to
  `Primitives.lean`) are rejected by the loader until `Prim.lean` and the
  semantics are extended; the command then reports `load_failed`.
* `Encode` covers non-parametric inductives without nesting or mutual
  recursion (the extractor does not admit them either).
* Symbolic evaluation is `simp`-based; cost grows with the number of case
  splits and with arithmetic expression size, and the kernel re-checks every
  step. A definitional step whose two sides share a head symbol can make that
  re-check reduce arithmetic (failure class 5); the generic ones are avoided,
  but a new `rfl` lemma of that shape in `ir_simp` would bring the cost back. Source functions using well-founded
  recursion would need `unfold`-aware handling; none of the tracked ones do.
* Local helpers: the source lambda is identified through the producer's
  witness and capture names; a capture whose name is ambiguous among the
  lambda's free variables makes that helper `not_attempted` (not observed).
