# Ordinary Lean modules to pure Go

The module profile extends the audited normalizer pilot to user-supplied source
files, module names, root functions, and data schemas. It uses `lean-go-ir/v2` and
`pure-module/v1`; the old v1 validator, generator, model and proof claims remain
separate. There is no user-source hash allowlist. This remains local experimental
software with no published release.

## Reproduce

Use Go 1.27.4, Lean 4.35.0 at commit
`eb6cafb7290e8bd5f2937522d8d804f58f02bafc`, and Python 3.15 through uv. Python 3.15.2
and uv 0.13.5 were used locally. `.python-version`, `pyproject.toml` and `uv.lock`
select the Python minor; dependencies are currently standard-library only.

```sh
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Trees --root Trees.mirror \
  --out generated/trees --ir-output testdata/trees.ir.json
uv run --locked python scripts/generate.py \
  --source-dir examples/modules --module Choice.Transform --root Choice.rotate \
  --out generated/choice --ir-output testdata/choice.ir.json
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
```

Generation refuses a differing existing package. Use a fresh `--out` directory
and a separate `--ir-output` when inspecting changes. Output paths cannot overwrite
source files or producer inputs. The low-level Go CLI also accepts either IR
schema, but only the source-generation command compiles and audits Lean inputs;
validating supplied JSON does not authenticate its origin.

## Accepted source and compiler boundary

The root can take zero or more first-order arguments and return an admitted data
type. Data consists of nonempty monomorphic inductives without type parameters,
indices or universe parameters, including enums and recursive sums. `Bool` uses
its constructor schema. `UInt32` is an opaque scalar: values can pass through
functions and fields, with no arithmetic primitives. Functions can construct,
match, call one another and recurse on themselves. A mutually recursive function
cycle is rejected. A single invocation exports one root and its exact runtime
closure into one package named `extracted`.

The compiler's actual pure-mono bodies must use the admitted `let`, exhaustive
constructor `cases`, and `return` forms, with saturated direct calls and variable
arguments. Polymorphism, higher-order values, dependent data, empty inductives,
arithmetic, local joins/functions, projections, partial applications, effects and
other forms are unsupported. Source syntax alone does not determine admission;
Lean may lower an apparently simple function to an unsupported form.

The producer snapshots up to 256 local `.lean` files and 8 MiB of source, validates
module names and imports, and builds fresh objects from copied sources. It does
not use the source project's Lake scripts, dependency setup or cached objects. Pinned Lean/Init/Std imports can be resolved, but runtime dependencies
still have to pass the narrow source audit. Thus importing Std does not make all
Std functions extractable. Source elaboration and metaprogramming are trusted
local execution; the producer is not a sandbox for hostile Lean code.

An injected audit module wraps `compileDeclsRef` before `toDecl` performs compiler
rewrites. It records original dependencies and active CSimp mappings in a
persistent environment extension, with immediate compilation. Export follows
those source records as well as the optimized function closure. Final attributes
are also checked, so a forbidden attribute added after compilation cannot bypass
the earlier audit. This includes
helpers that were compiled under local/scoped rules and later inlined away.

User CSimp rules are rejected even when their equality theorem is valid. The
only admitted rule is `Bool.not` to `Bool.Internal.not`, witnessed by
`Bool.not_eq_internalNot` with exactly `Quot.sound`. Its statement and axiom set
are checked. `Bool`, `UInt32`, `Unit`, `Unit.unit` and the pinned Boolean eliminator
support form a narrow source boundary; administrative aliases are not admitted
as runtime types. Externs, implemented-by replacements, unsafe or partial source
definitions, unaccounted opaque dependencies, axiomatic source proofs, custom
compiler passes, and recursive implementation overrides fail admission.

The new profile asserts no application correctness theorem. Lean typechecks the
source and the producer audits this extraction boundary; the adapter, compiler
passes, Go emitter and runtimes remain trusted. This is not a proof of compiler
correctness or an automatic transfer of arbitrary Lean theorems to Go.

## Generated values and resource limits

The generated API uses `Input{Type, Constructor, UInt32, Fields}` and immutable
`Value` handles. Full Lean names avoid Go naming collisions. `Import` validates
field types and copies each occurrence. Cycles are rejected; sharing in a DAG is
copied per occurrence, bounded before expansion. Input must not be mutated during
import, but later mutation cannot alter the result. A zero Value is invalid.
Accessors return scalar data or fresh slices of immutable handles.

`Run([]Value, Limits)` checks the selected root's arity and argument types. Each
function's operations are emitted as actual Go statements in an explicit stack
machine; no runtime bytecode, JSON IR interpreter, native Lean runtime or C code
is deployed. Helper calls also use the explicit stack.

| Limit | Default maximum | Charged by |
| --- | ---: | --- |
| MaxNodes | 65,536 | Import occurrences; Run arguments collectively |
| MaxDepth | 4,096 | Import and Run input/output depth |
| MaxOutputNodes | 131,072 | Run output occurrences, including reused subtrees |
| MaxAllocations | 65,536 | Run constructor allocations |
| MaxSteps | 4,194,304 | Run generated control states |
| MaxFrames | 8,192 | Run simultaneously active calls |
| MaxSlots | 1,048,576 | Run cumulative allocated frame slots, including returned calls |

All seven fields must be positive and no greater than their caps in both calls.
Import uses node/depth budgets; the other five apply to Run. `ErrMalformed`,
`ErrLimits` and `ErrResource` distinguish input, configuration and exhaustion
failures. Errors return an invalid handle without a usable partial result.

The strict IR decoder rejects malformed Unicode, duplicate/unknown/missing/null
fields and trailing input. Independent validation checks lexical scopes, types,
exhaustiveness, source metadata and exact function/type closure. Limits include
4 MiB JSON, 10,000 structural items, code depth 256, 256 functions/types,
256 constructors per type, arity 64 and names of at most 512 UTF-8 bytes.

## Evidence and limits

`Trees.mirror` extracts two functions and two data types; `Choice.rotate` extracts
one function and one enum from an imported module. The integration suite checks
all 147 colored trees through depth two and 500 seeded larger trees. The same 647
cases are compared with a separately compiled native Lean reference. Renaming the
entire tree module and changing red-to-blue behavior to red-to-green produces and
executes changed Go without changing any source admission list. A separate scalar
root checks zero, one and the maximum UInt32 value.

Producer regressions cover modern module imports, preserved Boolean rewrite
records, nullary/multiple-argument roots, local and scoped rewrite rejection,
imported inlined helper history, extern/replacement/unsafe/partial boundaries,
arithmetic and polymorphism, erased forged proofs, custom passes and recursive
implementation overrides. Failed admission preserves the previous output.

The complete Python suite passed 37 test methods under uv and Python 3.15,
including 15 rejected-source subcases and five fresh-module test methods.
The full Go suite passed with all native, regeneration and module gates enabled;
Go vet also passed. Independent specification and code-quality reviews passed.

Backend tests cover malformed IR, compiler-input mutation, wrong arity/types,
immutable ownership, cycles, exponential sharing, deep trees, every resource
limit and output reuse. A standalone consumer runs with PATH restricted to Go,
module fetching disabled, and `CGO_ENABLED=0`. Repeated fresh builds are compared
against each other and tracked artifacts. Decoder fuzzing after the Unicode fix
completed 212,088 executions without a failure.

Linux/amd64 execution is the local acceptance target. Windows/amd64 cross-builds
do not establish Windows execution; actual Windows runs and tagged-release
consumer validation remain outstanding release gates. The earlier feasibility
and pilot reports describe their original, narrower milestones.
