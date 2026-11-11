# Ordinary Lean to Pure Go Feasibility Implementation Plan

> **For agentic workers:** Use `superpowers:subagent-driven-development` or
> `superpowers:executing-plans` to execute this plan task by task. Steps below
> are implementation instructions; keep execution status in the task tracker.

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

**The Lean listings and expected feasibility results have not been executed.**
Version checks establish tool availability only. Executing and correcting the
listings against the pinned compiler is the first implementation evidence this
plan must produce. Do not weaken theorems, add axioms, or silently upgrade Lean
to make a probe pass.

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

**Files:** Create `feasibility/check_mutations.py`.

1. Create the following complete script. It never rewrites the real model;
   each mutant is a temporary file inside the Lake package:

   ```python
   from pathlib import Path
   import re
   import subprocess
   import tempfile

   source = Path("LeanGo/Feasibility/Model.lean").read_text()
   semantic_old = "then .disj (normalizeAux true a) (normalizeAux true b)"
   assert source.count(semantic_old) == 1
   semantic = source.replace(
       semantic_old,
       "then .conj (normalizeAux true a) (normalizeAux true b)",
   )
   normalize_old = "def normalize (e : Expr) : Expr :=\n  normalizeAux false e"
   assert source.count(normalize_old) == 1
   shape = source.replace(normalize_old, "def normalize (e : Expr) : Expr :=\n  e")
   proof_old = "  simpa [normalize] using normalizeAux_preserves_eval e rho false"
   assert shape.count(proof_old) == 1
   shape = shape.replace(proof_old, "  rfl")

   cases = [
       ("wrong De Morgan rule", semantic, "theorem normalizeAux_preserves_eval",
        "theorem normalize_preserves_eval"),
       ("identity normalizer", shape, "theorem normalize_is_normal",
        "end LeanGo.Feasibility"),
   ]
   for label, text, start, end in cases:
       lines = text.splitlines()
       lo = next(i + 1 for i, line in enumerate(lines) if line.startswith(start))
       hi = next(i + 1 for i, line in enumerate(lines) if line.startswith(end))
       with tempfile.NamedTemporaryFile(
           mode="w", encoding="utf-8", dir=".", prefix="Mutant",
           suffix=".lean", delete=False,
       ) as stream:
           stream.write(text)
           path = Path(stream.name)
       try:
           run = subprocess.run(
               ["lake", "env", "lean", path.name],
               text=True, capture_output=True,
           )
           output = run.stdout + run.stderr
           positions = [int(n) for n in re.findall(
               re.escape(path.name) + r":(\d+):\d+: error:", output
           )]
           if run.returncode == 0 or not any(lo <= n < hi for n in positions):
               print(output)
               raise SystemExit(f"{label}: expected theorem failure was not observed")
           print(f"{label}: rejected at the expected theorem")
       finally:
           path.unlink()
   assert Path("LeanGo/Feasibility/Model.lean").read_text() == source
   ```

2. From `feasibility/`, run:

   ```bash
   python3 check_mutations.py
   ```

   Expected: both mutations are rejected at their named theorem. An unavailable
   compiler, import error, or unrelated parse error does not count. If the pinned
   compiler diagnostic format differs, inspect it and adjust only the diagnostic
   parser, never convert every nonzero exit into a passing test.

3. Commit the negative checks:

   ```bash
   git add feasibility/check_mutations.py
   git diff --cached --check
   git commit -m "test: reject invalid normalizer mutations"
   ```

## Task 4: Add fail-fast source, claim, and stage inspection

**Files:** Create `feasibility/LeanGo/Feasibility/ProbeSupport.lean`,
`feasibility/Inspect.lean`, `feasibility/ForeignFixture.lean`,
`feasibility/ReplacementFixture.lean`, and `feasibility/BadClaim.lean`.

