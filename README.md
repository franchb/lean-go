# lean-go

Generate pure Go from a supported subset of ordinary Lean 4.

The module extractor accepts user-supplied ordinary Lean source within a supported
pure, monomorphic subset and generates a standalone Go package. Generated consumers
use only the Go standard library and run with `CGO_ENABLED=0`. Lean and Python are
needed for regeneration, not deployment.

The original audited normalizer remains available as a separate v1 profile.
Module exports now use `pure-module/v4` with v6 IR, including UInt32 and UInt64
arithmetic, comparisons, local joins and source record names for typed Go APIs.
Earlier v2–v5 IR remain accepted with their original restrictions and APIs.
There is no fixed model hash or application name allowlist. This is local
experimental software; no release has been published.

## Extract a module

Use Go **1.27.4**, the pinned Lean **4.35.0**, and **uv** with Python **3.15**.
Python dependencies are managed by `uv.lock`; currently the tooling uses only the
standard library. From the repository root:

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Trees --root Trees.mirror \
  --out generated/trees --ir-output testdata/trees.ir.json
```

Point `--source-dir` at your Lean sources and select a module and fully qualified
root. Local imports are rebuilt from source. The command uses a fresh workspace,
ignores project build scripts and compiled caches, and accepts supported pinned
standard-library imports. External Lake packages are not supported. Module and root
CLI names currently use ASCII Lean identifiers. Output package and IR paths are
required; differing existing package contents are preserved and rejected.

Supported code includes nonempty, non-parametric enums and recursive sums, pure first-order
functions, saturated calls, exhaustive matches, and self recursion. `UInt32` and `UInt64`
supports literals, addition, subtraction, multiplication, left shift, equality,
and unsigned less-than/less-or-equal comparisons. Arithmetic wraps modulo 2^32 or
2^64 respectively; shift counts are reduced modulo 32 or 64. Conversions between
widths, division, remainder, other bit operations,
other numeric types, generic collections, polymorphism,
higher-order code, mutual function recursion, effects, and unsupported compiler
forms fail explicitly. Even ordinary Lean syntax can lower to an unsupported form.

Record fields, nested reads and updates use the same API. Single-field record
wrappers keep their source names and constructors in Go even when Lean removes
them internally. The producer inserts checked wrapping/unwrapping steps at
function and field boundaries; wrapping consumes the existing allocation and
step budgets. See the [record report](docs/research/extraction-records.md) for
examples and the supported representation rules.

Results of `if` and `match` expressions can feed later arithmetic and record
updates through nonrecursive local joins. Joins preserve captured values and
reuse the current Go frame; declarations and jumps consume control steps but
allocate no values or call frames. See the [join report](docs/research/extraction-joins.md).

Unused arguments can be removed inside compiler-generated `._redArg` helpers
while the exported root keeps its original source signature. The producer checks
the compiler event and forwarding argument map, then audits each helper through
its original source owner. See the
[reduced-argument report](docs/research/extraction-reduced-arguments.md) for the
supported forms and collision checks.

Directly called local helpers can capture supported data, including nested records.
The producer checks their original source types and the compiler's actual lifting
event, including capture order. Exported roots keep their source signatures;
internal helper parameters use checked compiler data representations. See the
[local-helper report](docs/research/extraction-local-helpers.md) for the exact
limits, including unsupported escaping closures and ambiguous helper reuse.

The producer records original source dependencies and active compiler rewrites
when each declaration compiles, including helpers later inlined away. User-defined
compiler rewrites are currently rejected; the pinned `Bool.not` rewrite is audited.
Numeric primitives and their standard-library operator support are also pinned
and audited, including historical changes to their compiler attributes.
Source must be trusted for Lean elaboration: this tool is not a sandbox for macros
or initializers. The new profile makes no application correctness-theorem claim.
See the [module report](docs/research/extraction-modules.md) for the exact limits
and the [UInt32 report](docs/research/extraction-uint32.md) for numeric semantics
and validation evidence. The [UInt64 report](docs/research/extraction-uint64.md)
describes exact wide values and mixed-width validation. See also the
[module design](docs/superpowers/specs/2026-11-11-lean-go-modules-design.md).

## Typed module API

New module exports include typed constructors, record getters, sum destructors and
`RunTyped`. ADT wrappers own immutable values; `UInt32`, `UInt64` and `Bool` use Go
`uint32`, `uint64` and `bool`. For example, the generated tree package can be called without Lean
names or field indexes:

```go
import x "github.com/franchb/lean-go/generated/trees"

limits := x.DefaultLimits()
red, err := x.NewColorRed(limits)
if err != nil { panic(err) }
leaf, err := x.NewTreeLeaf(red, limits)
if err != nil { panic(err) }
result, err := x.RunTyped(leaf, limits)
if err != nil { panic(err) }
color, ok := result.AsLeaf()
if !ok || !color.IsBlue() { panic("unexpected result") }
```

Records have named getters such as `count.Value()` and `state.Count()`, returning
a typed value and a validity flag. Sums have `IsLeaf`/`AsLeaf`-style methods.
Constructors reject invalid zero wrappers and enforce input node/depth limits.
`RunTyped` delegates to the same runtime and resource limits as `Run`. Generated
names are deterministic; the manifest records collision resolutions. See the
[typed API report](docs/research/extraction-typed-api.md) for the complete contract.

## Raw module API

The selected root is exposed as `Run`. Full Lean names identify values, avoiding
Go identifier collisions between different source modules:

```go
import x "github.com/franchb/lean-go/generated/trees"

