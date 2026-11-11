# Ordinary Lean to pure Go: initial design

Date: 2026-11-11

Maintainer: franchb <hello@franchb.com>

Working project name: lean-go

Status: approved design; bounded feasibility completed on 2026-11-11.
The [evidence report](../../research/extraction-feasibility.md) selects pure mono
LCNF for the controlled normalizer subset. Go backend and release work remain
future milestones.

## Purpose and scope

Make components written and proved in ordinary Lean 4 usable as pure Go
packages. The consumer imports generated Go source and builds with
`CGO_ENABLED=0`; the producer uses Lean to check proofs and regenerate the
package. There is no native Lean runtime, C library, subprocess, or virtual
machine in the deployed component.

The first release is an experimental extractor for a documented executable
subset, a reusable translation core, and one complete public example. All
three live in one repository, with separable interfaces rather than separate
release processes. Improvements useful to existing compiler and integration
projects can be proposed upstream as concrete needs emerge. No upstream
acceptance or maintainership is assumed.

The example is an independently authored normalizer for expressions in
standard strong-Kleene three-valued logic. Its specification, definitions,
proofs, inputs, and tests are original public material. Public artifacts and
history must not contain private application names, source, domain rules,
fixtures, identifiers, paths, or tracker references. Downstream applications
can consume the public package without becoming dependencies of this project.

The initial release does not promise arbitrary Lean extraction, an embedded
authoring language, general library coverage, or end-to-end verified Go.
It accepts ordinary Lean definitions and explicitly reports unsupported
executable dependencies. Broader extraction and a translation-correctness
proof are subsequent milestones, not implied properties of the first release.

## Public contract

A supported export produces deterministic Go source, its public type schema,
and a manifest linking that output to the input definitions and selected
theorems. If a required construct or primitive cannot be translated faithfully,
generation fails without publishing a partial package or changing an existing
successful output.

The producer environment contains a pinned Lean toolchain, dependency lock,
and extractor version. The consumer environment needs only Go and the declared
pure-Go dependencies. Any support library must be pure Go, explicitly versioned,
and included in the compatibility contract. The first example should need only
generated code and the Go standard library unless the feasibility result
demonstrates a specific need for shared helpers.

The initial execution targets are Linux/amd64 and Windows/amd64. Both require
semantic and boundary tests with cgo disabled. Cross-compilation alone does
not establish runtime support. Additional targets are added when equivalent
execution coverage is available.

## Architecture and data flow

### Export and claim discovery

Users write ordinary Lean types, executable definitions, and proofs. A
producer configuration selects export roots and the theorem associated with
each advertised claim; one export may have multiple claims. The extractor reads original declaration and public
type metadata before compiler lowering removes distinctions needed by the Go
API. A theorem must refer to the selected definition and the intended property;
a successful proof of an unrelated proposition is not accepted as evidence.

Executable reachability and proof dependencies are analyzed separately.
Importing a proof library does not require translating that library's tactics
or erased proof terms. Conversely, every executable dependency reached through
helpers, generated matchers, or compiler substitutions must be supported or
explicitly rejected.

### Pinned Lean compiler adapter

Reuse Lean's compiler facilities to lower ordinary definitions. The initial
candidate is a pure LCNF stage, with the monomorphic stage investigated first.
Original public type metadata accompanies that representation. The exact
stage is selected by the feasibility gate below; documentation alone is not
evidence that it preserves everything this backend needs.

The adapter owns version-specific compiler APIs. It produces a small,
versioned intermediate representation and a dependency/primitive inventory.
It does not expose Lean's internal objects as the public translation-library
API. Upgrading Lean requires rerunning the compatibility and semantic corpus.

### Reusable translation core

The core defines the supported intermediate constructs, value representations,
control flow, and primitive semantics. It checks incoming IR and exposes a
backend interface independent of the selected Lean version. This is an
internal compiler representation; users continue writing ordinary Lean.

