# Program-level differential testing

The campaign generates new small Lean source programs and compares an
independent source-AST model with six executions: a compiled native Lean
executable; the extracted metered Go package through its raw (`Value`) and
typed APIs; the same two APIs of a second metered package generated from the
same IR without the fast path, so that they run the explicit-stack state
machine; and the native-mode Go package emitted from the same IR. Go builds
and execution use `CGO_ENABLED=0`. The producer still requires the project's exact Go and Lean
toolchain pins.

The harness is the developer command `cmd/lean-go-dev` (package
`internal/differential`). It is not part of the `lean-go` command users
install; run it from a checkout with `go run`.

## Run a campaign

From the repository root, with the pinned tools on `PATH`:

```sh
go run ./cmd/lean-go-dev differential
go run ./cmd/lean-go-dev differential \
  --seed 2 --seed 3 --seed 4 --width 8 --width 64 --depth 4 --cases 64
```

Defaults are seeds 0 and 1, all four widths (8, 16, 32 and 64), generation depth 3 and 32 input pairs
per program: eight programs and 256 comparisons against each execution channel.
`--seed` and `--width` may be repeated. `--cases` is the total input count, including
the boundary prefix, with a maximum of 4096. The prefix covers zero, maxima,
unsigned high bits, shift counts around the width, zero divisors and UInt64 values above 2^53.
Further pairs use a deterministic seeded random stream. Small `--cases` values
truncate this prefix and therefore do not cover every boundary. A seed names
the same program on every platform: generation uses `math/rand/v2`'s PCG
generator, a specified algorithm.

The campaign builds this checkout's `lean-go` command once. Each candidate then
has a fresh workspace and native executable, a fresh `lean-go generate
--mode metered`, a `lean-go emit --mode native` of the same IR, the
state-machine package generated in process from that IR by
`modulegen.GenerateStateMachine` (deliberately not a command-line option), and
an independent Go consumer module with no external dependencies that imports
all three packages. The consumer binary runs with a `PATH` that contains no producer
tools. `LEAN_GO_CACHE_DIR` and `LEAN_GO_NO_CACHE` select the producer cache as
for `lean-go generate`; other `LEAN_*` and `LAKE_*` variables are removed.
Toolchain probes, commands, stdout, stderr, source, IR, manifests and results
remain in the campaign directory printed at exit. The default parent is the
ignored `.differential/` directory; use `--artifacts DIR` to select another
location. Artifacts include build products locally, so remove old campaign
directories when no longer needed.

## Replay and shrink

```sh
go run ./cmd/lean-go-dev differential \
  --replay .differential/campaign-EXAMPLE/case-0000/failure.json
go run ./cmd/lean-go-dev differential \
  --replay .differential/campaign-EXAMPLE/case-0000/failure.json \
  --shrink --shrink-attempts 20
```

Replace the example path with the printed failure path. Every case stores a
versioned full source AST and exact input pairs. Replay uses those bytes, not a
fresh program generated from the seed, and works with a passing `case.json` too.
`--replay` cannot be combined with `--seed` or `--width`. Reproduction still
requires compatible tooling and the recorded model schema; the artifact does
not freeze every future change to the renderer or compiler.

Why two metered pairs: since `lean-go-modulegen/v20` a metered root without
recursion runs the native emitter's code embedded in the metered package (the
fast path, `internal/modulegen/fastpath.go`) whenever the limits cover its
static bound, in `RunTyped` and in `Run`/`RunRoot` alike. Every generated
program is bounded and the consumer uses `DefaultLimits()`, so `raw` and
`typed` run that code: they are what users get. `raw_state_machine` and
`typed_state_machine` make the same calls with the same limits on the package
without the fast path, so the explicit-stack state machine stays compared with
native Lean and the model.

The campaign stops with a nonzero exit on the first mismatch, rejected program,
build failure, timeout or malformed result. Errors remain separate from semantic
mismatches. A mismatch records which of native Lean (`native`), raw Go (`raw`),
typed Go (`typed`), the state machine's raw and typed APIs
(`raw_state_machine`, `typed_state_machine`) and native-mode Go (`native_go`)
differ from the independent model, in that order, and their failing input indexes. UInt64 values remain exact integers
throughout; booleans, fractions, exponents, signs, missing values and
out-of-range results are protocol errors.

