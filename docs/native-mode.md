# Native Go mode

Native mode is the default output of `lean-go generate` and `lean-go emit`.
It emits plain, unmetered Go from the validated module IR. Lean scalars
become Go unsigned integers, Lean types become ordinary Go types, and Lean
functions become ordinary Go functions. There are no `Limits` parameters, no
resource budgets and no `Value`/`Input` raw API.

```sh
go run ./cmd/lean-go generate --source-dir examples/modules --module Trees \
  --root Trees.mirror --out /tmp/trees --ir-output /tmp/trees.ir.json
go run ./cmd/lean-go emit --in testdata/trees.ir.json --out /tmp/trees-native
go run ./cmd/lean-go emit --in testdata/trees.ir.json --out /tmp/trees-depth --native-max-depth 500
```

Metered mode, the original output, is the opt-in for callers who need
resource budgets on untrusted recursive input: pass `--mode metered` to
`generate` or `emit`. Both modes start from the same IR, which does not
depend on the mode. `--native-max-depth N` (`emit` only) sets the recursion
guard described below.

The legacy flag form `lean-go -in FILE -out DIR [-mode native]` keeps metered
as its default so that existing callers are unchanged. `lean-go regenerate` names the mode of every
tracked artifact explicitly (see [`internal/artifacts`](../internal/artifacts)).

The emitter is [`internal/nativegen`](../internal/nativegen). It reads only
the IR's types, functions, code, API record names and `moduleir.RootNames`,
after `moduleir.Validate`.

## Mapping

| Lean | Go | Example |
| --- | --- | --- |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | `uint8` … `uint64` | `func Clamp(v1 uint32, v2 uint32, v3 uint32) uint32` |
| `Bool` | `bool` | |
| structure, or any one-constructor type with fields that is not recursive | struct with exported fields named from the source record, or `Field0`… when the IR has no record names | `type State struct { Low uint32; High uint32 }` |
| other non-recursive inductive type (enumerations included) | value struct with unexported tag and fields; constructor functions; `Is`/`As` methods; enumerations also get `String` | `func ColorRed() Color`, `func (v ParseResult) AsParsed() (Header, bool)` |
| recursive inductive type | immutable handle `struct{ p *node }` to a shared node; constructor functions; `Is`/`As` methods | `func TreeBranch(a0 Tree, a1 Tree) Tree` |
| non-recursive function | function | `func Reserve(v1 State, v2 uint64) Outcome` |
| recursive function, or one calling a recursive function | function returning `(T, error)` | `func Mirror(v1 Tree) (Tree, error)` |
| byte fold root `def f (s : T) (xs : List UInt8) := xs.foldl step s` | loop over a borrowed `[]byte` | `func Update(state State, data []byte) State` |

The generated Adler-32 package is the whole of this:

```go
// Checksum is the Lean function Adler32.checksum.
func Checksum(v1 State) uint32 {
	return (v1.High << 16) | v1.Low
}

// reduce implements the Lean function Adler32.reduce.
func reduce(v1 uint32) uint32 {
	if v1 < uint32(65521) {
		return v1
	}
	return v1 - uint32(65521)
}

// step implements the Lean function Adler32.step.
func step(v1 State, v2 uint8) State {
	v7 := reduce(v1.Low + uint32(v2))
	return State{Low: v7, High: reduce(v1.High + v7)}
}

// Update is the Lean function Adler32.update, a left fold of Adler32.step over the bytes
// of data in order. data is only read during the call; it is neither retained
// nor modified, and its length is not limited.
func Update(state State, data []byte) State {
	for _, b := range data {
		state = step(state, b)
	}
	return state
}
```

plus `Initial`, the `State` struct, `MaxDepth` and `ErrDepth`. Roots are
exported; helper functions are unexported. Pure bindings read once,
constants and field projections are written inline, so small helpers stay
inside Go's inlining budget. Constants are folded with Lean semantics at
generation time: Go rejects constant expressions that overflow or divide by
zero. Local joins become a block, a label and `goto`s with the join
parameters as variables, without closures or allocation. Parameters are
named `v1`, `v2`, … because the IR does not carry source parameter names.

### Names

Exported names derive from the last component of the Lean name, with
characters other than letters, digits and `_` replaced by `_`, and an `X`
prefix when the first letter has no upper case. They are allocated in a fixed
order (types in IR order, constructor functions by Lean constructor name,
roots in root order), and a later name that collides gets a numeric suffix:
`Mirror`, then `Mirror2`. Unexported helper names avoid Go keywords,
predeclared identifiers and the local names `vN`, `nN`, `jN`, `d` and `err`.
Every doc comment carries the full Lean name. The manifest records every
mapping.