Every admitted primitive has an explicit mapping covering inputs, results,
and exceptional cases. Similar-looking operators are not sufficient evidence
of equivalence. Mathematical integers must not silently become bounded machine
integers, and arithmetic failure behavior must not change without a declared
refinement contract. Numerical facilities not needed by the example are
outside the initial subset.

### Go backend and package output

The backend translates validated IR into Go declarations and a public API,
formats the source, and emits the manifest. Names are assigned deterministically
with collision checks. Required helpers have explicit versions and no hidden
native dependencies. Generated files are reproducible artifacts, never a
second hand-maintained implementation.

The complete output is prepared and checked before replacing the destination.
An unsupported dependency or generation error preserves the previous output
and returns failure. The manifest identifies the generated files so stale
outputs cannot silently remain part of the package.

### Manifest and claim evidence

The manifest records the export roots, associated theorem names and statement
digests, executable source/dependency digests, public type schema, Lean and
extractor versions, IR/support-library versions, primitive mappings, admitted
proof dependencies, generated-file digests, and supported target matrix.

These records establish traceability and freshness, not compiler correctness.
Generated artifacts, claims, and dependency versions must be checked together
before a release is accepted.

## First public example

### Mathematical input and behavior

Values are `false`, `unknown`, and `true`. Negation swaps true and false and
preserves unknown. Conjunction and disjunction are respectively minimum and
maximum under the ordering `false < unknown < true`.

Expressions contain constants, variables identified by `UInt32`, negation, and binary
conjunction/disjunction. The normalizer pushes negations to variables and
constants using double negation and De Morgan rewrites. It preserves operand
order. It performs no distributive expansion or classical two-valued rewrite
that loses unknown values.

For every valid finite expression `e` and every valuation `rho`, Lean proves
the public theorem:

`eval(normalize(e), rho) = eval(e, rho)`

This theorem preserves the complete three-valued result. It does not merely
preserve whether the result is true. A second theorem establishes
`isNormalForm(normalize(e))`: output negations occur only directly on variables,
and negated constants have been evaluated. This excludes an identity function
that preserves evaluation but does not perform the promised normalization.
Operand-order preservation is checked in the conformance corpus. Idempotence,
optimality, and application integration properties are not first-release claims.

### Executable subset

The example drives support for finite enumerations, the concrete recursive
expression type, pattern matching, pure first-order helper calls, and the
structural recursion needed by normalization. Variable identities are opaque
`UInt32` payloads mapped exactly to Go `uint32`; normalization performs no
arithmetic on them. Preservation of all 32 bits is part of the public boundary
contract and the feasibility gate. This does not require general integer
arithmetic support.

The implementation must use ordinary Lean definitions, not a separately
authored DSL program. A reference evaluator and proof may use facilities that
do not survive into the reachable executable normalizer. General-purpose
lists, records, polymorphic collections, arithmetic, higher-order APIs, and
floating-point support are not requirements merely because the example has
a recursive datatype.

Unsupported executable effects include IO, task scheduling, arbitrary foreign
functions, and runtime-dependent values without an explicit representation.
Partial or unsafe computational definitions are rejected unless they are an
identified standard primitive with an admitted, documented semantic mapping.
Compiler replacements such as `implemented_by` and `csimp` must be audited; a
proved logical definition does not by itself justify a different compiled body.
Rewrite theorem kind and transitive axioms must be checked separately from the
public claims. Current-environment CSimp lookup cannot reconstruct arbitrary
earlier scoped rules: a general adapter needs compile-time provenance or
explicit history restrictions/rejection. The feasibility report records the
concrete Bool.not rewrite and its separate admitted proof basis.

### Go input boundary

The public API must establish the domain of the Lean theorem before invoking
generated logic. Conversion from host values checks constructor tags, required
children, and acyclicity. Conversion creates an owned immutable tree whose
fields callers cannot mutate during evaluation. The caller must not mutate
the source host representation while conversion is in progress; later changes
to that source cannot affect the converted value.

