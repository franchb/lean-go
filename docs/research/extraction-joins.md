# Branch results through local joins

The module producer supports results from ordinary Lean `if` and `match`
expressions that feed later arithmetic, function calls and record updates.
New exports use `lean-go-ir/v4` / `pure-module/v3`. The backend still accepts
v2/v1 and v3/v2, which retain their original restrictions and reject join forms.
The original audited v1 normalizer remains unchanged.

## Generate the example

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Joins --root Joins.update \
  --out generated/joins --ir-output testdata/joins.ir.json
```

Select fresh output paths when the existing contents differ. The example updates
a record after conditional and matched intermediate results, preserving a Count
wrapper and capturing values from the input record. Its root takes a State,
Count and Bool, in that order. `Joins.total` also demonstrates a wrapped result
returned from a join body.

## Control flow and representations

A join is a local continuation with typed parameters. Its body can read variables
in scope where it is declared and its own parameters. Its continuation can jump
to it, supplying the branch result. The body cannot jump to itself or to joins
declared later; ordinary top-level function self-recursion remains supported.
Lexical validation rejects forward references, sibling/cross-function targets,
parameter leaks and continuation-only captures. Unused bodies are also validated.

The versioned IR adds exactly two forms:

```json
{"op":"join","join":{"id":3,"params":[{"id":4,"type":"UInt32"}],"body":{"op":"return","var":4}},"next":{"op":"jump","target":3,"args":[1]}}
```

Here value 1 must already be a UInt32 in scope, and the enclosing function must
return UInt32. IDs are positive and unique across joins and all value binders
within each function. A zero-argument jump still requires `"args":[]`.

Join parameters use Lean's internal monomorphic representation. Existing source
adapters preserve original wrapper and record types at function and constructor
boundaries; returns from join bodies also reconstruct the enclosing source
result. Original dependency and compiler-rewrite audits still apply to code
inside branches and joins. No new runtime primitive or audit exemption is added.

The Go generator compiles join bodies to states in the current function frame.
A declaration selects its continuation; a jump copies arguments to parameter
slots with Go parallel assignment and selects the body. Captured values stay
in their existing slots, including while a helper function runs in another frame.
No closure, new frame or node is allocated by a join or jump.

Each declaration and jump consumes one control step. Join parameter slots count
toward the existing cumulative slot budget; join objects, binders and both code
subtrees count toward the structural/depth limits. Ordinary calls, arithmetic and
record reconstruction keep their previous costs. Resource failures return an
invalid value rather than a partial result. The runtime template and immutable
Go API are unchanged.

Manifests use `lean-go-manifest/v4` and `lean-go-modulegen/v3`, retaining exact
primitive catalogs and source hashes. Older schemas retain their manifest version.
Generated consumers use only Go's standard library with `CGO_ENABLED=0`.
Regeneration uses pinned Lean 4.35.0, Go 1.27.4 and Python 3.15 through uv.

## Scope

This milestone supported nonrecursive lexical joins, not local functions, closures, effects,
mutually recursive functions, polymorphic or dependent data. Optimizations may
duplicate a small continuation instead of retaining a join; source tests check
actual exported control flow as well as results. Extraction is tested and trusted,
not a compiler-correctness proof. Source elaboration remains trusted execution.

The later [local-helper milestone](extraction-local-helpers.md) adds directly called
local functions with checked captured data.

At the join milestone, a limitation remained for compiler-generated reduced-argument helpers:
an unused source parameter can cause Lean to introduce a `._redArg` declaration
that the producer cannot yet resolve. Such modules fail explicitly, even when
their remaining control flow uses supported joins.
The subsequent [reduced-argument milestone](extraction-reduced-arguments.md)
supports helpers whose compiler origin and argument mapping can be checked.

Actual Windows execution and a consumer built from a tagged release remain
separate release gates. No remote or release is created by this milestone.

## Verification

The main fixture contains four joins and eight jumps; the alternate wrapped-result
root contains one join. Both were checked over 3,428 inputs: 2,916 boundary
combinations and 512 seeded cases, against native Lean and independent Go
arithmetic. The native reference also rejects 14 malformed or oversized wire
inputs. Tests cover immutable ownership, malformed API values, exact budget
boundaries, repeated calls, isolated Go-only consumers and repeated fresh exports.

Backend tests cover zero-arity joins and JSON round trips, repeated/reordered
arguments, captures across helper calls and function self-recursion, unreachable
invalid bodies, escaping targets and variables, all ID collisions, missing/null/
extra payload fields, old-profile rejection and exact arity/depth/node boundaries.
Dedicated generated tests pin step, slot, frame and allocation costs.

The full Go suite passed with native, regeneration and module gates enabled.
Go vet and Windows/amd64 build plus integration-test cross-compilation passed.
A 15-second decoder fuzz run completed 553,477 executions with no failure;
this bounded run supplements the adversarial tests rather than proving safety.

All 57 Python test methods passed with Python 3.15.2 through uv 0.13.5. The five
new source-admission methods perform ten fresh builds: five accepted exports and
five rejections for foreign code, proof axioms, compiler rewrites and retained
closures. Independent specification and code-quality reviews found no actionable
defects, including direct review of the Lean producer and exported lexical scopes.

Earlier tree, choice, numeric and record examples produce byte-identical Go
source. Their IR differs only in schema/profile and input identity metadata.
The runtime template, source-audit rules, record representation module and
audited normalizer are unchanged.

Ripwire found no incompatible indexed callers. Its quality delta reports 33
gating findings: 32 similarity findings across standalone generated runtimes,
tests of distinct generated APIs and the separate audited/module IR visitors,
plus a test-class size increase from 60 to 62 lines for freshness coverage.
These were reviewed as intentional; no metric-driven refactor or blanket
suppression was applied. Lean sources are not indexed and were reviewed directly.
