# Ordinary Lean to Pure Go Feasibility Implementation Plan

> **For agentic workers:** Use `superpowers:subagent-driven-development` or
> `superpowers:executing-plans` to execute this plan task by task. This executed plan records
> implementation responsibilities; keep execution status in the task tracker.

**Goal:** Prove an original three-valued expression normalizer and collect enough
compiler, dependency, representation, and reuse evidence to select or reject a
bounded ordinary-Lean-to-Go extraction approach.

**Architecture:** A disposable producer-side Lake package contains the public
model, its proofs, checks, and compiler probes. It captures original types and
both pure LCNF stages, including imported-body availability, without building a
Go backend. A final evidence report chooses the adapter stage and specifies the
smallest next implementation, or records a concrete reason to revise the design.

**Tech Stack:** Lean 4.35.0, Lake, Lean's `Std` and compiler libraries, Python 3.11+
standard library, Git. Elan provisions the pinned Lean version. No external Lean
packages or Rust toolchain are required for this investigation.

---

## Authority, limits, and current evidence

The [approved design](../specs/2026-11-11-lean-go-extraction-design.md) is the
contract. Its first implementation plan is this feasibility investigation.
This plan does not implement the production IR, Go emitter, support library,
host-input validator, platform CI, release packaging, or downstream integration.
It does not replace those requirements with a handwritten Go implementation.

Lean 4.35.0 and its compiler sources were inspected while writing this plan.
The `saveBase` and `saveMono` trace classes, compiler substitutions, and public
metadata APIs are grounded in the sources linked below. The initial planning
shell did not find Lean or Go on its PATH. After installation and an upgrade, a
follow-up local check on 2026-11-11 verified Go 1.27.4 (`linux/amd64`), Lean
4.35.0 (commit `eb6cafb7290e8bd5f2937522d8d804f58f02bafc`), and Lake
5.0.0-src+eb6cafb. The agent shell still requires the installed executable
directories on its PATH; explicit executable paths work. This is a shell
configuration issue, not an absent installation.

**Executed on 2026-11-11.** Tasks 1–6 produced the pinned model, source
proofs, strict mutation/capture controls, imported-stage evidence, and read-only
reuse assessment. The [Task 7 evidence report](../../research/extraction-feasibility.md)
records the source baseline, hashes, exact results, and decision: **proceed with
pure mono LCNF for the controlled normalizer subset**. No Go backend or release
is implemented. Task 1's pin and Task 2's model remain the approved foundation;
Tasks 3–5 below link the corrected committed implementation rather than retain
stale initial code proposals. Do not weaken theorems, add axioms, or silently
upgrade Lean to make a probe pass.

All new example material must be authored from the standard truth tables and
the design. Keep private application names, code, fixtures, paths, identifiers,
and task references out of source, evidence, documentation, and Git history.
Use repository-local Git identity `franchb <hello@franchb.com>`.

Execute Tasks 1–5 in order. The read-only candidate assessment in Task 6 may run
in parallel after Task 1. Task 7 joins the results. All shell commands run from
the repository root unless a step explicitly changes directory. A failed
command stops its step; expected rejection tests distinguish intended failure
from missing tools or unrelated compiler errors.

## File responsibilities

| Path | Responsibility |
| --- | --- |
| `.gitignore` | Ignore producer builds and raw evidence, not curated reports |
| `feasibility/lean-toolchain` | Exact Lean version |
| `feasibility/lakefile.toml` | Standalone producer package and native reference executable |
| `feasibility/LeanGo.lean` | Library root importing the model |
| `feasibility/LeanGo/Feasibility/Model.lean` | Ordinary definitions and the two source theorems |
| `feasibility/LeanGo/Feasibility/Checks.lean` | Truth tables, expected normal forms, exact claim types, runnable checks |
| `feasibility/LeanGo/Feasibility/ProbeSupport.lean` | Version-specific source/stage inspection, claim audit |
| `feasibility/Inspect.lean` | Public metadata and root-capture actions |
| `feasibility/ForeignFixture.lean` | External executable dependency fixture |
| `feasibility/ReplacementFixture.lean` | Logical-versus-compiled body substitution fixture |
| `feasibility/BadClaim.lean` | Axiom-audit rejection fixture |
| `feasibility/CSimpFixture.lean` | Accepted compiler rewrite and actual stage edges |
| `feasibility/BadCSimpFixture.lean` | Logical claim accepted, forged compiler rewrite rejected |
| `feasibility/check_mutations.py` | Temporary, targeted negative proof tests |
| `feasibility/capture.py` | Reproducible same-file/imported compiler captures and logs |
| `feasibility/lake-manifest.json` | Lake-generated dependency lock, reviewed when first created |
| `feasibility/.evidence/` | Ignored raw logs, generated C, temporary sources, hashes |
| `docs/research/extraction-feasibility.md` | Observed results, evidence tables, stage choice, and next-step decision |

