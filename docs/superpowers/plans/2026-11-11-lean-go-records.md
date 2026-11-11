# Record representation implementation plan

**Goal:** Extract ordinary record fields and updates, including single-field
wrappers erased by Lean, while preserving original record shapes in Go.

**Architecture:** A producer-only adapter checks the pinned compiler's wrapper
mapping and inserts existing constructors/cases at function and data boundaries.
Reuse the unchanged typed IR validator, Go emitter, immutable API and budgets.

**Tech stack:** Pinned Lean 4.35.0, Go 1.27.4, Python 3.15 through uv. Execute
with subagent-driven development and independent specification/quality reviews.
The task tracker owns progress; these numbered sections specify implementation.

## 1. Establish failing source tests and mapping

Create `scripts/test_record_admission.py`. Start with a fresh source test:

```lean
structure Count where
  value : UInt32
@[noinline] def increment (x : Count) : Count := ⟨x.value + 1⟩
def run (x : Count) : Count := increment x
```

The current export must fail on its signature mismatch. Extend the same test
class with modern imports, nested wrappers, nontrivial fields containing wrappers,
Bool wrappers, and a wrapper around a sum. Add rejection cases for multiple
source fields including a proof, generic wrappers, UInt64, local/late accessor
rewrites and foreign helpers. Assert previous output survives rejection and
reject for the intended admission reason rather than syntax errors.

Create `producer/LeanGo/Module/Records.lean` for shared type validation and checked
wrapper descriptions. Move existing `typeKey`, `signature`, and `constructor`
helpers there if needed to avoid a circular import. Use a description with source
type, constructor and source field type. Bound wrapper traversal to 64 and reject
cycles. For each candidate, verify safe/nonrecursive/no parameters/no indices/no
universes/exactly-one-source-field before comparing `hasTrivialStructure?` and
`toMonoType`. Keep UInt32 as the existing scalar exception.

Run the targeted test with uv and confirm the initial failure before implementing:

```sh
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_record_admission.py'
```

## 2. Encode checked representation adapters

Modify `producer/LeanGo/Module/Export.lean`, extracting encoding helpers into a
focused module if it makes the new boundary logic easier to audit. Preserve
source signatures and schemas. The compiled parameter/body types are normalized
source types. Introduce fresh synthetic binders via the same function-local
counter as compiler binders; preserve the FVarId-to-mono-value scope mapping.

An unwrap of source `Count` binding `x` must encode this existing IR shape:

```json
{"op":"cases","var":1,"type":"Count","result":"Count","alts":[
  {"constructor":"Count.mk","params":[{"id":2,"type":"UInt32"}],"body":
    {"op":"let","bind":{"id":3,"type":"Count"},
     "value":{"kind":"constructor","name":"Count.mk","args":[2]},
     "next":{"op":"return","var":3}}}]}
```

Apply wrap/unwrap at all five boundaries in the design: function entry, return,
ordinary call arguments/results, constructor fields, and case binders. Primitive
signatures remain fixed; their already-mono UInt32/Bool arguments are unaffected.
Check case result types against normalized source result while emitting the
source result name. Charge generated instructions/binders/depth, and ensure all
generated type references enter the exact type closure. Avoid new runtime IR
kinds, synthetic functions, custom compiler passes or audit exemptions.

## 3. Register identity and exercise actual source API

Modify `scripts/export_module.py` `PRODUCER_INPUTS` for every new Lean producer
module and update its unit tests. Keep v3/pure-module-v2 output and all existing
CLI/schema dispatch unchanged. Producer identity changes require fresh artifacts.

Create `examples/modules/Records.lean` with Count, nested Count wrappers, a Bool
wrapper and a multi-field record. Its primary root updates numeric fields through
a noinline wrapper helper and preserves source shapes. Avoid local join forms.
Create `examples/reference/RecordsReference.lean` with a bounded JSON codec and
native execution of that root, and `integration/records_test.go` for the generated
API. Use boundary numeric values and a seeded corpus; compare with independent
modular Go arithmetic and native Lean. Include a separate wrapper-return root.

Exercise invalid source-shaped inputs, ownership, returned field types/names,
allocation/depth/step exhaustion, reused values and repeated calls. A Go-only
consumer must run copied generated files with cgo disabled and no Lean/Python
tools on PATH. Regeneration must match fresh and tracked artifacts byte-for-byte.

## 4. Integrate and verify

Generate the new records package/IR and regenerate the previous general-module
fixtures through fresh output directories. Copy generator outputs only; do not
hand-edit generated files. Keep the audited normalizer sources/artifacts unchanged.

```sh
uv run --locked python scripts/generate.py --source-dir examples/modules --module Records --root Records.update --out /tmp/records-output --ir-output /tmp/records-output.ir.json
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```

Independent specification review checks wrapper erasure, original type shapes,
all adapter positions, provenance and resource accounting; quality review follows.
Resolve concrete findings and rerun affected gates. Run Ripwire edit checks and
quality delta, recording intentional generated/test duplication honestly.

## 5. Document and commit locally

Update README and create `docs/research/extraction-records.md` with exact supported
shapes, adapter costs, examples and observed verification. Mark earlier reports as
historical where their limitations changed. Validate links, public/private
separation, `git diff --check`, correct branch and clean checkout. Commit locally
as `franchb <hello@franchb.com>`, close the mapped tracker task and sync it. Do not
create a GitHub remote, push, or claim actual Windows execution.
