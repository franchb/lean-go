# UInt32 and UInt64 width conversions

The current development checkout (`0.1.0a7.dev0`) admits exactly two new unary
runtime leaves. The published alpha.6 CLI retains its earlier capability set.

| Lean leaf | Signature | Exact extern | Go expression | Arithmetic meaning |
| --- | --- | --- | --- | --- |
| `UInt32.toUInt64` | `UInt32 → UInt64` | `lean_uint32_to_uint64` | `uint64(a)` | Zero extension; the value is unchanged |
| `UInt64.toUInt32` | `UInt64 → UInt32` | `lean_uint64_to_uint32` | `uint32(a)` | The value modulo 2^32 |

Operand and result use separate scalar payload fields in the existing immutable
raw and typed APIs. Each conversion allocates one result node. Existing resource
limits and zero-result error behavior apply. Other widths, identity casts,
lookalike names, division/remainder runtime primitives and generic collections
remain unsupported. The development checkout also admits ordinary `<<<` notation
for UInt32 and UInt64; the packing example uses this audited notation.

## Audited source boundary

Both safe definitions come from `Init.Data.UInt.BasicAux` in Lean 4.36.0-rc2,
commit `b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`. Their exact signatures have
one operand, no universes and no axioms. They have the exact externs above and
no `implemented_by`, compiler rewrite or initializer.

The existing pristine numeric boundary captures their full logical dependency
closures before user code is imported. That closure includes `UInt32.toNat`,
`UInt64.toNat`, `Nat.toUInt32`, BitVec/Fin operations and proof helpers. These
helpers are not independently admitted runtime primitives. Source origin,
extern, implementation, rewrite and initialization observations are compared
at source compilation and export; local, scoped, late and erased uses keep the
existing history checks. A source-only cast is still audited even when it does
not survive into executable code.

## Exact capability versions

One traversal collects executable primitive nodes across all functions, let
continuations, joins and case branches. Source/provenance references, ordinary
call names, dead casts and erased casts cannot activate a capability.

| Strongest emitted capability | Single-root IR / manifest / profile | Multiple-root IR / manifest / profile |
| --- | --- | --- |
| Arithmetic and comparisons | v6 / v6 / `pure-module/v4` | v7 / v7 / `pure-module/v5` |
| Bitwise operations | v8 / v8 / `pure-module/v6` | v9 / v9 / `pure-module/v7` |
| Either width conversion | v10 / v10 / `pure-module/v8` | v11 / v11 / `pure-module/v9` |

All v2–v9 schemas reject the conversion leaves. The new versions inherit UInt64,
bitwise operations, joins and required typed API metadata. Root fields remain
exact: `root` for one root, `roots` for 2–64 sorted distinct roots. CLI dispatch
and proof manifests accept only matching version/profile/root combinations.
The current generator identity is `lean-go-modulegen/v9`.

## Standalone proved example

The current checkout's [Words module](../../examples/proofs/words) is a copyable
Go module with five selected roots: `widen`, `narrow`, `pack`, `high` and `low`.
Its generated package uses v11 IR/manifest, `pure-module/v9` and
`lean-go-modulegen/v9`. Seven independently specified universal source claims
cover `toNat` preservation/modulo, both conversion round trips, the two packing
projections and full unpack/repack. The exact source statements and axiom
receipts are in its README and fresh proof evidence. `pack_bits` and remainder
belong only to proofs; runtime packing uses `<<<`, lowered to `UInt64.shiftLeft`.

The standalone Go tests compare all five typed/raw operations with an arithmetic
model and check the seven laws over 699 triples (169 boundary pairs, 18 wide
boundaries, 512 seeded triples), plus exact budgets, error results and input
ownership. `scripts/check_words_demo.py` regenerates every artifact with the
installed CLI, compares exact bytes, freshly checks all seven claims, and then
tests and runs a Go-only deployment. Both direct and sdist-rebuilt wheel smoke
paths include Words. Reproduction requires the CLI from the same development
checkout; the published alpha.6 CLI and tag do not include Words.

## Bounded evidence

Run the focused campaign with the pinned tools and Python 3.15 through uv:

```sh
uv run --locked python scripts/check_width_conversions.py --artifacts /tmp/width-evidence
```

The output directory must be new. It retains source, executable IR, pure-mono
trace, tool identities, commands and compared values. The campaign compares five
roots (`widen`, `narrow`, `pack`, `high`, `low`) over 206 boundary and seeded
triples against native Lean, raw Go, typed Go and an independent Python
arithmetic model. The model uses identity, modulo 2^32, `hi * 2^32 + lo`, integer
division by 2^32 and remainder. Values cover zero, maxima, bits 31/32/63, the
neighbors of 2^32, alternating bits and deterministic random samples.

The pure-mono trace confirms the exact leaves. The campaign searches resource
thresholds, checks failed calls return no partial output, and records observed
allocation/step counts: widen, narrow and low use 1/2; high uses 3/4; pack uses
5/6. The focused Go tests also cover signatures, unary arity, reversed widths,
wrong results, unknown names, all legacy gates and payload isolation. Fresh
producer tests mutate externs, implementation/initializer attributes, mono
signatures, pristine closure helpers and scoped/late rewrites while preserving
existing output on rejection. The `width_conversions` compatibility case has an
independent Go consumer for both conversions and packing/unpacking.

CI discovers `test_width_conversions.py` and `test_width_campaign.py`; the
extraction job enables their fresh Lean and differential paths. These are
bounded tests of a trusted extraction pipeline, not a verified compiler or a
universal proof of Go correctness.
