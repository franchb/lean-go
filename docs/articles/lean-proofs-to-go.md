# From Lean source proofs to a Go package you can copy

A small function is enough to show the whole handoff: write ordinary Lean code,
state its requirements independently, check the proofs, generate Go, and call
that Go from an application with no Lean runtime. The
[standalone clamp example](../../examples/proofs/clamp) includes every artifact
needed to inspect that process and a module that runs offline with Go alone.

This is an experimental extraction tool with **tested, trusted extraction**.
Lean checks source theorems; the tool does not provide an end-to-end proof that
its generated Go preserves those theorems. Keeping that distinction visible
makes the evidence useful to someone deciding whether to adopt it.

## Start with ordinary source

The public function delegates to a local module:

```lean
import Clamp.Bounds

namespace Clamp

def clamp (value limit : UInt64) : UInt64 :=
  Bounds.cap value limit

end Clamp
```

The helper is ordinary Lean as well:

```lean
namespace Clamp.Bounds

def cap (value limit : UInt64) : UInt64 :=
  if value ≤ limit then value else limit

end Clamp.Bounds
```

There is no special expression language to write and no application-name
allowlist. The extractor selects `Clamp.clamp`, follows its supported local
helper dependencies, and retains their source provenance. Here the function
uses an unsigned comparison and a branch, so it introduces no arithmetic
overflow cases. `UInt64` covers the full range from zero through 2^64−1.

## Separate the requirement from its proof

The expected propositions are definitions in `proof/ClampContract.lean`:

```lean
def bounded : Prop := ∀ value limit : UInt64, Clamp.clamp value limit ≤ limit

def preserves : Prop := ∀ value limit : UInt64,
  value ≤ limit → Clamp.clamp value limit = value

def idempotent : Prop := ∀ value limit : UInt64,
  Clamp.clamp (Clamp.clamp value limit) limit = Clamp.clamp value limit
```

Their enclosing namespace is `ClampContract`. Separately,
`proof/ClampProofs.lean` establishes the same statements. For example, the last
proof uses the first two:

```lean
theorem idempotent (value limit : UInt64) :
    Clamp.clamp (Clamp.clamp value limit) limit = Clamp.clamp value limit := by
  exact preserves (Clamp.clamp value limit) limit (bounded value limit)
```

A JSON contract selects the runtime module and root, then maps
`ClampProofs.bounded`, `ClampProofs.preserves`, and `ClampProofs.idempotent` to the
three expected definitions. The expectations do not import or reuse the proofs.
The checker compares each theorem's type against its independently supplied
expectation; it does not turn the discovered theorem type into the requirement.

A reviewer still has to judge the requirements. These three properties alone
do not uniquely characterize minimum: a function returning zero for every
above-limit input can satisfy them too. The supplied source returns the actual
limit, and the independent Go model checks that stronger behavior over concrete
inputs. Neither a proof of the wrong requirement nor finite tests should be
mistaken for a complete application specification.

## Produce artifacts that can be checked again

