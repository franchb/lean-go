# Audited ordinary Lean to pure Go: pilot evidence

Date: 2026-11-11 · Maintainer: franchb <hello@franchb.com>

## Result and scope

The local pilot exports actual ordinary Lean function bodies through pure
monomorphic LCNF to typed JSON IR, validates that IR independently in Go, and
generates an executable pure-Go normalizer. The generated consumer contains
no Lean runtime, Python dependency, C dependency, or IR interpreter. Its only
imports are from the Go standard library. Normalization control flow comes
from the three captured function bodies; it is not a handwritten normalizer.
Self recursion uses typed explicit continuation frames.

The admitted program is exactly
[`feasibility/LeanGo/Feasibility/Model.lean`](../../feasibility/LeanGo/Feasibility/Model.lean),
SHA-256 `c67a835949d1786638d2809c970ef69861a074a23cf6fdbcae3f8cad1043771f`.
The producer rejects modified model bytes and arbitrary source modules. Each
export uses a fresh fixed Lake workspace with no package dependencies, checks
the Lean version and commit, and snapshots the tracked producer inputs.
This result implements the [approved pilot plan](../superpowers/plans/2026-11-11-lean-go-pilot.md)
following the [feasibility investigation](extraction-feasibility.md).

The tested toolchains were Lean **4.35.0**, commit
`eb6cafb7290e8bd5f2937522d8d804f58f02bafc`, and Go **1.27.4** on
**Linux/amd64**. This is a local milestone: no GitHub publication, tag, or release
was made. Broader Lean module admission and end-to-end translation proofs are
outside this pilot.

## Pipeline and evidence identity

The pipeline is:

1. [`scripts/export.py`](../../scripts/export.py) verifies the exact model and
   producer configuration and builds the pinned Lean exporter in a fresh workspace.
2. The original exporter captures `normalize`, `normalizeAux`, and `Tri.neg`
   under `LeanGo.Feasibility`, plus constructor, theorem, and source metadata.
3. [`internal/ir`](../../internal/ir) independently checks the closed
   `lean-go-ir/v1`, `audited-normalizer/v1`, `pure-mono` profile, types, scope,
   calls, constructors, claims, and dependency closure.
4. [`internal/gogen`](../../internal/gogen) emits typed Go control flow and
   ownership/resource helpers. [`cmd/lean-go`](../../cmd/lean-go) exposes this
   validated IR-to-package boundary without requiring Lean.
5. [`scripts/generate.py`](../../scripts/generate.py) composes fresh export and
   generation, verifies the actual producer source hashes, and records the IR
   only after successful package generation.

The standalone CLI validates the supplied IR and its metadata; it does not
check producer source bytes currently on disk. Use `scripts/generate.py` for
the composed source-freshness check. Metadata records traceability from the
trusted producer; it does not authenticate a malicious producer.

The tracked artifacts are [`normalizer.ir.json`](../../testdata/normalizer.ir.json),
[`control.go`](../../generated/normalizer/control.go),
[`runtime.go`](../../generated/normalizer/runtime.go), and
[`manifest.json`](../../generated/normalizer/manifest.json). The manifest records
producer input hashes, generator and validator source identities, theorem
statement digests and axioms, schemas and mappings, and generated-file digests.
Its IR digest `32083183ed989beee3d42b2c84fbe5ed59cd933115f3ee24a94cd07c7bbdde5f`
is over canonical Go-encoded IR; the raw fixture file digest is separately
`1f98283f5d30d47edbec6325ae2152c98726c800fca04aa759e06a6b8fbb3a53`.
Both describe the same program with different byte encodings.

## Source properties and trust boundary

Lean kernel-checks these two claims about the exact source definition:

- `normalize_preserves_eval`: for every finite expression and valuation,
  evaluating the normalized expression equals evaluating the input.
- `normalize_is_normal`: every normalized output satisfies the source
  normal-form predicate.

