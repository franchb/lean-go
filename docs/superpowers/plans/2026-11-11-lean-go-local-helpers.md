# Local helpers implementation plan

**Goal:** Extract directly called local Lean helpers with checked captured data.

**Architecture:** Observe the pinned compiler's actual lifting event, retain
separate compiler evidence, audit original local source signatures and captures,
then reuse the current v4 IR and Go backend. The task tracker owns progress.

**Toolchain:** Lean 4.35.0, Go 1.27.4, Python 3.15 through uv, CGO disabled.

## 1. Establish the boundary

Use an ordinary local `bump : UInt32 -> UInt32` that closes over `bias`, chains
eight calls to a noinline addition helper, and is called twice. Verify that
`saveMono` contains a real lifted helper and that baseline export fails with
missing compilation provenance. Repeat with a `Bool` and nested record capture.
Inspect pinned lambda lifting, auxiliary caching and pass-manager lifecycle.

## 2. Producer evidence and source checks

Add focused modules under `producer/LeanGo/Module/`: `Local.lean` for persistent
evidence and export-time validation, `LocalSource.lean` for original source local
signature/capture checks, and `LocalObserve.lean` for temporary pass observation.
Reuse `Records.lean` type and representation checks; factor shared full ADT
validation there if needed. Keep source-definition provenance separate.

In `Audit.lean`, record local-source issues with each original source record and
run the original compilation callback with temporary builtin observers. Restore
the pass manager on success and failure. Resolve generated helper source owners
through authenticated evidence, preserving existing source closure/rewrite audits.

In `Export.lean`, add local helper resolution with exact checked mono signatures,
owner validation in source audit, pre-classification collision checks and helper
derivation edges. Do not admit closures or change Go runtime/IR semantics. Preserve
the existing reduced-argument checks and ordinary source function adapters.

## 3. Admission regression tests

Create `scripts/test_local_admission.py` using the fresh source harness by
composition. Write the basic surviving-helper test before implementation and
observe the expected admission failure. Require emitted helper functions and
provenance, not merely successful export of an inlined expression.

Add independent fresh builds for all cases in the design, including a local
unsupported parameter/capture erased by optimization, wrong capture order/type,
stale final declarations and owner-side changes after helper calls disappear.
Check stable output on each rejection and reject syntax/build failures as test
substitutes. Adapt tests to actual evidence fields once the compiler probe fixes
their representation.

## 4. Driver, example and native comparisons

Add new producer files to `scripts/export_module.py` and its freshness regression
in `scripts/test_export_module.py`. Add `examples/modules/Local.lean`, a strict
native wire reference `examples/reference/LocalReference.lean`, and
`integration/local_test.go`. Use nested captured record data, a conditional and
repeated calls to surviving helpers. Cover boundary arithmetic and seeded inputs,
immutable capture ownership, resource limits, Go-only use and two fresh exports.

Generate `generated/local/` and `testdata/local.ir.json` through fresh output
directories. Regenerate all previous general packages. Compare their Go bytes and
IR after removing only input hashes. Preserve the audited normalizer artifacts.

## 5. Verification and local delivery

Execute independent implementation tasks in isolated scratch copies, integrate
their owned files, and obtain specification then quality review. Run:

```sh
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```

Cross-compile integration tests, run Ripwire edit/quality checks, and review Lean
directly because it is unindexed. Document limits and verification in
`docs/research/extraction-local-helpers.md`, update README and the prior report's
historical limitation. Check local documentation links, public identity/privacy
and `git diff --check`. Commit as `franchb <hello@franchb.com>`, transfer reviewed
commits locally, verify Go tests and input identities in the actual checkout, and
close/synchronize the task tracker. Do not create a remote or publish a release.
