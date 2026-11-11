# Try the experimental preview

The generated packages run with Go 1.27.4 and `CGO_ENABLED=0`. Lean, Python, uv
and a C compiler are not needed by a consumer. The preview version is
`v0.1.0-alpha.2`; its API is experimental.

## Run a generated package

Create a fresh directory and initialize a Go module:

```sh
mkdir lean-go-example
cd lean-go-example
go mod init example.com/lean-go-example
go get github.com/franchb/lean-go@v0.1.0-alpha.2
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

## Extract your own Lean module

Install Go 1.27.4, Lean 4.35.0 and uv. Use Python 3.15 through uv; the repository
declares that requirement and has a locked dependency set. Clone the pinned
preview into a separate directory:

```sh
git clone --branch v0.1.0-alpha.2 https://github.com/franchb/lean-go.git
cd lean-go
uv python install 3.15
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
