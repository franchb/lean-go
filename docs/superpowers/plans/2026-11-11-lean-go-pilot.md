# Audited Lean to Go Pilot Implementation Plan

> Use subagent-driven development with independent specification and quality
> review. Numbered steps describe implementation; execution status lives in the
> private task tracker, never in this public document.

**Goal:** Generate and execute a standalone pure-Go normalizer from the exact
ordinary Lean program established by the feasibility investigation.

**Architecture:** A fresh pinned Lean producer exports a closed typed JSON IR.
An independent Go validator admits that IR, and a source emitter compiles it
into typed Go control flow with bounded continuation frames. A generated host
boundary copies valid input into owned immutable values before running that code.

**Tech stack:** Lean 4.35.0, Go 1.27.4, Python 3.11+ orchestration, standard
libraries only. No consumer dependency on Lean, Python, C, or a deployed IR
interpreter. The producer is tested on Linux/amd64; Windows execution remains
a release gate until actually performed.

**Outcome (2026-11-11):** The audited local pilot is implemented and verified.
The [pilot evidence report](../../research/extraction-pilot.md) records the
actual generated package, commands, semantic and resource tests, fresh-build
reproducibility, isolated Go-only consumer, and measured benchmarks. Windows
runtime and tagged-release consumer gates remain pending. The numbered steps
below retain the approved implementation contract.

## Authority and scope

This implements the [approved design](../specs/2026-11-11-lean-go-extraction-design.md)
using the [feasibility result](../../research/extraction-feasibility.md).
The user selected **audited example first** for this milestone. Accept only
`LeanGo.Feasibility.Model` at SHA-256
`c67a835949d1786638d2809c970ef69861a074a23cf6fdbcae3f8cad1043771f`.
Changed model bytes and arbitrary source modules are rejected. This is ordinary
Lean extraction of one reviewed program revision, not generic source admission.
The IR and emitter must derive behavior from the captured bodies; a handwritten
normalizer or copied expected output is not an extraction implementation.

The existing feasibility package, source proofs, and recorded baseline are
preserved. New producer files live separately. No GitHub publication, tag,
release claim, new language features, general compiler fork, or native fallback
is part of this milestone. Public content and history remain independent;
author and committer are `franchb <hello@franchb.com>`.

## Producer and proof boundary

Use a new temporary Lake workspace for every export. Copy only the exact model
and fixed exporter sources into it; create a fixed dependency-free Lake TOML
configuration and pinned lean-toolchain. Do not reuse repository `.olean` files,
load arbitrary user modules, accept Lake scripts, or install user compiler
passes. Verify the actual Lean version and commit before compilation. Toolchain
Std/Lean libraries and the producer are trusted execution inputs.

The model's original kernel declarations must be safe and have the exact two
claim types. Check that claims are theorem declarations and definitionally equal
to explicit expected statement types; audit their transitive axioms. Require
both actual claim lists `[propext]`. Require the reviewed compiler rewrite
`Bool.not -> Bool.Internal.not` justified by `Bool.not_eq_internalNot` with
`[Quot.sound]`; require theorem kind and audit its actual type and dependencies.
Reject unexpected source extern/implemented_by/CSimp changes in the reviewed
source closure. Fixed model and compiler scope are the provenance restriction;
final-environment inspection alone is not a general history proof.

The exporter retrieves actual `.code` pure-mono bodies and exhaustive original
constructor metadata. It checks safe declarations, signatures, every code/value/
argument/alternative form, constructor saturation, and variable scope. The only
code functions are `normalize`, `normalizeAux`, and `Tri.neg` under
`LeanGo.Feasibility`. The only self recursion is in `normalizeAux`.

## JSON IR contract: `lean-go-ir/v1`

Use the following Go model as the interface contract (all JSON field spellings
are normative). The complete file must be valid UTF-8, at most 4 MiB, with no
unknown fields, duplicate object keys, trailing JSON, or more than 10,000 IR
nodes / 256 nested code levels. All variable IDs are positive, unique per
function, and assigned deterministically from original FVarIds. Let RHSs see
earlier variables only; case parameters are local to their alternative.

