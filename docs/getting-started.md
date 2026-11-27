# Getting started

This walkthrough extracts a small Lean module into a Go package, calls it from
Go, and then adds translation validation. It needs Go 1.27.4 and Lean 4.36.0
at commit `611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9` with `lean` and `lake`
on `PATH`.

## Install the command

From a lean-go checkout:

```sh
go install ./cmd/lean-go
lean-go doctor
```

`doctor` reports the Go release the binary was built with (`WARN` if it is
not Go 1.27.4, which only byte-exact reproduction of tracked artifacts
needs), then Lean, the Lake environment and the producer cache, and exits
nonzero if one of those fails. `go install` puts the binary in `$(go env GOPATH)/bin` unless
`GOBIN` is set; that directory must be on `PATH`.

## Run a tracked example with Go alone

Every example under `examples/proofs/` is a self-contained Go module with its
generated package checked in:

```sh
cd examples/proofs/clamp
go test -count=1 ./...
go run .
```

The program prints `clamp(7, 42) = 7`, `clamp(100, 42) = 42` and
`clamp(18446744073709551615, 42) = 42`. Its `README.md` explains the Lean
source, the three theorems and both generated packages.

## Extract your own module

In a new directory, create `counter-example/lean/Counter.lean`:

```lean
namespace Example

structure Counter where
  value : UInt64

def increment (counter : Counter) (amount : UInt64) : Counter :=
  ⟨counter.value + amount⟩

end Example
```

Then generate a package from `counter-example`:

```sh
cd counter-example
lean-go generate --source-dir lean --module Counter --root Example.increment \
  --out counter --ir-output counter.ir.json
```

`--source-dir` holds only your `.lean` files; the run happens in a fresh
private Lake workspace. `counter/` now contains `manifest.json`, `native.go`,
`types.go` and `functions.go`. The structure becomes a Go struct and the
function a Go function:

```go
// Increment is the Lean function Example.increment.
func Increment(v1 Counter, v3 uint64) Counter {
	return Counter{Value: v1.Value + v3}
}
```

Parameters are numbered because the IR does not keep source parameter names.
Run `go mod init example.com/counter` and save this `main.go`:

```go
package main

import (
	"fmt"

	counter "example.com/counter/counter"
)

func main() {
	output := counter.Increment(counter.Counter{Value: 41}, 1)
	fmt.Println(output.Value) // 42
}
```

`go run .` prints `42`. The generated Go package is named `extracted`, hence
the import alias. Every Go value of a generated type, including the zero value,
is a valid Lean value, and functions that do not recurse return no error. See
[native mode](native-mode.md) for the full mapping.

If a source form is outside the supported subset, `generate` fails with a
diagnostic naming the stage, the reason and the source location, and leaves
existing output untouched; see [diagnostics](diagnostics.md) and the support
table in the [README](../README.md#what-it-supports-today). Running `generate`
again with the same inputs is accepted; differing existing output is refused
unless you add `--replace`.

## Metered output

For a package that must bound its work on untrusted recursive input, add
`--mode metered`. The same IR then runs on an explicit-stack interpreter
with seven resource budgets per call, and every call takes `Limits`:

```sh
lean-go generate --source-dir lean --module Counter --root Example.increment \
  --out metered --ir-output metered.ir.json --mode metered
```

```go
limits := counter.DefaultLimits()
input, err := counter.NewCounter(41, limits)
if err != nil {
	panic(err)
}
output, err := counter.RunTyped(input, 1, limits)
if err != nil {
	panic(err)
}
value, valid := output.Value()
fmt.Println(value, valid) // 42 true
```

Here `counter` is imported from `example.com/counter/metered`. Metered record
wrappers are immutable and their zero values are invalid; errors distinguish
malformed inputs, invalid limits and exhausted budgets.

## Validate the translation

Add `--validate report` to prove, inside Lean, that the IR program agrees
with the Lean function:

```sh
lean-go generate --source-dir lean --module Counter --root Example.increment \
  --out counter --ir-output counter.ir.json --validate report
```

It prints `validation: 1/1 functions proved, 1/1 roots established` and
writes `counter.validation.json` next to the IR. The proved statement is
partial correctness: whenever the IR program returns a value, it is the
encoding of `Example.increment`'s result. `--validate require` fails, and
publishes nothing, unless everything is proved. The Go printer and the Go
compiler remain trusted; see the [CLI guide](cli.md#translation-validation).

## Prove properties and bind them to the package

Theorems about your Lean functions are checked with a
[proof contract](proof-contracts.md): a JSON file that maps each theorem to a
separately written expected statement. From `examples/proofs/clamp`:

```sh
lean-go check-proofs --contract proof-contract.json \
  --manifest clampnative/manifest.json --evidence native-proof-evidence.json
```

This rebuilds the proofs, compares the theorem statements with the expected
ones, and checks that the evidence still matches the generated files. Use
`--output FILE` instead of `--evidence` to write fresh evidence for your own
project.

The proofs are about the Lean source. Lean source is elaborated with macros
and initializers enabled, so only extract code you trust.