1. Create `ProbeSupport.lean` with the imports and API checks below. Compile
   this opening section before adding traversal logic:

   ```lean
   import Lean
   import Lean.Compiler.LCNF
   import Lean.Compiler.ExternAttr
   import Lean.Compiler.ImplementedByAttr
   import Lean.Util.CollectAxioms

   open Lean Elab Command
   open Lean.Compiler.LCNF

   #check getBaseDecl?
   #check getMonoDecl?
   #check DeclValue.forCodeM
   #check Code.forM
   #check Lean.getExternAttrData?
   #check Lean.Compiler.getImplementedBy?
   #check Lean.getConstInfoInduct
   #check Lean.getConstInfoCtor
   #check Lean.collectAxioms
   ```

   Run from `feasibility/`:

   ```bash
   lake build LeanGo.Feasibility.ProbeSupport
   ```

   Expected: all named APIs resolve. If not, inspect the corresponding pinned
   source under the toolchain, record the actual signature, and adjust the
   version-specific probe. Do not switch to unversioned documentation or another
   Lean release without recording a design revision. In particular, the web API
   reference for `PhaseExt` was not version-qualified during planning.

2. Append the source audit and transitive axiom check:

   ```lean
   namespace Probe

   def reportExtern (data : ExternAttrData) : CoreM Unit := do
     for entry in data.entries do
       match entry with
       | .standard backend symbol =>
           logInfo m!"EXTERN standard backend={backend} symbol={symbol}"
       | .inline backend pattern =>
           logInfo m!"EXTERN inline backend={backend} pattern={pattern}"
       | .adhoc backend =>
           logInfo m!"EXTERN adhoc backend={backend}"
       | .opaque =>
           logInfo "EXTERN opaque: body unavailable"

   partial def sourceClosure
       (pending : List Name) (seen : List Name := []) : CoreM Unit := do
     match pending with
     | [] => pure ()
     | name :: rest =>
         if seen.contains name then
           sourceClosure rest seen
         else
           let env ← getEnv
           let some info := env.find? name
             | throwError "Unresolved source dependency {name}"
           logInfo m!"SOURCE {name}"
           if let some replacement := Compiler.getImplementedBy? env name then
             logInfo m!"SOURCE_REPLACEMENT {name} -> {replacement}"
           if let some data := getExternAttrData? env name then
             logInfo m!"SOURCE_EXTERN {name}"
             reportExtern data
           let dependencies := match info with
             | .defnInfo value => value.value.getUsedConstants.toList
             | .opaqueInfo value => value.value.getUsedConstants.toList
             | _ => []
           sourceClosure (dependencies ++ rest) (name :: seen)

   def auditClaim (name : Name) : CoreM Unit := do
     let info ← getConstInfo name
     logInfo m!"CLAIM {name}: {info.type}"
     let axioms ← collectAxioms name
     for axiomName in axioms do
       unless [``propext, ``Quot.sound, ``Classical.choice].contains axiomName do
         throwError "Unexpected axiom {axiomName} in {name}"
     logInfo m!"AXIOMS_ACCEPTED {name}: {axioms}"

   end Probe
   ```

   This source traversal is deliberately an over-approximation used to find
   original-body attributes before lowering erases them. It is not the runtime
   closure. Theorem bodies are audited separately; no import is rejected merely
   because it contains proof code.