The probe utilities are producer code. Their use of IO, compiler internals, or
partial traversal functions does not expand the normalizer's supported runtime
subset. Do not confuse all declarations in a module with the executable closure
of `LeanGo.Feasibility.normalize`.

## Task 1: Establish the pinned producer workspace

**Files:** Create `.gitignore`, `feasibility/lean-toolchain`,
`feasibility/lakefile.toml`, and `feasibility/LeanGo.lean`.

1. Verify the clean starting point and author identity:

   ```bash
   git status --short --branch
   git config --local user.name franchb
   git config --local user.email hello@franchb.com
   git config --local --get user.name
   git config --local --get user.email
   python3 --version
   export PATH="${ELAN_HOME:-$HOME/.elan}/bin:/usr/local/go/bin:$PATH"
   command -v elan lean lake
   ```

   Expected: a clean working tree, the stated identity, Python 3.11 or newer,
   and the Elan, Lean, and Lake executables. The PATH adjustment covers the
   locally verified installation locations and affects only this shell; adapt
   it for installations elsewhere. A failed command lookup does not establish
   that a tool is uninstalled. Only if Elan is actually absent, install it using
   the [official installation instructions](https://lean-lang.org/install/);
   installation must respect the execution environment's permission policy.
   Tool absence is not a failed feasibility result about extraction.

2. Create `.gitignore` with exactly:

   ```gitignore
   /feasibility/.lake/
   /feasibility/.evidence/
   /feasibility/__pycache__/
   /feasibility/Mutant*.lean
   ```

3. Create `feasibility/lean-toolchain`:

   ```text
   leanprover/lean4:v4.35.0
   ```

4. Create `feasibility/lakefile.toml`:

   ```toml
   name = "lean_go_feasibility"
   version = "0.0.0"
   defaultTargets = ["LeanGo"]

   [[lean_lib]]
   name = "LeanGo"

   [[lean_exe]]
   name = "normalizer-checks"
   root = "LeanGo.Feasibility.Checks"
   ```

5. Create `feasibility/LeanGo.lean`:

   ```lean
   import LeanGo.Feasibility.Model
   ```

6. Reuse and verify the exact installed toolchain:

   ```bash
   cd feasibility
   elan run leanprover/lean4:v4.35.0 lean --version
   lake env lean --version
   lake --version
   cd ..
   ```

   The local follow-up already confirmed this pin is installed. On another
   machine, if Elan reports that this specific toolchain is missing, install it
   with `elan toolchain install leanprover/lean4:v4.35.0` and rerun the checks.
   Do not reinstall solely because the shell PATH omitted Elan's binaries.

   Expected: Lean 4.35.0. Record the actual compiler version/commit in the final
   report. The release tag resolves to
   `eb6cafb7290e8bd5f2937522d8d804f58f02bafc`. Do not run the library build yet:
   the model is introduced by the next task. If Lake creates `lake-manifest.json`,
   review and commit its empty external-package set; do not invent a lockfile.

7. Check whitespace and commit the workspace configuration:

   ```bash
   git add .gitignore feasibility/lean-toolchain feasibility/lakefile.toml feasibility/LeanGo.lean
   git diff --cached --check
   git commit -m "build: pin Lean feasibility workspace"
   ```

## Task 2: Specify checks, then implement and prove the normalizer

**Files:** Create `feasibility/LeanGo/Feasibility/Checks.lean` and
`feasibility/LeanGo/Feasibility/Model.lean`.

1. Create `Checks.lean` before the model, using this complete file:

   ```lean
   import LeanGo.Feasibility.Model

   open LeanGo.Feasibility

   example :
       [Tri.neg .f, Tri.neg .u, Tri.neg .t] = [.t, .u, .f] := rfl

   example :
       [Tri.meet .f .f, Tri.meet .f .u, Tri.meet .f .t,
        Tri.meet .u .f, Tri.meet .u .u, Tri.meet .u .t,
        Tri.meet .t .f, Tri.meet .t .u, Tri.meet .t .t] =
       [.f, .f, .f, .f, .u, .u, .f, .u, .t] := rfl

   example :
       [Tri.join .f .f, Tri.join .f .u, Tri.join .f .t,
        Tri.join .u .f, Tri.join .u .u, Tri.join .u .t,
        Tri.join .t .f, Tri.join .t .u, Tri.join .t .t] =
       [.f, .u, .t, .u, .u, .t, .t, .t, .t] := rfl

   def sample : Expr :=
     .neg (.conj (.var 7) (.disj (.lit .u) (.neg (.var 9))))

   def expected : Expr :=
     .disj (.neg (.var 7)) (.conj (.lit .u) (.var 9))

   example : normalize sample = expected := rfl
   example : normalize (.neg (.neg (.neg (.var 7)))) = .neg (.var 7) := rfl
   example : normalize (.neg (.lit .u)) = .lit .u := rfl
   example : normalize (.var (0 : UInt32)) = .var (0 : UInt32) := rfl
   example : normalize (.var (4294967295 : UInt32)) =
       .var (4294967295 : UInt32) := rfl

   example (e : Expr) (rho : UInt32 → Tri) :
       eval rho (normalize e) = eval rho e := normalize_preserves_eval e rho
   example (e : Expr) : IsNormalForm (normalize e) := normalize_is_normal e

   #print axioms LeanGo.Feasibility.normalize_preserves_eval
   #print axioms LeanGo.Feasibility.normalize_is_normal

   def main : IO Unit := do
     unless normalize sample == expected do
       throw (IO.userError "unexpected normal form or operand order")
     let rho : UInt32 → Tri := fun _ => .u
     unless eval rho (normalize sample) == eval rho sample do
       throw (IO.userError "evaluation changed")
     IO.println "normalizer smoke: OK"
   ```

2. Run the initial failing check from `feasibility/`:

   ```bash
   lake env lean LeanGo/Feasibility/Checks.lean
   ```

   Expected: missing `LeanGo.Feasibility.Model`. A toolchain or Lake error is
   not the intended failure and must be resolved first.

3. Create the model's type definitions and truth-table functions. Start
   `Model.lean` with this complete opening section:

   ```lean
   import Std

   namespace LeanGo.Feasibility

   inductive Tri where
     | f | u | t
     deriving Repr, DecidableEq, BEq

   namespace Tri

   def neg : Tri → Tri
     | .f => .t
     | .u => .u
     | .t => .f

   def meet : Tri → Tri → Tri
     | .f, _ => .f
     | .u, .f => .f
     | .u, _ => .u
     | .t, b => b

   def join : Tri → Tri → Tri
     | .t, _ => .t
     | .u, .t => .t
     | .u, _ => .u
     | .f, b => b

   theorem neg_neg (a : Tri) : neg (neg a) = a := by
     cases a <;> rfl

   theorem neg_meet (a b : Tri) :
       neg (meet a b) = join (neg a) (neg b) := by
     cases a <;> cases b <;> rfl

   theorem neg_join (a b : Tri) :
       neg (join a b) = meet (neg a) (neg b) := by
     cases a <;> cases b <;> rfl

   end Tri
   ```

4. Append the executable definitions and the output predicate:

   ```lean
   inductive Expr where
     | lit : Tri → Expr
     | var : UInt32 → Expr
     | neg : Expr → Expr
     | conj : Expr → Expr → Expr
     | disj : Expr → Expr → Expr
     deriving Repr, DecidableEq, BEq

   def eval (rho : UInt32 → Tri) : Expr → Tri
     | .lit value => value
     | .var id => rho id
     | .neg e => Tri.neg (eval rho e)
     | .conj a b => Tri.meet (eval rho a) (eval rho b)
     | .disj a b => Tri.join (eval rho a) (eval rho b)

   def normalizeAux (p : Bool) : Expr → Expr
     | .lit value => .lit (if p then Tri.neg value else value)
     | .var id => if p then .neg (.var id) else .var id
     | .neg e => normalizeAux (!p) e
     | .conj a b =>
         if p then .disj (normalizeAux true a) (normalizeAux true b)
         else .conj (normalizeAux false a) (normalizeAux false b)
     | .disj a b =>
         if p then .conj (normalizeAux true a) (normalizeAux true b)
         else .disj (normalizeAux false a) (normalizeAux false b)

   def normalize (e : Expr) : Expr :=
     normalizeAux false e

   def IsNormalForm : Expr → Prop
     | .lit _ => True
     | .var _ => True
     | .neg (.var _) => True
     | .neg _ => False
     | .conj a b => IsNormalForm a ∧ IsNormalForm b
     | .disj a b => IsNormalForm a ∧ IsNormalForm b
   ```

5. Append the semantic proof:

   ```lean
   theorem normalizeAux_preserves_eval (e : Expr) (rho : UInt32 → Tri) :
       ∀ p, eval rho (normalizeAux p e) =
         if p then Tri.neg (eval rho e) else eval rho e := by
     induction e with
     | lit value =>
         intro p
         cases p <;> rfl
     | var id =>
         intro p
         cases p <;> rfl
     | neg e ih =>
         intro p
         cases p <;>
           simp [normalizeAux, eval, ih, Tri.neg_neg]
     | conj a b ihA ihB =>
         intro p
         cases p <;>
           simp [normalizeAux, eval, ihA, ihB, Tri.neg_meet]
     | disj a b ihA ihB =>
         intro p
         cases p <;>
           simp [normalizeAux, eval, ihA, ihB, Tri.neg_join]

   theorem normalize_preserves_eval (e : Expr) (rho : UInt32 → Tri) :
       eval rho (normalize e) = eval rho e := by
     simpa [normalize] using normalizeAux_preserves_eval e rho false
   ```

6. Append the normal-form proof and close the namespace:

   ```lean
   theorem normalizeAux_is_normal (e : Expr) :
       ∀ p, IsNormalForm (normalizeAux p e) := by
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

   theorem normalize_is_normal (e : Expr) :
       IsNormalForm (normalize e) :=
     normalizeAux_is_normal e false

   end LeanGo.Feasibility
   ```

7. From `feasibility/`, compile the actual source, checks, and native reference:

   ```bash
   lake build LeanGo
   lake env lean LeanGo/Feasibility/Checks.lean
   lake build normalizer-checks
   lake exe normalizer-checks
   ```

   Expected: both proofs and all checks pass, and the reference prints
   `normalizer smoke: OK`. Record actual axiom lists rather than predicting
   them. If an elementary proof needs syntax adjustments for the pin, retain
   its exact theorem statement and commit the corrected proof.

8. From the repository root, commit the model and checks:

   ```bash
   git add feasibility/LeanGo/Feasibility/Model.lean feasibility/LeanGo/Feasibility/Checks.lean
   git diff --cached --check
   git commit -m "feat: prove three-valued normalization properties"
   ```

## Task 3: Confirm that the proofs reject incorrect implementations

**Verified implementation:** [check_mutations.py](../../../feasibility/check_mutations.py).
This replaces the initial proposed Python listing; use the committed script.

The script checks that each source edit has exactly one intended target, creates
an owned temporary file, and removes it in `finally`. It changes the negated
conjunction's De Morgan rule and requires diagnostics only inside
`normalizeAux_preserves_eval`. Separately, it replaces `normalize` with the
identity and adjusts its evaluation proof to `rfl`; only failure inside
`normalize_is_normal` counts. Both cases require exit 1, at least one ordinary
Lean error, and every error positioned in the intended theorem range. Missing
tools, abnormal exits, unpositioned errors, and unrelated failures cannot pass.
The original model must remain byte-identical.

Run from `feasibility/`:

```bash
python3 check_mutations.py
```

Expected: both mutations rejected at their intended theorems. These committed
controls passed. The report distinguishes additional one-off diagnostic fault
checks from committed reproducible coverage.

## Task 4: Add fail-fast source, claim, and stage inspection

**Verified source files:**

| File | Responsibility |
| --- | --- |
| [ProbeSupport.lean](../../../feasibility/LeanGo/Feasibility/ProbeSupport.lean) | Pinned API checks, claim/source auditing, conservative named dependency inventory, saved-stage lookup |
| [Inspect.lean](../../../feasibility/Inspect.lean) | Audit two exact claims, source closure and both runtime stages; record full public constructor types |
| [ForeignFixture.lean](../../../feasibility/ForeignFixture.lean) | Classify a missing external dependency without linking/calling it |
| [ReplacementFixture.lean](../../../feasibility/ReplacementFixture.lean) | Show a logical identity whose compiled body uses a different implemented_by target |
| [BadClaim.lean](../../../feasibility/BadClaim.lean) | Reject an unexpected project axiom |
| [CSimpFixture.lean](../../../feasibility/CSimpFixture.lean) | Accept an axiom-free compiler rewrite and observe actual base/mono edges |
| [BadCSimpFixture.lean](../../../feasibility/BadCSimpFixture.lean) | Accept the logical root theorem independently, then reject forged rewrite evidence |

These links replace the initial split Lean listings, which predated compiler
simplification auditing. The committed files are the implementation authority.
`ProbeSupport` checks the pinned compiler APIs at elaboration. `auditClaim`
collects transitive axioms and accepts only the stated foundational allowlist.
`sourceClosure` reports original extern/implemented_by attributes and current
CSimp mappings, requires rewrite evidence to exist and be a theorem, audits its
axioms, and follows its target. The normalizer actually uses
`Bool.not → Bool.Internal.not`, justified by `Bool.not_eq_internalNot` with
`[Quot.sound]`; its two public claims separately use `[propext]`.

Current-environment CSimp lookup cannot reconstruct arbitrary earlier scoped
rewrites. This is sufficient only for the controlled module with its actual
bodies inspected. A general adapter needs compile-time provenance or explicit
history restrictions/rejection; end-environment absence is not a proof of no
substitution. The replacement fixture additionally reaches `Nat.rec` CSimp
through its UInt32 zero literal; those are fixture dependencies.

The runtime inventory traverses nested bodies and direct named calls, reporting
local join edges. It is not a full automatic admission checker: empty-argument
fvar references, partial applications, every typed IR form, and primitive
semantics require exhaustive admission or the bounded manual audit recorded in
the report. `safe=true` alone does not prove purity of arbitrary dependencies.
Original metadata records all eight Tri/Expr constructors, parameters, tags,
field counts, and complete constructor types, including passed-through Tri.u
and Expr.var.

Run from `feasibility/`:

```bash
lake build LeanGo.Feasibility.ProbeSupport
lake env lean Inspect.lean
```

Expected: both bodies available as `.code` after import, full original schema,
and the exact source proof/rewrite basis. Task 5 executes all fixtures with the
strict process/diagnostic checks rather than treating any nonzero exit as proof.

## Task 5: Capture reproducible evidence across the module boundary

**Verified implementation:** [capture.py](../../../feasibility/capture.py).
This replaces the initial proposed Python listing, which lacked the final
CSimp and exact-type gates. Use the committed script, not a copied old listing.

The capture creates a same-file probe from the model and `Inspect.lean` actions,
builds the pinned dependencies, captures base/mono with compiler checking, and
compares its sorted inventory to imported inspection. It hashes all known input
files before checking for an unowned temporary probe, hashes the generated probe
when owned, records exact subprocess argv/exits, and preserves failure JSON/logs.
Exclusive creation and owned cleanup protect a pre-existing temporary source.
Generated C is diagnostic producer material, never a runtime fallback.

Successful capture requires all of the following:

- Exact Lean 4.35.0 compiler commit, nonempty base/mono roots, raw root trace
  blocks, and complete named runtime closures at both stages.
- Equal same-file/imported inventories, no unexpected proof/foreign/unsafe
  runtime dependency, and all eight exact original constructor type records.
- Exact collected and accepted axiom records, checked before deduplication;
  no missing, duplicate, or extra records. Both public claims use `[propext]`;
  the actual Bool.not rewrite uses `[Quot.sound]` and its target is traversed.
- Source and stage classification of the foreign and implemented_by fixtures.
  The missing foreign symbol is never linked/called.
- An axiom-free positive CSimp rule with actual root-to-target edges at both
  stages, and rejection of the forged rewrite after its logical claim passes.
- Exit 1 and the sole exact intended diagnostic for each of BadClaim and
  BadCSimpFixture; every other captured command must exit 0. Wrong exits,
  missing errors, or mixed/unrelated diagnostics fail.

Run from `feasibility/`:

```bash
python3 capture.py
cp .evidence/capture.json .evidence/capture-first.json
python3 capture.py
cmp .evidence/capture-first.json .evidence/capture.json
sha256sum .evidence/capture.json
```

Observed final result: 16 input hashes, ten subprocess entries, equal 129-record
same-file/imported inventories, and byte-identical repeated manifests. Only
bad-claim and bad-csimp exit 1 intentionally. The report records the exact hash
and readable curated bodies/schema/closure. Capture's literal successful verdict
still requires manual IR and primitive audit; it does not admit every LCNF form
or prove Go extraction. Keep raw evidence ignored and review the complete three
actual function bodies in each stage before selecting the adapter.

## Task 6: Assess a bounded reuse candidate without importing its implementation

**Files:** Write findings into `docs/research/extraction-feasibility.md` in Task 7.
Use ignored `feasibility/.evidence/lean4-prod/` only for source inspection.

1. Inspect the public `auser/lean4-prod` candidate at exactly
   `991956fe427bb646498fd0912c89b71be71e7beb`:

   ```bash
   mkdir -p feasibility/.evidence
   git clone --no-checkout https://github.com/auser/lean4-prod.git feasibility/.evidence/lean4-prod
   git -C feasibility/.evidence/lean4-prod checkout --detach 991956fe427bb646498fd0912c89b71be71e7beb
   git -C feasibility/.evidence/lean4-prod rev-parse HEAD
   ```

   Expected: the exact commit above. If the directory already exists, inspect
   its origin, status, and revision instead of overwriting it. Read source only;
   do not run its scripts or compiled modules for this assessment.

2. Inspect these exact boundaries:

   ```bash
   rg -n 'extractNames|extractTagged|ExtractedDef' feasibility/.evidence/lean4-prod/lean/Prod/Extract.lean
   rg -n 'validateCodeDefinition|namedClosure|exportNames' feasibility/.evidence/lean4-prod/lean/Prod/Export.lean
   rg -n 'lowerTypeDecl|lowerType|lowerDecl|recursive|numParams|numIndices' feasibility/.evidence/lean4-prod/lean/Prod/Lower.lean
   rg -n 'main|loadExts|enableInitializersExecution|importModules' feasibility/.evidence/lean4-prod/lean/Prod/Emit.lean
   rg -n 'enum Type|enum Expr|enum TypeDecl|struct TypeDecl' feasibility/.evidence/lean4-prod/rust/prod-ir/src/lib.rs
   ```

   Read the complete enclosing definitions, not only matching lines. Record
   which ideas could support a reusable extraction/conformance core and which
   depend on a Rust representation or emitter. In particular, verify the
   recursive-inductive rejection in `lowerTypeDecl`: the monomorphic `Expr`
   needed here is recursive. A matching function name is not API compatibility.

3. Check version and license evidence:

   ```bash
   git -C feasibility/.evidence/lean4-prod show HEAD:lean/lean-toolchain
   rg --files --hidden feasibility/.evidence/lean4-prod -g '*LICENSE*' -g '*COPYING*' -g '*NOTICE*' -g '*toolchain*' -g 'Cargo.toml' -g '!**/.git/**'
   rg -n 'license|MIT|Apache|copyright' feasibility/.evidence/lean4-prod/README.md feasibility/.evidence/lean4-prod/lean feasibility/.evidence/lean4-prod/rust -g '*.md' -g '*.lean' -g '*.toml'
   ```

   Planning-time evidence: Lean 4.32.1 and Rust 1.97.1 are pinned; Rust crates
   declare MIT, but an explicit license grant covering the Lean-side code was
   not found. Verify these facts at the pinned commit. Do not copy or adapt that
   Lean code unless its license coverage is established. The original probe in
   this plan can proceed independently. This task authorizes no outreach or
   upstream publication.

4. Record one of three reuse outcomes, with pinned file links and reasons:

   - Reuse a clearly licensed component whose contracts fit this example.
   - Propose a narrowly scoped upstream change, with no code reuse until its
     license and version compatibility are resolved.
   - Keep the adapter original; document only independently applicable API and
     architecture lessons.

   Account explicitly for recursive datatypes, `UInt32` representation, full
   runtime closure, substitutions, compiler-version coupling, and the boundary
   where source modules execute initializers. Do not broaden this investigation
   into general `Nat` extraction or a Rust backend implementation.

## Task 7: Write the evidence report and make the adapter decision

**Files:** Create `docs/research/extraction-feasibility.md`; update `README.md`
with a link to the report and its actual conclusion. Reconcile this plan and
the design status with the observed evidence. Do not claim a Go package
or a verified compiler exists at this point.

1. Write a report with these exact section responsibilities, using the captured
   observations rather than predicted outcomes:

   | Section | Required evidence |
   | --- | --- |
   | Scope and toolchain | Exported root, Lean version/commit, host platform, source hashes, rerun commands, candidate revision |
   | Source claims | Exact two theorem types, transitive axiom lists, smoke results, both targeted proof-mutation failures and strict rejection diagnostics |
   | Source versus runtime | Original `extern`/`implemented_by` attributes, actual stage substitutions, excluded proof/evaluation declarations, all substitution/foreign fixtures' results |
   | Stage comparison | Base and mono root bodies, same-file/imported results, helper declarations, semantic differences introduced by each stage |
   | Representation | Original `Tri` and `Expr` constructor names/tags/field types, lowered field positions, erasure, `UInt32` identity and range |
   | Runtime closure | One row per reachable declaration/constructor/external leaf with its stage, callers, body or primitive contract, and planned pure-Go mapping |
   | IR forms | Every observed `Code`/`LetValue` form and its proposed representation; local calls, join points, recursion, and allocation strategy |
   | Reuse assessment | Candidate boundaries, recursive-datatype limitation, version/license evidence, and chosen reuse outcome |
   | Trust and limits | Exact remaining trusted components, unsupported cases, resource/termination limits, and claims not established by this investigation |
   | Decision | One outcome from step 3 below, evidence supporting it, and bounded next scope |

   Include short, attributed trace excerpts and exact root-relative source
   locations. Keep raw `.evidence/` artifacts ignored. Publish enough curated
   rows and rerun commands that another developer can reproduce the decision;
   hashes without readable evidence are insufficient.

2. Resolve these questions from the evidence:

   - Can the selected stage retain ordinary Lean function bodies across an
     import boundary without a patched Lean compiler?
   - Is every runtime dependency either inspectable ordinary code or a
     specifically defined operation implementable in pure Go? For each external
     leaf, identify its semantics and implementation obligations; a C symbol
     name is not a Go mapping. Reject unsafe or partial computational definitions
     except for explicitly identified standard primitives with admitted mappings.
   - Is the original public schema sufficient to recover constructors and
     `UInt32` fields after erasure and specialization?
   - Can a small backend express all observed control flow, recursive calls,
     and allocations while preserving constructor order and the unknown value?
   - Which stages already apply implementation substitutions? `base` must not
     be treated as the original logical body merely because it precedes `mono`.
   - What must the future IR interpreter, primitive conformance tests, and
     native-reference differential tests compare to expose a wrong extraction?

   Describe a finite runtime stack/resource strategy for this example. The
   source termination proof does not establish a safe Go call-stack budget.
   Iterative host validation cannot by itself prevent overflow in recursive
   generated execution. Record which measurements are needed to choose the
   future maximum depth/node/work constants; do not invent measured limits.

3. Choose exactly one outcome:

   - **Proceed with pure mono LCNF.** Both source claims and negative checks
     hold; original metadata survives; both roots and their complete closures
     are available across imports; every observed form and primitive has an
     explicit pure-Go mapping; substitutions have a stated trust policy; reuse
     or original implementation has a lawful path. Author a separate full
     backend implementation plan from these concrete findings.
   - **Adjust and repeat a base-focused investigation.** The observed mono
     representation loses essential information or makes this subset needlessly
     difficult, while base has a concrete promising alternative. State the
     precise missing information, change only the probe needed to test base,
     and repeat the closure/metadata/claim gates before planning a backend.
   - **Stop and revise the design.** Necessary bodies or semantics cannot be
     recovered, runtime dependencies cannot be supported within the intended
     pure-Go subset, or an unexplained proof/runtime mismatch remains. Record
     the smallest counterexample and the specific design assumption it refutes.

   Successful compilation alone cannot select the first outcome. License
   uncertainty about one candidate blocks copying that candidate's code, not
   an independently implemented adapter. A negative feasibility result is a
   valid completion of this investigation when its evidence is reproducible.

4. Run the final producer checks from `feasibility/`:

   ```bash
   lake build LeanGo normalizer-checks
   lake env lean LeanGo/Feasibility/Checks.lean
   lake exe normalizer-checks
   python3 check_mutations.py
   python3 capture.py
   ```

   Expected for a proceed result: successful source checks and smoke execution,
   intended mutation rejections, and complete capture evidence. For an adjust
   or stop result, record the exact failing capture condition and retain its
   minimal reproduction; do not report the whole sequence as passing.

5. Check the public boundary and commit the report. The README addition must
   use the observed decision and link to
   `docs/research/extraction-feasibility.md`; describe its producer-prototype status
   accurately. Review every staged line for original generic content, identity,
   and source attribution:

   ```bash
   git status --short
   git diff --check
   git add README.md docs/research/extraction-feasibility.md docs/superpowers/plans/2026-11-11-lean-go-feasibility.md docs/superpowers/specs/2026-11-11-lean-go-extraction-design.md
   git diff --cached --check
   git diff --cached
   git commit -m "docs: record Lean extraction feasibility decision"
   ```

   If a reviewed `feasibility/lake-manifest.json` was created, stage it
   explicitly in the task that first creates it; confirm it has no unexpected external
   packages. Never stage raw evidence, generated C, toolchains, or candidate
   repository contents. Local commits do not authorize GitHub publication.

## Design coverage and the next plan

| Approved requirement | Treatment in this plan |
| --- | --- |
| Ordinary Lean definitions and proofs | Task 2, with exact source claims and Task 3 rejection tests |
| Generic strong-Kleene example and meaningful normalization | Truth tables, ordered De Morgan rules, eval preservation, normal-form theorem |
| Pinned compiler adapter and preserved public types | Tasks 1, 4, and 5 |
| Runtime dependencies, primitives, and implementation substitutions | Tasks 4–5 fixtures and Task 7 complete inventory |
| Reusable core and contribution path | Task 6 bounds reuse; Task 7 defines the minimal adapter/IR seam |
| Pure-Go consumer with `CGO_ENABLED=0` | Hard mapping constraint in Task 7; actual emitter and Go build are a subsequent milestone |
| Immutable validated Go input, cycles/sharing/work limits | Subsequent backend plan; Task 7 supplies representation and stack/resource evidence |
| IR interpreter, conformance corpus, differential and mutation tests | Task 7 defines their observed semantic surface; implementation belongs in the backend plan |
| Later semantic-preservation proof and explicit trust boundary | Source claim audit now; no claim of verified extraction or a verified Go compiler |
| Linux/amd64 and Windows/amd64 runtime tests, independent consumer, releases | Subsequent release plan after a functioning backend; cross-compilation alone is insufficient |
| Public identity and application independence | All tasks and every commit |

The next backend plan must set concrete Go API ownership rules, node/depth/work
limits, serialization and error behavior; define every IR operation and runtime
primitive found here; implement generated Go plus a small reusable support
package; and test `CGO_ENABLED=0` execution on both required platforms. It must
carry forward every approved release requirement. None is considered satisfied
by a successful producer probe.

## Primary sources checked during planning

- [Lean 4.35.0 release](https://github.com/leanprover/lean4/releases/tag/v4.35.0).
- [Pinned LCNF pass ordering and saved stages](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Compiler/LCNF/Passes.lean).
- [Pinned compilation driver and code-generation eligibility](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Compiler/LCNF/Main.lean).
- [Pinned pure LCNF syntax and traversal APIs](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Compiler/LCNF/Basic.lean).
- [Saved-stage API reference](https://lean-lang.org/doc/api/Lean/Compiler/LCNF/PhaseExt.html), unversioned; Task 4 verifies the pinned signatures.
- [Pinned external-function attributes](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Compiler/ExternAttr.lean).
- [Pinned implementation substitutions](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Compiler/ImplementedByAttr.lean).
- [Pinned original declaration metadata](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Declaration.lean).
- [Pinned transitive axiom collector](https://github.com/leanprover/lean4/blob/v4.35.0/src/Lean/Util/CollectAxioms.lean).
- [Reuse candidate at the inspected commit](https://github.com/auser/lean4-prod/tree/991956fe427bb646498fd0912c89b71be71e7beb).

These links document compiler facts and the reuse assessment. Execution results
and the bounded adapter decision are recorded in the
[feasibility report](../../research/extraction-feasibility.md). Task 1/2 listings
retain the approved workspace/model; Task 3–5 source links identify the corrected
implementation used for the evidence. Future Go and release requirements remain
unexecuted.
