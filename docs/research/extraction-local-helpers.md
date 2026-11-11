# Local helpers with captured data

The local-helper milestone extends the ordinary module producer to directly
called, monomorphic local functions with supported captured data. Generated
consumers remain pure Go with `CGO_ENABLED=0`; regeneration uses the pinned Lean
4.35.0 and Go 1.27.4 toolchains and Python 3.15 through uv.

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Local --root Local.run \
  --out generated/local --ir-output testdata/local.ir.json
```

Use fresh output paths when existing package contents differ. The example accepts
a configuration containing a nested count and Boolean flag, plus two source
`Count` arguments, and returns a `Count`. Its local helper captures the configuration
and is called twice. With the flag set, the result is `x + y + 16 * bias`; otherwise
it is `9 * (x + y)`, with UInt32 arithmetic modulo 2^32.

## Source and compiler boundary

A local helper can capture `UInt32`, `Bool` and supported records, including nested
records. It must have supported data parameters and a data result, and every
surviving use must be a saturated direct call. Escaping function values, higher-order
captures, nested or recursive local helpers, and untraceable compiler transformations
are outside this increment. Ordinary source code may still lower to an unsupported
form; a successful Lean build alone does not establish extractability.
These restrictions concern surviving compiler forms. A curried root that compiles
to an ordinary function with data parameters still uses the existing flattened
root signature; its arrow syntax does not by itself mean a runtime closure escapes.
Likewise, a local recursive definition that elaborates into an ordinary safe kernel
definition still follows the existing self-recursion path.

Lean can inline small local functions. Positive tests use sufficiently large
helpers called more than once and check that a real lifted helper occurs in the
runtime function graph. A generated spelling such as `._lam_0` grants no special
authority. Source-authored declarations receive ordinary source checks.

## Checked observations

The producer temporarily observes builtin compiler passes while the existing
compilation callback runs. It preserves the installer list and restores the prior
pass manager on exit. The source audit also restores the fresh-name generator, so
inspection does not perturb later compiler variable ordering. It does not install
a user compiler pass or change the
compiler's output. Existing rejection of user compiler passes remains in place.

The source witness precedes optimization. Original local signatures and captured
data types are checked through their complete ADT fields; unsupported unused data
cannot become admissible merely by disappearing. Live compiler variable identities
connect source witnesses to surviving local functions. Saved base declarations
normalize those identities, so they cannot substitute for the live observation.

The lifting witness records the local body, its ordered environment, the compiler's
replacement binding and the resulting auxiliary declaration. Final mono snapshots
are checked separately because simplification runs after lifting. Export repeats
identity, module, mapping and snapshot checks through both helpers and source
owners. Call-side checks run before constructor classification.

Lean's auxiliary cache can reuse an existing helper from another owner. This
increment rejects an ambiguous derivation instead of inferring ownership from a
name. An unchanged earlier valid owner must remain independently exportable.
Unwitnessed compiler lambdas grant no authority. Their presence in an audited
source dependency, such as an operator dictionary, does not by itself reject a
root that compiles entirely to supported code. A surviving runtime call still
requires evidence, and cache reuse cannot transfer another owner's authority to
an unwitnessed helper.

## API and provenance

Exported roots and ordinary source functions retain their original signatures and
record adapters. Internal lifted helpers use checked mono data signatures. Capture
arguments precede the explicit local parameters, in the witnessed compiler order.
Helpers are ordinary generated function calls and consume existing frame, slot,
step and allocation budgets; they do not require a closure runtime.

Generated evidence remains separate from source-definition provenance. A helper's
provenance record points to its original source owner, whose original dependency
and rewrite graph is still audited. The derivation cannot authorize an imported
axiom or bypass extern, implemented_by, initializer, unsafe, proof-axiom or compiler
rewrite checks. Source elaboration remains trusted execution.

The IR schema, profile, Go backend, runtime and immutable API are unchanged.
Producer input identities include the new observer and evidence modules. The
audited normalizer remains separate. Actual Windows execution and tagged-release
consumer validation remain release gates; this milestone publishes no release.

## Verification

The generated example contains an actual two-parameter lifted helper with eight
arithmetic calls; the root calls it twice and retains its three source parameters.
Native Lean and generated Go match an independent arithmetic oracle over 1,970
inputs: 1,458 boundary combinations and 512 seeded cases. The native reference
rejects 21 malformed or oversized wires and accepts the exact 8,192-row and
4-MiB limits. Immutable ownership, original root validation, all resource budgets,
a Go-only consumer and two fresh regenerations passed.

The new source-admission suite contains 20 methods covering 54 fresh builds.
It exercises original and shadowed captures, classic and modern imports,
source-authored helper-like names, erased unsupported data, source rewrite history,
retained closures, changed snapshots and mappings, missing evidence, and auxiliary
cache reuse both across owners and within one lifting pass. Existing currying and
kernel-recursion examples retain their original behavior.

Review found and fixed missing owner-side checks when evidence and calls
disappeared together, and source-type checks bypassed by conditional or aliased
function bindings. Compatibility checks also caught fresh-name consumption by
source inspection; restoring the generator preserves compiler output.
Operator-dictionary compatibility and anonymous helper cache reuse are covered
separately: compiler-only lambdas grant no authority, and reuse rejects the new
owner while preserving the earlier authenticated owner.
Independent review also found nested anonymous reuse exposed by later outer
inlining. Untraceable nested helpers are rejected before lifting; regression
probes cover outer inlining under a raised compiler threshold.

All seven general packages were regenerated. The six earlier packages retain
byte-identical Go files and identical IR after removing only input identity hashes.
All 97 Python tests passed with module checks enabled using Python 3.15.2 through
uv 0.13.5. Test files ran in isolated subprocesses with three concurrent workers.
The full Go suite passed with native, regeneration and module gates enabled.
Go vet, Windows/amd64 build and integration-test cross-compilation passed.
Independent specification and code-quality reviews found no remaining implementation
defects. Lean source was reviewed manually because Ripwire does not index it.

Ripwire reported 45 gating similarity findings: 35 duplication groups and 10
clones of reused helpers. These cover intentionally standalone generated runtimes
and tests of distinct generated APIs or source-evidence fixtures. Generic test
harness operations are reused where their contracts match. The remaining findings
were reviewed without blanket suppression; new generated-code and test complexity
advisories were also inspected. Edit checks found no incompatible indexed callers.
