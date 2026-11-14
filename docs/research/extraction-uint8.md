# Scalar UInt8 extraction and the proved byte codec

The [alpha.8 candidate](../releases/v0.1.0-alpha.8.md), Python package `0.1.0a8`,
adds scalar `UInt8`, exact UInt8/UInt32 casts and the
[proved byte codec](../../examples/proofs/bytes) to the pure monomorphic module
extractor. Before publication, use the CLI from this same checkout for current
manifests and proof evidence. All current module exports use
`lean-go-modulegen/v10`. Published alpha.7 has generator v9 and no UInt8 support.
Historical release and research reports describe their original revisions.

The producer remains pinned to Lean **4.36.0-rc2**, commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`. Use Go **1.27.4** with
`CGO_ENABLED=0` and Python **3.15** through uv. Runtime Python dependencies use
only the standard library; generated Go consumers need only Go and its standard
library. Broad generic collections remain deferred.

## Exact runtime capability

Admission adds fourteen exact leaves, with their audited pinned externs and
source signatures. It does not trust a namespace or arbitrary extern.

| Leaves | Signature | Semantics |
| --- | --- | --- |
| `UInt8.add`, `UInt8.sub`, `UInt8.mul` | `UInt8 → UInt8 → UInt8` | Arithmetic modulo 256 |
| `UInt8.decEq`, `UInt8.decLt`, `UInt8.decLe` | Two byte operands; lowered result `Bool` | Equality and unsigned comparisons |
| `UInt8.land`, `UInt8.lor`, `UInt8.xor` | `UInt8 → UInt8 → UInt8` | Eight-bit AND, OR and XOR |
| `UInt8.complement` | `UInt8 → UInt8` | Eight-bit complement |
| `UInt8.shiftLeft`, `UInt8.shiftRight` | `UInt8 → UInt8 → UInt8` | Shift count modulo 8; left shift wraps, right shift is logical |
| `UInt8.toUInt32` | `UInt8 → UInt32` | Zero extension, preserving the value |
| `UInt32.toUInt8` | `UInt32 → UInt8` | Value modulo 256 |

The comparison source constants have Lean's dependent `Decidable` forms;
ordinary `decide` and comparison notation lower to the audited Boolean leaves.
Literals and ordinary arithmetic, bitwise and shift notation are supported.
Existing UInt32/UInt64 operations and their two casts remain available. Direct
UInt8/UInt64 casts, UInt16, runtime division/remainder, `UInt8.toNat`, unsupported
aliases, lookalike names and boxed leaf names remain rejected. Proof modules may
use natural-number and bit-vector helpers independently of runtime admission.

Typed Go uses `uint8`. Raw input uses `Type: "UInt8"` and the `UInt8` payload,
with no constructor or fields and zero inactive UInt32/UInt64 payloads.
`Value.UInt8()` returns `(uint8, bool)`. Other types reject nonzero inactive byte
payloads. Import copies mutable inputs into immutable owned nodes; output field
slices are copies. Invalid zero wrappers, limits and no-partial-result error
behavior follow the existing runtime contract. Each scalar primitive allocates
one result node.

## Selective versions

| Strongest executable capability | Single-root IR / manifest / profile | Multiple-root IR / manifest / profile |
| --- | --- | --- |
| UInt32/UInt64 arithmetic and comparisons | v6 / v6 / `pure-module/v4` | v7 / v7 / `pure-module/v5` |
| UInt32/UInt64 bitwise operations | v8 / v8 / `pure-module/v6` | v9 / v9 / `pure-module/v7` |
| UInt32/UInt64 width conversions | v10 / v10 / `pure-module/v8` | v11 / v11 / `pure-module/v9` |
| UInt8 values or UInt8/UInt32 casts | v12 / v12 / `pure-module/v10` | v13 / v13 / `pure-module/v11` |

Byte capability is selected by surviving executable types and nodes: parameters,
results, record fields, local bindings, joins, case bindings, literals and exact
primitive leaves. An identity function on UInt8 therefore needs the byte schema.
Source-only provenance and erased byte operations are audited but do not by
themselves activate a new schema. Selection traverses continuations, joins and
case branches. Unknown primitives are rejected rather than admitted by a name
prefix.

Earlier schemas keep their restrictions and reject byte types, literals,
primitives and payloads. Single-root `root` and multiple-root `roots` fields
retain their existing exact forms. All current module exports use generator v10;
the regenerated Clamp, Flags and Words manifests and proof receipts change with
producer/generator hashes even where their IR schemas stay unchanged. Pair the
CLI and artifacts from the same checkout or, after publication, the same alpha.8
tag. Published alpha.7's CLI cannot reproduce the candidate's generator v10 bytes.

## Source audit boundary

The producer captures pristine numeric dependency closures before importing user
code and checks exact source origins, definitions, signatures, externs, safety,
axioms, implementations, compiler rewrites and initializer attributes. The byte
leaves and their standard operator instances use the same history checks as the
existing widths. Dead and erased uses still pass through source auditing.
Logical helpers in a primitive's dependency closure are not independently
admitted runtime leaves.

Focused mutation tests cover all fourteen leaves: 84 monomorphic declaration
corruptions, 84 source-attribute corruptions and 28 safety/axiom corruptions.
Additional cases cover operator instances, pristine closure helpers and
local/scoped/late rewrites, preserving existing output on rejection. This extends
the exact audited catalog without broadening the source or proof axiom allowlists.
Lean elaboration is trusted and can execute macros or initializers; audit and
workspace integrity checks do not sandbox hostile source.

## Concrete proved library

The [ByteCodec runtime](../../examples/proofs/bytes/source/ByteCodec.lean) has four
roots, `ByteCodec.encodeLE`, `ByteCodec.decodeLE`, `ByteCodec.encodeBE` and
`ByteCodec.decodeBE`, and a global
`ByteQuad` record containing `b0` through `b3 : UInt8`. The runtime uses concrete
records, byte/word casts, UInt32 shifts and OR, with no Lean arrays or generic
collections. Its Go application reads the record into `[4]byte` and prints both
orders for `0x12345678`.

The [proof contract](../../examples/proofs/bytes/proof-contract.json) checks twelve
universal source claims against independently authored expected propositions:
four inverse laws and eight arithmetic byte positions. Position expectations use
natural-number divisors 1, 256, 65536 and 16777216 followed by `% 256`, reversed
for big endian. Evidence records `propext` for positions and `propext` plus
`Quot.sound` for inverses. The four-root bundle uses v13 IR/manifest and
`pure-module/v11`.

Independent Go tests compare typed and raw roots against `encoding/binary` over
652 cases and check ownership, malformed inputs, invalid wrappers and budgets.
Encoder thresholds are 11 allocations / 12 steps; decoder thresholds are 13
allocations / 15 steps. The [application walkthrough](../articles/proved-byte-codec.md)
contains the runnable handoff, exact output, reproduction commands and assurance
boundary.

## Reproduce focused validation

From the checkout root, refresh the installed CLI and run the fresh demo check:

```sh
uv sync --locked --reinstall-package lean-go-tools
uv run --locked lean-go doctor
uv run --locked python scripts/check_bytes_demo.py --cli "$(pwd)/.venv/bin/lean-go"
```

Use `.venv/Scripts/lean-go.exe` on Windows. This compares exact generated
artifacts, freshly checks twelve proofs and runs a Go-only deployment. The full
installed-wheel smoke runs Clamp, Flags, Words and Bytes for both direct and
sdist-rebuilt wheels: 30 freshly checked source claims per wheel.

Focused engine and source-admission checks are separate from the codec proofs:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./internal/moduleir ./internal/modulegen
LEAN_GO_MODULES=1 uv run --locked python scripts/test_uint8.py
uv run --locked python scripts/check_bytes.py --artifacts /tmp/byte-evidence-new
```

