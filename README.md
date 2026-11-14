# lean-go

Generate pure Go from a supported subset of ordinary Lean 4.

The module extractor accepts user-supplied ordinary Lean source within a supported
pure, monomorphic subset and generates a standalone Go package. Generated consumers
use only the Go standard library and run with `CGO_ENABLED=0`. Lean and Python are
needed for regeneration, not deployment.

The original audited normalizer remains available as a separate v1 profile.
Exports using existing operations retain `pure-module/v4` with v6 IR for one root,
including UInt32 and UInt64 arithmetic, comparisons, local joins and source record
names for typed Go APIs. Multiple roots use `pure-module/v5` with v7 IR and share
one generated package.
Alpha.6 adds bitwise AND, OR, XOR, complement and logical right shift for both
widths. Emitted bitwise operations select v8/v9 IR with `pure-module/v6` or
`pure-module/v7`; see the [bitwise report](docs/research/extraction-bitwise.md).
Earlier v2–v7 IR retain their original restrictions and APIs.
Published alpha.7 adds exact `UInt32.toUInt64` zero extension and
`UInt64.toUInt32` truncation modulo 2^32. Emitted conversions select v10/v11 IR
with `pure-module/v8` or `pure-module/v9`; see the
[width-conversion report](docs/research/extraction-width-conversions.md).

The **alpha.8 candidate**, Python package `0.1.0a8`, adds scalar `UInt8`, exact
byte/word conversions and a [proved byte codec](examples/proofs/bytes).
Byte-enabled exports use v12/v13 IR, `pure-module/v10` or `pure-module/v11`, and
`lean-go-modulegen/v10`; see the [UInt8 report](docs/research/extraction-uint8.md).
Before publication, regenerate with the CLI from this same checkout. Published
alpha.7 uses generator v9 and has no UInt8 support.

There is no fixed model hash or application name allowlist. APIs and supported
compiler forms may change before a stable release. Start with the
[walkthrough](docs/getting-started.md) or read the
[alpha.8 candidate notes](docs/releases/v0.1.0-alpha.8.md).
[Structured extraction diagnostics](docs/diagnostics.md) are available since
alpha.5. Alpha.6 reports valid custom compiler rewrites as `admission` /
`source_audit` with the source owner and location; genuine build failures retain
their `build` stage. The producer still requires Lean **4.36.0-rc2**, commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`.
The [compatibility harness](docs/compiler-compatibility.md) is research tooling
included in the Git checkout, separate from the installed CLI and wheel. Later
compiler releases require fresh qualification and review before promotion;
production extraction requires the exact supported version and commit.

The installable CLI, recursive frame-storage reuse and standalone proved-clamp
demo remain available alongside multi-root extraction, quota and proof contracts. The
[proof-to-Go walkthrough](docs/release-candidate.md) checks source theorems,
extracts fresh Go and exercises an isolated consumer.

Before alpha.8 publication, use the
[checkout installation](docs/cli.md#development-checkout) and
[local candidate checks](docs/release-candidate.md). After `v0.1.0-alpha.8` is
published, install its [`lean-go` CLI](docs/cli.md) to generate and check proofs
from any working directory without keeping a checkout beside your project:

```sh
uv tool install --python 3.15 "git+https://github.com/franchb/lean-go.git@v0.1.0-alpha.8"
```

To reproduce published alpha.7, use its
[tagged guide](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.7/docs/cli.md)
and examples from that same tag.

Copy the [standalone clamp module](examples/proofs/clamp) to run a complete
example offline with Go alone, or read
[From Lean source proofs to a Go package you can copy](docs/articles/lean-proofs-to-go.md)
for the three claims, reproducible commands and extraction boundary.
To regenerate the current checkout's examples and proof evidence, install the
CLI from that same [development checkout](docs/cli.md#development-checkout).
Published-release reproduction requires the example and CLI from the same tag.

The [standalone flag-mask module](examples/proofs/flags), included in alpha.6,
demonstrates four `UInt32` bitwise operations with eight universally quantified
source proofs and independent Go tests over 4,752 input pairs. Its checked-in
package runs with Go alone. Use the example and CLI from the same tag, or install
from the same checkout when regenerating development artifacts.

Alpha.7 includes a [standalone word-packing module](examples/proofs/words)
with five scalar roots and seven universal source proofs: value-preserving
widening, modulo narrowing, both conversion round trips, and packing/unpacking
identities. Independent typed/raw Go checks cover 699 boundary and seeded triples.
Words uses v11 IR/manifest and `pure-module/v9`. The alpha.8 candidate contains
all four standalone demos with module generator v10, including demos whose IR
schema stays unchanged. Pair examples and CLI from the same checkout or tag;
published alpha.7 reproduction uses its historical artifacts and generator v9.

## Use the proved byte codec

The alpha.8 candidate [byte codec module](examples/proofs/bytes) encodes a
`uint32` into four byte fields and decodes them in either byte order. Its
[Go application](examples/proofs/bytes/main.go) reads those fields into `[4]byte`
and prints both round trips. From this checkout's root, run it offline:

```sh
(
  cd examples/proofs/bytes
  CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
  CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
)
```

```text
LE(0x12345678) = [78 56 34 12] -> 0x12345678
BE(0x12345678) = [12 34 56 78] -> 0x12345678
```

Twelve kernel-checked source claims establish both inverse directions for each
order and all eight arithmetic byte positions. Independent raw/typed Go tests
compare 652 cases with `encoding/binary` and check ownership, errors and resource
budgets. The [byte codec walkthrough](docs/articles/proved-byte-codec.md) explains
the application, proofs and fresh regeneration. The source uses a concrete
four-field Lean record; broad generic collections remain deferred. Source proofs
establish Lean behavior; compilation, extraction and Go execution remain tested
and trusted.

## Extract a module

Use Go **1.27.4**, the pinned Lean **4.36.0-rc2**, and **uv** with Python **3.15**.
Python dependencies are managed by `uv.lock`; currently the tooling uses only the
standard library. These commands use the current development checkout and its
tracked artifacts. For an installed command, use the CLI from that same
[checkout](docs/cli.md#development-checkout). From the repository root:

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
functions, saturated calls, exhaustive matches, and self recursion. `UInt8`,
`UInt32` and `UInt64` support literals, addition, subtraction, multiplication,
left shift, equality, unsigned less-than/less-or-equal comparisons, `&&&`, `|||`,
`^^^`, `~~~` and logical right shift `>>>`. Arithmetic wraps modulo 2^8, 2^32 or
2^64; shift counts are reduced modulo 8, 32 or 64 respectively. Ordinary `<<<`
notation is supported. Exact casts are `UInt8.toUInt32`, `UInt32.toUInt8`,
`UInt32.toUInt64` and `UInt64.toUInt32`; widening preserves the value and narrowing
reduces it modulo 2^8 or 2^32 respectively.
UInt8 is available only in this development checkout. Earlier schemas keep their
restrictions: left shift alone on UInt32/UInt64 keeps v6/v7 IR and
`pure-module/v4` or `pure-module/v5`. Other conversions, including direct
UInt8/UInt64 casts, division, remainder, other numeric operations and types,
generic collections, polymorphism,
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
`RunTyped`. ADT wrappers own immutable values; `UInt8`, `UInt32`, `UInt64` and
`Bool` use Go `uint8`, `uint32`, `uint64` and `bool`. For example, the generated tree package can be called without Lean
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

## Export several functions together

Multi-root extraction is available starting with `v0.1.0-alpha.3`.

Repeat `--root` to share types, constructors and runtime across selected functions:

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Counters \
  --root Counters.initial --root Counters.advance \
  --root Counters.read --root Counters.total \
  --out generated/counters --ir-output testdata/counters.ir.json
```