```go
type Param struct { ID int `json:"id"`; Type string `json:"type"` }
type Constructor struct {
    Name string `json:"name"`; Tag int `json:"tag"`; Fields []string `json:"fields"`
}
type TypeDecl struct {
    Name string `json:"name"`; Constructors []Constructor `json:"constructors"`
}
type Claim struct {
    Name string `json:"name"`; Statement string `json:"statement"`; Axioms []string `json:"axioms"`
}
type Rewrite struct {
    Source string `json:"source"`; Target string `json:"target"`
    Theorem string `json:"theorem"`; Axioms []string `json:"axioms"`
}
type Value struct {
    Kind string `json:"kind"`; Name string `json:"name"`; Args []int `json:"args"`
}
type Alt struct {
    Constructor string `json:"constructor"`; Params []Param `json:"params"`; Body *Code `json:"body"`
}
type Code struct {
    Op string `json:"op"`
    Bind *Param `json:"bind,omitempty"`; Value *Value `json:"value,omitempty"`; Next *Code `json:"next,omitempty"`
    Var int `json:"var,omitempty"`; Type string `json:"type,omitempty"`; Result string `json:"result,omitempty"`
    Alts []Alt `json:"alts,omitempty"`
}
type Function struct {
    Name string `json:"name"`; Params []Param `json:"params"`; Result string `json:"result"`; Body *Code `json:"body"`
}
type Program struct {
    Schema string `json:"schema"`; Profile string `json:"profile"`; Stage string `json:"stage"`
    LeanVersion string `json:"lean_version"`; LeanCommit string `json:"lean_commit"`
    ModelSHA256 string `json:"model_sha256"`; Root string `json:"root"`
    Inputs map[string]string `json:"inputs"`
    Types []TypeDecl `json:"types"`; Claims []Claim `json:"claims"`; Rewrites []Rewrite `json:"rewrites"`
    Functions []Function `json:"functions"`
}
```

Fixed metadata: schema `lean-go-ir/v1`, profile `audited-normalizer/v1`, stage
`pure-mono`, Lean version `4.35.0`, commit
`eb6cafb7290e8bd5f2937522d8d804f58f02bafc`, and the audited model hash above.
Root is `LeanGo.Feasibility.normalize`. Input keys are normalized relative
producer paths, values lowercase SHA-256; include `feasibility/LeanGo/Feasibility/Model.lean` equal to the audited
model digest and every producer source/configuration. The input key set is exactly:
`feasibility/LeanGo/Feasibility/Model.lean`, `producer/lean-toolchain`,
`producer/lakefile.toml`, `producer/Export.lean`,
`producer/LeanGo/Pilot/Export.lean`, `producer/Reference.lean`, and
`scripts/export.py`. Hash the actual byte snapshots copied into the fresh build.
The generated dependency lock is checked for an empty package set; the fixed
Lake configuration is itself a tracked hashed input.
Decode validates the exact key set/hash syntax and model equality. It cannot
authenticate arbitrary producer metadata. Before package publication,
`scripts/generate.py` independently recomputes each tracked input hash and rejects
any mismatch with freshly exported IR; stale-source tests exercise this rule.
The standalone IR-to-Go CLI validates IR, not current source files, and documents
that narrower boundary. No timestamps, absolute paths, or ephemeral identifiers.

Type keys are `bool`, `tri`, `expr`, `u32`. Three TypeDecl entries describe
`bool`, `tri`, `expr`; u32 is the fixed opaque scalar, not an inductive to lower.
Constructor names are full Lean names. Bool has false/true tags0/1, no fields;
Tri has f/u/t tags0/1/2, no fields. Expr lit/var/neg/conj/disj tags0/1/2/3/4 have
fields `[tri]`, `[u32]`, `[expr]`, `[expr,expr]`, `[expr,expr]` respectively.
All original type parameters/indices/universes are absent for admitted types.

There are exactly two claims, named
`LeanGo.Feasibility.normalize_preserves_eval` and
`LeanGo.Feasibility.normalize_is_normal`, each with exactly `[propext]`.
There is exactly one Rewrite: source `Bool.not`, target `Bool.Internal.not`,
theorem `Bool.not_eq_internalNot`, axioms exactly `[Quot.sound]`.
This records the reviewed source/compiler substitution; `Bool.Internal.not`
is not an additional runtime primitive in the admitted IR.
Duplicate or unrelated claims/rewrites are errors.
Statements are exact stable contract text after kernel type checks:

- `forall e : Expr, forall rho : UInt32 -> Tri, eval rho (normalize e) = eval rho e`
- `forall e : Expr, IsNormalForm (normalize e)`

