# Proved quota library design

The first focused library is an immutable bounded quota counter using UInt64.
It exercises real numeric decisions and shared state across multiple extracted
functions. Lean proves properties of the exact source definitions; differential
tests cover extraction and the Go boundary. The public package remains generic.

## Behavior and API

`Quota.State` contains `capacity` and `used`. `create capacity` initializes usage
to zero. `reserve state amount` accepts only when the state is valid
(`used <= capacity`) and the amount fits the remaining capacity. It then increases
usage by the amount. `release` accepts only for valid states when the amount is
at most current usage, and decreases usage by that amount. Both return an
`Outcome` containing the next state and an acceptance flag. Rejection preserves
the input state. Zero-sized operations on valid states are accepted.

`remaining` returns capacity minus usage for valid states and zero for invalid
ones. The guarded reserve calculation avoids unsigned overflow. Release rejects
underflow. Full UInt64 capacities and amounts are supported. Runtime checks also
handle invalid states constructed through the generated low-level constructor.

The public Go `quota` package wraps the generated immutable state privately.
`New` returns an initial state. `Reserve` and `Release` return a new state, an
acceptance flag and an error. `Capacity`, `Used` and `Remaining` return exact
uint64 values and errors. A zero Go state is invalid. Rejected requests are normal
results, while malformed values or execution/resource errors are errors. The
facade delegates quota decisions and arithmetic to the generated implementation.

The counter has no clock, refill schedule or token ownership. Consumers retain
the returned state and serialize updates when sharing it. This library proves
accounting bounds, not application authorization or external resource ownership.

## Proof contract

Ten named theorems cover initialization, each operation's exact acceptance
condition, preservation of bounds, rejection identity, exact natural-number
arithmetic on acceptance, and exact remaining-capacity arithmetic for valid
states. These properties refer to the actual executable definitions, not a
separate algorithm linked only by examples.

Proof checking occurs in a fresh dependency-free pinned Lean workspace. The
checker compares each theorem's type with an independently written proposition
and inspects its transitive axioms. Only Lean's standard foundational axioms
`propext`, `Classical.choice` and `Quot.sound` may occur. Missing/weakened theorems,
`sorry`, custom axioms and compiler-trust proof shortcuts must fail the gate.

Runtime source and proof/checker files live separately under `libraries/quota`.
The extractor receives only the runtime source. A versioned deterministic proof
evidence file records exact theorem types, axiom dependencies, toolchain identity
and hashes of proof inputs. The proof command binds the runtime source hash to
the generated package manifest and checks the declared generated-file hashes.
This sidecar does not change the ordinary module IR's empty application claims.

## Validation and boundaries

The proof gate includes negative tests for a weakened theorem, admitted proof,
semantic source mutation and stale or corrupt evidence. An independent
arbitrary-precision model compares boundary/random transitions and longer Go
sequences. Compiled native Lean, raw generated Go, typed generated Go and the
public facade are checked, including invalid raw states, zero and maximum values,
rejection and immutable reuse. Regeneration must match tracked package and IR
bytes. A separate Go-only consumer exercises deployment without producer tools.

Benchmarks report allocations and representative operation costs without asserting
machine-specific timing thresholds. The runnable example uses the public facade.
Go consumers use `CGO_ENABLED=0`; Python tooling stays on 3.15 through `uv`.

Lean's kernel checks the source proofs. The theorem checker, source-to-IR adapter,
compiler transformations, Go generator/runtime, facade and host integration still
belong to the tested/trusted boundary. No end-to-end proof of Go correctness is
claimed. Broad generic collections and new extraction features remain outside
this milestone.
