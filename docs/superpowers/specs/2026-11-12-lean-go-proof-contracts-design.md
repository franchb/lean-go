# Reusable source-proof contracts

The source-proof gate must work for another library without copying quota-specific
checker code. It will retain fresh pinned Lean builds, exact expected theorem
types, a fixed foundational axiom policy and source/generated-file hash binding.
The extractor, Go runtime and library APIs keep their existing behavior.

## Contract and project boundary

A strict JSON object with schema `lean-go-proof-contract/v1` contains exactly
`schema`, `source_dir`, `proof_dir`, `module`, `roots` and `claims`. A claim maps
`theorem` to `expected`, both fully qualified ASCII Lean names. Roots are unique
and canonicalized; one to 64 roots and one to 64 distinct theorem claims are
accepted. No custom executable hooks, configurable axioms or external packages
are supported.

Source/proof directories are canonical relative paths under the contract file's
parent. Both are snapshotted as regular Lean files with existing file/byte bounds;
their combined inventory must also fit those bounds. Symlinks, overlapping module
names and reserved modules fail. Existing caches and project build scripts are
ignored. Tool sources come from the lean-go checkout, independently of the user
project, so external projects need only their runtime, proofs and contract.

Expected propositions are independently authored Lean `def`s of type `Prop`.
They must be reviewed as the intended requirements. The tool never derives an
expected proposition from the theorem it is checking. Polymorphic declaration
parameters are outside this initial proof-checker contract.

## Fresh checking and evidence

A trusted shared Lean checker and generated harness import and rebuild all
snapshotted runtime/proof modules in a fresh dependency-free workspace. Each
claim must name a theorem whose type is definitionally equal to the specified
closed proposition definition. Transitive axioms are restricted to `propext`,
`Classical.choice` and `Quot.sound`. Missing declarations, weaker statements,
admitted proofs, custom axioms and compiler-trust axioms fail explicitly.

The manifest must describe the selected module and canonical roots with the
pinned Lean identity. Single roots use existing v6 IR/profile v4 and v6 manifests;
multiple roots use v7 IR/profile v5 and v7 manifests. Runtime source input hashes
must match exactly, including inventory. The generated directory must contain
exactly its manifest and the four generated Go files, whose hashes are checked.
Inputs and binding are checked again after Lean finishes.
Workspace source/configuration/checker bytes are checked around compilation;
the generated harness is staged after project compilation and checked after
execution. These checks do not sandbox arbitrary Lean compile-time code, which
remains a trusted part of elaboration.

Deterministic `lean-go-proof-evidence/v1` records contract, project and trusted
tool input hashes, Lean identity, module/roots, generated manifest hash, theorem
names, expected-definition names, checked statements and axiom dependencies.
It contains no machine-specific absolute paths. The evidence establishes source
statements; compilation/extraction and Go execution remain tested and trusted.
Ordinary module IR claims remain unchanged.

## Commands and compatibility

The new command is `uv run --locked python scripts/check_proofs.py --contract
FILE --manifest FILE`, followed by exactly one of `--evidence FILE` to freshly
verify existing evidence or `--output FILE` to publish checked evidence. Failed
checks preserve existing output. Outputs cannot replace contract/source/proof/tool
inputs or appear anywhere inside the generated package.

`scripts/check_quota.py` remains a small wrapper with its existing `--root`,
`--manifest` and `--output` options. Quota gains an explicit JSON contract and a
separate proposition module, and migrates its evidence to the shared schema.
Its runtime source and generated Go bytes stay unchanged.

## Validation

Quota exercises multi-root contracts. A small ordinary `UInt64` clamp fixture
exercises single-root contracts, an external project directory, fresh extraction,
proof checking and a pure-Go consumer. Protocol and real Lean tests cover schema,
path/inventory/resource limits, theorem and expected-definition validation,
axiom rejection, source/file mutations, stale evidence, output protection and
deterministic fresh checks. Existing quota model/native/regeneration tests remain
in force. Python stays on 3.15 through `uv`; Go consumers use `CGO_ENABLED=0`.
Broad generic collections remain deferred.
