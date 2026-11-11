# Lean extraction feasibility

Date: 2026-11-11 · Maintainer: franchb <hello@franchb.com>

**Decision: Proceed with pure mono LCNF for the controlled normalizer subset.**
An original, version-pinned adapter can recover the required bodies and public
schema without a patched Lean compiler or native runtime dependency in the
candidate closure. This investigation implements and tests the Lean producer
prototype. It does not implement Go extraction, prove translation correctness,
or satisfy the future release gates in the [approved design](../superpowers/specs/2026-11-11-lean-go-extraction-design.md).

## Scope, toolchain, and reproduction

The export root is `LeanGo.Feasibility.normalize : Expr → Expr`. The
[model](../../feasibility/LeanGo/Feasibility/Model.lean) is ordinary Lean: a
strong-Kleene three-valued expression normalizer, not a separate authoring DSL.
The investigation covers concrete `Tri`, recursive `Expr`, opaque variable
identities of type `UInt32`, pattern matching, and pure first-order structural
recursion. It does not claim general Lean, collection, integer, or effect support.

| Item | Recorded value |
| --- | --- |
| Source baseline | `d85c0e123e139e0fa434a0240c836b153e9300d5` |
| Lean pin | `leanprover/lean4:v4.35.0` |
| Lean compiler commit | `eb6cafb7290e8bd5f2937522d8d804f58f02bafc` |
| Lake | `5.0.0-src+eb6cafb` |
| Producer/native reference host | Linux/amd64 |
| External Lean packages | None; Lake-generated lock records an empty package set |
| Capture tooling | Python 3 standard library; Python 3.11+ required by the plan |
| Available Go | Go 1.27.4; not used to build or execute a generated Go artifact |
| Reuse candidate | `auser/lean4-prod` at `991956fe427bb646498fd0912c89b71be71e7beb` |

With the pinned Lean toolchain installed and `lake` on `PATH`, run from the
repository root. The commands regenerate ignored evidence; they do not publish
anything. Run sequentially, with no pre-existing `feasibility/ProbeSameFile.lean`.

```bash
cd feasibility
lake env lean --version
lake --version
lake build LeanGo normalizer-checks
lake env lean LeanGo/Feasibility/Checks.lean
lake exe normalizer-checks
python3 check_mutations.py
python3 capture.py
cp .evidence/capture.json .evidence/capture-first.json
python3 capture.py
cmp .evidence/capture-first.json .evidence/capture.json
sha256sum .evidence/capture.json
```

The final source build, direct checks, native smoke, both mutation rejections,
complete capture, and repeated-manifest comparison passed. Two full captures
were byte-identical; a further final run agreed. The manifest SHA-256 was
`f194dfb01439c4bb9707ab8541af042a7f0285861fc140eff55839c7ac9e165c`.
It records 16 input hashes, ten subprocess entries, and equal sorted 129-record
same-file/imported inventories. Eight subprocesses exit 0; only `bad-claim` and
`bad-csimp` intentionally exit 1. Its literal verdict remains
`capture checks passed; manual IR and primitive audit required`.
The stage/representation audit below supplies that bounded manual inspection;
the script is not a general IR admission checker.

The [capture script](../../feasibility/capture.py) records exact argv and results.
It enables `compiler.postponeCompile=false`, `compiler.check=true`,
`trace.Compiler.saveBase=true`, and `trace.Compiler.saveMono=true`, using `-c`
for diagnostic C output. The foreign fixture's diagnostic C is never linked or
called. Raw logs/C remain ignored under `feasibility/.evidence/`; the candidate was
inspected outside the tracked project. The temporary same-file probe is created exclusively
and removed only when owned by the capture invocation.

Input SHA-256 values below are relative to `feasibility/`. `ProbeSameFile.lean`
is deterministically assembled by `capture.py` from the model and inspection
actions, hashed, and removed. The baseline plus these hashes identifies all
producer inputs without publishing raw logs.

