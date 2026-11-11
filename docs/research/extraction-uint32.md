# UInt32 extraction

Ordinary module extraction now emits `lean-go-ir/v3` with `pure-module/v2`.
The Go backend still accepts v2 IR with the original `pure-module/v1`
restrictions. The audited normalizer's v1 implementation is unchanged.
This milestone is local experimental software; no release has been published.

## Use it

Use Go 1.27.4, Lean 4.35.0 at commit
`eb6cafb7290e8bd5f2937522d8d804f58f02bafc`, and Python 3.15 through uv:

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Numbers --root Numbers.compute \
  --out generated/numbers --ir-output testdata/numbers.ir.json
```

Generation preserves and rejects differing existing output. Select fresh output
paths when inspecting a modified producer. The example takes two UInt32 values
and returns a `Numbers.Results` record. `Numbers.scalar` is an alternative root
returning `(a + 1) * (b - 1)` directly. Both use the existing immutable `Import`,
`Run`, and `Value` API described in the [README](../../README.md).

## Exact semantics

| Pure-mono leaf | Result | Go semantics |
| --- | --- | --- |
| UInt32.add | UInt32 | `a + b`, modulo 2^32 |
| UInt32.sub | UInt32 | `a - b`, modulo 2^32 |
| UInt32.mul | UInt32 | `a * b`, modulo 2^32 |
| UInt32.shiftLeft | UInt32 | `a << (b & 31)` |
| UInt32.decEq | Bool | unsigned `a == b` |
| UInt32.decLt | Bool | unsigned `a < b` |
| UInt32.decLe | Bool | unsigned `a <= b` |

Each takes exactly two UInt32 arguments. Ordinary Lean notation is supported
when its compiled form lies in this subset, including `+`, `-`, `*`, `==`, and
`decide (a < b)` or `decide (a ≤ b)`. Lean can optimize multiplication by a power
of two into `shiftLeft`; this is why the shift leaf is part of the catalog.
Dependent source `Decidable` results are erased to Bool by pure-mono compilation.

Literals in IR are canonical decimal strings from 0 through 4294967295 with no
arguments. Lean normalizes source literals first: `(4294967297 : UInt32)` becomes
1. The validator rejects noncanonical or out-of-range IR literals, unknown
primitives, wrong arity/types, mismatched schema/profile pairs, and noncanonical
Bool constructors. Numeric values are rejected under the earlier profile.

Division, remainder, other bit operations, other integer widths, arbitrary Nat
computation, and unsupported compiler forms remain rejected. The ordinary module
data, function and control-flow restrictions still apply.

## Trust boundary

`Primitives.lean` names the seven runtime leaves, their pure-mono signatures,
native extern symbols, and expected axiom sets. The source catalog separately
names notation wrappers, literal conversion and the fixed UInt32 order package;
these do not authorize arbitrary runtime calls. The toolchain commit is checked,
and this catalog is included in the producer input hashes.

The trusted producer captures context-sensitive compiler attributes over the
full dependency closure of each named source boundary before compiling user
sources. That baseline is stored in a persistent environment extension. Source
capture and final export compare declaration origins, extern data,
implemented-by targets, CSimp rules and initialization attributes against it.
This includes computational dependencies hidden behind the standard order
package. Only named boundaries terminate the source audit; being present in a
baseline dependency closure does not make a declaration an admitted boundary.

`UInt32.shiftLeft` has the expected axiom set `[propext]`; the other six runtime
leaves have none. The fixed private UInt32 order package uses
`[propext, Classical.choice, Quot.sound]`. Two order-package projections have
`[propext]`. These are explicit pinned support exceptions, not permission for
arbitrary user axioms or replacement implementations. The previously audited
Boolean rewrite remains available.

The Go emitter uses private `uint32` fields and canonical Bool nodes. Every
literal and primitive result allocates a fresh node and consumes the existing
allocation and control-step budgets. Input ownership and output size/depth
checks are unchanged. No runtime template, C linkage, interpreter or dependency
was added. `lean-go-manifest/v3` records the exact sorted set of used primitives,
their signatures and semantics; generation remains deterministic.

Lean elaboration is trusted local execution, not a sandbox for macros or
initializers. Supplied JSON metadata is not source authentication. Source audit,
compiler passes, Go emission and runtimes remain trusted: the result is tested
extraction, not an end-to-end compiler-correctness proof.

## Validation

The fixture covers all seven leaves, literal normalization, optimized doubling
and a mixed expression. The differential corpus contains 1,193 pairs: the
Cartesian product of 13 boundary values plus 1,024 seeded pairs. A widened Go
arithmetic oracle checks results independently of uint32 overflow. A separately
compiled native Lean executable evaluates the same inputs.

Backend regressions exercise malformed numeric IR, primitive registry mutation,
legacy-profile rejection, canonical Bool, budget exhaustion, invalid partial
results, operand ownership and a deliberately changed arithmetic primitive.
The isolated consumer copies only generated Go files and runs with cgo disabled
and a Go-only PATH. Regeneration compares fresh builds with each other and with
tracked artifacts.

Source admission tests cover local, scoped and late replacements, including a
replacement target hidden in the pinned standard library. A custom Add instance
implemented with subtraction exports subtraction; one calling a foreign
implementation rejects. Unsupported division, even when optimized away, bitwise
operations, UInt64, foreign implementations and real axioms also reject while
preserving prior output.

Independent specification and code-quality reviews passed. Whole-checkout
Ripwire analysis reported 11 gating duplication findings. Review retained the
generated runtime copies because every output package must stand alone, retained
the existing separate test runners for the two backends, and identified a
token-shape match between unrelated tests. Lean source is not indexed by Ripwire;
its review and fresh compiler tests provide the evidence for that part.

Linux/amd64 execution is the acceptance target. Windows cross-compilation alone
does not establish Windows execution. Actual Windows execution and consumer
validation from a tagged release remain separate release gates.

The final checkout passed all native, regeneration and module Go gates, Go vet,
and all 43 Python test methods using Python 3.15.2 and uv 0.13.5. The numeric
admission file alone performs 20 fresh exports: four accepted cases and 16
rejections. Numeric-seeded decoder fuzzing completed 1,582,979 executions without
a failure. Windows/amd64 build and integration-test compilation also passed.

```sh
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build ./...
```
