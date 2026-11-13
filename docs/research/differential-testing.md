# Program-level differential testing

The campaign generates new small Lean source programs and compares four results:
an independent Python source-AST model, a compiled native Lean executable, and
the extracted Go package through its raw and typed APIs. Go builds and execution
use `CGO_ENABLED=0`; Python tooling uses Python 3.15 through `uv`. The producer
still requires the project's exact Go and Lean toolchain pins.

## Run a campaign

From the repository root, with the pinned tools on `PATH`:

```sh
uv run --locked python scripts/differential.py
uv run --locked python scripts/differential.py \
  --seed 2 --seed 3 --seed 4 --width 32 --width 64 --depth 4 --cases 64
```

Defaults are seeds 0 and 1, both widths, generation depth 3 and 32 input pairs
per program: four programs and 128 comparisons against each execution channel.
`--seed` and `--width` may be repeated. `--cases` is the total input count, including
the boundary prefix, with a maximum of 4096. The prefix covers zero, maxima,
unsigned high bits, shift counts around the width and UInt64 values above 2^53.
Further pairs use a deterministic seeded random stream. Small `--cases` values
truncate this prefix and therefore do not cover every boundary.

Each candidate has a fresh workspace and native executable, fresh extraction,
and an independent Go consumer module with no external dependencies. The consumer
binary runs with a `PATH` that contains no producer tools. Toolchain probes,
commands, stdout, stderr, source, IR, manifest and results remain in the campaign
directory printed at exit. The default parent is the ignored `.differential/`
directory; use `--artifacts DIR` to select another location. Artifacts include
build products locally, so remove old campaign directories when no longer needed.

## Replay and shrink

```sh
uv run --locked python scripts/differential.py \
  --replay .differential/campaign-EXAMPLE/case-0000/failure.json
uv run --locked python scripts/differential.py \
  --replay .differential/campaign-EXAMPLE/case-0000/failure.json \
  --shrink --shrink-attempts 20
```

Replace the example path with the printed failure path. Every case stores a
versioned full source AST and exact input pairs. Replay uses those bytes, not a
fresh program generated from the seed, and works with a passing `case.json` too.
Reproduction still requires compatible tooling and the recorded model schema;
the artifact does not freeze every future change to the renderer or compiler.

The campaign stops with a nonzero exit on the first mismatch, rejected program,
build failure, timeout or malformed result. Errors remain separate from semantic
mismatches. A mismatch records which of native Lean, raw Go and typed Go differ
from the independent model, and their failing input indexes. UInt64 values remain
exact integers throughout; booleans, floating-point numbers, missing values and
out-of-range results are protocol errors.

`--shrink` tries smaller input sets, smaller unsigned inputs and type-valid source
subexpressions. It preserves exactly the original set of failing execution
channels. Rejected programs, timeouts and malformed output do not count as
preserved semantic failures. Reduction is deterministic and bounded by
`--shrink-attempts` (0..1000); each trial may rebuild a program. The saved
`minimized.json` and `Minimized.lean` describe the best accepted candidate within
that budget, not a globally minimal counterexample. Original artifacts remain.

## Grammar and implementation boundaries

`scripts/differential_model.py` owns immutable expressions, validation,
serialization, generation, rendering and the independent evaluator. It imports
neither compiler IR nor extraction implementation. Numeric semantics use Python
arbitrary-precision integers with explicit reduction modulo 2^32 or 2^64 and
shift counts modulo the selected width.

Programs have two scalar inputs and a scalar result. The alpha.6 bitwise
extension adds AND, OR, XOR, unary complement and logical right shift at both
widths; serialized ASTs preserve unary arity for replay. Expressions combine numeric
literals, addition, subtraction, multiplication, left shift, equality and unsigned
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
The renderer uses the already admitted direct shift primitive rather than the
unaudited `HShiftLeft` notation.

`scripts/differential_backend.py` builds the native reference with Lake and the
extracted consumer with Go. IO reference code stays outside extraction sources.
`scripts/differential_cases.py` handles exact result comparison, replay data and
reduction; `scripts/differential.py` is the campaign CLI. Backend and reducer unit
tests inject wrong results and execution failures to establish that detection,
artifact capture, replay and shrinking work even when the real compiler agrees.

## CI and assurance

The Linux extraction job runs the four-program default corpus with up to 12
shrink trials on a semantic failure. It retains selected source, JSON evidence,
command logs and generated Go as a `differential-evidence` artifact for 14 days;
native binaries and build caches are excluded. The existing Linux and Windows
Go gates continue to exercise tracked generated packages. Optional local backend
and compiled fault-injection tests (also enabled in CI) can be run with:

```sh
LEAN_GO_DIFFERENTIAL=1 uv run --locked python scripts/test_differential_backend.py
LEAN_GO_DIFFERENTIAL=1 uv run --locked python scripts/test_differential.py
```

Native Lean and the extractor share compiler transformations. Agreement between
them is useful evidence, not independence; the source-AST evaluator provides the
separate semantics model. The model and renderer can also contain bugs. This
bounded corpus strengthens tested, trusted extraction without proving compiler
correctness or covering all accepted Lean programs. Broad generic collections
remain outside this milestone.

`--timeout` bounds each external command, with a default of 300 seconds. On
POSIX the runner terminates the compiler process group on timeout, including
children of Lake and the exporter. The non-POSIX fallback terminates only the
direct child; descendant cleanup and hard timeout bounds are not guaranteed
there. Native campaign CI currently runs on Linux.