Both root claims have actual transitive axiom lists `[propext]`. The reviewed
source/compiler substitution `Bool.not -> Bool.Internal.not`, justified by
`Bool.not_eq_internalNot`, records `[Quot.sound]` separately. This rewrite is
source audit metadata, not an extra runtime primitive in the admitted IR.
The exporter checks expected theorem types and theorem declaration kinds;
inspecting the final environment is not generic provenance analysis.

The result is **proved Lean logic with tested, trusted extraction**. The
compiler adapter and lowering, IR construction/validation, Go emitter,
representations and host conversions, toolchains, and runtimes remain trusted.
Native Lean and generated Go can share compiler transformations, so their
agreement cannot independently prove those shared transformations correct.
The mathematical theorem applies to all valid finite expressions; the Go API
admits the bounded subset described below.

## Consumer boundary and limits

The public API is in [`runtime.go`](../../generated/normalizer/runtime.go):
`Import(*Input, Limits) (Expr, error)` and `Normalize(Expr, Limits) (Expr, error)`.
`Input` has explicit literal, variable, negation, conjunction, and disjunction
kinds. Tri values are `False`, `Unknown`, and `True`; variable IDs are `uint32`.
Strong-Kleene negation preserves unknown, while conjunction/disjunction use
the ordered false/unknown/true minimum/maximum. Normalization evaluates negated
literals and moves remaining negations onto variables, preserving operand order.

`Expr` owns immutable nodes with unexported fields. `Kind`, `Literal`,
`Variable`, and `Children` return scalar copies or immutable handles.
`Expr{}` is invalid; a valid false literal is explicit `Input{Kind: Literal,
Value: False}`. Input must not be mutated during `Import`. Mutating it after
successful import cannot affect the owned expression, including concurrent
normalization of that expression.

Import validates active payloads and required/forbidden children. Active-path
cycle detection rejects cycles while permitting acyclic sharing. Every shared
occurrence is copied and charged before expansion; shared input is not
memoized. Owned size and depth are retained for later limit checks.

`DefaultLimits()` sets these maximum allowed policy caps. Every field must be
positive and at most its cap; callers can select smaller limits.

| Field | Cap | Accounting |
| --- | ---: | --- |
| `MaxNodes` | 65,536 | Expanded input occurrences |
| `MaxDepth` | 4,096 | Input and result depth; root depth is 1 |
| `MaxOutputNodes` | 131,072 | Reachable result occurrences, including reused children |
| `MaxAllocations` | 65,536 | Fresh generated expression nodes during normalization |
| `MaxSteps` | 4,194,304 | Generated control states executed |
| `MaxFrames` | 8,192 | Simultaneous generated function/continuation frames |

Import enforces node/depth limits; Normalize checks owned input size/depth and
charges generated steps, frames, allocations, and output occurrences. These
are logical budgets, not host-process heap guarantees or performance service
levels. For example, `MaxAllocations` does not count every Go heap allocation.
`ErrMalformed` covers invalid handles/shapes and cycles, `ErrResource` covers
budget exhaustion, and `ErrLimits` covers invalid policies. Failures return an
invalid `Expr`, never a partially normalized result.

## Reproduction and output publication

From the repository root, with the pinned tools installed:

```sh
python3 scripts/generate.py
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
```

The first command defaults to `generated/normalizer` and
`testdata/normalizer.ir.json`. To generate a separate candidate, pass
`--out DIR --ir-output FILE` with non-overlapping paths. IR alone can be
validated and compiled with `CGO_ENABLED=0 GOTOOLCHAIN=local go run ./cmd/lean-go
-in testdata/normalizer.ir.json -out DIR`.

A new package is fully staged and published by rename. An identical existing
complete package is a no-op; a differing package is refused and preserved.
Cooperating publishers use an exclusive sibling lock. This does not guarantee
safety against arbitrary unrelated filesystem writers racing publication;
transactional upgrades of differing existing packages remain future work.
Unsupported IR and failed generation preserve the previous package.

