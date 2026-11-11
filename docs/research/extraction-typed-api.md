# Typed Go APIs

The module generator adds a typed facade to v5 ordinary-module exports. The
existing raw `Input`, `Value`, `Import` and `Run` interface remains available.
Generated consumers use only the Go standard library with `CGO_ENABLED=0`.
Regeneration retains Python 3.15 through uv and the pinned Lean and Go toolchains.

## Source names and compatibility

IR v5 retains `pure-module/v3` execution semantics and adds `api.records`:

```json
{"records":{"Records.Count":["value"],"Records.Nested":["count"]}}
```

The producer takes these labels from Lean's direct structure fields in constructor
order. Parent records remain explicit subobjects. The validator requires known
single-constructor types, exact arities, and bounded distinct field names. This
metadata controls API presentation only; source admission, runtime types and field
order remain authoritative. Earlier IR profiles reject the new metadata and
continue to generate their raw APIs. The audited normalizer is unchanged.

## Interface and ownership

Non-scalar Lean types receive Go wrappers with private immutable storage.
`UInt32` and canonical `Bool` map to `uint32` and `bool`. Records have typed
constructors and named getters; ordinary inductives have constructors and
`IsAlternative`/`AsAlternative` methods. Getters and destructors return a Boolean
validity result. A wrapper's zero value is invalid and returns false from these
methods; it is rejected by constructors and root calls.

`RunTyped` accepts the original root's typed arguments and a final `Limits`
argument, then returns a typed result and an error. It delegates to the existing
`Run`, preserving its validation and execution accounting. Errors return a zero
result. No reflection, recursive conversion or alternate evaluator is involved.

Constructors validate all limit fields and child types. They allocate fresh
immediate field storage and may share immutable child nodes. The existing cached
subtree sizes count each occurrence, so repeated children still consume repeated
input-node budget. Construction enforces `MaxNodes` and `MaxDepth`; root invocation
also checks the aggregate arguments and all execution/output limits. This matches
the input-boundary role of `Import` rather than charging execution allocations
before a call begins.

## Generated names

Names prefer exported Go spellings of source names. A deterministic allocator
handles namespaces, punctuation, Unicode, case changes, reserved runtime names and
constructor/type collisions. Methods use a separate namespace for each wrapper.
The manifest's `go_api` section records the root entrypoint, type names,
constructors, record accessors and sum destructors. Consumers can consult these
mappings when source names collide.

## Validation

The validation suite covers strict metadata and legacy compatibility, source field
order, inherited records, native scalar roots, recursive sums, deterministic
collision handling, invalid zero wrappers, wrong alternatives, shared occurrence
counts, and typed/raw result and resource-error equivalence. Independent Go-only
consumers exercise typed records and trees; a separate negative compilation case
checks that a wrong argument type fails at the Go type boundary.

All 100 Python tests passed with module checks enabled using Python 3.15 through
uv. The full Go suite passed with native Lean, regeneration and module checks
enabled. Typed/raw/oracle comparisons covered 1,970 captured-helper cases, 1,970
record cases, 1,193 numeric cases and 647 trees. The independent Go-only consumer
passed, and its wrong-argument example failed specifically at type checking.

An additional public-boundary regression confirms that explicit Go conversions
between wrappers cannot substitute a value of the wrong Lean type: getters reject
it and constructors/root calls return `ErrMalformed` with zero results.

All seven general modules were regenerated. Their previous runtime, schema and
control Go files remain byte-identical, and their semantic IR is unchanged after
excluding input identities, the schema marker and API metadata. The new facade is
emitted separately. Producer/source/generated-file identities and documentation
links were checked. Go vet, Windows/amd64 builds and integration-test
cross-compilation passed; actual Windows execution remains a release gate.

Independent specification and code-quality reviews passed. Ripwire's two gating
findings were short-horizon churn from the current sequence of changes; new-code
complexity and test/generated-code similarity advisories were reviewed without
blanket suppression. Edit checks found no incompatible callers. Lean source was
reviewed directly because it is not indexed by Ripwire.
