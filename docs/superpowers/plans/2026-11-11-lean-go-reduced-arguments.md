# Reduced-argument helpers implementation plan

**Goal:** Export authenticated compiler-generated reduced helpers while retaining
ordinary Lean source signatures and source-audit coverage.

**Architecture:** Separate compiler evidence from source provenance, resolve runtime
helper signatures through their source owner, and reuse v4 IR and Go generation.
Use subagent-driven implementation and independent specification/quality reviews.
The task tracker owns progress; the numbered sections below specify implementation.

**Toolchain:** Lean 4.35.0, Go 1.27.4, Python 3.15 via uv; consumer CGO disabled.

## 1. Establish the failure and compiler evidence

Create `scripts/test_reduced_admission.py` with a fresh-source test around:

```lean
@[noinline] def add (x y : UInt32) : UInt32 := x + y
def run (flag : Bool) (_unused : UInt32) (x y bias : UInt32) : UInt32 :=
  let selected := if flag then x else y
  let first := add selected bias
  let second := add first bias
  let third := add second bias
  add third bias
```

Run through `uv run --locked python scripts/export_module.py` using a temporary
source directory. Observe `Unknown constant ...run._redArg` before the change.

Create `producer/LeanGo/Module/Reduced.lean` for compiler evidence, snapshot/mapping
validation and the persistent extension. Use the pinned `Decl .pure` structural
equality for snapshots, not a hash or FVarId equality between declarations.
The stored evidence has owner, helper, source module, owner/helper declarations,
retained positions and all-unused status; rejected collision events remain visible.

Modify `Audit.lean` around its existing `original names opts` callback: preserve
pre-compilation source capture, collect candidate prior states, invoke the compiler,
then observe new/changed helpers and validate exact owner wrappers. Do not write
generated helpers to source `provenanceExt`. Keep normal source failures scoped
to the selected dependency/runtime closure rather than failing unrelated modules.

## 2. Resolve source-shaped helper functions

Modify `Export.lean` function/call resolution. Ordinary functions continue through
`getConstInfo`; evidenced helpers use their owner's full source signature and
retained positions. Move the existing `exportTypes` definition above the resolver
if needed, then use `discard <| exportTypes (domains ++ [result])` to validate full
source field closure independently of emitted runtime types.

Existing argument/result representation adapters remain in place. A helper with
retained parameters binds them using the corresponding source domains. An
all-unused helper must have one void compiler parameter but zero emitted parameters;
call emission consumes exactly the erased dummy without constructing a value.
Reject any unexpected dummy variable use. Keep root signatures unchanged.

Resolve generated provenance during `auditSource`: require valid separate evidence,
emit the exact helper-to-owner record, and audit the owner using unchanged rules.
Preserve the initial root audit, final full-closure audit, mutual-recursion rejection,
and all IR/resource accounting. No backend, runtime or schema change is required.

## 3. Admission and integration tests

Expand fresh admission tests for unused arguments at all positions, multiple drops,
all-unused and nullary owners, source wrappers, direct noinline calls, recursion,
classic/modern imports, and a source function merely named with `_redArg`.
Assert original root signatures, actual helper runtime functions, reduced source
signatures and derivation edges pointing to unchanged owner provenance.

Test collision declarations before and after owner compilation, including an
overwritten authored helper selected as root; stale snapshots, wrong module/owner,
nonordered/duplicate retained positions, unsupported erased source fields, invalid
dummy operands and retained closures must reject. Cover erased foreign/axiomatic
dependencies and local/scoped/late rewrite/extern/implemented_by changes. Rejections
must preserve prior output and must not be mere source syntax errors.

Create `examples/modules/Reduced.lean`, `examples/reference/ReducedReference.lean`
and `integration/reduced_test.go`. Use source wrappers and a root with unused
arguments that calls reduced helpers. Compare boundary and seeded cases with
native Lean and Go arithmetic. Check root arity/type validation even for unused
inputs, immutable ownership, budgets, Go-only consumption and fresh reproducibility.
Exercise all-unused and recursive helper roots in isolated generated packages.

## 4. Producer identity and generation

Add `producer/LeanGo/Module/Reduced.lean` to `scripts/export_module.py` producer
inputs and its freshness regression test. Keep v4/profile-v3 metadata unchanged.
Generate the new fixture and regenerate all previous general packages through
fresh output directories, then copy generated outputs. Check unchanged old Go
bytes and semantic IR after removing input hashes; preserve audited v1 artifacts.

## 5. Verification, documentation and local handoff

Run targeted tests, then:

```sh
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```

Obtain independent specification then quality review. Run Ripwire edit checks and
quality delta; manually review Lean, which is not indexed. Resolve defects and
rerun affected gates. Add `docs/research/extraction-reduced-arguments.md`, update
README and mark the prior join report's limitation as historical. State derivation
edge semantics, evidence requirements, wrapper disappearance and unsupported forms.
Check links, source identity, privacy separation and `git diff --check`.

Commit as `franchb <hello@franchb.com>`, transfer the reviewed local branch to the
standalone checkout, verify identical inputs and Go tests there, then close and
sync the tracker task. No remote is created and no GitHub push is performed.