3. Append the stage closure collector:

   ```lean
   namespace Probe

   def directCalls (decl : Decl .pure) : CoreM (Array Name) := do
     let found ← IO.mkRef (#[] : Array Name)
     decl.value.forCodeM (m := CoreM) fun code =>
       code.forM (m := CoreM) fun node => do
         match node with
         | .let binding _ =>
             match binding.value with
             | .const name .. =>
                 found.modify fun names => names.push name
             | .fvar _ args =>
                 unless args.isEmpty do
                   throwError "Unresolved indirect call in {decl.name}"
             | _ => pure ()
         | .jmp target _ =>
             logInfo m!"JOIN_EDGE {decl.name} target={target.name}"
         | _ => pure ()
     found.get

   partial def reportClosure
       (stage : String)
       (lookup : Name → CoreM (Option (Decl .pure)))
       (pending : List Name)
       (seen : List Name := []) : CoreM Unit := do
     match pending with
     | [] => pure ()
     | name :: rest =>
         if seen.contains name then
           reportClosure stage lookup rest seen
         else
           let seen := name :: seen
           let env ← getEnv
           if let some replacement := Compiler.getImplementedBy? env name then
             logInfo m!"REPLACEMENT {stage} {name} -> {replacement}"
             reportClosure stage lookup (replacement :: rest) seen
           else
             match env.find? name with
             | some (.ctorInfo ctor) =>
                 logInfo m!"CONSTRUCTOR {stage} {name} tag={ctor.cidx} fields={ctor.numFields}"
                 reportClosure stage lookup rest seen
             | _ =>
                 let some decl ← lookup name
                   | throwError "Unresolved {stage} dependency {name}"
                 match decl.value with
                 | .extern data =>
                     logInfo m!"EXTERNAL_LEAF {stage} {name}"
                     reportExtern data
                     reportClosure stage lookup rest seen
                 | .code _ =>
                     logInfo m!"CODE {stage} {name} safe={decl.safe} recursive={decl.recursive}"
                     let calls ← directCalls decl
                     for callee in calls do
                       logInfo m!"EDGE {stage} {name} -> {callee}"
                     reportClosure stage lookup (calls.toList ++ rest) seen

   def captureRoot (name : Name) : CoreM Unit := do
     let some base ← getBaseDecl? name
       | throwError "Missing base-stage root {name}"
     let some mono ← getMonoDecl? name
       | throwError "Missing mono-stage root {name}"
     match base.value with
     | .code _ => logInfo m!"CAPTURED base {base.name} size={base.size}"
     | .extern _ => throwError "Base root has no implementation body"
     match mono.value with
     | .code _ => logInfo m!"CAPTURED mono {mono.name} size={mono.size}"
     | .extern _ => throwError "Mono root has no implementation body"
     reportClosure "base" getBaseDecl? [name]
     logInfo "CLOSURE_COMPLETE base"
     reportClosure "mono" getMonoDecl? [name]
     logInfo "CLOSURE_COMPLETE mono"

   end Probe
   ```

   This is a conservative named-dependency inventory with rejection of
   unresolved indirect applications, not a complete call graph. It traverses
   nested local functions and join-point bodies; `JOIN_EDGE` records local jumps
   for manual control-flow review. Constant applications also include constructor
   uses and partially applied/global values. Empty-argument local references do
   not prove that higher-order values cannot escape. Audit these and every
   other `Code` and `LetValue` form found
   in the actual traces against pinned `Basic.lean`. An empty edge list is not
   evidence that arbitrary IR forms are safe. Unresolved indirect calls are a
   bounded-subset failure requiring investigation, not permission to drop calls.
   An external leaf is classified, not accepted for extraction automatically.

4. Create `Inspect.lean`. The marker is used by Task 5 to run identical actions
   with same-file definitions and imported definitions:

   ```lean
   import LeanGo.Feasibility.ProbeSupport
   import LeanGo.Feasibility.Model

   -- PROBE ACTIONS
   set_option trace.Compiler.saveBase false
   set_option trace.Compiler.saveMono false

   open Lean Elab Command
   open Lean.Compiler.LCNF

   run_cmd do
     liftCoreM do
       Probe.auditClaim ``LeanGo.Feasibility.normalize_preserves_eval
       Probe.auditClaim ``LeanGo.Feasibility.normalize_is_normal
       Probe.sourceClosure [``LeanGo.Feasibility.normalize]
       Probe.captureRoot ``LeanGo.Feasibility.normalize

   run_cmd do
     for name in [``LeanGo.Feasibility.Tri, ``LeanGo.Feasibility.Expr] do
       let info ← getConstInfoInduct name
       logInfo m!"TYPE {info.name} parameters={info.numParams}"
       for ctorName in info.ctors do
         let ctor ← getConstInfoCtor ctorName
         logInfo m!"CTOR {ctor.name} tag={ctor.cidx} parameters={ctor.numParams} fields={ctor.numFields}"
         logInfo m!"CTOR_TYPE {ctor.type}"
   ```

5. Create `ForeignFixture.lean`:

   ```lean
   import LeanGo.Feasibility.ProbeSupport

   namespace ForeignFixture
   @[extern "lean_go_missing_foreign"]
   opaque foreign : UInt32 → UInt32
   def root (value : UInt32) : UInt32 := foreign value
   end ForeignFixture

   open Lean Elab Command
   open Lean.Compiler.LCNF
   run_cmd do
     liftCoreM do
       Probe.sourceClosure [``ForeignFixture.root]
       Probe.reportClosure "base" getBaseDecl? [``ForeignFixture.root]
       Probe.reportClosure "mono" getMonoDecl? [``ForeignFixture.root]
   ```

   This fixture only compiles to evidence, never links or calls its foreign
   symbol. Its external leaf must remain visible in the inventory.

