# lean-go

Generate pure Go from a supported subset of ordinary Lean 4.

The **working local extraction pilot** compiles one audited, proved
three-valued expression normalizer through Lean's pure monomorphic LCNF,
a strict typed JSON IR, and an original Go emitter. The generated package uses
only the Go standard library and builds and runs with `CGO_ENABLED=0`.
Consumers need Go; proof checking and regeneration need Lean and Python.

The current profile admits exactly the reviewed
[`Model.lean`](feasibility/LeanGo/Feasibility/Model.lean) revision. Modified model
bytes and arbitrary Lean modules are rejected. This is a completed local pilot;
there is no published release or general module admission yet.

## Run the pilot

Use Go **1.27.4**, Python **3.11+**, and the Lean **4.35.0** toolchain named in
[`producer/lean-toolchain`](producer/lean-toolchain). The exporter checks the
exact Lean commit, `eb6cafb7290e8bd5f2937522d8d804f58f02bafc`. Run from the
repository root, with these tools available on `PATH`:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./...
python3 scripts/generate.py
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