`--shrink` tries smaller input sets, smaller unsigned inputs and type-valid source
subexpressions. It preserves exactly the original set of failing execution
channels. Rejected programs, timeouts and malformed output do not count as
preserved semantic failures. Reduction is deterministic and bounded by
`--shrink-attempts` (0..1000); each trial may rebuild a program. The saved
`minimized.json` and `Minimized.lean` describe the best accepted candidate within
that budget, not a globally minimal counterexample. Original artifacts remain.

## Artifacts

A campaign directory `campaign-XXXX/` holds:

| File | Content |
| --- | --- |
| `report.json` | Schema `lean-go-differential-campaign/v1`: `results` (one object per completed program, as printed), `elapsed_seconds`, `planned_programs` and `completed_programs` |
| `case-NNNN/case.json` | The program and inputs (schema `lean-go-differential-case/v1`); for a passing program also its observation |
| `case-NNNN/Generated.lean` | The rendered source |
| `case-NNNN/failure.json` | A mismatch with its observation, or an error with its message |
| `case-NNNN/minimized.json`, `Minimized.lean` | The reduced counterexample (`--shrink` only) |
| `backend/lean-go` | The `lean-go` command built from the checkout |
| `backend/check-*/` | One build per tried candidate: `commands.jsonl`, `toolchain.json`, `inputs.json`, `results.json`, `program.ir.json`, `source/`, `native/` (Lake project) and `consumer/` (with the `extracted/`, `statemachine/` and `native/` packages) |

A case file is one JSON object with exactly the keys `schema`, `program` and
`inputs`, plus optional `observation` and `error`:

```json
{
  "schema": "lean-go-differential-case/v1",
  "program": {
    "schema": "lean-go-differential/v1", "seed": 0, "width": 32,
    "expr": {"op": "add", "args": [{"op": "a", "args": [], "value": 0},
                                   {"op": "lit", "args": [], "value": 4294967296}], "value": 0},
    "helper": {"op": "a", "args": [], "value": 0}
  },
  "inputs": [[0, 0], [4294967295, 1]],
  "observation": {"expected": [0, 4294967295],
                  "outputs": {"native": [0, 4294967295], "raw": [1, 0], "typed": [0, 4294967295],
                              "raw_state_machine": [0, 4294967295], "typed_state_machine": [0, 4294967295],
                              "native_go": [0, 4294967295]},
                  "mismatches": {"native": [], "raw": [0, 1], "typed": [],
                                 "raw_state_machine": [], "typed_state_machine": [], "native_go": []}}
}
```

Each expression has exactly `op`, `args` and `value`; `value` is the literal
payload of `lit` (0..2^65-1, so literals may exceed the width and wrap) and 0
elsewhere. Inputs are pairs of exact integers below 2^width. Replay reads only
`program` and `inputs`: the observation and error are evidence, not input.
Files above 2 MB, unknown keys, other schemas, and programs beyond the model
limits are rejected before anything runs. Each `commands.jsonl` line records
the stage, argv, working directory, timeout, a fixed allowlist of environment
values, exit status, stdout, stderr and elapsed time; the in-process
`state-machine-emit` stage has argv `["(in process)",
"modulegen.GenerateStateMachine", IR, output]` and no output streams.

## Grammar and implementation boundaries