The generated typed functions compose directly:

```go
import (
    "fmt"
    c "github.com/franchb/lean-go/generated/counters"
)

limits := c.DefaultLimits()
state, err := c.RunInitial(10, 9007199254740993, true, limits)
if err != nil { panic(err) }
delta, err := c.NewCount(3, limits)
if err != nil { panic(err) }
next, err := c.RunAdvance(state, delta, limits)
if err != nil { panic(err) }
value, err := c.RunRead(next, limits)
if err != nil { panic(err) }
fmt.Println(value) // 13
```

Each call starts fresh resource budgets; `state` remains unchanged. Raw callers
use `RunRoot("Counters.read", args, limits)`. Only selected roots are exposed.
The manifest maps full Lean names to typed entrypoints, including collision
suffixes. Selection order is irrelevant; duplicates and more than 64 roots fail.
Multi-root packages have no default `Run`/`RunTyped`; single-root packages retain
both. See [multi-root extraction](docs/research/extraction-multiroot.md).

## A library with Lean-proved quota rules

The `quota` package provides immutable bounded accounting: create a quota,
reserve units, release units and read remaining capacity. Its four operations
share generated types, and its Lean source has ten checked theorems covering
acceptance, invariant preservation, rejection identity and exact arithmetic.

```sh
CGO_ENABLED=0 go run ./examples/quota
uv run --locked python scripts/check_quota.py
```

The proof evidence is bound to the source and generated package hashes. These
are proofs of the Lean rules; extraction and Go execution remain tested and
trusted. See the [quota guide](docs/quota.md) for the Go API, checks and limits.
This library is available starting with `v0.1.0-alpha.3`.

Other libraries can use the same proof gate through a small JSON contract that
maps named theorems to independently authored expected propositions. The checker
supports both single-root and multi-root generated packages, rebuilds proofs
from source, and records deterministic evidence bound to the generated bytes.
See [reusable proof contracts](docs/proof-contracts.md) for the external-project
workflow and its assurance boundary.

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
and the `UInt64` field. Byte-enabled packages also expose `Value.UInt8()` and
accept `Type: "UInt8"` with the `UInt8` field. Nonzero inactive scalar payloads are
rejected.

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

To compare newly generated programs against an independent source model, native
Lean, and raw/typed Go, run:

```sh
uv run --locked python scripts/differential.py
```

The default corpus combines two seeds, both unsigned widths and 32 input pairs
per program. Full program artifacts support replay and bounded failure shrinking.
See [program differential testing](docs/research/differential-testing.md) for
the grammar, commands and evidence limits.

See [generated Go performance](docs/research/generated-go-performance.md) for
maintained handwritten/typed/raw benchmarks and measured frame-slot reuse results.

## Run the pilot

Use Go **1.27.4**, Python **3.15** managed by **uv**, and the Lean **4.36.0-rc2** toolchain named in
[`producer/lean-toolchain`](producer/lean-toolchain). The exporter checks the
exact Lean commit, `b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`. Run from the
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
consumer passed. CI runs Go checks on Linux and Windows; the published prerelease
record links the observed platform and exact-tag consumer results.

The [approved design](docs/superpowers/specs/2026-11-11-lean-go-extraction-design.md),
[feasibility report](docs/research/extraction-feasibility.md), and
[pilot plan](docs/superpowers/plans/2026-11-11-lean-go-pilot.md) explain the scope,
architecture, and evidence boundaries.

Maintainer: [franchb](https://github.com/franchb) · hello@franchb.com

License: [Apache 2.0](LICENSE). See [CONTRIBUTING.md](CONTRIBUTING.md) for the
pinned tools and validation workflow.