| Input | SHA-256 |
| --- | --- |
| `BadCSimpFixture.lean` | `fb8c090dc9f03becf049c85c18c399408be47813b71970013d01eb1693aaa26b` |
| `BadClaim.lean` | `a1510e1e3e24b5999ea2693962b6801399cf1f21e0c2c5744a84dbbdec3f580b` |
| `CSimpFixture.lean` | `0d141cc75151e0ad9cf22e9f94d5c8a3f616083018b4b8f37b85428d11d523f5` |
| `ForeignFixture.lean` | `5e2345c6789b1cd4bef662b2363636d6755013fed09df3e9726e6af555ad6d27` |
| `Inspect.lean` | `70353310945611a1c6eaa427afbd50a479a286598bff02b4c11ec71b523eb3cf` |
| `LeanGo.lean` | `6392b295ead17165f8d5922f65cc860c8c6b450061c97258a9345764fcd97577` |
| `LeanGo/Feasibility/Checks.lean` | `bef9b64ce91d3d0df0284d43946e15d25d44b59d04653f0676f8654f7a775c3d` |
| `LeanGo/Feasibility/Model.lean` | `c67a835949d1786638d2809c970ef69861a074a23cf6fdbcae3f8cad1043771f` |
| `LeanGo/Feasibility/ProbeSupport.lean` | `0d113b4920bd4373881a4933e23ded950c6b88e04863ad889663e4bc3a5e42b3` |
| `ProbeSameFile.lean` | `f30ffacb637b5fa062d4484ecd20dc0a6060b826315abc0fffe5b69754dc83aa` |
| `ReplacementFixture.lean` | `429de607687e30331500f6d5668f4f92d0882f7da275080236cd8bffed89f469` |
| `capture.py` | `9d6500f08759c434d7b5e47392530b983c5ba22ced0dfa033a01dc5aa3016f00` |
| `check_mutations.py` | `9fab8b4502649f35dbb78beca8edb8815eac2bdd97a0b08913ca03d915c8659f` |
| `lake-manifest.json` | `cdb3da2aef19d2df00a4a2a2e18a52941d970be78fe369e52b1f535dee8a1bc9` |
| `lakefile.toml` | `9c6249283261a928da2686bebe4a27a48126fd978e783f80d2cd1052dd3e3f81` |
| `lean-toolchain` | `ca5f1f601d34c49079e38d7e7041eb0f2a9c227fbc1df0f404820069a052330c` |

## Source claims and negative controls

The exact public theorem signatures are in `Model.lean:101–103,128–130`
(namespace `LeanGo.Feasibility`):

```lean
theorem normalize_preserves_eval (e : Expr) (rho : UInt32 → Tri) :
    eval rho (normalize e) = eval rho e

theorem normalize_is_normal (e : Expr) :
    IsNormalForm (normalize e)
```

Both quantify over arbitrary finite `Expr` without additional hypotheses. The
first preserves the complete three-valued result for every valuation. The
second excludes an evaluation-preserving identity implementation. `IsNormalForm`
permits negation only directly on variables and evaluates negated constants.
Operand order is separately checked by the expected-expression check; neither
idempotence nor optimality is an advertised theorem.

| Proof audit | Transitive axioms actually reported |
| --- | --- |
| `LeanGo.Feasibility.normalize_preserves_eval` | `[propext]` |
| `LeanGo.Feasibility.normalize_is_normal` | `[propext]` |
| Compiler rewrite `Bool.not_eq_internalNot` | `[Quot.sound]` |

The [probe](../../feasibility/LeanGo/Feasibility/ProbeSupport.lean) uses
`Lean.collectAxioms`, allowing only `propext`, `Quot.sound`, and
`Classical.choice` when actually used. `Classical.choice` is not reported for
these three proofs. A source spelling scan is not the audit. Rewrite evidence
must exist and be a theorem before its transitive axiom audit is accepted.
The root signatures are also independently asserted in the checks.

