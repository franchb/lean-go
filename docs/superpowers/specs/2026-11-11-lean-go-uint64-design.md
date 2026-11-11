# UInt64 extraction

## Scope

Extend ordinary module extraction with `UInt64` literals, addition, subtraction,
multiplication, left shift, equality and unsigned less-than/less-or-equal. This is
the second approved increment, after typed Go APIs and before the public preview.
Consumers remain pure Go with `CGO_ENABLED=0`; Python stays at 3.15 through uv.
Conversions, division, remainder, other bit operations and additional widths are
outside this increment.

Arithmetic wraps modulo 2^64. Shift counts are reduced modulo 64. Literal payloads
remain canonical decimal strings in IR, so values above 2^53 never pass through a
floating-point representation. Go APIs use native `uint64`.

## Producer and trusted boundary

The exact pinned primitive table gains seven UInt64 entries with their observed
`lean_uint64_*` extern symbols. A primitive descriptor identifies its operand
type as well as its result. Signature and argument validation must use that type,
not assume UInt32. UInt64's source notation, ofNat and order-package support are
checked through the existing pristine dependency-closure and historical-attribute
mechanisms; no namespace-wide extern permission is introduced.

Pinned Lean 4.35.0 observations establish that add/sub/mul/decEq/decLt/decLe have
no axioms, shiftLeft has `propext`, and the UInt64 order package has the existing
order-package assumptions. Multiplication by a power of two may lower to left
shift. Source changes to extern, implemented_by, compiler rewrites or initializers
must still reject even if optimization removes the affected call.

UInt64 is a scalar leaf in source signatures, complete ADT field closure, record
representations and captured-local validation. LCNF uint64 literals retain their
exact decimal values. Existing owner/helper evidence and source API adapters keep
their contracts; they do not acquire a separate numeric shortcut.

## IR and compatibility

IR v6 uses `pure-module/v4`, retains the v5 `api.records` metadata, and permits the
new `uint64` value kind and UInt64 primitive signatures. Canonical decimal parsing
uses a 64-bit bound, at most twenty digits and a decimal round trip. Mixing widths
in arguments, bindings, fields or returns is rejected.

Older profiles retain their existing scalar and primitive restrictions. Recognizing
a new builtin scalar must not reinterpret an ordinary type declaration in older
IR. API metadata remains required for v5/v6 and rejected in earlier schemas.

## Go representation and execution

Preserve the existing UInt32 storage and overflow semantics. Add distinct UInt64
storage rather than widening `node.scalar` blindly. UInt32 keeps type ID zero;
UInt64 receives a nonnegative ID after the program's ADTs in the new profile.
The existing negative invalid-type sentinel is never used as a scalar ID.

The v6 raw boundary adds `Input.UInt64` and `Value.UInt64()`. UInt32 inputs reject
nonzero UInt64 payloads, UInt64 inputs reject nonzero UInt32 payloads, and ADTs
reject both. Constructors and execution continue to account for each scalar as
one node. All existing node, depth, output, allocation, step, frame and slot
budgets remain authoritative.

Generate UInt64 operations against the distinct `uint64` field; comparisons
produce canonical Bool. Left shifts use a mask of 63. The typed facade maps UInt64
to `uint64` in parameters, constructors, getters and results, preserving its
immutable wrapper and error contracts. Runtime/template changes are conditional
on the new profile so legacy IR generation stays compatible.

## Evidence

Use `math/big` modulo 2^64 as the independent Go arithmetic oracle, not the same
uint64 expressions used by the emitter. Include boundaries around 2^32, 2^53,
2^63 and 2^64-1, shifts 63/64/65, wrapped source literals and seeded full-width
inputs. Native Lean wire parsing must reject out-of-range values and preserve
twenty-digit integers exactly; Go decodes them directly into uint64 fields.

Exercise mixed-width records, single-field wrappers, joins, captured helpers,
reduced arguments and typed consumers. Test inactive payload rejection and exact
resource boundaries. Source admission tests cover altered primitive/support
attributes, scoped/late rewrites, operator dictionaries and erased unsupported
operations. Existing negative tests using UInt64 as an unsupported width move to
another unsupported width, without weakening their diagnostic assertions.

Regenerate existing modules and the new wide example, run all admission and Go
native/regeneration checks, vet and Windows cross-compilation. Complete independent
specification and quality review before committing and beginning preview work.
