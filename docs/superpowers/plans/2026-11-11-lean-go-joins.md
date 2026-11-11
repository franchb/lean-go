# Local joins implementation plan

**Goal:** Support ordinary Lean branch results through lexically scoped local
continuations while preserving source-shaped records and pure Go execution.

**Architecture:** Extend the typed IR with join declarations and terminal jumps;
compile them to existing frame states. Preserve source audits and representation
adapters. The task tracker owns execution status; this document specifies work.

**Toolchain:** Lean 4.35.0, Go 1.27.4, Python 3.15 through uv. Use independent
implementation areas and subsequent specification and quality review.

## 1. Strict IR and compiled control flow

Add v4/profile-v3 constants and Join/Code fields to `internal/moduleir`.
Use `args,omitzero` to retain required empty jump arrays. Extend strict decoding,
version gates, lexical validation, type/function closure, structural accounting,
and irrelevant-field rejection. Establish failing tests before implementation,
including absent/null arrays, hidden invalid bodies, scope escapes and boundaries.

Extend `internal/modulegen` collection to both subtrees and join parameter slots.
Emit declaration transitions and parallel slot assignments at jumps. Test captured
values across calls, nested joins, zero arguments, repeated/reordered operands,
exact step/slot failures and no extra frame/allocation charge. Bump generator and
manifest versions without changing the runtime template. Cover old profile rejection.

## 2. Checked Lean producer

Start with a failing source case whose conditional result feeds a nontrivial
record update. Extend `producer/LeanGo/Module/Export.lean` with a lexical join scope
and shared fresh IDs. Check compiler join signatures, typed fvar arguments and
scope before emission. Bind join parameters directly as mono values; keep the
enclosing source result for return boxing. Charge both code trees and all binders.
Keep `.fun`/closures unsupported and source audits unchanged. Add fresh-source
tests for conditionals, matches, wrapper results, nested joins, helper calls and
admission failures hidden behind branch continuations.

## 3. Driver, fixtures and integration

Update `scripts/export_module.py` to v4/profile-v3 and CLI schema dispatch; update
fake producer/schema assertions while retaining explicit compatibility tests.
Add `examples/modules/Joins.lean`, a native JSON reference and Go integration
tests. Use boundary and seeded numeric inputs, record updates, captured values
and wrapper results; assert actual join nodes in freshly exported IR. Exercise
invalid inputs, repeated calls and Go-only generated consumers.

Regenerate all general packages through `uv run --locked python
scripts/generate.py`, copying generated outputs rather than editing them.
Preserve audited v1 artifacts and check old packages' Go bytes remain unchanged.

## 4. Integrated gates and review

Run targeted tests followed by:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```

Run bounded strict-decoder fuzzing with join seeds. Obtain independent specification
review, then quality review, and resolve concrete findings. Run Ripwire edit checks
and quality delta, reviewing its limitations for Lean and generated duplication.

## 5. Documentation and local handoff

Update README and add `docs/research/extraction-joins.md` with actual supported
forms, costs, scope rules and measured verification. Check documentation links,
privacy separation and `git diff --check`. Copy reviewed files into the public
checkout, verify there, and commit locally with the approved identity. Close and
sync the tracker task separately. Do not create a remote or publish.
