# Compiler-generated reduced-argument helpers

Support ordinary Lean functions whose unused arguments cause the pinned compiler
to generate `._redArg` helpers. Preserve source-shaped root signatures, original
dependency/rewrite audits, `CGO_ENABLED=0` consumers and Python 3.15 through uv.
This milestone targets reduced-argument helpers, not arbitrary generated names,
closures, specialization or unsupported source types.

## Evidence and runtime functions

Lean 4.35.0 stores these helpers in mono compiler declarations rather than kernel
constants. Its parameter mask is transient. A surviving source-owner mono wrapper
provides an exact witness: `let result := helper retainedArgs; return result`.
Optimized callers can directly call the helper, and recursive helpers call
themselves. Emit explicit internal helper functions; bounded call-site inlining
cannot cover recursion or recover discarded arguments.

Keep compiler evidence in a separate persistent extension, never in the source
`provenanceExt`. Before compilation, observe the candidate helper's kernel/mono
presence. After successful compilation, capture owner/helper mono declarations,
defining module and forwarding argument positions. Admit only safe, closed mono
declarations with an absent-before helper and the exact forwarding witness.
Retained positions must be strictly increasing, unique owner parameter positions,
with matching helper types and result. Match arguments against owner parameter
FVarIds; independently normalized helper FVarIds are not position evidence.

Revalidate captured declarations, module identity, safe compiled source owner and
current attributes at export. Reject kernel-name collisions before or after the
event, repeated compilation, modified snapshots, missing evidence and malformed
wrappers. Detect an overwritten pre-existing helper as a collision even if a later
optimization removed the owner wrapper; it must not fall through to ordinary
source-function admission. An ordinary source name containing `_redArg` grants
no privilege and can still use the ordinary source path when no collision exists.

Small wrappers can disappear through simplification. Their unused helpers remain
unadmitted without a witness; ordinary owner bodies that no longer call them keep
working. The existing legacy export harness retrieves full mono bodies from modern
imports. Unavailable or opaque helper bodies fail explicitly rather than adding
an extern exemption.

## Source shapes and erased arguments

Validate the owner's complete original signature and all its source ADT fields
before selecting retained domains. Discarded proof, polymorphic, dependent,
higher-order or unsupported numeric/data parameters must not bypass admission.
This validation does not add unused types to the emitted runtime type closure.

Internal helpers expose the retained original source parameter types and the
owner's original source result. Existing record adapters handle mono parameters,
calls and returns. The root still exposes its full original signature and calls
its internal helper through the existing ordinary call operation.

If every source parameter is unused, accept only the compiler's exact single
`lcVoid` dummy parameter and `.erased` argument convention. Emit zero runtime
parameters, require the erased operand at calls, and never bind the dummy as a
runtime value. Any data use of it fails. Self-recursive helper calls use the same
checked mapping; mutual runtime recursion remains rejected.

## Provenance and compatibility

Keep v4 IR, profile v3, existing Go validator/emitter and runtime unchanged.
The existing provenance graph can represent a compiler-derivation edge:

```json
{"name":"Example.f._redArg","dependencies":["Example.f"],"rewrites":[]}
```

This record comes only from authenticated internal compiler evidence. It is not
a source-definition record and cannot authorize imported kernel axioms. Retain
the owner's complete original provenance and historical rewrites. Audit the root
before extraction, then the runtime closure, following generated nodes to owners.
Current and historical externs, implemented_by, initializers, custom passes,
axiomatic proofs and unsupported rewrites remain rejected.

Generated helpers use existing function frames, slots, steps and allocation limits.
Source identity includes the new trusted producer module. Existing exports must
retain byte-identical Go source where they contain no newly admitted helpers;
their input hashes change. The audited pilot is unchanged.

## Verification and delivery

Establish the current unknown-constant failure before implementation. Test dropped
positions, several unused arguments, all-unused and already-working small owners,
direct calls, recursive helpers, wrapped fields/results and classic/modern imports.
Assert real helper calls and owner derivation records. Reject collisions,
untraceable/mismatched evidence, invalid dummy/masks and erased unsafe dependencies;
preserve previous output on failure.

Compare an ordinary source fixture with native Lean and an independent Go oracle,
check the full source API and malformed unused inputs, exercise budgets and a
Go-only consumer, and verify deterministic regeneration. Run full Go/Python suites,
vet and Windows cross-build, with independent specification and quality reviews.
Commit locally as franchb. No remote publication or tagged release is included.