Function signatures: normalize `(expr)->expr`; normalizeAux `(bool,expr)->expr`;
Tri.neg `(tri)->tri`. Functions are sorted by full name. Constructor order and
alternative, argument, and instruction order are preserved. Recompute closure
and require all three functions reachable, no unresolved names or extra code.

Code discriminants have exact payloads (irrelevant fields are errors):

- `let`: bind, value, next. Value kind is `constructor` or `call`; typed/saturated
  arguments are previously defined variable IDs. Only explicit named calls.
- `cases`: var, type (discriminant), result, alts. Exhaustive unique alternatives
  for that type, ordered typed field binders, every branch returns result.
- `return`: var, with the enclosing function result type.

Reject every other LCNF construct, including erased values, literal values,
projections, fvar aliases/applications, partial application, locals, joins,
defaults, unreachable nodes, effects, and external bodies. The Go validator
independently checks all of these structural/type/profile obligations. Proof
metadata is traceability from the trusted exporter, not proof of this JSON's
translation or resistance to a malicious producer.

## Generated consumer API and resource contract

Emit package `normalizer`, standard library only. Public surface:

```go
type Tri uint8
const (False Tri = iota; Unknown; True)
type Kind uint8
const (Invalid Kind = iota; Literal; Variable; Not; And; Or)
type Input struct { Kind Kind; Value Tri; ID uint32; Left, Right *Input }
type Limits struct {
    MaxNodes, MaxDepth, MaxOutputNodes, MaxAllocations, MaxSteps, MaxFrames uint64
}
func DefaultLimits() Limits
func Import(input *Input, limits Limits) (Expr, error)
func Normalize(input Expr, limits Limits) (Expr, error)
type Expr struct { /* unexported immutable node reference */ }
func (e Expr) Kind() Kind
func (e Expr) Literal() (Tri, bool)
func (e Expr) Variable() (uint32, bool)
func (e Expr) Children() (Expr, Expr)
var ErrMalformed, ErrResource, ErrLimits error
```

`Expr{}` is invalid. A false literal is explicitly constructed as Input Literal
with False. Import validates tags, required/forbidden children, Tri values, and
inactive payload fields. The caller must not mutate Input during Import; later
changes cannot affect any Expr. Iterative active-path detection rejects cycles;
acyclic sharing is copied per occurrence, charged before expansion, with no
memoization. Record owned size/depth for bounded later reuse. Node fields remain
unexported; all accessors expose immutable handles or scalar copies.

Default and maximum permitted policy caps (smaller positive limits allowed):
65,536 input occurrences; depth4,096 with root depth1; 131,072 output occurrences;
65,536 fresh generated Expr allocations; 4,194,304 generated control steps;
8,192 simultaneous generated frames. Zero or above-cap fields return ErrLimits.
These are pilot policy caps, not measured production recommendations. Import
checks node/depth before expansion; Normalize rechecks owned size/depth against
its limits. Every generated state consumes work before execution, every frame
push and allocation checks its budget before taking effect. Result occurrence
size/depth is computed from immutable node metadata, including reused children;
MaxOutputNodes counts reachable occurrences, not fresh allocations. Errors
return an invalid Expr, never a partially normalized result. Counters use
checked bounds before addition/subtraction, never wrapping arithmetic.

## Direct Go control flow

Generate typed local fields and typed function signatures from IR, with stable
names independent of source binder spelling. Nonrecursive direct calls may use
ordinary Go calls. For the self-recursive function, emit a typed frame struct
with named local slots and a resume state, plus a bounded slice of frames.
Flatten the lexical Code tree to compiler-assigned states. Each emitted state
contains direct Go assignments, constructors, switches, calls or returns.
At a self call: save the next state, check frame budget, and push the typed
argument frame. On return: pop into a typed result variable; a statically emitted
resume state assigns that result into the original destination. Preserve left
then right processing exactly. Do not embed IR/opcodes or a generic dispatch
interpreter in the generated package. A test-only IR interpreter is separate.

Only constructor/type representation and resource/ownership helpers are reusable
templates; normalization control flow and Tri.neg must come from actual IR.
Constructor generation uses audited schema tags and fields, never a C object ABI.

## Files and ownership

