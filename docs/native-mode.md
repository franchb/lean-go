# Native Go mode

Native mode emits plain, unmetered Go from the same validated module IR as
the default metered mode. Lean scalars become Go unsigned integers, Lean
types become ordinary Go types, and Lean functions become ordinary Go
functions. There are no `Limits` parameters, no resource budgets and no
`Value`/`Input` raw API.

```sh
go run ./cmd/lean-go -in testdata/trees.ir.json -out /tmp/trees -mode native
uv run --locked python scripts/generate.py --mode native \
  --source-dir examples/modules --module Trees --root Trees.mirror \
  --out /tmp/trees --ir-output /tmp/trees.ir.json
```

`-mode metered` remains the default. The installed `lean-go generate`
command accepts the same `--mode`. `-native-max-depth N` (Go command only)
sets the recursion guard described below.

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

## Differences from metered mode

| | Metered | Native |
| --- | --- | --- |
| Values | boxed immutable nodes; zero wrapper is invalid | Go values; every value, zero included, is valid |
| Calls | explicit-stack state machine, `(T, error)` everywhere | Go calls; `error` only on recursive chains |
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
  comparison.
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
  root accepts all-zero arguments; with `MaxDepth = 64`, chains of 64 calls
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

Reproduce (prints the samples; parity tests run first):

```sh
LEAN_GO_NATIVE_BENCH=. go test -count=1 -v -run TestDifferential ./internal/nativegen
```

## Limits

- No bound on memory or time other than the recursion depth guard.
- `==` on recursive handles, or on values containing them, compares identity.
- Byte folds are recognized only for roots of the exact `foldl` shape.
- A type with no finite value is rejected rather than given an error result.
- The proof-contract checker and the example verifiers read metered
  manifests; they do not yet accept native manifests.
- Native output is tested and trusted, like metered output; there is no
  proof that the Go code implements the Lean source.
