# Local join-point extraction

Ordinary Lean `if` and `match` results must be usable in subsequent arithmetic,
calls and record updates. The approved milestone adds nonrecursive local join
points to the existing pure Go compiler without changing its immutable value API
or source-admission policy. Lean 4.35.0, Go 1.27.4 and Python 3.15 via uv remain
pinned; generated consumers continue to run with `CGO_ENABLED=0`.

## Representation and scope

New exports use `lean-go-ir/v4` paired exactly with `pure-module/v3`.
The v2/v1 and v3/v2 pairs retain their existing meaning and reject new control
forms. Numeric operations and canonical Bool requirements carry into v4.

Two exact JSON forms extend Code:

```json
{"op":"join","join":{"id":3,"params":[{"id":4,"type":"UInt32"}],"body":{"op":"return","var":4}},"next":{"op":"jump","target":3,"args":[1]}}
```

A join returns the enclosing function's result type. Its ID shares the positive,
function-wide unique namespace used by all value binders. Parameters are bounded
by the existing arity limit. Empty parameter and argument arrays are required and
must survive JSON round trips; absent, null, duplicate and extra fields fail.

The join body sees variables at its declaration plus its parameters and previously
visible joins. It cannot see itself. The continuation sees the new join plus the
original scopes, without its parameters. A jump is terminal and must target a
visible join with exactly typed arguments. These rules exclude self, forward,
sibling and cross-function targets and local control-flow cycles structurally.
Unused bodies still undergo all validation, closure collection and auditing.

## Producer

Lean's pure LCNF checker follows the same lexical scope order. Verify each `.jp`
signature against its parameter types and the normalized enclosing source result.
Bind its parameters as compiler-internal monomorphic values; do not box wrapper
types at jumps. Accept `.jmp` only with visible target and matching fvar operands.
Encode both subtrees with the enclosing source result so existing return adapters
reconstruct original wrapper and record types. Discover calls and types in both
subtrees. Local functions, closures and recursive local joins remain unsupported.
No new extern, axiom or compiler-rewrite exemption is added.

## Go compiler and limits

Compile joins to states in the existing function frame. Declaration states select
the continuation; jumps assign parameter slots in parallel then select the body.
Captured values remain in lexical slots. Neither operation allocates nodes,
closures or new call frames. Declarations and jumps each consume a control step;
join parameter slots count against the existing cumulative frame-slot budget.
The runtime template and public API remain unchanged.

Count join objects and parameters in the structural budget. Apply code-depth
limits to both body and continuation, and share code-pointer cycle/reuse checks.
The manifest becomes `lean-go-manifest/v4` and generator identity becomes
`lean-go-modulegen/v3`; the primitive catalog remains exact. Existing profiles
continue to produce their matching manifest versions.

## Verification and delivery

Validate scope, type, shape, ID uniqueness, version, closure and resource limits
with adversarial IR tests. Exercise zero-arity joins, nested visible targets,
captured values, calls and function self-recursion across joins. Source tests must
assert actual joins survive compiler optimization. Compare conditional record
updates and wrapped results with native Lean and independent Go arithmetic over
boundary and seeded inputs. Verify immutable ownership, exact control budgets,
Go-only consumers, deterministic regeneration, vet and Windows cross-compilation.

Preserve the audited v1 normalizer unchanged. Regenerate previous general exports
with the new producer identity/schema, expecting unchanged Go source for programs
without joins. Document observed coverage and unsupported features. Commit locally
as franchb; publishing, tagged releases and actual Windows execution remain out of
this milestone.
