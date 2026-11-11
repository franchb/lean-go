# Record fields and source-shaped wrappers

This milestone makes ordinary record field access, nested reads and updates work
when Lean removes single-field wrappers during compilation. Multi-field record
access was already supported through Lean's projection-to-cases lowering.

## Generate a record function

Use the pinned Go 1.27.4 and Lean 4.35.0 toolchains, with Python 3.15 through uv:

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Records --root Records.update \
  --out generated/records --ir-output testdata/records.ir.json
```

Existing differing output is preserved and rejected. Choose fresh paths when
trying a changed producer. The example updates a record containing scalar, Bool
and nested record wrappers. Its `Count` helper is kept as a separate function to
exercise wrapped arguments and results across calls.

For example, a Count is supplied and inspected as a Count node, with one UInt32
field; callers do not need to know that Lean uses UInt32 internally:

```go
count := &x.Input{
    Type: "Records.Count", Constructor: "Records.Count.mk",
    Fields: []*x.Input{{Type: "UInt32", UInt32: 7}},
}
```

The existing `Import`, `Run`, and immutable `Value` API is unchanged. Full source
names and original field types remain in the manifest and generated schema.
Repeated calls cannot mutate imported values; returned field slices remain copies.

## Representation boundary

The producer independently computes a wrapper chain and checks it against the
pinned compiler's `hasTrivialStructure?` and `toMonoType` information. An erased
wrapper must be safe and nonrecursive, have no type parameters, indices or
universes, and have exactly one constructor with exactly one source field. Its
field must be an admitted type. Proof-bearing and dependent records remain
unsupported even when Lean gives them only one computationally relevant field.
Wrapper chains are bounded to 64 layers. Ordinary recursive data retains its
existing representation; it is not treated as an erasable wrapper.

Adapters occur at function entry and return, ordinary call arguments/results,
constructor arguments, and case-bound fields. They reconstruct the expected
source type at each position. Two different source records may both compile to
UInt32, so the scalar type alone is never sufficient to decide which to build.
Nontrivial records retain their original schemas even when their fields contain
wrappers. A wrapper may contain a nontrivial recursive sum.

Unwrapping uses existing exhaustive cases; wrapping uses existing constructors.
There are no new IR operations, synthetic functions, runtime representations or
compiler passes. Exports remain `lean-go-ir/v3` / `pure-module/v2`, and the Go
validator, emitter and runtime template are unchanged. The trusted producer
input hashes include the new record adapter source.

All inserted instructions and binders count toward IR limits. At runtime,
unwrapping consumes control steps; reconstructing a wrapper also consumes a
constructor allocation and output size/depth budgets. Even an identity function
over a wrapper can reconstruct its outer node. Limits describe the generated Go
execution rather than Lean's allocation count. Exhaustion returns an invalid
value without a usable partial result.

Original source dependencies still pass the historical and final audits,
including generated accessors, inlined helpers and imported modern modules.
Unsupported replacements, externs and axioms are not permitted through erasure.
No new standard-library exception was added. Generics, proof/dependent fields,
other numeric types, local joins and local functions remain unsupported; a
conditional record update may still lower to a rejected join.

Source elaboration remains trusted local execution. This work preserves source
data shapes through tested extraction; it does not prove compiler correctness.
Generated consumers need only the Go standard library with `CGO_ENABLED=0`.

## Verification scope

The tests exercise scalar and Bool wrappers, nesting, wrappers inside multi-field
records, wrappers around sums, noinline helpers, original nominal types, record
updates, classic source files and modern module imports. The integration fixture
has a separately compiled native Lean reference and an independent Go oracle.
Source rejection tests preserve previous output and distinguish admission failures
from invalid Lean syntax.

The isolated consumer contains only generated Go files and runs with a Go-only
PATH. Dedicated reconstruction tests use a scalar argument and nested record
result to separate output allocation/depth failures from input validation.
Reproducibility checks compare repeated fresh builds and tracked artifacts.

Windows cross-compilation does not establish Windows execution. Actual Windows
runs and consumption from a tagged release remain separate release gates.

The complete Go suite passed with native, regeneration and module gates enabled.
The record corpus contains 1,970 cases: 1,458 boundary combinations and 512 seeded
records, checked against native Lean and the independent Go oracle. The reference
also rejects six malformed wire inputs. Both alternate wrapper roots execute in
isolated generated packages. Reconstructing the nested result from a scalar
requires exactly two constructor allocations, depth three and three output nodes;
tests verify that reducing each limit fails with an invalid result.

All 52 Python test methods passed using Python 3.15.2 through uv 0.13.5. The new
record admission file performs 20 fresh exports: seven accepted cases and 13
rejections. Its wrapper-depth boundary accepts 64 layers and rejects 65. Go vet,
Windows/amd64 build and integration-test cross-compilation also passed.

Independent specification and code-quality reviews passed, including three extra
fresh-export probes through the unchanged Go validator/generator. Prior tree,
choice and numeric IR is identical after removing input hashes; all their
generated Go files, the Go backend and the audited normalizer are unchanged.
Ripwire reported 17 gating duplication findings, reviewed as standalone generated
runtime copies and independent fixture-test harnesses. Lean files are not indexed
by Ripwire and were reviewed directly.

```sh
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```