| Committed control | Observed result and interpretation |
| --- | --- |
| `Checks.lean:5–18` | Exhaustive 3-entry negation and 9-entry meet/join truth tables pass by reduction. |
| `Checks.lean:20–35` | Ordered expected normal form, triple negation, unknown literal, and variable IDs 0/4294967295 pass; exact public claim types check. |
| `Checks.lean:40–46` | Native Linux/amd64 Lean smoke prints `normalizer smoke: OK`; ordered shape and evaluation are checked. |
| `check_mutations.py`: wrong De Morgan rule | Replacing the negated conjunction's disjunction with conjunction fails inside `normalizeAux_preserves_eval`. |
| `check_mutations.py`: identity normalizer | Returning the input, with its evaluation proof adjusted to `rfl`, fails inside `normalize_is_normal`. |
| `BadClaim.lean` | Exactly one intended diagnostic rejects `BadClaim.untrusted` in `BadClaim.advertised`; exit 1. |
| `ForeignFixture.lean` | Source extern and external leaf classified at both stages; fixture succeeds as diagnostic generation, without linking/calling the missing symbol. |
| `ReplacementFixture.lean` | Logical identity differs from compiled replacement; source/base/mono substitution is visible. |
| `CSimpFixture.lean` | Axiom-free rewrite accepted with `[]`; actual base/mono root edges call the replacement. |
| `BadCSimpFixture.lean` | Logical identity first accepted with `[]`; forged rewrite theorem depends on `BadCSimpFixture.forged` and is rejected with the sole intended diagnostic, exit 1. |

The [mutation checker](../../feasibility/check_mutations.py) requires exit 1
and only theorem-range diagnostics; an unavailable compiler or unrelated error
cannot count as rejection. Capture checks exact positive/negative proof records
before inventory deduplication, all eight original constructor types, root
presence, complete named closures, and the source substitution. Missing,
duplicate, or extra proof records, incorrect metadata, unexpected runtime
proof/foreign/unsafe edges, mixed diagnostics, and wrong process exits fail the
gates. Failure evidence is retained and unowned temporary sources are preserved.

During implementation, 24 mutation-diagnostic and 45 capture-fault scenarios,
plus a direct-axiom CSimp evidence-kind rejection, were exercised as one-off
checks. They are not committed test suites and the commands above do not
reproduce those ephemeral campaigns. The committed controls listed in the
table are the reproducible rejection corpus.

## Source definitions versus compiled execution

Source dependency traversal intentionally overapproximates the source; its
constants are not the selected runtime closure. It reports original `extern`
and `implemented_by` attributes, queries the current CSimp environment, audits
each rewrite theorem, and follows replacement targets. The normalizer source
has no reported `extern` or `implemented_by`, but it does have a real CSimp rule.
Curated excerpt from the same-file log at lines 1341–1346 (also imported at
54–59):

```text
SOURCE_CSIMP Bool.not -> Bool.Internal.not theorem=Bool.not_eq_internalNot
CLAIM Bool.not_eq_internalNot: not = Bool.Internal.not
AXIOMS Bool.not_eq_internalNot: [Quot.sound]
AXIOMS_ACCEPTED Bool.not_eq_internalNot: [Quot.sound]
SOURCE Bool.Internal.not
```

This accepted foundation is distinct from the two root claims' `[propext]`.
In the actual candidate bodies, boolean negation is represented by cases and
constants; it adds no runtime helper/foreign leaf. In the substitution fixture,
`root value := value` has `@[implemented_by replacement]`, where replacement
returns UInt32 zero. Following that target's source discovers `Nat.rec` →
`Nat.recCompiled`, its `[Quot.sound]` evidence, and UInt32 literal dependencies.
These are fixture dependencies, not normalizer requirements. The foreign
fixture records symbol `lean_go_missing_foreign`; a symbol is classification
evidence, not a pure-Go semantic mapping or permission to admit it.

