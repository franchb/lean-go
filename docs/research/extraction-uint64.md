# UInt64 extraction

Ordinary module exports use IR v6 and `pure-module/v4` to add exact UInt64
computation. Consumers remain Go standard-library packages with `CGO_ENABLED=0`.
Current regeneration uses pinned Lean 4.36.0-rc2, Go 1.27.4 and Python 3.15 through uv.

## Semantics and admission

The supported operations are addition, subtraction, multiplication, left shift,
equality and unsigned less-than/less-or-equal. Arithmetic wraps modulo 2^64;
shift counts are reduced modulo 64. Literals are canonical decimal strings in IR,
including values above 2^53 and the maximum 18446744073709551615. They never pass
through floating-point storage. [Bitwise operations](extraction-bitwise.md) and,
in the development checkout, [two width conversions](extraction-width-conversions.md)
use their own capability schemas. Division, remainder and additional numeric
types remain unsupported.

The producer checks exact primitive names, extern symbols, signatures and axiom
sets against the pinned compiler. It audits operator wrappers and the UInt64 order
package through the same pristine dependency and historical-attribute checks as
UInt32. UInt64 is admitted in record fields, wrappers, joins and authenticated
captured/reduced helpers; those forms retain their existing provenance checks.

## Go interfaces and compatibility

The typed facade uses Go `uint64` for parameters, results, record fields and
constructors. For example:

```go
result, err := wide.RunTyped(18446744073709551615, 1, wide.DefaultLimits())
if err != nil { panic(err) }
sum, ok := result.Sum() // 0, true
```

Here `wide` imports `github.com/franchb/lean-go/generated/wide`.
The raw interface adds `Input.UInt64` and `Value.UInt64()`. UInt64 uses separate
storage from UInt32, preserving each width's overflow behavior. Scalars count as
one node, and all seven resource budgets retain their original meaning. Invalid
widths, nonzero inactive scalar fields and malformed wrappers return errors with
zero results.

Earlier IR versions retain their original restrictions and generated Go rendering.
In particular, an ordinary ADT named `UInt64` in older IR remains an ADT. The new
numeric interpretation applies only to v6. API metadata is required for v5/v6 and
rejected by earlier schemas. The audited v1 normalizer is unchanged.

## Validation

The wide example returns thirteen results covering all seven operations, wrapped
literals, zero, maximum, an above-2^53 literal and a helper computation. Its 953
input pairs combine 21 boundary values with 512 seeded full-width pairs. The
independent oracle uses arbitrary-precision `math/big` arithmetic and reduction
modulo 2^64. Native reference parsing preserves twenty-digit values and rejects
malformed or overflowing input; its suite exercises 21 rejection cases, an empty
batch and the exact 8192-row and 4 MiB boundaries.

Backend tests additionally exercise mixed-width records, joins, scalar and Bool
roots, nullary roots, typed/raw behavior, inactive payload rejection and exact
resource limits. Source tests exercise actual compiler-generated helpers, local
imports, custom operators, historical attribute mutations and erased unsupported
operations. An independent consumer requires only Go.

The full Go suite passed with native Lean, regeneration and module gates enabled;
the integration package completed in 204.985 seconds. The 953 wide cases matched
native Lean, the independent oracle and both Go APIs. Exact wide-example limits
were two input nodes, seventeen allocations, twenty steps, two frames, twenty-two
slots, depth two and fourteen output nodes; reducing each boundary rejected the
call. Go vet, Windows/amd64 builds and integration-test cross-compilation passed.
Actual Windows execution remains a preview-release gate.

All 111 Python tests passed with module admission enabled under Python 3.15
through uv (413.0 seconds with three independent test-file workers). The new
UInt64 suite contains eleven methods and thirty-four fresh exports; the original
UInt32 admission checks also passed.

All eight ordinary modules were regenerated and their recorded input/output
identities checked. The seven existing modules retain identical semantic IR after
excluding only schema, profile and input identities. Separately, twenty-one
admitted legacy fixture/profile combinations produced byte-identical Go files
with the previous and new generators, including v5 typed facades.

Independent specification and quality reviews passed. Ripwire still reports 44
gating rows: sixteen generated-copy findings, nineteen fixture-specific test
repetition findings and nine self-churn findings. Each was reviewed; no suppression
was added and this is not a clean-metric claim. Lean changes were reviewed directly.
