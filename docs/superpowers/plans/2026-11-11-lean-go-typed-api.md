# Typed Go API Implementation Plan

**Goal:** Generate immutable typed Go facades for ordinary Lean module exports.

**Architecture:** Add checked source record-name metadata in IR v5, keeping
`pure-module/v3` execution semantics. Generate a separate `api.go` that forwards
through the existing immutable node representation and `Run` budgets.

**Tech Stack:** Lean 4.35.0, Go 1.27.4 with CGO disabled, Python 3.15 through uv.

Implementation uses independent owned-file tasks followed by specification and
quality review. The task tracker owns status; this document describes the work.

## 1. Add versioned source API metadata

Modify `producer/LeanGo/Module/Export.lean` to collect direct structure fields from
the exported type closure and emit `api.records` with v5. Preserve fresh compiler
names and all original admission checks. Add focused source tests in
`scripts/test_api_admission.py`, including nested/inherited records and enums.

In `internal/moduleir/types.go`, introduce:

```go
const SchemaV5 = "lean-go-ir/v5"
type APIMetadata struct {
    Records map[string][]string `json:"records"`
}
// Program gains API *APIMetadata with json:"api,omitempty".
```

Extend strict shape decoding in `internal/moduleir/decode.go`, profile validation
and type checks in `internal/moduleir/validate.go`, using a focused `api.go` helper
for presentation metadata. v5 requires `api.records`; legacy schemas reject it,
including explicit null or empty metadata. Validate map size, known type names,
one-constructor shape, exact field arity and bounded distinct field names. Add
`internal/moduleir/api_test.go` with malformed, duplicate, null and legacy cases.

Update schema dispatch in `cmd/lean-go/main.go`, `scripts/export_module.py` and
`scripts/generate.py`, plus their focused compatibility tests. Existing test
fixtures for older schemas keep testing older behavior.

Run `CGO_ENABLED=0 GOTOOLCHAIN=local go test ./internal/moduleir ./cmd/lean-go` and
`LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_api_admission.py'`.
First observe rejection of a valid v5 document, then require acceptance after
implementation and continued rejection of each malformed document.

## 2. Emit the typed facade

Add `internal/modulegen/api.go` and `api_names.go`; extend `Generate` in `emit.go`
to emit `api.go` only for v5. Include new generator inputs in the source digest,
advance the generator version, and expose the generated name map in the manifest.
Use `templates/api.go.tmpl` only if shared helper text materially simplifies the
emitter. Keep existing runtime semantics and schema/control emission intact.

Generated wrappers contain a private `Value`. Constructors accept native scalar
values or typed wrappers and a final `Limits` argument; return `(Type, error)`.
Record getters return `(FieldType, bool)`. Sum methods `AsAlternative` return
typed fields plus `bool`. `RunTyped` creates immediate scalar nodes/argument
handles, delegates to `Run`, and wraps its result or returns a zero value on error.

Use `checkLimits`, `fieldType`, `measure`, and private immutable node storage in
the same generated package. Constructor measurement uses input node/depth limits,
not execution allocation counters. Keep scalar and Boolean conversion separate
from ADT wrapper emission so UInt64 can later extend the scalar mapping without
changing the wrapper contract.

Add `internal/modulegen/api_test.go`. Compile emitted packages to test records,
sums and scalar roots. Assert deterministic output, naming collisions involving
`Value`, `Input`, `RunTyped`, constructors and Unicode spellings, invalid zero
handles, wrong alternatives, shared occurrence counts and raw/typed equivalence.
Use existing `runGenerated` for executable generated tests; no duplicate harness.

Run `CGO_ENABLED=0 GOTOOLCHAIN=local go test ./internal/modulegen`.

## 3. Exercise independent typed consumers

Add `integration/typed_test.go` using generated numeric, record, tree and captured
helper examples. Cover immutable ownership, typed output reads and error values.
Build a temporary independent Go-only consumer with typed imports and no producer
tools. Compile a second source with a deliberately wrong argument type and assert
an actual type-checking failure, not an import or syntax failure.

Regenerate all seven ordinary modules through fresh output directories with
`uv run --locked python scripts/generate.py --source-dir examples/modules` and
each fixture's module/root/out/ir-output arguments. Validate all outputs before
copying them into `generated/` and `testdata/`; leave the normalizer unchanged.

## 4. Verify and deliver this increment

Run all Python tests with module checks enabled, then:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```

Cross-compile integration tests. Obtain specification review before quality
review; fix concrete findings and rerun affected checks. Run Ripwire edit and
quality checks, reviewing Lean manually because it is unindexed. Update README
with a typed example and add `docs/research/extraction-typed-api.md` describing
the API, naming, compatibility, accounting and actual validation evidence.

Check documentation links, public identity/privacy, generated identities and
`git diff --check`. Commit as `franchb <hello@franchb.com>`. The next implementation
begins only after this milestone is verified and committed.