| Files | Responsibility |
| --- | --- |
| `go.mod` | Module github.com/franchb/lean-go, Go1.27.4, no dependencies |
| `internal/ir/{types,decode,validate,identity}.go` and tests | Strict typed boundary, profile and source identity |
| `producer/LeanGo/Pilot/Export.lean`, `producer/Export.lean` | Original typed exporter and fixed kernel checks |
| `producer/Reference.lean` | Native batch reference over the same fixed model |
| `producer/lean-toolchain`, `producer/lakefile.toml` | Fixed dependency-free producer configuration |
| `scripts/export.py` and tests | Input pin, clean temporary build, deterministic export publication |
| `internal/gogen/{emit,publish,identity}.go`, templates and tests | Source emission, consumer API, complete output validation, preservation on failure |
| `cmd/lean-go/main.go` | IR-to-package CLI; no Lean dependency |
| `scripts/generate.py` | Fixed producer + Go CLI composition and source identity |
| `generated/normalizer/` | Tracked generated Go files and manifest; never hand-edit |
| `testdata/normalizer.ir.json` | Real producer output for independent validator/emitter tests; regenerate, never hand-edit |
| `internal/irtest/` | Bounded test-only IR interpreter, excluded from consumer |
| `integration/` | Independent truth tables/evaluator, enumeration/fuzz/boundary and pipeline tests |
| `docs/research/extraction-pilot.md`, README | Observed commands, results, limits and remaining release gates |

Separate implementation tasks may edit disjoint owned paths against this frozen
contract. Coordinate changes to interfaces before writing consumers. Stage and
commit only owned files; never include build logs/raw temporary sources.

## Native reference and command interfaces

`scripts/generate.py --out DIR --ir-output FILE` defaults to
`generated/normalizer` and `testdata/normalizer.ir.json`. Export into an owned
temporary file, run the Go CLI, and publish the IR fixture only after generation
succeeds. All paths in manifests remain relative identifiers, not command paths.

The producer also supplies `scripts/export.py --reference-input FILE
--reference-output FILE` alongside required `--output IRFILE`. This optional
mode builds and invokes the native `normalizer-reference` Lake executable in
the same fresh workspace. Tests supply at most 1,024 shallow expressions, 1 MiB
input, and depth64. The binary reads a JSON array of trees and writes one result
per input: `{ "normal": TREE, "values": [TRI,...] }`. Values are eval of the
original expression under the nine valuations for variable IDs0 and1, ordered
by value0 outer and value1 inner in f/u/t order; other IDs map to unknown.
Compare exact normalized shape plus values in Go tests. The tree codec is:
`{"tag":0,"tri":0}`, `{"tag":1,"id":4294967295}`,
`{"tag":2,"left":TREE}`, or binary tags3/4 with left/right TREE. This codec
is test/producer-only and does not become the consumer API. Reject invalid
shapes/tags/ranges; do not coerce UInt32 through floating point. Input and output
are explicit files; the executable does not import or execute client code.

## Task 1: Strict IR and independent admission

1. Implement the exact structs/constants and `Decode(io.Reader) (*Program,error)`
   with byte limit, duplicate-key rejection, unknown-field rejection, EOF check
   and nesting limits. Decode calls `Validate(*Program) error`.
2. Build a tiny explicit valid test Program with the three signatures and full
   schemas. Test missing/extra fields, wrong types/calls/arity, wrong claims,
   duplicate binders/constructors, let self-reference, sibling-scope leakage,
   bad returns, unresolved names, extra code, recursion cycles, oversize/deep
   input, duplicate JSON keys and trailing data. All must fail before emission.
3. Verify constructor/call type checks and exact closure on the actual exported
   fixture once Task2 produces it. Do not equate safe source spelling with IR
   validity. Bound all producer-side traversals.
4. Embed source identities deterministically for manifest freshness. Run
   `CGO_ENABLED=0 GOTOOLCHAIN=local go test ./internal/ir` and `go vet` there.

## Task 2: Audited model export

1. Write the original exporter using actual compiler fields/FVarIds, original
   constructors and `.code` mono functions. Check full function arrow types
   against Param types (Decl.type is the entire signature). Enumerate code
   exhaustively with wildcard rejection. Emit stable JSON using the contract.
2. Add explicit expected theorem-type witnesses; require theorem kind and exact
   types plus actual axiom lists. Audit the reviewed source substitution and
   constructor types. Keep producer IO outside the exported closure.
3. Implement `python3 scripts/export.py --output PATH`. Verify exact model and
   toolchain before compilation; fresh temp build every run, dependency-free
   fixed imports/config. Add input hashes after successful export. Output is
   written only after valid complete JSON exists; failures preserve destination.
