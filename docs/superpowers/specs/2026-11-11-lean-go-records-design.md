# Source-shaped records through Lean representation erasure

The selected milestone is ordinary record field access, nested fields and record
updates, retaining pure Go consumers and Python 3.15 tooling through uv.

## Observed gap and approach

The existing producer already extracts ordinary multi-field records: the pinned
Lean compiler lowers their projections to exhaustive cases. Single-field records
are different. Lean removes their constructor and uses the field's representation;
the current producer rejects the resulting signature or field-type mismatch.

Preserve original source types in exported IR and the generated Go API. Insert
explicit wrapping and unwrapping operations at typed boundaries while encoding
the actual pure-mono body. These operations use the existing constructor and
exhaustive-case IR. A direct runtime-erased API would lose the source record
names; exporting a compiler stage before monomorphization would broaden the
compiler boundary unnecessarily. Neither alternative is used.

No IR schema, profile, Go validator, emitter, runtime template, or public API
extension is needed. Fresh exports remain v3/pure-module-v2. Earlier profiles
retain their restrictions. Source provenance and all numeric primitive checks
remain in effect; no new standard-library exception is added.

## Supported mapping

Normalize only already-admitted closed, monomorphic, safe source data types.
UInt32 and nontrivial data keep their type names. A wrapper must have exactly
one constructor with exactly one source field, no parameters, indices, universe
parameters, dependent fields, proof fields or recursive inductive declaration.
The field must itself be admitted. Nested wrapper chains are allowed, up to 64
layers; detect cycles explicitly. Confirm the pinned compiler's
`hasTrivialStructure?` descriptor names this constructor, has no parameters and
selects field zero. Check the independently computed result against `toMonoType`.
Do not infer permission merely from Lean marking a type trivial: proof-bearing
structures can have one runtime field but multiple source fields.
An ordinary recursive single-constructor type that the compiler does not erase
keeps its existing nominal schema; this milestone does not add a rejection for
previously supported recursive data.

All source schemas retain full Lean names and original field types. For example,
`Count { value : UInt32 }` remains a Count node in Go, and a record containing a
Count retains that field type. The compiled body may operate on the underlying
UInt32 temporarily. Runtime-schema names and synthetic functions are unnecessary.

## Adapter placement

1. Function parameters use their source types. Unwrap each wrapper parameter into
   its checked mono type before encoding the function body.
2. Returns wrap the mono result back into the function's source result type.
3. Calls wrap arguments to the callee's source parameter types. The source-typed
   call result is unwrapped to the compiler let binder's mono type.
4. Constructor applications wrap mono arguments to original source field types.
5. Case alternatives bind original source field types, then unwrap those fields
   into the compiler's mono case parameters before encoding the branch.

This also covers a wrapper around a nontrivial recursive sum without changing the
sum itself. Wrapper normalization follows only wrapper chains, never recursively
expands an entire data schema. Existing exact type-closure validation supplies
all original field types.

Every inserted binder is fresh within its function. The actual compiler FVarId
maps to the final mono-typed value, and generated temporary IDs cannot collide
with source binders. Inserted cases are exhaustive over the one constructor and
carry the enclosing function's source result type. Signature checks compare
actual mono types with normalized source types at every boundary.

## Resource and trust rules

Unwrapping reads immutable child nodes using an emitted case and allocates no
runtime value. Wrapping creates ordinary constructor nodes and consumes the
existing allocation, output-node, depth and step budgets. Adapter instructions
and binders count toward existing IR/code-depth limits. Budget failure returns
an invalid value, including when failure occurs during return reconstruction.
There is no guarantee that an unchanged custom budget accepts newly supported
programs; the extra representation work is deliberately charged.

Generated record accessor definitions and helpers remain subject to historical
and final source audits, including inlined helpers and imported module bodies.
Unsupported externs, replacements, axioms, dependent/proof-bearing records,
generics, local joins and local functions remain rejected. Conditional record
updates that lower to joins are still outside this milestone. Source elaboration
is trusted, and extraction remains tested rather than formally verified.

## Evidence

Exercise scalar and Bool wrappers, nested wrapper chains, wrappers inside
multi-field records, wrapper parameters/results, noinline helper calls, record
updates and nested field access. Verify both classic and modern imported modules.
Include a wrapper around an existing nontrivial sum and wrapper-return roots.
The generated API must expose source-shaped values at every boundary.

Use a separately compiled native Lean reference and an independent Go oracle over
boundary and seeded records, including UInt32 overflow. Test ownership after
input mutation, malformed wrapper fields, exact allocation/step/depth failures,
and output reconstruction. Reject proof fields, polymorphism, unsupported numeric
types, and altered accessor/helper compiler attributes without replacing existing
output. Confirm repeated fresh generation, old examples and an isolated Go-only
consumer with `CGO_ENABLED=0`. Independently review specification coverage and
code quality; distinguish Windows cross-compilation from runtime execution.