### Manifest

`manifest.json` has `"schema": "lean-go-native-manifest/v1"`, `"mode":
"native"`, the generator and validator source digests, the IR digest,
`max_depth`, the program, the SHA-256 of each generated file, the
primitives used, and `go_api`: for each Lean type its Go name, kind, zero
constructor, fields or constructor functions and methods; for each root its
Go name, whether it returns an error, and its byte-fold step.

## Guarantees

**Safety invariant.** A generated function never panics and never returns a
wrong answer for any argument a caller can construct without `unsafe` or
reflection. Native mode makes invalid values unrepresentable instead of
checking them:

- *Scalars and `Bool`*: every Go value is a Lean value.
- *Records*: every field value is valid, so every struct is.
- *Sums and enumerations*: the tag and fields are unexported. Values come
  only from constructor functions, other generated values and the zero
  value, so an out-of-range tag cannot be built. `reflect` cannot set
  unexported fields.
- *Recursive types*: the node pointer is unexported and nodes are immutable,
  so values cannot be mutated or made cyclic.
- *Zero values*: every Go zero value denotes a specific Lean value, stated in
  the type's doc comment and the manifest. A record's zero value has zero
  fields. A sum's zero value, and a nil handle, denote the first constructor
  whose fields' zero values do not lead back to the type (for example
  `Trees.Tree.leaf Trees.Color.red`, or `List.nil` even when `List.nil` is not
  tag 0). Internal tags are renumbered so the zero constructor has tag 0. The
  emitter rejects a type with no finite value.

Functions are therefore total, except for the depth guard.

**Depth guard.** Recursion over values is plain Go recursion. Every function
on a recursive call chain takes the current depth; a self-recursive function
returns `ErrDepth` when a chain would exceed `MaxDepth` nested calls (exactly
`MaxDepth` succeed). The guard is fixed at generation time
(`nativegen.Options.MaxDepth`, `-native-max-depth`; default 10,000, at most
2^24). Building an arbitrarily deep value is safe; only recursion over it is
bounded. Non-recursive call chains are bounded by the number of functions.

**Primitives.** Arithmetic wraps modulo 2^width; shift counts are reduced
modulo the width before Go shifts; `x / 0 = 0` and `x % 0 = x`, guarded
because Go would panic; comparisons are unsigned; widening preserves the
value and narrowing reduces it modulo 2^target. The emitter implements the
whole unsigned matrix named as in `producer/LeanGo/Module/Primitives.lean`:
`add sub mul div mod land lor xor complement shiftLeft shiftRight decEq decLt
decLe` for all four widths and all twelve `toUIntN` conversions, 68
primitives, although the validator admits only part of it today.

**Byte folds.** The `[]byte` argument has no length cap. It is read once in
order, neither retained nor modified, and a scalar or small-record
accumulator causes zero heap allocations. When nothing else uses
`List UInt8`, the list type and the recursive IR function are not emitted. A
fold whose step can fail returns `(T, error)`.

**Output.** Deterministic, gofmt-clean, `go vet`-clean, standard library only
(`errors`), builds with `CGO_ENABLED=0`.

## Proof evidence

`lean-go check-proofs` binds evidence to a native package as strictly as to a
metered one. The manifest must have the native schema, `"mode": "native"`
and the current `generator`; its `generated_files` lists the package's Go
files (plain lower-case non-test names), and the directory must hold exactly
the manifest and those files, each with its recorded SHA-256. A missing,
extra or changed file is rejected. Program, roots, Lean pin and runtime source
hashes are checked as for metered packages. Native evidence records
`"generated_mode": "native"`, as does each native component of a composition;
metered evidence is unchanged. The
[clamp example](../examples/proofs/clamp) tracks both packages:

```sh
cd examples/proofs/clamp
lean-go generate --source-dir source --module Clamp --root Clamp.clamp \
  --out clampnative --ir-output clamp.ir.json
lean-go check-proofs --contract proof-contract.json \
  --manifest clampnative/manifest.json --evidence native-proof-evidence.json
```

The three [wire-format examples](wire-formats.md) are native too.

## Differences from metered mode