limits := x.DefaultLimits()
input := &x.Input{
    Type: "Trees.Tree", Constructor: "Trees.Tree.leaf",
    Fields: []*x.Input{{Type: "Trees.Color", Constructor: "Trees.Color.red"}},
}
owned, err := x.Import(input, limits)
if err != nil { panic(err) }
result, err := x.Run([]x.Value{owned}, limits)
if err != nil { panic(err) }
// result.Fields()[0].Constructor() == "Trees.Color.blue"
```

`Import` copies every input occurrence into immutable storage. Do not mutate the
input during import; later mutation cannot affect the owned value. Cycles fail,
and shared acyclic subtrees are copied and charged per occurrence. `Value{}` is
invalid. `Type`, `Constructor`, `UInt32`, `UInt64`, and `Fields` inspect owned values;
`Fields` returns a fresh slice. A scalar input uses `Type: "UInt32"` and the
`UInt32` field, with no constructor or children; a wide scalar uses `Type: "UInt64"`
and the `UInt64` field. Nonzero inactive scalar payloads are rejected.

`Import` enforces the input node and depth budgets; the other five budgets apply
to `Run`. All seven limit fields must be valid in both calls.
`Run` checks root argument types/arity and enforces node, depth, output-node,
allocation, step, frame, and cumulative frame-slot limits. Errors return an invalid
value and distinguish `ErrMalformed`, `ErrLimits`, and `ErrResource`. The manifest
records the schema, root signature, source provenance, and output hashes. Control
flow is compiled Go code, not a deployed Lean or IR interpreter.

## Check the project

```sh
uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
```

The first Go command uses tracked generated packages; the opt-in gates compile
native Lean references, rebuild both profiles, and test changed source modules.
Generated packages can be copied into independent Go modules without producer
files or tools. Extraction remains tested and trusted, not formally verified.

## Run the pilot

Use Go **1.27.4**, Python **3.15** managed by **uv**, and the Lean **4.35.0** toolchain named in
[`producer/lean-toolchain`](producer/lean-toolchain). The exporter checks the
exact Lean commit, `eb6cafb7290e8bd5f2937522d8d804f58f02bafc`. Run from the
repository root, with these tools available on `PATH`:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./...
uv run --locked python scripts/generate.py
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 go test -count=1 ./...
```

The first test command exercises the tracked generated package without running
Lean. The final command also builds a native Lean reference and regenerates
twice in fresh producer workspaces; those two gates are opt-in.

Generation defaults to [`generated/normalizer`](generated/normalizer) and
[`testdata/normalizer.ir.json`](testdata/normalizer.ir.json). Identical existing
output is a no-op; differing existing output is refused and preserved. To
inspect a changed generator, select a new output directory and a separate IR
file with `--out DIR --ir-output FILE`; those paths must not overlap.

## Consumer API

Within this checkout, import `github.com/franchb/lean-go/generated/normalizer`:

```go
package main

import (
    "fmt"

    n "github.com/franchb/lean-go/generated/normalizer"
)

func main() {
    limits := n.DefaultLimits()
    input := &n.Input{
        Kind: n.Not,
        Left: &n.Input{Kind: n.Literal, Value: n.False},
    }
    owned, err := n.Import(input, limits)
    if err != nil {
        panic(err)
    }
    result, err := n.Normalize(owned, limits)
    if err != nil {
        panic(err)
    }
    value, ok := result.Literal()
    fmt.Println(value == n.True && ok) // true
}
```

`Import` validates and copies each input occurrence into an immutable `Expr`.
Do not mutate `Input` during `Import`; later input mutations cannot affect the
owned expression. `Expr{}` is invalid. Cycles and malformed shapes are rejected;
acyclic sharing is copied per occurrence and charged against the limits.
`Normalize` returns `ErrMalformed`, `ErrResource`, or `ErrLimits` on failure,
with an invalid result. See the [pilot report](docs/research/extraction-pilot.md)
for the six enforced budgets, ownership rules, validation evidence, benchmarks,
and remaining release gates.

The assurance is **proved Lean logic with tested, trusted extraction**. Lean
checks evaluation preservation and output normal form; the adapter, lowering,
Go emitter, host boundary, compilers, and runtimes remain trusted. There is no
end-to-end compiler-correctness proof. Linux/amd64 tests and an isolated Go-only
consumer passed; Windows/amd64 has only been cross-built, and execution from a
tagged release remains pending.

The [approved design](docs/superpowers/specs/2026-11-11-lean-go-extraction-design.md),
[feasibility report](docs/research/extraction-feasibility.md), and
[pilot plan](docs/superpowers/plans/2026-11-11-lean-go-pilot.md) explain the scope,
architecture, and evidence boundaries.

Maintainer: [franchb](https://github.com/franchb) · hello@franchb.com