6. Create `ReplacementFixture.lean`:

   ```lean
   import LeanGo.Feasibility.ProbeSupport

   namespace ReplacementFixture
   def replacement (_ : UInt32) : UInt32 := 0
   @[implemented_by replacement]
   def root (value : UInt32) : UInt32 := value
   theorem logical_identity (value : UInt32) : root value = value := rfl
   end ReplacementFixture

   open Lean Elab Command
   open Lean.Compiler.LCNF
   run_cmd do
     liftCoreM do
       Probe.sourceClosure [``ReplacementFixture.root]
       Probe.reportClosure "base" getBaseDecl? [``ReplacementFixture.root]
       Probe.reportClosure "mono" getMonoDecl? [``ReplacementFixture.root]
   ```

   The logical identity theorem can pass while the compiler replaces its
   function. The source report must identify that substitution. Do not infer
   equivalence from the replacement's type or from either function being safe.

7. Create `BadClaim.lean`:

   ```lean
   import LeanGo.Feasibility.ProbeSupport

   namespace BadClaim
   axiom untrusted : False
   theorem advertised : False := untrusted
   end BadClaim

   open Lean Elab Command
   run_cmd do
     liftCoreM <| Probe.auditClaim ``BadClaim.advertised
   ```

8. From `feasibility/`, rebuild support:

   ```bash
   lake build LeanGo.Feasibility.ProbeSupport
   ```

   Expected: successful compilation. Task 5 executes every fixture and checks
   the expected positive and negative evidence. Do not include their functions
   in the normalizer's export set.

9. Commit the probe sources:

   ```bash
   git add feasibility/LeanGo/Feasibility/ProbeSupport.lean feasibility/Inspect.lean feasibility/ForeignFixture.lean feasibility/ReplacementFixture.lean feasibility/BadClaim.lean
   git diff --cached --check
   git commit -m "feat: inspect Lean source and pure compiler stages"
   ```

## Task 5: Capture reproducible evidence across the module boundary

**Files:** Create `feasibility/capture.py`. Generate ignored artifacts under
`feasibility/.evidence/`; temporarily create and remove
`feasibility/ProbeSameFile.lean`.