Use Go **1.27.4**, Python **3.15** and the pinned Lean/Lake **4.36.0-rc2** toolchain.
These are development artifacts; the published alpha.4 demo still uses Lean
4.35.0. Follow the [development CLI instructions](../cli.md#development-checkout),
copy the example directory, and
run these commands inside the copy:

```sh
lean-go doctor
lean-go generate --source-dir source --module Clamp --root Clamp.clamp --out clamp --ir-output clamp.ir.json
lean-go check-proofs --contract proof-contract.json --manifest clamp/manifest.json --output proof-evidence.json
lean-go check-proofs --contract proof-contract.json --manifest clamp/manifest.json --evidence proof-evidence.json
```

Generation compiles snapshotted source in a fresh producer workspace, validates
the supported compiler representation, and emits a Go package plus manifest.
It records IR in `clamp.ir.json`. The manifest identifies profile
`pure-module/v4`, IR `lean-go-ir/v6`, and generator `lean-go-modulegen/v7`, with
hashes of the generator, validator, source inputs and generated files. The pinned
Lean identity is commit `b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`, not merely a
matching version string.

Proof checking rebuilds runtime and proof sources and checks exact statements
and transitive axiom dependencies. `sorry`, custom axioms and compiler-trust
shortcuts such as `native_decide` fail the gate. Evidence binds the contract,
project and checker inputs, theorem statements, permitted axiom dependencies,
and generated manifest. Rechecking detects stale evidence and changed output.
Both `--output` and `--evidence` perform fresh proof checks; one writes evidence
and the other compares it. See [proof contracts](../proof-contracts.md) for the
precise format and permitted axioms.

The source claims are in `proof-evidence.json`. The manifest's
`program.claims` is empty: binding source proofs to generated bytes is not the
same thing as proving compiler correctness. Hashes identify the checked inputs
and outputs; they do not prove semantics preservation or authenticate a
compromised compiler.

## Consume the generated package as Go

The example's `go.mod` declares `example.com/proved-clamp`. It has no dependencies
or replacement directives. The generated typed entrypoint accepts ordinary Go
integers:

```go
import clamp "example.com/proved-clamp/clamp"

result, err := clamp.RunTyped(value, limit, clamp.DefaultLimits())
if err != nil {
    return err
}
```

The checked-in executable demonstrates an unchanged value, a reduced value,
and the largest `uint64`. From the copied directory:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
```

It prints:

```text
clamp(7, 42) = 7
clamp(100, 42) = 42
clamp(18446744073709551615, 42) = 42
```

The generated control flow is Go code. Deployment does not run a Lean or IR
interpreter and needs no Python, uv, Lake, Lean or C compiler. Logical resource
budgets still apply: calls can return an invalid-limits or exhausted-budget
error. Source theorems about the total Lean function do not assert that every
budgeted Go call succeeds. The consumer must handle errors before using results.

The tests compare the generated result with an independent minimum model and
check all three properties over 169 boundary pairs and 512 deterministically
seeded full-width pairs. They include zero, equal values, the 32-bit boundary,
values above 2^53, the high bit, and the largest `uint64`. Additional tests check
error behavior and exact printed output.

Repository verification copies the whole example to a fresh temporary project,
regenerates through an installed wheel, compares package/IR/evidence bytes and
rechecks proofs. It then removes producer tools from `PATH` and tests and runs
the copy with cgo and module networking disabled. The existing installed-CLI
workflow runs this gate on Linux and Windows. Root Go tests separately copy only
`go.mod`, the consumer and its tests, and generated Go sources, then test and run
that deployment without any Lean sources, manifests or proof artifacts. This
also keeps the nested module covered by ordinary test runs.

## Where the boundary still is

The trusted path includes source elaboration, the pinned Lean compiler, the
extraction adapter and validation, Go emission and generated runtime, and the Go
compiler/runtime. Source elaboration can execute macros and initializers;
producer workspaces are not a sandbox for hostile Lean code. Independent Go
models, native Lean comparisons and regeneration checks test that path, but do
not close it with a compiler-correctness theorem.

The supported subset is deliberately bounded: pure monomorphic first-order
code, supported scalar operations, concrete records and sums, supported local
helpers and joins, and self recursion. Generic collections, polymorphism,
higher-order or escaping closures, effects, mutual function recursion, external
Lake packages and unsupported numeric/compiler forms fail explicitly. Ordinary
Lean syntax can still lower to an unsupported form. The
[repository guide](../../README.md#extract-a-module) gives the current boundary.

This example is about a reproducible proof-to-package handoff. It makes no
performance claim for clamp. The separate
[performance report](../research/generated-go-performance.md) compares generated
and handwritten Go and records measured frame-storage improvements for specific
recursive tree workloads, including their limitations and unchanged workloads.

Useful feedback is concrete: which small Lean function fails extraction, which
compiler form does it produce, which property would you want independently
specified, and where does the typed API or resource-limit contract complicate a
Go integration? Share a minimal source, selected root, `lean-go doctor` output,
and the exact command in the public
[lean-go issue tracker](https://github.com/franchb/lean-go/issues). This example
is small enough to copy and inspect; the same evidence boundary should remain
explicit as the supported subset grows.