## Observed validation

All commands below completed successfully on the stated Linux host. Ordinary
`go test ./...` leaves native Lean and fresh regeneration opt-in; the recorded
full command above enabled both.

| Gate | Observed result |
| --- | --- |
| Full cgo-disabled suite, native Lean and regeneration enabled | All Go packages passed |
| `go vet ./...` | Passed |
| Python orchestration tests | 16 passed: 11 export, 5 generation |
| Independent Go semantic oracle | 7,265 expressions through depth 2, each under all 9 valuations; 1,000 larger expressions from seed 20261111 |
| Test-only IR interpreter versus generated Go | Exact normalized shape matched for all 7,265 expressions |
| Native Lean reference versus generated Go | Shape and valuation results matched on 515 trees: 512 sampled and 3 explicit |
| Boundary and resource tests | Malformed shapes, nil/zero values, UInt32 endpoints, cycles/sharing, later mutation, depth 4,096, exponential shared DAGs, and below/at/above budgets passed |
| Mutation and rejection controls | A valid IR negation mutation generated successfully and failed the intended exact conformance check; unsupported IR preserved the prior package |
| Fresh regeneration | Two independent fresh producer builds matched each other and every tracked package/IR byte and file set |
| Isolated consumer | Only generated Go files copied to a temporary standalone module; built and ran with Go alone |

The isolated consumer used `CGO_ENABLED=0`, disabled dependency network access,
restricted `PATH` to the Go binary directory, and explicitly verified that
`lean`, `lake`, `python3`, `cc`, and `gcc` were unavailable. It required neither
extractor internals nor repository-relative runtime data.

The original feasibility gates were also rerun unchanged: `lake build LeanGo
normalizer-checks`, `lake env lean LeanGo/Feasibility/Checks.lean`, `lake exe
normalizer-checks`, and `python3 check_mutations.py`, all from `feasibility/`.
Source claims again reported `[propext]`, the native smoke passed, and both
proof mutants were rejected by their intended theorem checks.

Additional observed checks:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test ./integration -run '^$' -fuzz '^FuzzNormalize$' -fuzztime=5s -parallel=2
CGO_ENABLED=1 GOTOOLCHAIN=local go test -race ./integration -run 'TestConcurrentOwnedNormalization|TestMalformedAndOwnership'
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local go test -c ./integration -o /tmp/lean-go-integration.test.exe
```

The five-second randomized fuzz campaign passed after 248,261 executions; that
count is an observation, not a deterministic coverage guarantee. The race audit
passed for owned concurrent normalization and boundary ownership. Its instrumented
host build used cgo; this is separate from the cgo-disabled consumer evidence.
The Windows command cross-built successfully. It did **not** execute on Windows.

## Benchmark observations and remaining gates

On an Intel Core i5-14600K, Linux/amd64, Go 1.27.4, the following command measured
100 iterations per case, excluding Import from the Normalize benchmarks:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test ./integration -run '^$' -bench 'Benchmark(NormalizeDeep|NormalizeWide|ImportShared)$' -benchtime=100x -count=1
```

| Case | Shape | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| NormalizeDeep | 4,096 input nodes | 763,630 | 3,628,646 | 16 |
| NormalizeWide | Full binary tree, 12 edges deep, 8,191 nodes | 234,438 | 204,230 | 4,099 |
| ImportShared | 13 unique host nodes expanding to 8,191 occurrences | 427,811 | 394,760 | 8,198 |

These are single-host observations, not production recommendations, resource
cap justifications, or performance guarantees. Actual Windows/amd64 semantic
and boundary execution and an external consumer from a tagged release remain
required by the [release acceptance contract](../superpowers/specs/2026-11-11-lean-go-extraction-design.md#validation-and-release-acceptance).
No published release, generic extraction support, or end-to-end compiler proof
is asserted by this pilot.