The actual CSimp API is
`(Lean.Compiler.CSimp.ext.getState env).map.find? sourceName`, returning source,
target, and theorem names. Its [pinned definition](https://github.com/leanprover/lean4/blob/eb6cafb7290e8bd5f2937522d8d804f58f02bafc/src/Lean/Compiler/CSimpAttr.lean#L18-L38)
is a scoped environment extension. End-environment lookup cannot reconstruct
arbitrary earlier local/scoped rewrites. The concrete controlled plain module,
its observed rule, and all three actual bodies have been inspected here; that
closes this experiment's admitted basis. A general adapter must record rules
at compilation time or explicitly restrict/reject unsupported history.
End-environment absence is not universal provenance evidence.

## Base/mono comparison and imported availability

The [pinned pass ordering](https://github.com/leanprover/lean4/blob/eb6cafb7290e8bd5f2937522d8d804f58f02bafc/src/Lean/Compiler/LCNF/Passes.lean#L92-L158)
already performs simplification, eager lifting, specialization, join discovery,
and `implementedBy := true` before `saveBase`. Mono adds monomorphization,
simplification, arity reduction, and lambda lifting. It is saved before impure
lowering, boxing, reference counting, and reset/reuse. Base is not an untouched
logical source boundary, and switching to base would not remove substitution
trust. These stages are not proved correct by this investigation.

| Reached function | Signature | Size in each stage | Same-file base / mono lines |
| --- | --- | ---: | --- |
| `normalize` | `Expr → Expr` | 2 | 1168–1172 / 1173–1177 |
| `normalizeAux` | `Bool → Expr → Expr` | 40 | 1064–1115 / 1116–1167 |
| `Tri.neg` | `Tri → Tri` | 6 | 272–282 / 283–293 |

All names in this and subsequent local tables are under `LeanGo.Feasibility`
unless explicitly qualified otherwise. All three code declarations are safe;
only `normalizeAux` is recursive. Manual comparison found their actual
pretty-printed bodies identical between base and mono. The root body in each
stage has SHA-256
`e9353071d3fa207cb43704de59072faf01d766f4560d1c6837275548e0e18ffa`.
Curated root excerpt:

```text
[Compiler.saveMono] size: 2
    def LeanGo.Feasibility.normalize e : Expr :=
      let _x.1 := false;
      let _x.2 := normalizeAux _x.1 e;
      return _x.2
```

`getBaseDecl?` and `getMonoDecl?` return `.code` after importing the model through
`Inspect.lean`. The sorted inventories agree across the same-file/imported
boundary and include the same three code functions and eight constructor
leaves. Imported inspection does not re-emit raw function bodies: it establishes
body availability and the same named closure, not an independent textual
comparison of every imported body. The [saved-stage source](https://github.com/leanprover/lean4/blob/eb6cafb7290e8bd5f2937522d8d804f58f02bafc/src/Lean/Compiler/LCNF/PhaseExt.lean#L84-L112)
also permits opaque external entries for some imported declarations; future
admission must reject missing bodies rather than generalize from this success.

| Raw evidence identity | SHA-256 |
| --- | --- |
| `samefile.log` (1444 lines) | `a5d069ee0fdcb4df87b727dca6fc5d34199faa3b5004608dbe107471efbc00b8` |
| `imported.log` (157 lines) | `e6b34ad0ea7da202561241e3a7007d26b1fbc71140cc4bab3b594159a685aaf0` |
| `csimp.log` (100 lines) | `0a715d6d04799998a873bc42ef54476f6964b8d6b2944462591d1f054190ae90` |
| `bad-csimp.log` (92 lines) | `fe1a31fadace47ea92e929980a333967d30f916d92c5213706328f7a5c559789` |

## Public types, fields, and UInt32

Original metadata in same-file lines 1427–1444 and imported lines 140–157
matches `Model.lean:5–7,41–47`. Every constructor below has zero parameters.
Pure alternative fields are ordered logical positions, not native object
offsets or a C ABI. The field types combine original constructor types and
observed pure alternatives; the next adapter must check typed `Param.type`,
`Cases.typeName`, and constructor arguments directly.

| Constructor | Tag | Field count and ordered field types | Observed alternative bindings |
| --- | ---: | --- | --- |
| `Tri.f` | 0 | 0 | none |
| `Tri.u` | 1 | 0 | none; return original Tri |
| `Tri.t` | 2 | 0 | none |
| `Expr.lit` | 0 | 1: position 0 `Tri` | `a.2` |
| `Expr.var` | 1 | 1: position 0 `UInt32` | `a.5` (unused) |
| `Expr.neg` | 2 | 1: position 0 `Expr` | `a.7` |
| `Expr.conj` | 3 | 2: position 0 left `Expr`, position 1 right `Expr` | `a.12`, `a.13` |
| `Expr.disj` | 4 | 2: position 0 left `Expr`, position 1 right `Expr` | `a.20`, `a.21` |

`Tri.u` and `Expr.var` remain required public schemas despite having no
constructor-call edge: unknown returns its input value, and a variable returns
the original node, or a fresh negation around that node. No UInt32 arithmetic,
equality, conversion, truncation, or reconstruction occurs in the runtime
closure. Its [source representation](https://github.com/leanprover/lean4/blob/eb6cafb7290e8bd5f2937522d8d804f58f02bafc/src/Init/Prelude.lean#L2732-L2752)
has size 4294967296 and a `BitVec 32` field with special compiler handling.
The future public mapping is exactly Go `uint32`, preserving 0 through
4294967295, with checked boundary decoding. Machine-sized `int` or float-based
JSON decoding is not an established equivalent.

## Complete reached runtime closure and mappings

This table enumerates every named function and constructor leaf reached at
**both base and mono**. There are no other named runtime leaves, specialized
helpers, opaque/external leaves, or implementation replacements in this closure.
Mappings are proposed pure-Go implementation obligations, not completed code.

| Declaration / leaf | Callers | Contract / proposed mapping |
| --- | --- | --- |
| `normalize` (`.code`) | export | Direct function initializes false polarity and calls `normalizeAux`; return the resulting Expr. |
| `normalizeAux` (`.code`, recursive) | `normalize`, itself | Exhaustive Expr/Bool switches; ordered child processing; compiled control flow with a bounded explicit work stack. |
| `Tri.neg` (`.code`) | `normalizeAux` | Exhaustive enum switch: f→t, u→input, t→f. |
| `Bool.false` (constructor) | `normalize`, `normalizeAux` | Go false; zero fields, tag 0. |
| `Bool.true` (constructor) | `normalizeAux` | Go true; zero fields, tag 1. |
| `Tri.f` (constructor) | `Tri.neg` | Validated enum tag 0, no fields. |
| `Tri.t` (constructor) | `Tri.neg` | Validated enum tag 2, no fields. |
| `Expr.lit` (constructor) | `normalizeAux` | Immutable node tag 0 with one Tri payload. |
| `Expr.neg` (constructor) | `normalizeAux` | Immutable node tag 2 with one Expr child. |
| `Expr.conj` (constructor) | `normalizeAux` | Immutable node tag 3 with left then right child. |
| `Expr.disj` (constructor) | `normalizeAux` | Immutable node tag 4 with left then right child. |

No native primitive mapping is needed by this concrete runtime closure.
`eval`, `Tri.meet`, `Tri.join`, UInt32 constructors/conversions, recursors,
derived repr/BEq/Decidable code, and theorem/probe machinery are outside it.
Their appearance elsewhere in logs is not runtime reachability evidence.

## Observed IR forms and control flow

The [pinned pure LCNF definitions](https://github.com/leanprover/lean4/blob/eb6cafb7290e8bd5f2937522d8d804f58f02bafc/src/Lean/Compiler/LCNF/Basic.lean#L170-L234)
and [control-flow forms](https://github.com/leanprover/lean4/blob/eb6cafb7290e8bd5f2937522d8d804f58f02bafc/src/Lean/Compiler/LCNF/Basic.lean#L368-L397)
were checked against all three actual bodies, not inferred from named edges.

| Category | Observed forms | Proposed mapping / admission constraint |
| --- | --- | --- |
| `Code` | `.let`, `.cases`, `.return` | Typed temporaries, exhaustive switches, and returns preserving sequencing. |
| `LetValue` | `.const` only | Saturated direct calls or fully supplied constructors; nullary Bool/Tri constructors are values. |
| Arguments | Existing value variables | No erased/type argument occurs in reached calls; call arities 1 (`Tri.neg`) and 2 (`normalizeAux`). |
| Alternatives | Pure `.alt` | Expr 5 arms, Bool 2 arms, Tri 3 arms; fields bound in constructor order. |
| Recursion | Direct self-call of `normalizeAux` | Structural input traversal; explicit bounded frames can implement it without Go recursion. |
| Value reuse | Return input for non-negated literals/variables and Tri.u | Safe only for owned immutable values. |

There are no local function/join declarations, join jumps, default/impure
alternatives, unreachable nodes, indirect or partial applications, escaping
function values, projections, erased runtime values, native primitives, or
impure allocation/boxing/RC/reset/reuse operations in these bodies. Constructors
at this pure stage are `.const` applications, not impure `.ctor` operations.
Future admission must exhaustively inspect every Code/LetValue/Arg/Alt form;
`directCalls` alone is deliberately insufficient.

For example, mono lines 1153–1158 preserve ordered De Morgan processing:

```text
| Bool.true =>
  let _x.17 := normalizeAux p a.12;
  let _x.18 := normalizeAux p a.13;
  let _x.19 := Expr.disj _x.17 _x.18;
  return _x.19
```

The surrounding printed `cases p : Expr` gives the **result type** `Expr`;
`p` is a Bool discriminant. It must not be decoded as an Expr case. The typed
IR must retain result type, discriminant type, and ordered field bindings
separately. Both binary children are normalized left then right; there is no
short circuit or exchange. The separately inspected evaluator bodies evaluate
left, then right, then meet/join, but are outside the export closure. The source
proofs are extensional, not an evaluation-trace theorem. Future evaluators must
use the full strong-Kleene truth tables rather than Go `&&`/`||`.

## Reuse assessment

**Choose an original adapter.** The following is source inspection of the pinned
candidate only: no candidate build, test, module initializer, or generated
output was executed; no implementation was copied and no outreach occurred.

| Boundary | Pinned primary evidence and consequence |
| --- | --- |
| Version coupling | [Lean 4.32.1 pin](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/lean-toolchain#L1), [Rust 1.97.1 pin](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust-toolchain.toml#L1-L5), and [ExtractedDef/extraction](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Extract.lean#L21-L98): stores Lean `.pure` compiler declarations and invokes `toDecl`/simplification; a 4.35 port is unverified. |
| Named roots and validation | [Export validation](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Export.lean#L92-L139) rejects theorem/opaque/unsafe/partial roots. [Named export](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Export.lean#L252-L272) checks unresolved calls/skips. Ordinary roots need no custom attribute, but this is not a proof of translation. |
| Decisive type mismatch | [Type lowerer](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Lower.lean#L729-L853) rejects recursive inductives, parameters, indices, and mutual blocks; the concrete recursive Expr fails. Tri alone fits the enum shape. [Rust codegen](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust/prod-codegen/src/lib.rs#L416-L425) turns recursive type markers into errors. |
| Coverage hazard | [Type emission](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Export.lean#L30-L75) and [coverage output](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Export.lean#L238-L272) treat unsupported types separately and write an empty `unsupported_types` array in named coverage. Export success alone does not establish Expr acceptance. |
| Numeric policy | [IR](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust/prod-ir/src/lib.rs#L27-L85), [literal lowering](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Lower.lean#L490-L496), and [Rust mapping](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust/prod-codegen/src/lib.rs#L1635-L1647): UInt32→u32 exists, while Nat is bounded u64 and mathematical Int is rejected. This gives no general Nat extraction guarantee. |
| Closure/substitutions | [Named closure](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Export.lean#L141-L227) restricts independently generated definitions to root namespaces and skips selected primitives/projections. Reviewed validation does not explicitly audit implemented_by/CSimp. [Local functions](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/LocalFunctions.lean#L1-L23) must be saturated, nonescaping, and acyclic; this is not general closure conversion. |
| Import effects | [Entrypoint](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/Prod/Emit.lean#L225-L287) enables initializer execution before root validation. A future producer needs an explicit import/initializer policy. |
| License scope | All eight Rust manifests declare MIT, including [prod-ir](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust/prod-ir/Cargo.toml#L1-L9) and [prod-codegen](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust/prod-codegen/Cargo.toml#L1-L9). At the [pinned tree](https://github.com/auser/lean4-prod/tree/991956fe427bb646498fd0912c89b71be71e7beb), no root LICENSE/COPYING/NOTICE or explicit Lean-source grant was found; the license-named file is a [test fixture](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/rust/prod-codegen/tests/fixtures/LICENSE-MIT#L1-L17), and the [Lean package file](https://github.com/auser/lean4-prod/blob/991956fe427bb646498fd0912c89b71be71e7beb/lean/lakefile.lean#L1-L9) supplies no license metadata. Rust's explicit MIT evidence must not be extended silently to the Lean extractor. This is a component-specific uncertainty, not a claim that the whole repository is unlicensed. |

License clarification, recursive-type support, and compiler-version agreement
would be needed before adapting that Lean implementation. Rust component reuse
is unnecessary for this target. A narrow future contribution could improve
unsupported-type coverage or a reusable validated extraction boundary after
those questions are resolved; upstream acceptance is not assumed.

## Trust limits and resource strategy

The admitted source proof basis is explicit, but Lean compiler stages,
Lean-to-IR lowering, future IR admission, Go emission, public conversion, Go
compiler, and Go runtime remain trusted until separately justified. Native Lean
and future Go differential checks can share compiler transformations; agreement
cannot prove those shared steps. No Go emitter, generated runtime, Go-only
consumer, platform runtime suite, tagged release, or end-to-end translation
proof exists at this milestone. Linux/amd64 here describes producer/native Lean
checks only. `CGO_ENABLED=0` execution on Linux/amd64 and Windows/amd64 remains
required; cross-compilation will not satisfy it.

The concrete bodies permit directly compiled Go control flow with an explicit
bounded work stack and immutable tagged nodes or an owned checked arena. This
is a proposed strategy, not a deployed VM or native fallback. On a finite tree,
normalization visits input nodes linearly, stack demand follows depth, and
fresh construction is linear: binary nodes and leaves reached under true
polarity allocate; eliminated negations and unchanged leaves reuse values.
These are structural observations, not measured Go performance or product limits.

Carry forward the approved conversion contract: iterative validation rejects
bad tags, nil roots/missing children, cycles, and depth/node/work limit
violations. Callers must not mutate the source during conversion. Conversion
creates an owned immutable tree; later source mutations cannot affect it.
Shared acyclic DAG subtrees are **copied per occurrence**, charging each
occurrence before expansion; active-path cycle detection distinguishes sharing
from a cycle. Counting only unique nodes is insufficient. Generated processing
also needs a bounded explicit stack and checked output-allocation budget;
bounded validation alone cannot justify unbounded recursive processing.
Logical termination proves no time, memory, or host-stack budget. Select limits
from measured deep/skewed/wide cases and shared-input expansion costs, then test
below/at/above each limit with distinct malformed/resource errors.

Unsupported forms, unresolved calls, unsafe/partial code, missing imported
bodies, effects, unknown primitives, untracked substitutions, or malformed typed
IR must fail closed. The current probe plus manual audit establishes only this
controlled subset, not that general admission machinery.

## Decision and smallest next scope

Proceed with pure mono LCNF because all required bodies remain available across
imports, original metadata retains every schema and UInt32 field, both actual
stages have the same small bodies, every reached operation has an explicit
pure-Go implementation path, and the concrete substitution/proof basis is
accounted for. Base offers no observed benefit for this input. The independently
authored adapter avoids the candidate's recursive-type/version/license blockers.

The next milestone is a separate bounded backend plan for this exact subset:

1. Define the exact Go API, ownership/error contract, depth/node/work limits,
   and output-allocation budget, retaining per-occurrence sharing conversion.
2. Specify a versioned typed IR for the observed forms, exhaustive fail-closed
   admission, full runtime closure/type validation, and compiler-substitution
   provenance or explicit history restrictions. Define producer import policy.
3. Implement original adapter and directly compiled Go control flow with bounded
   frames; preserve tags, field order, unknown, UInt32, and eager child order.
4. Add primitive conformance, a small IR reference interpreter for testing,
   independent Go evaluation, finite valuation/expression enumeration,
   reproducible fuzzing, differential execution, boundary cases, and targeted
   mapping/stale-digest/unsupported-dependency faults. A test interpreter is not
   a deployed runtime dependency.
5. Produce deterministic atomic output plus a complete version/digest/claim
   manifest, preserving the last good package on failure; test regeneration.
6. Run a Go-only external consumer and semantic/boundary execution with
   `CGO_ENABLED=0` on Linux/amd64 and Windows/amd64 before release acceptance.

This evidence supports planning that bounded implementation. It does not
claim those future checks passed or expand the approved first-release contract.