The campaign's output directory must be new. It retains source, inputs, tool
identities, commands, native/extracted artifacts, compared results and a report.
Across 353 boundary and seeded triples it compares 19 outputs from native Lean,
raw Go and typed Go with an independent Python arithmetic/Boolean model. Four
roots exercise byte operations, branch selection, widening and narrowing.
Allocation/step threshold search records 17/18 for its calculation root. The
separate generated scalar Go tests cover 481 boundary and seeded pairs across
all fourteen new leaves, including malformed payloads and failed resource calls.

The [compatibility corpus](../../compatibility/corpus/manifest.json) has fourteen
cases: eight accepted and six required rejections. The complete same-pin
baseline/candidate qualification passes with the pinned compiler on both sides;
it does not establish support for a later Lean version. The `bytes` case adds an
independent consumer for arithmetic, comparisons, bitwise operations, masked
shifts and the two byte/word casts. See the
[compatibility guide](../compiler-compatibility.md) for explicit installation
paths, evidence and replay commands.

These are bounded tests of the trusted pipeline. The twelve codec theorems prove
the selected Lean statements, without proving the Lean compiler, extraction,
Go emitter/runtime or Go compiler correct. Resource limits may reject an
otherwise total source call. No compiler-correctness or universal Go-correctness
claim follows from the finite campaigns.
