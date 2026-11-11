# General Lean modules with pure Go output

This extends the approved extraction architecture beyond the audited normalizer.
Users supply an ordinary Lean module name, a source directory, and one exported
function name. Generation runs in a fresh producer workspace. Consumers need only
Go and the generated package, with `CGO_ENABLED=0`.

## Language boundary

The new `pure-module/v1` profile uses `lean-go-ir/v2`. It accepts monomorphic
first-order functions over nonempty, non-parametric, non-indexed inductive types, including
enums and recursive sums. `Bool` is an inductive type; `UInt32` is an opaque scalar
that can be passed through fields and functions. Runtime code consists of typed,
saturated constructor/function applications, exhaustive constructor cases, and
returns. Self recursion is supported. Mutual function recursion, arithmetic,
polymorphism, higher-order values, IO, externs, unsafe/opaque dependencies,
empty inductives, dependent data, projections, local join points, and other unsupported compiler
forms receive explicit errors. A valid Lean program is not necessarily extractable.

There is no source hash allowlist for user modules or hardcoded application names.
The emitted function set is the exact reachable call closure. Type declarations
describe all transitively used field types. One invocation exports one root;
another root is a separate generated package. No root correctness theorem is
inferred: the new profile records no application proof claims. The existing v1
pilot and its checked claims retain their original meaning.

## Compilation-time provenance

A trusted Lean audit module wraps `compileDeclsRef` before `toDecl`. Every fresh
local source module imports it before its own declarations. The audit records
original declaration dependencies and active compiler simplification rules in a
persistent environment extension. Compilation is immediate rather than postponed.
Admission follows original source dependencies, including helpers later inlined
away, as well as the resulting runtime closure. Missing local provenance fails. Final extern/replacement/initialization and CSimp
attributes are also checked, including attributes attached after compilation.

This initial general profile rejects user compiler rewrites. The pinned built-in
`Bool.not -> Bool.Internal.not` rewrite is the only permitted rewrite, with its
exact theorem and audited axiom set. Historical local/scoped attributes cannot
be ignored merely because the final imported environment no longer contains them.
Source dependencies with unsupported extern/replacement/unsafe/axiom behavior,
custom compiler passes, and unaccounted imported runtime bodies are rejected.
The pinned source-support boundary additionally includes `Unit` and `Unit.unit`,
which occur in compiler-generated enum eliminators, and the pinned `Bool.casesOn`
recursor wrapper. Administrative aliases are not admitted runtime types. Pinned compiler and standard-library support remains trusted.

The producer copies source files into an isolated fresh workspace, ignoring user
build scripts and compiled caches. Its import policy permits included local files
and supported pinned standard-library modules; missing local source is an error.
The producer executes Lean elaboration and is intended for trusted source, not as
a sandbox for hostile macros or initializers. Input hashes provide traceability,
not cryptographic authentication of an arbitrary IR file.

## Go boundary and implementation

The v2 backend lives in separate `internal/moduleir` and `internal/modulegen`
packages. The old v1 profile remains usable. Its JSON decoder rejects duplicate,
unknown, missing, null, malformed, unbounded, and incorrectly typed payloads.
Independent validation checks names, constructors, signatures, lexical scopes,
exhaustiveness, closure, and the admitted recursion graph.

Generated packages use a schema-checked immutable `Value` API rather than deriving
Go identifiers from arbitrary Lean names. Callers construct `Input` trees, import
them against a Lean type name, and invoke `Run` with the root's argument values.
`Input.Type` carries the expected Lean type name.
Constructor/type names and fields can be inspected without exposing mutable
storage. The manifest documents the root signature and constructor schema.
The source input may not be mutated during import; later mutation has no effect.
Cycles fail, DAG sharing is copied and charged per occurrence, and zero values
are invalid. Errors distinguish malformed data, invalid limits, and exhaustion.

All input traversal and self-recursion use explicit stacks. Node, depth, output,
allocation, step, frame, and cumulative frame-slot budgets are enforced with conservative finite caps.
`Import` uses node/depth budgets; `Run` additionally uses the other five budgets,
including cumulative frame slots (default maximum 1,048,576). All limit fields
are validated by both calls. Failure yields no usable partial result. Output subtree reuse is charged per
occurrence. Control flow is generated Go code with function-specific states,
not an IR interpreter. The output depends only on the Go standard library.

## Python and verification

Pin the Python minor version to 3.15 with `.python-version`, `pyproject.toml`, and
`uv.lock`. Use `uv run --locked python` for producer scripts and Python tests;
dependencies, if introduced, are managed by uv. The current implementation uses
the standard library. Generated consumers do not depend on Python or uv.

Acceptance includes independently named ordinary modules, a changed implementation
with no allowlist update, recursion and enum/scalar roots, local multi-module
imports, negative provenance/unsupported-feature fixtures, reproducible generation,
native Lean comparisons, strict malformed IR tests, boundary/resource tests, and
a standalone Go-only consumer with cgo disabled. Preserve the existing pilot tests.
Linux execution and Windows cross-build evidence must be distinguished from actual
Windows execution. This is tested, trusted extraction, not verified compilation.