Validation uses an iterative traversal with bounded work, detects cycles, and
checks configured depth and node limits before recursive normalization begins.
Malformed inputs and resource-limit violations produce distinct errors, never
a normal result computed from truncated input. Acyclic shared subexpressions
are copied per occurrence, with each occurrence charged to the node and work
limits before it is expanded. Cycle detection uses the active traversal path
so ordinary sharing is not confused with a cycle. A nil root, a missing child,
or an invalid zero-valued expression handle is rejected. A valid false
constant has an explicit constructor and is not confused with an invalid handle.

The mathematical theorem applies to all valid finite expressions. The public
Go operation admits the bounded subset its release documents. Logical
termination does not establish stack, time, or memory bounds. Implementation
planning must select concrete limits and evaluation strategy using the
feasibility example; release documentation and tests must state and enforce
those limits.

## Assurance contract

### Kernel-checked source properties

The advertised theorem is checked by Lean and bound to the exact exported
definition. Its transitive proof dependencies are recorded. The example admits
only Lean's documented foundational axioms (`propext`, `Quot.sound`, and
`Classical.choice`) when actually used; no project axioms or proof holes are
accepted. Additional trust assumptions require an explicit change to this
contract. A dependency on `sorryAx` or a native evaluation shortcut outside
the admitted basis fails the claim audit.

This policy concerns claims, not source spelling: scanning for the word
`sorry` is insufficient. The exported theorem statement itself is reviewed
for unintended hypotheses that would make the guarantee vacuous or weaker
than the public contract.

### Tested extraction

The initial Lean adapter, intermediate lowering, primitive mappings, Go
emitter, and public conversions are tested and trusted. Differential execution
does not turn them into formally verified translations. In particular, the
native Lean reference and generated Go may share compiler transformations;
agreement cannot independently validate those shared steps.

The precise first-release description is: **proved Lean logic with tested,
trusted extraction**. The project must not advertise the resulting Go package
as end-to-end verified or imply that an absence of proof holes verifies
compiler stages for which no theorem exists.

### Subsequent proof milestone

After a complete working pilot, define semantics for the supported Go subset
and prove preservation from the project's IR into that modeled subset. The
claim must name its representation relation, admitted inputs, primitive
semantics, and treatment of termination and errors.

That milestone alone does not verify Lean-to-IR lowering, source printing,
public conversions, the Go compiler, or its runtime. Each remaining link stays
explicitly trusted or receives separate evidence. Broader language coverage
must not be presented as a substitute for strengthening this assurance chain.

## Feasibility gate

The first implementation plan covers a bounded feasibility investigation.
After written-specification review, it constructs the public Lean example
and inspects its executable dependency closure on a pinned toolchain. Before
planning the complete backend, establish that the candidate compiler stage
provides sufficient control-flow and representation information, and that
source metadata can recover the intended public schema.

The result must identify every reached primitive, the encoding of recursive
values and variable identities, the recursion strategy, and all substitutions
between logical definitions and executable bodies. It must also identify the
smallest required IR and the trusted steps. No compiler behavior is assumed
merely because an external project demonstrates a different example.

Evaluate existing adapters before building another. Reuse requires source and
license review, compatible semantics, and a maintainable version boundary.
If pure monomorphic LCNF loses necessary information, evaluate an earlier pure
stage with retained metadata. If neither yields a bounded implementation,
revise the design explicitly; do not silently introduce a custom authoring
language, a native runtime, or a general compiler fork.

## Validation and release acceptance

The release is accepted only when all of the following evidence exists:

- Lean verifies both evaluation preservation and the output normal-form theorem,
  and the claim audit accepts
  exactly its documented dependencies. A theorem for a different export,
  a vacuous weakened claim, an unexpected axiom, and a proof hole are rejected
  by the corresponding automated checks or recorded statement review.
- Truth tables are exhaustively checked. Small expressions and their finite
  valuations are enumerated within a declared test budget; larger inputs are
  fuzzed with reproducible seeds and minimized failure cases.
- An independently written Go evaluator follows the truth-table specification
  directly. Native Lean and generated Go are compared on the same corpus,
  and evaluation before and after normalization is checked independently.
  The Go corpus also checks output normal form and operand order so a no-op
  implementation cannot satisfy acceptance by returning its input.
