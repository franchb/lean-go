# Try the experimental preview

The generated packages run with Go 1.27.4 and `CGO_ENABLED=0`. Lean, Python, uv
and a C compiler are not needed by a consumer. This checkout prepares
`v0.1.0-alpha.7`; its API is experimental. Tagged commands below apply after
publication. Before then, use this checkout and its
[paired CLI](cli.md#development-checkout).

Alpha.7 adds exact UInt32/UInt64 width conversions, ordinary left-shift notation
and a standalone proved word-packing example. Bitwise operations, the CLI,
multi-root extraction, quota and reusable proof contracts remain available.
Its producer uses Lean 4.36.0-rc2. The
[proof-to-Go walkthrough](release-candidate.md) checks this candidate; generated
Go consumers do not require the producer compiler.

## Run a generated package

Create a fresh directory and initialize a Go module:

```sh
mkdir lean-go-example
cd lean-go-example
go mod init example.com/lean-go-example
go get github.com/franchb/lean-go@v0.1.0-alpha.7
```

Save this as `main.go`:

```go
package main

import (
    "fmt"
    wide "github.com/franchb/lean-go/generated/wide"
)

func main() {
    result, err := wide.RunTyped(^uint64(0), 1, wide.DefaultLimits())
    if err != nil {
        panic(err)
    }
    sum, valid := result.Sum()
    above53, exact := result.Above53()
    fmt.Println(sum, valid, above53, exact)
}
```

Run it with cgo disabled:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go run .
```

In PowerShell, set `$env:CGO_ENABLED = "0"` and
`$env:GOTOOLCHAIN = "local"`, then run `go run .`.
The output is `0 true 9007199254740993 true`: addition wraps at 2^64, and the
larger literal remains exact. The source is
[`Wide.lean`](../examples/modules/Wide.lean). Other tracked packages demonstrate
recursive trees, record updates, joins and compiler-generated local helpers.

## Pack words with alpha.7

Alpha.7 includes a [copyable Words module](../examples/proofs/words). It packs
two `UInt32` values into a `UInt64`, unpacks both halves, widens without loss and narrows modulo 2^32. Seven universal Lean source
proofs accompany independent Go checks over 699 boundary and seeded triples.

From the alpha.7 checkout, run the generated example offline:

```sh
cd examples/proofs/words
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
```

The copyable module needs only Go. To regenerate its v11 artifacts or recheck
proof evidence, pair the example and [CLI](cli.md) from the same alpha.7 tag, or
use the same checkout before publication, and follow the example's verifier
instructions. The extraction and Go runtime remain tested and trusted; the proofs
establish the selected Lean statements.
Runtime division/remainder, other numeric widths and broad generic collections
remain deferred.

## Extract your own Lean module

Use Go 1.27.4, Lean 4.36.0-rc2 and uv for a checkout of `v0.1.0-alpha.7`.
The exact Lean commit is `b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`.
Use Python 3.15 through uv; the repository declares that requirement and has a
locked dependency set. From the release checkout:

```sh
uv python install 3.15
uv sync --locked --reinstall-package lean-go-tools
uv run --locked lean-go doctor
```

Create `../counter-example/lean/Counter.lean`:

```lean
namespace Example

structure Counter where
  value : UInt64

def increment (counter : Counter) (amount : UInt64) : Counter :=
  ⟨counter.value + amount⟩

end Example
```

From the lean-go checkout, generate a package in the sibling project:

```sh
uv run --locked python scripts/generate.py \
  --source-dir ../counter-example/lean \
  --module Counter --root Example.increment \
  --out ../counter-example/counter \
  --ir-output ../counter-example/counter.ir.json
```

The generated directory is standalone Go source. In `../counter-example`, run
`go mod init example.com/counter` and save this `main.go`:

```go
package main

import (
    "fmt"
    counter "example.com/counter/counter"
)

func main() {
    limits := counter.DefaultLimits()
    input, err := counter.NewCounter(41, limits)
    if err != nil { panic(err) }
    output, err := counter.RunTyped(input, 1, limits)
    if err != nil { panic(err) }
    value, valid := output.Value()
    fmt.Println(value, valid) // 42 true
}
```

Run with the same cgo-disabled Go command. Record wrappers are immutable; their
zero values are invalid. Constructors and calls validate limits. Errors distinguish
malformed inputs, invalid limit settings and exhausted resource budgets.

The producer accepts the documented pure, monomorphic subset and rejects
unsupported forms. Its input Lean source must be trusted: elaboration can execute
macros and initializers. The extraction pipeline is tested but not formally
verified, and it does not establish an application correctness theorem. See the
[README](../README.md) for supported forms and the research reports for evidence.
