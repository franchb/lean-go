# Typed Go APIs

## Goal and scope

Generate a convenient, statically typed Go interface for the supported ordinary
Lean module subset. A consumer should construct records and trees, inspect named
fields or sum alternatives, and invoke the selected root without Lean-name strings
or positional `Value.Fields` indexing. Consumers remain standard-library-only Go
with `CGO_ENABLED=0`; regeneration uses Python 3.15 through uv.

This is the first of three ordered increments: typed APIs, UInt64 support, then an
experimental public preview. Each increment is verified and committed before the
next implementation begins. Compiler admission and execution semantics are not
expanded by the typed API increment.

## Metadata boundary

Module IR v5 retains `pure-module/v3` execution semantics and adds a required
top-level `api` object containing `records`, a map from full Lean type names to
their ordered direct field names. An empty map is valid. Existing constructor and
type wire shapes remain unchanged. IR v2 through v4 keep their prior interpretation
and reject this new metadata.

The producer uses `getStructureInfo?` and `StructureInfo.fieldNames` for structures
in the exported type closure. Names must correspond exactly to the sole source
constructor's field order and arity. Inherited records retain direct parent
subobject fields. The producer must not infer names from optimized function bodies
or flatten parent projections. The Go validator checks that record keys identify
existing types with one constructor, field counts match, and field names are
nonempty, unique and bounded. Metadata is for presentation; it cannot authorize a
source definition, change field order, or bypass source-type checks.

## Go interface

Every non-scalar type has an exported wrapper containing a private immutable
`Value`. `UInt32` and canonical `Bool` use native `uint32` and `bool`. A wrapper's
zero value is invalid. Callers cannot replace or mutate its internal node.

Records get a typed constructor and named getters, for example:

```go
count, err := NewCount(uint32(7), limits)
n, ok := count.Value()
result, err := RunTyped(config, count, anotherCount, limits)
```

Sums get one typed constructor and one typed destructor per alternative:

```go
tree, err := NewTreeBranch(left, right, limits)
left, right, ok := tree.AsBranch()
```

Getters and destructors return `ok=false` for an invalid wrapper or the wrong
alternative. `RunTyped` has the original root's argument and result types and
returns `(Result, error)`; errors return the zero result. The existing raw API
remains available. Only v5 exports receive the new facade, keeping legacy IR
generation compatible.

Names prefer readable exported Go identifiers. Allocation is deterministic, uses
separate package and method scopes, reserves runtime identifiers, and resolves
namespace, spelling, Unicode, constructor/type and method collisions. Generated
names never enter source code without identifier validation. The manifest records
the chosen entrypoint, type, constructor and accessor/destructor mappings.

## Ownership and limits

Constructors check all limit fields, reject zero or malformed children, verify
field types, and reuse immutable child nodes. They compute occurrence-counted size
and depth using existing measurement rules and `MaxNodes`/`MaxDepth`. Sharing a
child does not reduce its charged occurrence count. Construction has the same
input-boundary accounting role as `Import`; execution budgets are enforced by
the existing `Run` implementation.

`RunTyped` converts only its immediate scalar arguments and wrapper handles, then
calls `Run`. It does not recursively clone or walk arguments, use reflection, or
add a second evaluator. Output wrappers retain immutable ownership. No API method
exposes writable node storage. The audited normalizer remains a separate profile.

## Validation and completion

Tests cover deterministic naming and collisions, source field metadata and strict
legacy compatibility, malformed metadata, scalar and Boolean roots, records,
recursive sums, zero handles, wrong alternatives, all existing resource limits,
shared-subtree occurrence charging, and equality with the raw API. An external
consumer must compile without Lean-name strings or positional field access, while
a wrong typed argument must fail Go compilation.

Regenerate all general examples, run Python admission/freshness tests and the full
Go native/regeneration/module suite, vet and Windows cross-compilation. Review
specification compliance and implementation quality independently. Record the
observed coverage and limits before committing this increment.
