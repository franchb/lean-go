# Multi-root extraction design

One extraction may select several ordinary Lean functions and export them as a
single Go package. Their nominal types, constructors, helper functions and runtime
are shared. A value returned by one typed entry point can be passed directly to
another. Consumers continue to require only Go with `CGO_ENABLED=0`; producer
tooling keeps Python 3.15 via `uv` and the existing pinned Go/Lean versions.

## Selection and compatibility

`generate.py` and `export_module.py` accept repeated `--root` options alongside
one `--module` and source directory. That module imports any needed local modules.
Selection is bounded to 64 unique names. Duplicate roots are rejected, and the
remaining names are sorted before compilation and serialization. Reordering the
same roots must produce identical IR and generated files.

A single selected root keeps v6 IR, `pure-module/v4`, the scalar `root` field,
and the existing `Run` and `RunTyped` Go APIs. Multiple roots use v7 IR,
`pure-module/v5` and a nonempty sorted `roots` array with at least two names;
there is no scalar `root` or implicit default. Earlier IR versions remain accepted
under their original restrictions. Root fields belonging to another schema,
including null or empty legacy/multi-root fields, are rejected by the decoder.

## Auditing and closure

The Lean producer audits every selected source root before lowering. It traverses
the union of reachable functions, deduplicates helpers and nominal types, and
applies the existing source provenance, primitive, rewriting, helper-origin and
mutual-recursion checks to the whole union. A rejected secondary root fails the
entire extraction before any package is published.

The Go IR validator independently checks every root's existence and signature,
the combined reachable closure, and the absence of unrelated functions/types.
IR provenance remains traceability, not authentication. Supporting multiple roots
does not broaden the admitted Lean language subset or trusted primitives.

## Generated APIs

For v7, `RunRoot(name string, args []Value, limits Limits)` selects a root by its
full Lean name. Only explicitly selected roots are callable through this API;
internal helper names and unknown names return `ErrMalformed` with an invalid
value. Each call checks that root's parameter/result types and starts fresh
resource budgets. State and values stay immutable across entry points.

Each root also receives a typed function named from `Run` plus its source basename,
such as `RunAdvance`. Types and constructors are allocated first; root names are
then allocated in canonical source order with the existing deterministic suffix
rule. Reserved implementation names cannot collide. The manifest's
`go_api.entrypoints` maps every full Lean root name to its typed Go name. The
shared type/constructor/accessor mappings retain their existing meaning.

The runtime uses compiled Go control flow and one function table. It initializes
the selected root's frame and checks its output shape; it does not expose arbitrary
internal function IDs. V7 uses manifest v7 and module generator v6. Legacy
generated code retains its API and behavior; generator/validator/source identity
hashes change when those implementation inputs change.

## Validation and scope

The example composes initialization, two state updates and scalar reads, with
shared nested records and UInt32/UInt64 results. Independent arithmetic, compiled
native Lean, raw Go and typed Go must agree. Tests cover root-order determinism,
name collisions, missing/duplicate/unsupported secondary roots, union reachability,
hidden helpers, invalid wrappers and per-call resource limits. An isolated consumer
must build/run without Lean, Python, C tools or cgo.

Existing single-root examples are regenerated from fresh producer workspaces and
their executable Go output is compared for unintended changes. Existing admission,
native/reference, pure-Go and generated-program differential gates remain relevant.

Multiple source directory trees, external Lake packages, generic collections,
polymorphic instantiation and changing root selection after package generation are
outside this milestone. Broad generic collections remain deferred. This remains
tested, trusted extraction rather than a compiler correctness proof.