1. Create the capture harness with this complete file:

   ```python
   #!/usr/bin/env python3
   import hashlib
   import json
   import re
   import subprocess
   from pathlib import Path

   ROOT = Path(__file__).resolve().parent
   OUT = ROOT / ".evidence"
   OUT.mkdir(exist_ok=True)
   MODEL = ROOT / "LeanGo/Feasibility/Model.lean"
   INSPECT = ROOT / "Inspect.lean"
   TEMP = ROOT / "ProbeSameFile.lean"
   PREFIX = "LeanGo.Feasibility."
   MARKERS = (
       "CAPTURED", "CLOSURE_COMPLETE", "CODE", "EDGE", "CONSTRUCTOR",
       "REPLACEMENT", "EXTERNAL_LEAF", "EXTERN", "TYPE", "CTOR", "CTOR_TYPE",
       "SOURCE", "SOURCE_REPLACEMENT", "SOURCE_EXTERN", "AXIOMS_ACCEPTED",
   )
   PATTERN = re.compile(r"\b(" + "|".join(MARKERS) + r")\s+.*")
   FLAGS = [
       "-Dcompiler.postponeCompile=false",
       "-Dcompiler.check=true",
       "-Dtrace.Compiler.saveBase=true",
       "-Dtrace.Compiler.saveMono=true",
   ]
   report = {"commands": [], "inputs": {}, "inventories": {}, "verdict": "failed"}

   def run(label, command):
       result = subprocess.run(
           command, cwd=ROOT, text=True, stdout=subprocess.PIPE,
           stderr=subprocess.STDOUT, check=False,
       )
       (OUT / f"{label}.log").write_text(result.stdout)
       report["commands"].append({
           "label": label, "argv": command, "exit_code": result.returncode,
       })
       return result

   def require(condition, message):
       if not condition:
           raise RuntimeError(message)

   def inventory(text):
       return sorted(set(
           match.group(0).strip()
           for line in text.splitlines()
           if (match := PATTERN.search(line))
       ))

   def check_normalizer(label, result):
       require(result.returncode == 0, f"{label}: compiler failed; inspect log")
       records = inventory(result.stdout)
       report["inventories"][label] = records
       for stage in ("base", "mono"):
           require(any(row.startswith(f"CAPTURED {stage} {PREFIX}normalize size=")
                       for row in records), f"{label}: missing {stage} root")
           require(f"CLOSURE_COMPLETE {stage}" in records,
                   f"{label}: incomplete {stage} closure")
       for theorem in ("normalize_preserves_eval", "normalize_is_normal"):
           require(any(row.startswith(f"AXIOMS_ACCEPTED {PREFIX}{theorem}:")
                       for row in records), f"{label}: missing claim audit")
       for name in ("Tri", "Expr"):
           require(f"TYPE {PREFIX}{name} parameters=0" in records,
                   f"{label}: missing original type metadata")
       for name in ("Tri.f", "Tri.u", "Tri.t", "Expr.lit", "Expr.var",
                    "Expr.neg", "Expr.conj", "Expr.disj"):
           require(any(row.startswith(f"CTOR {PREFIX}{name} tag=")
                       for row in records), f"{label}: missing constructor {name}")
       forbidden = (PREFIX + "eval", PREFIX + "normalize_preserves_eval",
                    PREFIX + "normalize_is_normal")
       runtime_rows = [row for row in records
                       if row.startswith(("CODE ", "EDGE "))]
       require(not any(name in row for row in runtime_rows for name in forbidden),
               f"{label}: proof/reference code appears in runtime closure")
       return records

   def compile_file(label, filename):
       return run(label, ["lake", "env", "lean", *FLAGS,
                          "-c", f".evidence/{label}.c", filename])

   created_temp = False
   try:
       for path in sorted(ROOT.rglob("*.lean")):
           relative = path.relative_to(ROOT)
           if any(part in {".lake", ".evidence"} for part in relative.parts):
               continue
           require(path != TEMP, "Temporary probe already exists; inspect it first")
           report["inputs"][str(relative)] = hashlib.sha256(path.read_bytes()).hexdigest()
       for name in ("lean-toolchain", "lakefile.toml", "capture.py",
                    "check_mutations.py"):
           report["inputs"][name] = hashlib.sha256((ROOT / name).read_bytes()).hexdigest()

       version = run("version", ["lake", "env", "lean", "--version"])
       require(version.returncode == 0 and "version 4.35.0" in version.stdout,
               "Wrong or unavailable Lean version")
       for target in ("LeanGo", "LeanGo.Feasibility.ProbeSupport"):
           result = run("build-" + target, ["lake", "build", target])
           require(result.returncode == 0, f"Build failed: {target}")

       sections = INSPECT.read_text().split("-- PROBE ACTIONS")
       require(len(sections) == 2, "Expected exactly one probe-action marker")
       source = ("import LeanGo.Feasibility.ProbeSupport\n"
                 + MODEL.read_text() + "\n" + sections[1])
       with TEMP.open("x") as handle:
           handle.write(source)
       created_temp = True
       report["inputs"][TEMP.name] = hashlib.sha256(source.encode()).hexdigest()
       same = compile_file("samefile", TEMP.name)
       same_records = check_normalizer("samefile", same)
       for stage in ("Base", "Mono"):
           require(f"[Compiler.save{stage}]" in same.stdout,
                   f"No raw save{stage} trace; inspect compiler trace configuration")

       imported = compile_file("imported", "Inspect.lean")
       imported_records = check_normalizer("imported", imported)
       require(same_records == imported_records,
               "Same-file/imported inventories differ; explain before choosing a stage")

       foreign = compile_file("foreign", "ForeignFixture.lean")
       require(foreign.returncode == 0, "Foreign fixture failed before classification")
       require("SOURCE_EXTERN ForeignFixture.foreign" in foreign.stdout,
               "Foreign source attribute was missed")
       for stage in ("base", "mono"):
           require(f"EXTERNAL_LEAF {stage} ForeignFixture.foreign" in foreign.stdout,
                   f"Foreign executable dependency was missed at {stage}")
       report["inventories"]["foreign"] = inventory(foreign.stdout)

       replaced = compile_file("replacement", "ReplacementFixture.lean")
       require(replaced.returncode == 0, "Replacement fixture failed before classification")
       require("SOURCE_REPLACEMENT ReplacementFixture.root -> ReplacementFixture.replacement"
               in replaced.stdout, "Original replacement attribute was missed")
       for stage in ("base", "mono"):
           require(f"REPLACEMENT {stage} ReplacementFixture.root -> ReplacementFixture.replacement"
                   in replaced.stdout, f"Substitution was missed at {stage}")
       report["inventories"]["replacement"] = inventory(replaced.stdout)

       bad = compile_file("bad-claim", "BadClaim.lean")
       require(bad.returncode != 0 and "Unexpected axiom BadClaim.untrusted" in bad.stdout,
               "Axiom fixture did not fail for the intended reason")
       report["verdict"] = "capture checks passed; manual IR and primitive audit required"
       print(report["verdict"])
   finally:
       if created_temp:
           TEMP.unlink()
       (OUT / "capture.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
   ```

   These checks establish capture and classification, not Go correctness.
   The native C output is ignored diagnostic material. It is never a consumer
   artifact or a fallback for the proposed pure-Go runtime.