| | Metered | Native |
| --- | --- | --- |
| Values | boxed immutable nodes; zero wrapper is invalid | Go values; every value, zero included, is valid |
| Calls | explicit-stack state machine, or native code for roots without recursion when the limits cover their static bound; `(T, error)` everywhere | Go calls; `error` only on recursive chains |
| Budgets | seven limits per call | recursion depth only |
| Byte folds | at most 4,095 bytes per call | unlimited |
| Raw API | `Value`, `Input`, `Import`, `RunRoot` | none |
| Equality | values compared through accessors | records and sums are comparable with `==`; recursive handles compare by identity, not structure |

Native mode does not bound memory or time: a caller passing a huge slice or
a huge tree pays for it. Use metered mode where inputs need resource limits.

## Evidence

`go test ./internal/nativegen` (skipped with `-short`) does the following:

- **Differential tests.** Every tracked IR file (`testdata/*.ir.json` and
  `examples/proofs/*/*.ir.json`, 19 programs) is
  emitted in both modes into a fresh module. A generated driver compares every
  root on 1,500 seeded random inputs, exhaustive boundary products for
  scalar roots of up to three arguments, the zero value of every argument
  type, and 40 recursive values with spines up to 1,537 deep. Byte folds are
  compared at lengths 0, 1, 255, 256, 257, 4,095, 4,096 and 1 MiB, with
  random data from the zero state and three random states, and with all-0xff
  data; the metered side is chunked. Variants with every constructor list
  reversed (zero constructors away from tag 0, and `List UInt8` as an exported
  recursive type) and with deliberately colliding names run through the same
  comparison. The metered side of the comparison is generated without the
  [metered fast path](research/extraction-modules.md#metered-fast-path)
  (`modulegen.GenerateStateMachine`): the fast path runs this emitter's own
  code, so only the state machine is independent of the native side. The
  default metered output is generated next to it and benchmarked as
  "Metered"; for Numbers and Quota the benchmarks also report the state
  machine as "MeteredStateMachine".
- **Independent checks.** Native Adler-32 agrees with `hash/adler32`,
  including arbitrary chunking up to 1 MiB, allocates nothing, and leaves its
  input unchanged. Native CRC-16/XMODEM agrees with an independent
  table-driven implementation from the polynomial (check value `0x31C3`) on
  inputs up to 1 MiB and on every 16-bit state.
- **Fuzzing.** `FuzzAdler32` (fold) and `FuzzMirror` (recursive ADT, including
  zero handles) assert no panic and agreement with the metered package. A
  30-second run each found no failure (386,873 and 74,862 executions):
  `LEAN_GO_NATIVE_FUZZ=30s go test -run TestDifferential ./internal/nativegen`.
- **Hostile inputs.** Every generated sum and recursive type exposes no
  settable field; every zero value is the documented constructor and every
  root accepts all-zero arguments; on constructed and zero values exactly
  one `Is` method holds, the matching `As` returns true and every other `As`
  returns zero values and false; with `MaxDepth = 64`, chains of 64 calls
  succeed and 65 fail with `ErrDepth`; a million-level tree yields `ErrDepth`
  rather than a stack overflow.
- **Primitives.** Every primitive is compiled through the emitter's own
  templates and compared with a `math/big` model of the Lean definition:
  287,300 variable applications, 147,516 constant-operand applications and
  folds, including division and remainder by zero and shift counts at and
  above the width.
- **Determinism and output.** Two emissions are byte-identical; all packages
  pass `go vet`, `gofmt -l` and depend only on the standard library.

## Benchmarks

Medians of five runs, `-cpu 1`, `-benchtime 300ms`, Go 1.27.4,
`CGO_ENABLED=0`, Linux/amd64, a shared 4-core Intel Xeon @ 2.10GHz. Other
work ran concurrently, so ratios are more meaningful than absolute times.
"Handwritten" is a line-for-line Go transliteration of the Lean source; for
Numbers, Quota and Trees it is the handwritten code in
[`benchmarks/`](../benchmarks). Metered folds are chunked at 4,095 bytes
(Adler-32) and 256 bytes (CRC-16). Raw samples:
[native-mode-bench.txt](research/data/native-mode-bench.txt).

| Workload | Native | Handwritten | Metered | Native / handwritten | Metered / native |
| --- | ---: | ---: | ---: | ---: | ---: |
| Adler-32, 64 KiB | 114.8 µs (571 MB/s) | 124.7 µs | 1,200 µs (55 MB/s) | 0.92 | 10.5 |
| `hash/adler32`, 64 KiB | | 30.9 µs | | | |
| CRC-16, 64 KiB | 633.7 µs (103 MB/s) | 638.2 µs | 5,416 µs | 0.99 | 8.5 |
| Numbers | 11.4 ns | 12.0 ns | 1,193 ns | 0.95 | 105 |
| Quota reserve | 1.59 ns | 1.81 ns | 434.5 ns | 0.88 | 273 |
| Quota release | 1.41 ns | 1.48 ns | 401.5 ns | 0.95 | 285 |
| Quota remaining | 1.00 ns | 0.95 ns | 192.5 ns | 1.06 | 193 |
| Tree, balanced depth 7 | 7,559 ns | 7,445 ns | 53,759 ns | 1.02 | 7.1 |
| Tree, skewed 64 | 4,994 ns | 5,042 ns | 39,824 ns | 0.99 | 8.0 |

Native folds, Numbers and Quota allocate nothing. Native trees allocate
exactly as much as the handwritten version (6,120 B in 255 allocations for
the balanced tree); metered uses 32,488 B in 608 allocations. Every native
workload is within 1.1x of handwritten Go, inside the 1.5x target. The
remaining gap to `hash/adler32` is algorithmic: the standard library defers
the modulo over blocks of 5,552 bytes and unrolls the loop, whereas the Lean
source reduces after every byte.

The `lean-go-nativegen/v2` emitter produces byte-identical Go for all of
these programs (its one change, `As` methods reading the receiver in place,
touches only sums whose constructors carry fields). Re-measured back to back
against v1 in one session, every native workload stayed within noise:
Numbers 11.0 ns (v1 11.2), Quota reserve 1.58 ns (1.58), Tree balanced
6,876 ns (6,898), Adler-32 108.8 µs (107.1), with the same allocations.

### Metered fast path

The metered column above is `lean-go-modulegen/v19`. Since v20 a metered
root without recursion or recursive types runs native code when the limits
cover its static worst case
([metered fast path](research/extraction-modules.md#metered-fast-path)), as
Numbers and Quota do with the default limits. The harness builds the metered
package twice from the same IR, as `--mode metered` emits it and without the
fast path (`modulegen.GenerateStateMachine`, which is what v19 emitted), and
benchmarks both in one binary. Medians of five on an idle machine (raw
samples:
[metered-fastpath-bench.txt](research/data/metered-fastpath-bench.txt)):

| Workload | Without the fast path | Metered (v20) | Native (same run) | Metered / native |
| --- | ---: | ---: | ---: | ---: |
| Numbers | 1,305 ns (22 allocations) | 372.0 ns (1 allocation) | 10.8 ns | 35 |
| Quota reserve | 397.0 ns (9) | 172.1 ns (1) | 1.69 ns | 102 |
| Quota release | 370.5 ns (8) | 180.0 ns (1) | 1.24 ns | 145 |
| Quota remaining | 155.0 ns (2) | 11.5 ns (0) | 0.79 ns | 15 |

What is left is the conversion at the boundary: the arguments are read
from their nodes, and a record or sum result is rebuilt as fresh nodes in
one allocation (two when its size depends on the constructor); a scalar
result needs none. Byte folds and Trees are not eligible and run the code
they ran before; in the same run metered measured Adler-32 1,224 µs, CRC-16
4,975 µs, Tree balanced 48,507 ns and Tree skewed 27,533 ns, 6.4 to 12.1
times the native figures of that run.

### Records and sums

Workloads dominated by records of more than four fields are further from
hand-written Go. Go keeps such values in memory, not in registers, so every
by-value argument, result and receiver is a block copy, and a copy of a
value that was just written field by field stalls on store-to-load
forwarding. The extracted code itself is cheap; the cost is at the
boundaries of the public, by-value API. Parse plus serialize of the 12-byte
DNS header through the generated adapter takes 106.6 ns against 31.0 ns for
a hand-written `encoding/binary` codec (3.4 times; 5.1 times with v1); see
[wire formats](wire-formats.md#native-against-metered) for UDP, the demo
header and the profile.

Reproduce (prints the samples; parity tests run first):

```sh
LEAN_GO_NATIVE_BENCH=. go test -count=1 -v -run TestDifferential ./internal/nativegen
```

## Limits

- No bound on memory or time other than the recursion depth guard.
- `==` on recursive handles, or on values containing them, compares identity.
- Byte folds are recognized only for roots of the exact `foldl` shape.
- A type with no finite value is rejected rather than given an error result.
- The installed-CLI demo checks (`go run ./cmd/lean-go-dev installed-cli`)
  reproduce the tracked metered demo packages only; the native clamp package
  is checked by `lean-go regenerate` and the demo check of
  [compiler-compatibility.md](compiler-compatibility.md).
- Native output is tested and trusted, like metered output; there is no
  proof that the Go code implements the Lean source.
