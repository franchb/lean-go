# lean-go

lean-go turns functions written in a small, first-order subset of
[Lean 4](https://lean-lang.org/) into a standalone Go package. You write the
logic in Lean, prove properties about it there, and ship ordinary Go that
depends only on the standard library. It is for Go developers who want a few
small, critical functions (a header parser, a checksum, a quota rule) to come
with machine-checked statements about what they compute. Using a generated
package needs only Go; generating one needs Lean as well.

## A complete example

The [clamp example](examples/proofs/clamp) is a Lean function in
`source/Clamp.lean`:

```lean
def clamp (value limit : UInt64) : UInt64 :=
  Bounds.cap value limit
```

with its helper in `source/Clamp/Bounds.lean`:

```lean
def cap (value limit : UInt64) : UInt64 :=
  if value ≤ limit then value else limit
```

and three theorems about it, one of which is (`proof/ClampProofs.lean`):

```lean
theorem bounded (value limit : UInt64) : Clamp.clamp value limit ≤ limit := by
  by_cases within : value ≤ limit
  · simp [Clamp.clamp, Clamp.Bounds.cap, within]
  · simp [Clamp.clamp, Clamp.Bounds.cap, within]
```

From `examples/proofs/clamp`, extract the function, check the proofs against
the generated package, and run the example's Go tests:

```sh
lean-go generate --source-dir source --module Clamp --root Clamp.clamp \
  --out clampnative --ir-output clamp.ir.json
lean-go check-proofs --contract proof-contract.json \
  --manifest clampnative/manifest.json --evidence native-proof-evidence.json
go test -count=1 ./...
```

`generate` writes `clampnative/` (identical existing output is accepted;
differing output is refused without `--replace`). `check-proofs` rebuilds the
proofs and prints `Checked 3 source theorems; evidence and generated files
match.` The generated function (`clampnative/functions.go`) is:

```go
// cap2 implements the Lean function Clamp.Bounds.cap.
func cap2(v1 uint64, v2 uint64) uint64 {
	if v1 <= v2 {
		return v1
	}
	return v2
}

// Clamp is the Lean function Clamp.clamp.
func Clamp(v1 uint64, v2 uint64) uint64 {
	return cap2(v1, v2)
}
```

A caller imports it like any Go package (`native_test.go`; generated packages
are named `extracted`, so import them under a name of your choice):

```go
native "example.com/proved-clamp/clampnative"

got := native.Clamp(value, limit)
```

## What is and is not guaranteed

This is not a verified compiler and does not formally verify Go programs. It
connects a Lean function to Go through three links of different assurance:

1. **Source theorems (proved, kernel-checked).** A
   [proof contract](docs/proof-contracts.md) maps your theorems to
   separately written expected statements. `lean-go check-proofs` rebuilds
   them with the pinned Lean, rejects `sorry`, `native_decide` and any axiom
   other than `propext`, `Classical.choice` and `Quot.sound`, and writes
   evidence that records the digest of every generated file. Changing the
   source, the proofs or a generated file makes the evidence fail. The
   theorems are about the Lean function; whether they say what you need is
   for a reviewer to decide.
2. **Translation validation (proved, kernel-checked, partial correctness).**
   Generation first produces an intermediate program (the *IR*, a JSON file
   such as `clamp.ir.json`). With `--validate report` or `--validate require`,
   or with `lean-go validate`, Lean proves for each exported function that
   *whenever the IR program returns a value, it is the encoding of the Lean
   function's result*. It does not prove that the IR program terminates.
   Validation is off by default. Every tracked example has a checked-in
   report; together they establish all 54 exported functions (88 of 88 IR
   functions) under the default budget
   ([coverage table](docs/research/translation-validation.md#9-coverage)).
3. **Trusted, not proved.** The Lean kernel; the meaning given to IR
   programs (`producer/LeanGo/Validate`) and the JSON-to-Lean bridge; the
   IR-to-Go printer and its runtime; the Go compiler; the producer's choice
   of exported functions; and your Lean source, which is elaborated with
   macros and initializers enabled. The IR-to-Go step is covered by
   differential tests and fuzzing ([evidence](docs/native-mode.md#evidence)),
   not by proof.

So a theorem from link 1 describes what the IR returns when it returns (link
2); that the Go code behaves like the IR is tested and trusted (link 3).

## What it supports today

Types: `Bool`, `UInt8`, `UInt16`, `UInt32`, `UInt64`, and your own
inductive types without parameters: structures, enumerations and recursive
types such as trees. Code: first-order functions, `if` and exhaustive
`match`, self-recursion, local `let` helpers that are called directly, up
to 64 exported functions sharing one package, and byte folds of the exact form `def f (s : T) (xs : List UInt8) := xs.foldl step s`, which
become a loop over a Go `[]byte`.

Integer operations admitted by the producer's primitive table
([`Primitives.lean`](producer/LeanGo/Module/Primitives.lean)):

| Type | `+` `-` `*` | `&&&` `\|\|\|` `^^^` `~~~` | `<<<` `>>>` | `==` `<` `≤` | Conversions |
| --- | --- | --- | --- | --- | --- |
| `UInt8` | yes | all four | both | yes | `toUInt16`, `toUInt32` |
| `UInt16` | no | `\|\|\|` and `^^^` only | both | no | `toUInt8`, `toUInt32` |
| `UInt32` | yes | all four | both | yes | `toUInt8`, `toUInt64` |
| `UInt64` | yes | all four | both | yes | `toUInt32` |

Literals of all four widths are supported. Lean's fixed-width integers
[wrap bit vectors](https://lean-lang.org/doc/reference/latest/Basic-Types/Fixed-Precision-Integers/);
`+`, `-`, `*` and `<<<` wrap modulo 2^n as in
[Go](https://go.dev/ref/spec#Integer_overflow), and shift counts are reduced
modulo the width.

Not supported, each rejected with a diagnostic: `/` and `%` at any width;
`UInt16` arithmetic and comparisons; conversions not in the table (for
example `UInt8.toUInt64`); `Nat`, `Int`, `String`, `Array`, `Option` and
lists other than the byte-fold input; functions as arguments; polymorphism;
mutual recursion; `partial` and `unsafe` definitions; custom compiler
rewrites and external Lake packages. Ordinary-looking Lean can still compile to
a form the producer rejects; [diagnostics](docs/diagnostics.md) say where.

## Two output modes

- **Native** (default): plain Go types and functions, as above. Every Go
  value of a generated type is a valid Lean value; functions return an error
  only on recursive call chains deeper than a fixed `MaxDepth`. There are no
  limits on memory or time.
- **Metered** (`--mode metered`): the same IR compiled to an explicit-stack
  state machine over boxed values, with seven resource budgets per call.
  Choose it when the package processes untrusted recursive input and must
  bound its work.

Measured in [native mode, Benchmarks](docs/native-mode.md#benchmarks): every
native workload is within 1.1x of a hand-written Go transliteration of the
same Lean source, and metered mode is 7.1 to 285 times slower than native.
A better algorithm still wins: `hash/adler32` takes 30.9 µs on 64 KiB
against 114.8 µs for the native Adler-32, whose Lean source reduces after
every byte. Record-heavy code is further off: for the DNS header, native
parse plus serialize takes 106.6 ns against 31.0 ns for a hand-written
`encoding/binary` codec, about 3.4 times slower (UDP 4.7, the demo header
2.3), and metered takes 10,914 ns. The gap is Go copying 12-field structs
through memory at the by-value API boundaries, not the extracted code
([wire formats](docs/wire-formats.md#native-against-metered)).

## Wire formats

`lean-go format generate -spec spec.json -out DIR` turns a JSON description
of a fixed-size binary header into the Lean parser and serializer, a proof
contract with generated proofs (exact acceptance conditions and both round
trips), the extracted native package, a `[]byte` Go API built not to panic
or retain the caller's slices, and generated property and fuzz tests that
check it. The
[DNS message header](examples/formats/dns) follows
[RFC 1035 section 4.1.1](https://www.rfc-editor.org/rfc/rfc1035#section-4.1.1);
the [UDP header](examples/formats/udp) follows
[RFC 768](https://www.rfc-editor.org/rfc/rfc768). See
[wire formats](docs/wire-formats.md) for the spec format and its limits.

## Install

To use a generated package you need only Go (the project pins and tests Go
1.27.4); generated code imports nothing outside the standard library and
builds with `CGO_ENABLED=0`.

To generate, you also need Lean 4.36.0 at commit
`611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9`, with `lean` and `lake` on
`PATH` (see [installing Lean](https://lean-lang.org/install/);
`producer/lean-toolchain` names the toolchain for
[elan](https://github.com/leanprover/elan)). The Lean side of the tool is
embedded in the binary. From a checkout:

```sh
go install ./cmd/lean-go
lean-go doctor
```

`lean-go doctor` checks the build's Go release, the exact Lean and Lake
versions, and the producer cache. The compiled producer is cached; with a
warm cache the clamp extraction above took 3.6 s here. The commands are `generate`, `emit`, `check-proofs`,
`validate`, `format`, `regenerate`, `doctor` and `version`; see the
[CLI guide](docs/cli.md).

Contributors additionally need Python 3.15 through
[uv](https://docs.astral.sh/uv/): part of the test suite and some
verification scripts are still Python. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Status

This is an alpha. The IR and manifest schemas, the generated Go API, the
supported Lean subset and the command-line options may change between
prereleases, and IR and manifests from earlier versions are rejected; pin a
commit and regenerate the package, IR and proof evidence together. Release
history is in [CHANGELOG.md](CHANGELOG.md).

Next: [getting started](docs/getting-started.md) (extract your own module),
the [CLI guide](docs/cli.md), [native mode](docs/native-mode.md),
[proof contracts](docs/proof-contracts.md), [wire formats](docs/wire-formats.md),
[diagnostics](docs/diagnostics.md), the [quota library](docs/quota.md) and
[translation validation](docs/research/translation-validation.md) (semantics,
proof method, coverage).

Maintainer: [franchb](https://github.com/franchb) · hello@franchb.com

License: [Apache 2.0](LICENSE). See [CONTRIBUTING.md](CONTRIBUTING.md) for
the tools and the checks to run before sending a change.
