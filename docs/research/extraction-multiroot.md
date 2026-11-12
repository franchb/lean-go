# Multi-root extraction

Several ordinary Lean functions can be extracted into one Go package. Their
dependency closures are combined, with one copy of each nominal type, constructor,
helper function and runtime. This lets Go consumers compose typed functions without
converting values between separate generated packages.

This feature is available from the source checkout. The published
`v0.1.0-alpha.2` preview does not include it.

The source producer accepts repeated `--root` selections from the chosen module
and its local imports. It audits every selected root, then lowers one shared
closure. Names are sorted; repeating the same root is an error. There may be at
most 64 roots. Any unsupported root fails the whole extraction before publication.

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Counters \
  --root Counters.initial --root Counters.advance \
  --root Counters.read --root Counters.total \
  --out generated/counters --ir-output testdata/counters.ir.json
```

Use Go 1.27.4, Lean 4.35.0 and Python 3.15 through `uv`, as for single-root
generation. Generated consumers require only Go and work with `CGO_ENABLED=0`.

## Typed and raw entrypoints

The counter package exports `RunInitial`, `RunAdvance`, `RunRead` and `RunTotal`.
`RunInitial` returns `State`; `RunAdvance` accepts and returns that same `State`.
The `Count` wrapper used for an increment has one shared constructor and identity.
`RunRead` returns `uint32`, and `RunTotal` returns `uint64`. No serialization or
reimport is needed between typed calls.

Types and constructors receive names first. Roots then receive `Run` plus the
exported source basename, in canonical source-name order. Collisions get numeric
suffixes. Consult `go_api.entrypoints` in `manifest.json` for the exact mapping;
the existing type, constructor, accessor and destructor maps describe the shared
facade. Adding another root may introduce name collisions, so review the mapping
when changing the exported set.

Raw code uses:

```go
result, err := counters.RunRoot("Counters.read", []counters.Value{state}, limits)
```

Here `state` is an owned raw `Value`, obtained from `Import` or another `RunRoot`
call. The dispatcher accepts only explicitly selected full Lean names. Helpers in
the runtime closure, such as `Counters.step`, are not exported roots. Unknown
names, wrong arity, wrong parameter types and invalid values return `ErrMalformed`.
Invalid limit settings return `ErrLimits`; budget exhaustion returns `ErrResource`.
Errors return invalid results without publishing partial values.

Every call has independent budgets. Function selection checks the selected
signature and initializes its own frame size and argument slots. Shared immutable
values remain usable after either a successful or failed call. As with a single
root, limits apply per invocation, not to an entire sequence of Go calls.

## Compatibility and evidence

Multiple roots use `lean-go-ir/v7`, `pure-module/v5` and `lean-go-manifest/v7`.
The IR contains a sorted `roots` array and no scalar `root`. One root still uses
v6/profile v4 and keeps `Run` and `RunTyped`. V7 has neither of those defaults.
The current module generator identity is v6. The decoder retains old profiles and
rejects mixed legacy/multi-root fields, even if the irrelevant field is null.

The independent validator checks signatures and reachability over the entire root
set: missing roots, unrelated runtime functions and mutual recursion through any
root remain errors. Source proof and rewrite auditing stays in the producer.
Multiple roots do not authenticate hand-written IR or broaden primitive trust.

The `Counters` integration fixture checks shared-state composition, immutability,
wrapping arithmetic, exact UInt64 values, native Lean agreement and an isolated
Go-only consumer. Fresh extraction in reversed root order must produce identical
IR and package bytes. Unit/admission tests cover malformed root sets, hidden
helpers, collision naming, nullary functions, source rejection and resource limits.

The existing single-root fixtures are regenerated when implementation identities
change. Their executable Go output and semantic IR are checked for unintended
changes; input/source hashes and generator identity are expected to update.
The original audited normalizer remains a separate profile.

This milestone does not add polymorphism, generic collections, external Lake
packages or an end-to-end proof of extraction correctness. Broad generic
collections remain deferred. The [design](../superpowers/specs/2026-11-12-lean-go-multiroot-design.md)
records the selection and compatibility contract.