4. Run twice into temporary files and compare bytes. Check modified model,
   unexpected config, wrong version and failed Lean process are rejected, with
   useful diagnostics and owned temp cleanup. No source fallback or auto-upgrade.

## Task 3: Pure-Go emitter, boundary and safe publication

1. Implement `gogen.Generate(*ir.Program) (map[string][]byte,error)`. Always
   Validate first. Generate typed direct code from every admitted IR instruction,
   then format and parse every Go file. Emitted control flow changes when a
   valid IR branch mapping changes; no constant normalizer blob is permitted.
2. Generate Import/Expr/Limits and budget helpers from templates. Test every
   malformed shape, cycle versus sharing, immutable ownership, UINT32 endpoints,
   nil/zero handles and each resource budget below/at/above its limit.
3. Implement the CLI `go run ./cmd/lean-go -in FILE -out DIR`. Package name is
   fixed normalizer for the pilot. Complete output includes source/profile/IR,
   proof-statement/axiom, schema/mapping, generator source and generated-file
   digests, and intended target matrix. Manifest records evidence identity, not
   a claim that unrun platform or correctness tests passed.
4. Publish a fully staged package using rename when destination is absent.
   If destination already exists, require its complete bytes/file set to match
   the newly generated package and succeed without changes; otherwise fail and
   preserve it. Reject symlinks and cooperating concurrent publishers using an exclusively
   created sibling lock directory, owned cleanup, and a final destination check.
   Do not remove another invocation's lock. The destination parent must not be
   mutated by unrelated filesystem writers during publication; os.Rename is not
   a portable no-replace primitive against such writers. This pilot
   deliberately refuses differing existing output; transactional upgrades of
   existing packages remain a later release capability. Never recursively
   delete an unknown destination. Regeneration failure leaves the prior package.
5. Test deterministic output, invalid IR, name collision/sanitization, malformed
   metadata, unsafe destinations, stale/extra output files, same-output no-op,
   and failed publication. Build generated source with cgo disabled.

## Task 4: End-to-end semantics, ownership and reproducibility

1. Compose exporter and CLI in `scripts/generate.py`; capture actual errors,
   record stable input/code identities, create tracked generated package from
   the real Lean output. Never edit generated control flow by hand.
2. Implement a bounded test-only IR evaluator independent of Go emission. Test
   constructor/case/call semantics and compare it with generated output on the
   same corpus. The consumer imports none of this interpreter or IR loader.
3. Independent Go oracle uses explicit strong-Kleene 3x3 tables and an evaluator
   over Input. Enumerate all expressions through depth2 over three constants
   and two variables (7,265 expressions), all nine valuations, then add seeded
   larger/deeper/random cases, UINT32 boundaries, double/triple negation and
   ordered De Morgan expectations. Check evaluation, normal form and order.
   Also compare a bounded batch corpus with native Lean reference execution.
4. Mutate a primitive mapping or IR branch in a temporary generated package:
   the conformance corpus must detect it. Reject stale model/proof/schema/input
   digests and unsupported reachable IR before output changes. Keep the
   feasibility's independent proof-mutation controls.
5. Run consumer semantic/boundary tests under CGO_ENABLED=0 on Linux/amd64,
   race-test producer/ownership paths where applicable, fuzz for a bounded
   reproducible duration, and measure representative deep/wide/shared cases
   to report observed allocations/time without claiming production suitability.
6. Copy only generated Go files into a temporary standalone module plus a tiny
   consumer, omit Lean/Python/C from its command environment, and execute it with
   Go alone. Cross-build Windows/amd64 and label this build-only until real
   Windows execution is available. No tagged-release assertion.
7. Regenerate twice from fresh producer workspaces and compare all output bytes,
   file sets and manifest digests. Verify changed input/failure cannot alter the
   last successful package. Run `go test ./...`, `go vet ./...`, appropriate
   producer tests and unchanged Lean checks, `git diff --check`, and public
   content/history checks. Review source and documentation independently.

## Task 5: Evidence and handoff

Record observed results in the pilot report and update README with the real
command/API. Preserve source-proof versus trusted-extraction distinction;
record exact runtime limits and caller mutation rule, the one audited source
revision, output-update refusal, and unexecuted Windows/tagged-release gates.
No performance claim without measurement and no end-to-end verification claim.
Commit the completed local milestone using the required public identity;
publication is separate. Close only verified task records and keep all private
tracking out of the public repository.
