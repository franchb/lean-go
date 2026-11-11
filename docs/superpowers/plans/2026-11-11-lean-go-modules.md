# General Lean module extraction implementation plan

**Goal:** Extract user-supplied ordinary Lean modules within the documented pure
monomorphic subset into bounded standalone Go packages with cgo disabled.

**Architecture:** Add a compilation-time source audit, dynamic pure-mono exporter,
strict v2 IR validator, and generic-data Go emitter alongside the existing v1
pilot. A Python 3.15 command managed by uv assembles fresh producer workspaces and
connects the stages. Keep source evidence distinct from proof of compiler correctness.

**Tech stack:** Lean 4.35.0 at the existing pinned commit, Go 1.27.4, Python 3.15,
uv, standard libraries only. Execute with subagent-driven development and independent
specification and quality reviews. Tracking lives in the task tracker; numbered
steps here describe implementation rather than a second completion checklist.

## 1. Preserve the v1 baseline and define the v2 contract

Create `docs/superpowers/specs/2026-11-11-lean-go-modules-design.md` and this plan.
Use `internal/moduleir/{types,decode,validate,identity}.go` for the new schema,
`internal/modulegen/{emit,identity}.go` and its runtime template for output, and
`producer/LeanGo/Module/{Audit,Export}.lean` for source admission and extraction.

Retain v1 code and fixture behavior. The new metadata includes schema/profile,
stage, pinned Lean version/commit, module/root, input digests, types, empty claims,
rewrites, source provenance, and functions. Existing param, constructor, value,
alternative, and code representations remain applicable. New public interfaces:

```go
func Decode(r io.Reader) (*Program, error)
func Validate(p *Program) error
func Generate(p *moduleir.Program) (map[string][]byte, error)
```

Use canonical deterministic serialization, full input identity, generator and
validator source digests, and generated-file hashes. Read-only supplied IR is not
authenticated merely because its metadata passes validation.

## 2. Capture and enforce source history

In `Audit.lean`, install an initializer wrapping the previous `compileDeclsRef`.
Before delegation, record each source declaration's direct constants and active
CSimp mappings. Store records in a synchronous persistent extension, including
replay support for async/module exports. Disable postponed compilation in the
controlled build. Traverse original source dependencies at export, not merely
optimized calls. Reject missing provenance for local definitions, unsupported
extern/implemented_by/unsafe/opaque/axiom dependencies and custom passes. Permit
only the audited built-in Bool rewrite and exact proof evidence.

First exercise temporary modules: a helper compiled inside a local CSimp scope,
the same source after scope removal, and a root that inlines the helper. A rewrite
backed by a fresh axiom must be rejected despite the empty final CSimp map. Compare
ordinary and `module` headers, imported source modules, and async elaboration.
Do not call this feature complete if local rewrite history can disappear.

## 3. Export dynamic pure-mono functions and types

In `Export.lean`, take configured module/root names and discover reachable mono
function bodies. Reuse the v1 encoder's explicit checks for const applications,
fvar arguments, exhaustive cases, and lexical bindings. Discover zero-parameter,
zero-index inductive schemas from function and constructor field types. Represent
UInt32 as an opaque primitive. Sort declaration lists deterministically and reject
unsupported LCNF with the responsible declaration and operation in the error.

Create two unrelated source families under `examples/modules/`: enum/tree
transformation with a recursive helper, and a distinct enum/selection module with
local imports. Test identity and scalar pass-through as additional roots. Changing
a function's accepted behavior must change generated output without editing an
admission hash. Test polymorphic data, arithmetic, effects, partial application,
and unsupported source rewrites as negative fixtures.

## 4. Validate and compile v2 IR

Write negative tests in `internal/moduleir/validate_test.go` before implementation:
duplicate/null/unknown/missing fields; duplicate constructor tags/names; unknown
types; wrong saturated calls; leaked branch binders; missing case arms; unbound
returns; unreachable extra functions; mutual recursion; cyclic code pointers;
oversized input. A renamed enum/function with the same well-typed body must pass.

In `internal/modulegen`, generate package `extracted` with a generic owned tree
representation. Public behavior is `Import(input, limits)` and
`Run(arguments, limits)`, plus immutable type/constructor/field/scalar accessors.
Map full Lean names to internal numeric identifiers so punctuation cannot inject
Go source. Emit real Go control statements and function-specific frame states.
Use explicit stacks for import and self recursion. Budget all repeated occurrences
before expansion, output reuse, allocations, steps and frames. Reject zero handles
and mismatched root arguments. Return an invalid Value on every error.

Unit tests compile generated temporary packages with `CGO_ENABLED=0`, exercise
semantic results, deep input, cycles, exponential sharing, source ownership,
every budget, incorrect root types/arity, and valid UInt32 zero/max values.
Reuse `gogen.Publish` for its existing non-destructive output publication contract.

## 5. Add fresh-module orchestration and Python pin

Create `.python-version`, `pyproject.toml`, and `uv.lock` with Python >=3.15,<3.16
and no unnecessary dependencies. Add `scripts/export_module.py` plus focused
tests. Its CLI accepts `--source-dir DIR --module NAME --root NAME --output FILE`.
Snapshot regular local Lean files, reject symlinks/escaping or reserved names,
inject the trusted audit import in a valid header position, and build only from
copied sources and pinned trusted producer files. Reject missing imports and
unsupported external project dependencies rather than using stale `.olean` files.
Check exact tool versions, dependency-free generated Lake configuration, output
size, and freshness. Output metadata uses relative logical paths, never machine
paths. Pass subprocess arguments as arrays and enforce execution timeouts.

Extend `scripts/generate.py` to select v2 only when all three module arguments
are provided. Preserve v1 defaults and existing output-overlap checks. Verify
snapshotted source/producer digests before invoking Go. Extend `cmd/lean-go` with
strict schema dispatch to the appropriate independent decoder/backend. Unknown
schemas fail. Keep identical-output no-op and differing-output preservation.

Move producer test invocations to `uv run --locked python`. Use `sys.executable`
for Python child processes. Add tests for wrong Python minor version, hostile
module/path arguments, stale inputs, imports, output collision, and subprocess
failure before publication. Update current usage docs; preserve dated feasibility
evidence with its original scope and tool-version context.

## 6. Integration, review, and local handoff

Create `integration/modules_test.go` with extracted independent examples and
opt-in fresh-producer tests. Verify repeat builds byte-for-byte and execute changed
source without an allowlist change. Compare representative generated results
against native Lean, and run an isolated standalone consumer with PATH limited to
Go and `CGO_ENABLED=0`, module fetching disabled, and no producer tools available.
Regenerate any v1 identities affected by deliberate Python script changes, retaining
the fixed model hash and meaning of v1 claims.

Run from the public repository:

```sh
uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
git diff --check
```

Run new module producer integration gates explicitly, Windows cross-compilation,
and relevant source-proof smoke tests. Obtain independent specification review,
fix every material finding, then obtain quality review and recheck fixes. Use
Ripwire edit checks for changed existing symbols and quality delta for the diff.
Document the usable command, generic API, exact supported subset, provenance
policy, test evidence, and platform gaps in README and a new research report.
Inspect all new public text/history for private project references. Commit locally
with the established public identity; do not publish to GitHub without instruction.
