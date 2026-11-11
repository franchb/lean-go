# Compiler-generated reduced-argument helpers

The producer supports checked `._redArg` helpers created by the pinned Lean
compiler when it removes unused function parameters. Exported roots retain their
complete original source signatures. Consumers remain pure Go with
`CGO_ENABLED=0`; regeneration uses Lean 4.35.0, Go 1.27.4 and Python 3.15 via uv.

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Reduced --root Reduced.run \
  --out generated/reduced --ir-output testdata/reduced.ir.json
```

Use fresh output paths when existing contents differ. Ordinary source code can
now call these reduced helpers directly, including through local joins and
recursive functions. Source wrapper types remain visible at Go boundaries.

## Checked compiler evidence

Reduced helpers live in Lean's mono compiler declarations and generally have no
kernel constant. Their original parameter mask is not persisted by the compiler.
The producer observes the source compilation event before and after it runs and
checks an exact surviving owner wrapper:

```text
let result := owner._redArg retainedOwnerArguments
return result
```

The arguments determine strictly increasing, distinct retained positions in the
owner's signature. Helper and owner FVarIds are normalized independently and are
not interchangeable. The producer checks the complete declarations, signatures,
defining module and safe source provenance, then stores separate compiler evidence.
It does not treat a matching name suffix as evidence.

Admitting a generated helper requires its name to be absent from both kernel and
mono declarations before the event. A forwarding witness at a pre-existing name,
a changed pre-existing mono entry, or a later kernel declaration colliding with a
recorded helper is rejected. Captured declarations and mappings are checked again
at export, even when a call has subsequently disappeared. A user-authored function
whose name contains `_redArg` receives ordinary source checks and no privileges.

Small forwarding wrappers sometimes disappear through optimization. Without an
exact witness, their leftover helpers are not admitted; ordinary owner bodies
that no longer call those helpers continue to work. Arbitrary compiler-generated
helpers, closures and specialization remained outside that milestone. The later
[local-helper milestone](extraction-local-helpers.md) adds checked directly called
local helpers with captured data.
If no forwarding witness exists and a pre-existing mono entry is unchanged,
ordinary source admission still applies; spelling alone does not establish a
compiler event or a collision.

## Source signatures and provenance

Validate the owner's entire original signature and source ADT field closure
before removing arguments. Unsupported proof, dependent, polymorphic,
higher-order or numeric fields cannot disappear past this check. Discarded source
types are not added to the exact emitted runtime type closure.

Each internal helper uses the retained original source parameter types and the
original source result. Existing record adapters handle the erased mono types.
For an all-unused function, the compiler's exact void dummy parameter and erased
call operand are checked and omitted from the runtime signature. Any attempt to
use that dummy as data fails.

Provenance remains a graph: ordinary records carry original source dependencies,
and generated helpers carry a compiler-derivation edge to their source owner:

```json
{"name":"Example.f._redArg","dependencies":["Example.f"],"rewrites":[]}
```

The owner retains its original source dependencies and historical rewrites,
including dependencies erased from runtime code. Generated evidence is never
inserted as a safe source-definition record, so it cannot authorize an imported
kernel axiom. The existing extern, implemented_by, initializer, custom-pass,
proof-axiom and rewrite checks still apply. Source elaboration remains trusted
execution; this is tested extraction, not a compiler-correctness proof.

## Runtime and compatibility

This is a producer change. The v4 IR, profile v3, Go backend, immutable value API
and runtime template are unchanged. Helpers are ordinary compiled functions and
consume the existing frame, cumulative slot, step and allocation budgets. A root
wrapper can add a call frame; limits describe generated execution. Even unused
root arguments must satisfy the original API's type, shape and resource checks.

The new producer module is included in source identity hashes. Existing generated
packages require fresh manifests/IR identities when regenerated. The audited v1
normalizer remains unchanged. Actual Windows execution and tagged-release consumer
validation remain separate release gates; no release is published here.

## Verification

The primary example retains five source arguments and returns a `Count`. Its
internal root helper takes four arguments; its `add` helper takes two `Count`
arguments. Separate roots verify zero-runtime-argument helpers and self-recursive
helpers while preserving the original two- and three-argument APIs.

The native Lean reference and generated Go were compared with an independent
arithmetic oracle over 1,970 inputs: 1,458 boundary combinations and 512 seeded
cases. The tests cover all three roots, and the native reference rejects 14
malformed or oversized wire inputs. Go-only consumers, immutable ownership,
validation of unused inputs, exact budget boundaries and repeated fresh
regeneration passed.

The 20 new source-admission test methods cover 48 fresh builds. They test retained
positions, source wrappers, classic/modern imports, recursion, all-unused calls,
discarded unsupported fields, erased unsafe dependencies, historical/current
rewrites, malformed compiler evidence and preserved output on rejection.

Regression tests reproduce two collision cases that require distinct checks:
a generated call whose name is also a kernel constructor must be checked before
constructor classification; a late name collision or changed owner body must be
checked through owner provenance even if optimization removed the helper call.

The combined Python suite passed all 77 methods under Python 3.15 through uv,
with fresh-module admission enabled. The full Go suite passed with native,
regeneration and module gates enabled and `CGO_ENABLED=0`. Go vet, Windows/amd64
build and integration-test cross-compilation passed. Independent specification
and code-quality reviews found no remaining actionable defects.
Earlier tree, choice, numeric, record and join examples retain byte-identical Go
source; their IR changes only in input identity hashes. The Go backend, CLI,
runtime template, primitive registry, record adapter and audited pilot are unchanged.

Ripwire found no incompatible indexed callers. Its 28 gating findings are
similarity reports across standalone generated runtimes and tests of distinct
generated APIs; they were reviewed without blanket suppression or metric-driven
refactoring. Lean is not indexed by Ripwire and was reviewed directly.
