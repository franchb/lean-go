# Local helpers with captured data

Support ordinary monomorphic local helper functions that survive compilation as
directly called auxiliary functions. Capture `UInt32`, `Bool` and supported
records, including nested records. Keep exported source signatures, immutable Go
values and all resource limits. Consumers use `CGO_ENABLED=0`; tooling uses Python
3.15 through uv. This is an experimental extraction extension, not a compiler proof.

## Compiler evidence

The pinned compiler lifts a sufficiently large local function used twice into a
helper such as `Example.run._lam_0`. Its captured values precede its explicit
parameters. Small functions can inline away, so positive tests must establish
that an actual lifted helper was exported. The compiler's auxiliary cache can
reuse names across owners; a suffix or prefix is not evidence of ownership.

Observe the builtin lambda-lifting pass through a temporary wrapper of the
existing pass manager during the existing compilation hook. Preserve the list of
custom installers, reject user compiler passes under the existing rules, and
restore the manager in `finally`, including failure and nested compilation paths.
Do not install a user `cpass` or modify compilation results.

Record each supported local declaration, its lexical captured operands, the
compiler's replacement binding, the exact generated declaration and its source
owner. Validate capture order, arity, types, closed monomorphic signatures, safe
declarations and saturated direct calls. Retain final mono snapshots and defining
module identities. Revalidate snapshots and recorded mappings at export. Check
both reached helpers and source owners, so disappearance of a call cannot conceal
changed evidence. Check collisions before deciding a call is a constructor.

The first increment covers nonrecursive local helpers with data captures and
direct calls. Escaping closures, function-valued captures/arguments/results,
recursive local helpers, ambiguous auxiliary-cache reuse and untraceable compiler
transformations reject explicitly. Nested cases and joins inside helper bodies
continue to use the existing supported control flow. Combining local helpers with
reduced source arguments requires an authenticated owner chain, or an explicit
unsupported diagnostic; it must not weaken either admission path.

## Source types and audit

Validate original source local-function let signatures and their free captured
variables before optimization. Check full source ADT field closure, including
unused parameters and captures whose use disappears. Limit this new source scan
to local function bindings rather than blanket-checking elaborator motives,
typeclass arguments or numeral machinery. Persist issues with the original source
owner so an unrelated declaration does not prevent a supported root from exporting.

Runtime helper signatures may use checked mono data types; original source names
remain visible at exported roots and ordinary source function boundaries through
the existing adapters. Generated helper records are separate compiler evidence,
never source-definition authority. Provenance has a helper-to-source-owner
derivation edge and preserves the owner's original dependencies and rewrite
history. Existing extern, implemented_by, initializer, unsafe, proof-axiom and
custom-pass checks remain in force, including erased dependencies.

## Compatibility and verification

Keep v4 IR, profile v3, Go backend and runtime unchanged. Add trusted producer files
to input identity hashes and regenerate all general examples. Earlier generated
Go sources and semantic IR must remain identical apart from input identities.

Test fresh classic and modern source modules; scalar and nested-record captures;
actual surviving helpers; capture order; multiple call sites; shadowing; source
wrapper boundaries; imports; collisions; stale snapshots; malformed evidence;
unsupported erased fields; escaping closures; unsafe historical dependencies; and
unchanged output on rejection. Compare native Lean and generated Go against an
independent oracle on boundary and seeded inputs. Verify immutable ownership,
resource limits, fresh reproducibility and a standalone Go-only consumer.

Run Python admission tests using uv, the Go suite with native/regeneration/module
gates, vet, and Windows cross-build/test compilation. Obtain independent
specification and quality reviews. Actual Windows execution and tagged-release
consumer checks remain separate release gates. Deliver a clean local branch with
the public project identity; remote publication is outside this milestone.