- Deliberately broken normalizations, changed primitive mappings, stale source
  digests, and unsupported reachable dependencies demonstrate that the relevant
  gates fail. These are targeted fault cases, not an assertion that testing
  detects every possible translation error.
- Boundary tests cover invalid tags, nil children, cycles, alias mutation,
  allowed sharing, and values just below, at, and above resource limits.
  Validation limits are exercised before recursive processing.
- Regeneration from identical pinned inputs produces identical tracked output.
  A failing generation leaves the previous successful package intact. A fresh
  generation agrees with the files and versions recorded in its manifest.
- Semantic and boundary tests pass under `CGO_ENABLED=0` on Linux/amd64 and
  Windows/amd64. Any additional cross-build results are labeled build-only
  until execution tests exist for those targets.
- A small consumer outside the extractor repository builds and runs from a
  tagged release with Go alone. It requires no Lean installation, C compiler,
  native library, repository-relative source, or private service.
- Public documentation explains the supported subset, exact source theorem,
  trusted extraction steps, error behavior, resource limits, and regeneration
  procedure. Examples and artifacts contain only public, independently authored
  material.

The feasibility report records completed Lean source proofs, truth-table/shape
checks, native Lean smoke, targeted proof mutations, and compiler/source audit
controls. These are bounded producer evidence. The generated-Go, differential,
boundary, two-platform runtime, external-consumer, and release requirements
above remain acceptance requirements for the future implementation and release.

## Compatibility, maintenance, and collaboration

The producer pins Lean and the full extraction dependency set. Compatibility
is established per tested toolchain version, not claimed for all Lean releases.
IR schema and helper-library versions are recorded explicitly. A consumer must
not combine generated files with an incompatible support-library version.

Tagged releases carry the complete reproducible example and exact claim
description. Compiler upgrades run the semantic, rejection, and boundary corpus
before the supported matrix changes. Initial experimental releases make
breaking changes explicit; version identifiers never stand in for evidence.

Keep compiler-version adaptation, semantic representations, Go generation,
and host validation independently understandable and testable. Shared findings
can become upstream fixes, compatibility tests, or documentation contributions.
Public review of useful releases and their evidence is the basis for building
maintainer trust; no particular external project's adoption is a prerequisite
for producing this first release.

## Public references and alternatives considered

- [Lean elaboration and compilation](https://lean-lang.org/doc/reference/latest/Elaboration-and-Compilation/)
  distinguishes kernel checking from executable compilation.
- [Lean axioms and dependency reporting](https://lean-lang.org/doc/reference/latest/Axioms/)
  describe the foundational axiom basis and transitive proof-dependency audit.
- [Lean LCNF compiler phases](https://lean-lang.org/doc/api/Lean/Compiler/LCNF/CompilerM.html)
  document base, monomorphic, and impure stages. Stage suitability remains a
  project feasibility result rather than an upstream promise.
- [Lean 4.30 release notes](https://lean-lang.org/doc/reference/latest/releases/v4.30.0/)
  describe completion of the LCNF compiler backend.
- [lean4-prod](https://github.com/auser/lean4-prod) is prior art for extracting
  ordinary Lean through LCNF and generating Rust. Its supported subset and
  semantic choices require inspection before reuse.
- [LeanPort](https://github.com/shrsv/LeanPort) demonstrates Go integration via
  native compilation and cgo. That deployment path does not satisfy this
  project's pure-Go consumer contract.
- [Trust-Lean](https://github.com/lambdaclass/trust-lean) is prior art for a
  reusable DSL/compiler IR architecture. Requiring users to author in a DSL
  would change this project's ordinary-Lean contract; theorem coverage must
  be assessed from actual statements rather than repository summaries.

The chosen route is a restricted ordinary-Lean extractor. Native/cgo bindings
and an embedded authoring DSL remain separate approaches, not hidden fallbacks
when an unsupported program is encountered.