`internal/differential/model.go` owns immutable expressions, validation,
generation, rendering and the independent evaluator; `serialize.go` the JSON
form and `cases.go` comparison, case files and reduction. These three files
import only the standard library, which a test enforces: they use neither
compiler IR nor the extraction implementation, and nothing shared with
`internal/modulegen` or `internal/nativegen`. Numeric semantics use
arbitrary-precision `math/big` integers with explicit reduction modulo 2^8,
2^16, 2^32 or 2^64, Lean's `x / 0 = 0` and `x % 0 = x`, shift counts modulo
the selected width, and per-bit arithmetic (not Go's bit operators) for AND,
OR and XOR.

Programs have two scalar inputs and a scalar result. The alpha.6 bitwise
extension adds AND, OR, XOR, unary complement and logical right shift at both
widths; serialized ASTs preserve unary arity for replay. Expressions combine numeric
literals, addition, subtraction, multiplication, division, remainder, left shift, equality and unsigned
ordering, conditionals, direct helpers, single-field wrappers and nested record
construction/updates. A generated helper consumes both arguments. Fixed
`noinline` wrapper/record accessors preserve structure boundaries through the
compiler, and branch results feed later computations. Each program uses one
numeric width. This exercises internal record composition; typed record getters
remain covered by the existing dedicated suite.

The source model is bounded to 2048 nodes and depth 24 on replay, with generation
depth limited to 0..4. It excludes recursion, effects, escaping closures,
polymorphism, generic collections and multi-root exports. A compiler rejection
is a campaign failure to investigate, even if the source model is well typed.
The renderer uses ordinary `<<<` notation. Its five exact source wrappers are
audited against the pinned compiler and lower to the existing `UInt32.shiftLeft`
and `UInt64.shiftLeft` leaves. Left shift alone retains v6/v7 IR with
`pure-module/v4` or `pure-module/v5`; it does not activate bitwise v8/v9.
The focused `test_shift_left.py` suite compares notation and direct calls against
native Lean, raw Go, typed Go and the arithmetic oracle at boundary and seeded inputs.

`internal/differential/backend.go` builds the native reference with Lake, the
extracted packages with `lean-go` and the consumer with Go. IO reference code
stays outside extraction sources. `campaign.go` runs a campaign. Backend and
reducer unit tests inject wrong results and execution failures to establish
that detection, artifact capture, replay and shrinking work even when the real
compiler agrees.

Two fixed campaigns compare a tracked corpus source on fixed samples with
native Lean, the raw and typed Go APIs and an arithmetic model, and record the
smallest resource budgets that succeed:

```sh
go run ./cmd/lean-go-dev widths --artifacts /tmp/width-evidence   # 206 UInt32/UInt64 conversion rows
go run ./cmd/lean-go-dev bytes --artifacts /tmp/byte-evidence     # 353 UInt8 rows
```

The thresholds of both campaigns equal the fast path's static bound of each
root: below it the state machine fails with `ErrResource`, at it the fast path
answers, and every row runs at `DefaultLimits()` and so on the fast path too.
Each campaign therefore runs its consumer twice, against the metered package
and against the state-machine package generated in process from the same IR.
Both runs must match the model on every row, and the state machine's
thresholds (`state_machine_budgets` in `report.json`) must equal the metered
ones (`budgets`); `results.json` holds the rows of every run.

## CI and assurance

The Linux extraction job runs the eight-program default corpus with up to 12
shrink trials on a semantic failure. It retains selected source, JSON evidence,
command logs and generated Go as a `differential-evidence` artifact for 14 days;
native binaries and build caches are excluded. The existing Linux and Windows
Go gates continue to exercise tracked generated packages. The Lean-gated tests
(native smoke; compiled fault injection with replay, once in the consumer's raw
channel and once in the state machine's emitted additions, which only the two
state-machine channels may report; and the width and byte campaigns) run in CI's `go test ./...` with `LEAN_GO_DIFFERENTIAL=1`; locally:

```sh
LEAN_GO_DIFFERENTIAL=1 go test -count=1 -p 1 ./internal/differential/
```

Native Lean and the extractor share compiler transformations. Agreement between
them is useful evidence, not independence; the source-AST evaluator provides the
separate semantics model. The model and renderer can also contain bugs. This
bounded corpus strengthens tested, trusted extraction without proving compiler
correctness or covering all accepted Lean programs. Broad generic collections
remain outside this milestone.

`--timeout` bounds each external command, with a default of 300 seconds. On
POSIX the runner (`internal/devexec`) kills the command's whole process group on
timeout, including children of Lake and the extractor. Elsewhere it terminates
only the direct child; descendant cleanup and hard timeout bounds are not
guaranteed there. Native campaign CI currently runs on Linux.
