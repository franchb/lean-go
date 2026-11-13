# Bitwise extraction

Alpha.6 adds exact bitwise operations on `UInt32` and `UInt64`.
It retains alpha.5's production compiler pin: Lean 4.36.0-rc2, commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`, with Go 1.27.4 and Python 3.15.
Generated consumers use the Go standard library with `CGO_ENABLED=0`.

## Semantics and signatures

For each width `w`, the executable registry admits only these five additional
names. Operands and results all have the exact same `UInt32` or `UInt64` type.

| Lean leaf | Ordinary notation | Arity | Generated Go semantics |
| --- | --- | --- | --- |
| `UInt{w}.land` | `a &&& b` | 2 | `a & b` |
| `UInt{w}.lor` | `a ||| b` | 2 | `a | b` |
| `UInt{w}.xor` | `a ^^^ b` | 2 | `a ^ b` |
| `UInt{w}.complement` | `~~~a` | 1 | `^a`, restricted to the operand width |
| `UInt{w}.shiftRight` | `a >>> b` | 2 | `a >> (b & 31)` or `a >> (b & 63)` |

Right shift is logical: high bits are filled with zero. Its count is reduced
modulo the operand width, so shifting by 32 or 64 respectively preserves the
input. Complement is `(2^w - 1) - a`; it never widens the operand. The producer,
IR validator, manifest signature catalog and emitter agree on unary complement.
Unknown names, extra or missing operands, mixed widths and result-type changes
are rejected before emission.

For example, ordinary source can now express:

```lean
def clear (state mask : UInt32) : UInt32 := state &&& ~~~mask
def highBits (state amount : UInt64) : UInt64 := state >>> amount
```

Each primitive still executes through the existing bounded runtime, allocates
one scalar value, and uses the existing step, slot, input and output checks.
Both raw and typed API failures return zero results. The development checkout
adds [two width conversions](extraction-width-conversions.md). Division,
remainder, other numeric widths and arbitrary foreign calls remain unsupported.

## Exact audit boundary

The pinned definitions of `land`, `lor`, `xor`, and `complement` have exactly
`[propext, Quot.sound]`; `shiftRight` has exactly `[propext]`. Their corresponding
concrete `instAndOpUInt32/64`, `instOrOpUInt32/64`, `instXorOpUInt32/64`,
`instComplementUInt32/64`, and `instShiftRightUInt32/64` wrappers have the same
respective axiom sets. The generic bridges and projections have empty sets:
`HAnd.hAnd`, `instHAndOfAndOp`, `AndOp.and`; the corresponding `HOr`, `HXor` and
`HShiftRight` names; and `Complement.complement`.

These are per-name expectations, checked while constructing the pristine
producer baseline. They do not enlarge the axiom allowance for unrelated
declarations. The extern names are exactly `lean_uint32_land`, `lean_uint32_lor`,
`lean_uint32_xor`, `lean_uint32_complement`, `lean_uint32_shift_right`, and their
`uint64` counterparts. Monomorphic parameter/result types, arity, safety and
absence of universe parameters are checked before exporting a runtime leaf.

Notation wrappers are source-only support, not executable IR calls. Their full
dependency closures retain the existing source-origin, extern, `implemented_by`,
compiler-rewrite and initializer checks. Local, scoped, late and erased source
uses are still audited against compilation history and the pristine baseline.
Custom instances follow their actual audited implementation.

## Selective schemas and manifests

| Emitted program | IR | Profile | Manifest | Root field |
| --- | --- | --- | --- | --- |
| Existing operations, single root | v6 | `pure-module/v4` | v6 | `root` |
| Existing operations, multiple roots | v7 | `pure-module/v5` | v7 | `roots` |
| New bitwise leaf, single root | v8 | `pure-module/v6` | v8 | `root` |
| New bitwise leaf, multiple roots | v9 | `pure-module/v7` | v9 | `roots` |

The producer selects v8/v9 for bitwise-only capability by traversing emitted primitive nodes in every
function, join body, continuation and case branch. Source strings, provenance,
erased operations and function names cannot select the new schema. A source-only
bitwise use can therefore retain v6/v7. Existing v2–v7 IR keep their original
primitive restrictions. The v8/v9 schemas inherit UInt64, joins and required
typed API metadata. Single-root and multi-root field shapes remain exact;
multi-root exports still require 2–64 sorted unique roots.

Alpha.6 used generator identity `lean-go-modulegen/v8`; the current
width-conversion development generator uses `lean-go-modulegen/v9`. Manifests list the exact used
primitive subset with signatures and semantics. Proof-contract verification
accepts only the compatible triples in the table, including the contract's
root cardinality, and rejects crossed manifest/IR/profile versions.

## Verification

Focused Go tests cover all ten signatures, invalid unary/binary arities, mixed
widths, unknown names, older schema rejection, v8/v9 root/API contracts, CLI
dispatch, manifest signatures and deterministic output. Generated consumer tests
compare raw and typed calls at zero, maximum, high-bit and alternating-mask
boundaries, with exact unary allocation limits and resource-error checks.

Fresh Lean tests cover direct calls and notation at both widths, selective
single/multi-root schemas, corrupted primitive/wrapper externs, local/scoped/late
compiler replacements, erased uses, unsupported widths/conversions/division/
remainder, forged axioms and preservation of previously published output. The
executable forged-axiom fixture is rejected by Lean code generation before the
extractor runs; its failure is not claimed as an extractor admission rejection.

The native differential test compiles each of the ten leaves independently and
checks 50 input pairs per leaf: 500 comparisons against each of native Lean,
raw Go and typed Go. Inputs include zero, maximum, the high bit, both alternating
masks, and shift counts 0, width−1, width, width+1 and maximum. The independent
source model computes bits with integer division/remainder and computes right
shift with integer division by a power of two. It does not read extraction IR.
Full AST serialization remains replayable, including unary operations.

The compatibility corpus includes an accepted `bitwise` case with both widths,
all ten leaves and an independently authored arithmetic Go consumer, executed
with producer tools absent from its runtime PATH. The complete corpus has 11
cases: five accepted and six expected rejections. The `custom_csimp` case checks
that a valid custom compiler rewrite yields `admission` / `source_audit` with
the source owner and location; genuine Lean build and invalid primitive failures
retain the `build` stage.

Reproduce the focused checks from the current checkout:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 \
  ./internal/moduleir ./internal/modulegen ./cmd/lean-go
CGO_ENABLED=0 GOTOOLCHAIN=local go vet \
  ./internal/moduleir ./internal/modulegen ./cmd/lean-go
LEAN_GO_MODULES=1 LEAN_GO_DIFFERENTIAL=1 \
  uv run --locked python -m unittest discover -s scripts -p test_bitwise.py -v
uv run --locked python scripts/differential.py --artifacts /tmp/bitwise-campaign
```

Set `LEAN_GO_BITWISE_ARTIFACTS` to an external directory to retain each native
test's full AST, inputs, commands, generated package and results for replay.
The focused checks passed on Linux. The expanded random grammar also passed the
default deterministic campaign: seeds 0 and 1 at both widths, 32 inputs per
program. This is tested, trusted extraction, not an end-to-end proof of Go
correctness. The [alpha.6 release notes](../releases/v0.1.0-alpha.6.md) describe
the standalone proved-flags example and the separate release validation gates.
Existing published alpha.5 artifacts remain reproducible from their tag.
