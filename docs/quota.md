# Bounded quota accounting

`github.com/franchb/lean-go/quota` is an immutable counter with a fixed `uint64`
capacity. It is available in this source checkout; the published
`v0.1.0-alpha.2` preview predates it. Consumers need Go 1.27.4 and can build with
`CGO_ENABLED=0`. They do not need Lean, Python, a C compiler or a native library.

```go
import "github.com/franchb/lean-go/quota"

state, err := quota.New(10)
if err != nil { panic(err) }
next, accepted, err := state.Reserve(7)
if err != nil { panic(err) }
if !accepted { panic("insufficient capacity") }
remaining, err := next.Remaining()
if err != nil { panic(err) }
// remaining == 3; state still has all 10 units available.
```

Run `CGO_ENABLED=0 go run ./examples/quota` for a complete example.

## Contract

| Operation | Result |
| --- | --- |
| `New(capacity)` | A valid state with zero usage, even at capacity zero or `math.MaxUint64` |
| `s.Reserve(n)` | Accepts exactly when `n` fits in the remaining capacity |
| `s.Release(n)` | Accepts exactly when at least `n` units are used |
| `s.Capacity()`, `s.Used()`, `s.Remaining()` | Read the corresponding quantity |

Transitions return `(State, bool, error)`. Rejection returns unchanged accounting,
`false`, and no error. Reserving or releasing zero units is accepted for valid
states. Accepted arithmetic cannot overflow or underflow. All quantities are
integers; there is no floating-point conversion.

The zero Go `State` is invalid: construct it with `New`. Invalid state operations
return `ErrInvalidState`; transition errors return an invalid state and `false`.
Other generated runtime errors are preserved. The facade uses the generated
default resource limits and delegates every transition and remaining-capacity
calculation to the extracted implementation.

Snapshots are immutable and safe to share. Callers decide which returned state
becomes current and synchronize concurrent updates themselves. This library does
not implement a shared atomic counter, clocks, refill, leases or ownership of
individual reservations. Releasing units asserts that the caller is allowed to
release them; the counter cannot establish that permission.

The lower-level `generated/quota` package exposes shared `State` and `Outcome`
wrappers, `RunCreate`, `RunReserve`, `RunRelease` and `RunRemaining`, plus the raw
`RunRoot` API. It permits constructing states whose usage exceeds capacity.
Such states reject both transitions and report zero remaining capacity. This is
separate from an invalid zero Go wrapper, which is a runtime error.

## What is proved

The runtime source is [Quota.lean](../libraries/quota/source/Quota.lean). Its
[proofs](../libraries/quota/proof/QuotaProofs.lean) establish ten named statements:

| Theorems | Guarantee |
| --- | --- |
| `create_valid` | Initial usage is within capacity |
| `reserve_accepted`, `release_accepted` | Acceptance is equivalent to the exact validity and amount guards |
| `reserve_preserves`, `release_preserves` | Valid inputs produce valid states |
| `reserve_rejected`, `release_rejected` | Rejection preserves the entire input state |
| `reserve_exact`, `release_exact` | Accepted natural-number arithmetic is exact, with unchanged capacity |
| `remaining_exact` | Remaining units plus used units equals capacity for valid states |

Quota uses the shared [proof-contract workflow](proof-contracts.md). Its
[contract](../libraries/quota/proof-contract.json) maps the theorems to raw
[expected propositions](../libraries/quota/proof/QuotaContract.lean), maintained
separately from the proofs. The checker rebuilds the exact runtime and proof sources in a fresh,
dependency-free Lean 4.35.0 project. It compares each theorem's type with an
independently written expected proposition and collects its axiom dependencies.
Only `propext`, `Classical.choice` and `Quot.sound` are allowed; current proofs use
only `propext` and `Quot.sound`. Admitted proofs, custom axioms and compiler-trust
axioms are rejected. No project build scripts or precompiled local proofs are
reused.

[Proof evidence](../libraries/quota/proof-evidence.json) uses the shared
`lean-go-proof-evidence/v1` schema. It records exact theorem statements, expected
definition names, axiom lists, Lean identity, contract/source/proof/checker hashes and the
generated manifest hash. Checking also verifies the selected roots, runtime
source hash and hashes of the four generated Go files. The normal module IR
does not acquire application theorem claims; this is separate source evidence.

The Lean kernel checks the source statements. The pinned Lean compiler, module
extractor, Go emitter/runtime, facade and Go compiler remain tested and trusted
parts of the path to executable Go. The sidecar does not prove that compilation
preserves semantics. Source elaboration is trusted, as for other module exports.

## Reproduce the checks

Use the repository's pinned Lean, Go 1.27.4, and Python 3.15 through `uv`:

```sh
uv run --locked python scripts/check_quota.py
LEAN_GO_QUOTA=1 uv run --locked python scripts/test_quota_proofs.py
CGO_ENABLED=0 go test -count=1 ./quota
CGO_ENABLED=0 LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 \
  go test -count=1 -run '^TestQuota' ./integration
CGO_ENABLED=0 go test -run '^$' -bench . -benchmem ./quota
```

The Go tests compare boundary values and seeded transition sequences with an
independent arbitrary-precision model, check immutability and invalid states,
and build an isolated consumer with only Go on its tool path. Integration tests
compare native Lean, raw Go and typed Go against the same independent contract,
and check byte-identical fresh generation. Proof-gate tests include weakened
statements, admitted/custom axioms, changed runtime rules and stale evidence.
Benchmarks report time and allocation costs without machine-dependent thresholds.

To generate a fresh candidate package and matching evidence in a new directory:

```sh
uv run --locked python scripts/generate.py \
  --source-dir libraries/quota/source --module Quota \
  --root Quota.create --root Quota.reserve \
  --root Quota.release --root Quota.remaining \
  --out /tmp/quota-candidate --ir-output /tmp/quota-candidate.ir.json
uv run --locked python scripts/check_quota.py \
  --manifest /tmp/quota-candidate/manifest.json \
  --output /tmp/quota-candidate-proof-evidence.json
```

Review source, proof, generated-code and evidence changes together. Existing
differing output is protected; use fresh candidate paths for a changed program.