2. Run from `feasibility/`:

   ```bash
   python3 -m py_compile capture.py check_mutations.py
   python3 check_mutations.py
   python3 capture.py
   ```

   Expected: both proof mutations rejected; then
   `capture checks passed; manual IR and primitive audit required`.
   Inspect `.evidence/capture.json` and every version, build, and fixture log.
   Every successful root capture must have a nonempty implementation body,
   complete classified dependency closure, and original constructor metadata.
   For the same-file run, locate the normalizer's actual body in each raw stage
   trace; a trace marker emitted for another definition does not meet this gate.
   Imported definitions need available bodies, not re-emitted compilation traces.

   A nonzero probe result is evidence to investigate. Missing saved bodies,
   unsupported indirect calls, unexpected substitutions, and unavailable
   dependencies must stay visible in the report. If pretty-print formatting
   alone differs, record the exact difference before correcting a marker parser;
   never delete a semantic check to obtain success.

3. Verify determinism with an unchanged source tree:

   ```bash
   cp .evidence/capture.json .evidence/capture-first.json
   python3 capture.py
   cmp .evidence/capture-first.json .evidence/capture.json
   ```

   Expected: `cmp` exits zero. The JSON uses relative paths, input hashes,
   exit statuses, and sorted inventories; raw logs retain the complete bodies.
   Explain any nondeterministic names or inventories before adopting this
   representation as an extraction boundary.

4. Read the captured bodies against pinned compiler source. Record all observed
   `Code` and `LetValue` forms, local functions/join points, recursion edges,
   allocations, constructor fields, erased values, and external leaves in the
   report from Task 7. Check whether specialization introduces helper declarations
   that must be exported with the root. Follow each dependency until a body,
   constructor, or explicitly described primitive is accounted for.

   The runtime collector is intentionally narrow: an unmodeled IR form or
   runtime operation blocks the extraction decision. Do not use its successful
   exit as evidence that all compiler forms are supported.

5. Commit only the harness:

   ```bash
   git add feasibility/capture.py
   git diff --cached --check
   git commit -m "test: capture compiler stages and audit fixtures"
   ```

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
with a link to the report and its actual conclusion. Do not claim a Go package
or a verified compiler exists at this point.

1. Write a report with these exact section responsibilities, using the captured
   observations rather than predicted outcomes:

   | Section | Required evidence |
   | --- | --- |
   | Scope and toolchain | Exported root, Lean version/commit, host platform, source hashes, rerun commands, candidate revision |
   | Source claims | Exact two theorem types, transitive axiom lists, smoke results, both targeted proof-mutation failures |
   | Source versus runtime | Original `extern`/`implemented_by` attributes, actual stage substitutions, excluded proof/evaluation declarations, both fixtures' results |
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
   `docs/research/extraction-feasibility.md`; keep its current design-only claims
   accurate. Review every staged line for original generic content, identity,
   and source attribution:

   ```bash
   git status --short
   git diff --check
   git add README.md docs/research/extraction-feasibility.md
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

These links document compiler facts and the reuse assessment. The feasibility
code listings are proposed original implementation, pending execution against
the pinned version.
