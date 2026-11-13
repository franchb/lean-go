# Reusable source-proof contracts

A proof contract lets a Lean library use the same checked-evidence workflow as
the [quota library](quota.md). It works with single-root v6 and multi-root v7
generated packages, plus the bitwise v8/v9 packages introduced in alpha.6.
Alpha.7 also accepts exact v10/`pure-module/v8` single-root and
v11/`pure-module/v9` multiple-root
[width-conversion packages](research/extraction-width-conversions.md).
The checker is included starting with the `v0.1.0-alpha.3` source release.

You supply ordinary Lean runtime code, proofs and separately authored expected
propositions. The checker rebuilds them with pinned Lean, verifies the exact
theorem statements and permitted axioms, then binds deterministic evidence to
the source and generated Go bytes. Reviewers still decide whether the selected
propositions express the intended requirements. This does not prove correctness
of the compiler or extractor.

## Project layout

Keep runtime code separate from proof-only modules:

```text
my-library/
  proof-contract.json
  source/
    Library.lean
  proof/
    LibraryContract.lean
    LibraryProofs.lean
```

The contract is a strict JSON object:

```json
{
  "schema": "lean-go-proof-contract/v1",
  "source_dir": "source",
  "proof_dir": "proof",
  "module": "Library",
  "roots": ["Library.clamp"],
  "claims": [
    {"theorem": "LibraryProofs.bounded", "expected": "LibraryContract.bounded"}
  ]
}
```

`source_dir` and `proof_dir` are disjoint directories relative to the contract's
parent, using portable paths without `..`, absolute paths or symlinks. Runtime
modules must match the source directory passed to extraction. All runtime and
proof `.lean` files are snapshotted and rebuilt; overlapping module names fail.
Existing project scripts, dependency configuration and compiled caches are not
used. Combined snapshots are limited to 256 files and 8 MiB.

The roots array contains one to 64 unique fully qualified names. Claims contain
one to 64 distinct theorem names mapped to expected definitions. Names use the
same ASCII Lean identifier syntax as extraction. Unknown fields, duplicate JSON
keys and invalid names are rejected. There are no custom hooks or configurable
axiom allowlists.

For a minimal runtime in `source/Library.lean`:

```lean
namespace Library
def clamp (value cap : UInt64) : UInt64 :=
  if value ≤ cap then value else cap
end Library
```

An expected proposition is a definition of type `Prop`, placed in
`proof/LibraryContract.lean`:

```lean
import Library
namespace LibraryContract
def bounded : Prop := ∀ (value cap : UInt64), Library.clamp value cap ≤ cap
end LibraryContract
```

`proof/LibraryProofs.lean` establishes that proposition independently:

```lean
import Library
namespace LibraryProofs
theorem bounded (value cap : UInt64) : Library.clamp value cap ≤ cap := by
  unfold Library.clamp
  split <;> simp_all
end LibraryProofs
```

Expected definitions and theorems must have no universe parameters. Each
expected declaration must be a `def` whose type is
exactly `Prop`; the checker compares its value to the theorem's type. It never
copies the theorem type to manufacture an expectation.

## Generate and check an external project

Run these commands from the lean-go checkout, using Go 1.27.4, pinned Lean 4.36.0-rc2
and Python 3.15 through `uv`. Use the same checkout to regenerate artifacts and
proof evidence; when using an installed CLI, install it from that
[development checkout](cli.md#development-checkout). Reproducing a published
release requires that release's examples and CLI together.
Replace `/path/to/my-library` with your project:

```sh
uv run --locked python scripts/generate.py \
  --source-dir /path/to/my-library/source \
  --module Library --root Library.clamp \
  --out /tmp/library-generated --ir-output /tmp/library.ir.json

uv run --locked python scripts/check_proofs.py \
  --contract /path/to/my-library/proof-contract.json \
  --manifest /tmp/library-generated/manifest.json \
  --output /path/to/my-library/proof-evidence.json

uv run --locked python scripts/check_proofs.py \
  --contract /path/to/my-library/proof-contract.json \
  --manifest /tmp/library-generated/manifest.json \
  --evidence /path/to/my-library/proof-evidence.json
```

Choose exactly one of `--output` and `--evidence`. Both freshly compile and check
the source proofs. `--output` publishes freshly checked evidence atomically;
`--evidence` requires the existing bytes to match the fresh result. Failed checks
preserve existing output. Evidence cannot overwrite inputs or live inside the
generated package. Use fresh candidate paths when changing extracted source.

Tooling is loaded from the lean-go checkout; your project does not need copies
of checker scripts or Lean checking infrastructure. The
[standalone clamp example](../examples/proofs/clamp) is a complete
single-root project with an imported local runtime module, generated package and
independent Go tests. The [article](articles/lean-proofs-to-go.md) explains its
three source claims and the tested extraction boundary. Quota demonstrates
multiple roots and keeps its existing command:

```sh
uv run --locked python scripts/check_quota.py
```

Go consumers use only generated code and the standard library with
`CGO_ENABLED=0`. Lean and Python are needed to regenerate/check evidence, not to
deploy the Go package.

The [standalone flag-mask example](../examples/proofs/flags) selects four
`UInt32` bitwise roots and checks eight universal laws against independent expected
propositions. It includes full-width Go regression tests and a fresh installed-CLI
verifier. Pair its tagged example and CLI for release reproduction, or install
the CLI from the same checkout for development artifacts.

The alpha.7 [Words example](../examples/proofs/words) selects five scalar
roots with v11 IR/manifest, `pure-module/v9` and `lean-go-modulegen/v9`. Seven
independent expected propositions cover widening, modulo narrowing, both
conversion round trips, low/high packing identities and full unpack/repack.
The fresh checker records no axioms for the two `toNat` claims, `propext` for
the conversion round trips, and `propext` plus `Quot.sound` for the three packing
laws. Proof-only remainder and bit-vector helpers do not enter the runtime IR.
Pair all three demos with the CLI from the same alpha.7 tag. Before publication,
use the examples and CLI from this same checkout; see the
[candidate walkthrough](release-candidate.md).

## Evidence and assurance

The fresh workspace uses fixed, dependency-free Lake scaffolding and the pinned
Lean identity. Each claim must name a theorem. Its type must match the selected
expected proposition, and all transitive axioms must be among `propext`,
`Classical.choice` and `Quot.sound`. `sorry`, custom axioms and compiler-trust
shortcuts such as `native_decide` are rejected.

Manifest checks require the module, roots, profile and Lean identity to match.
The full runtime source hash inventory must match the extraction inputs. The
generated directory must contain exactly the manifest and four generated Go
files, and every file hash must match. Snapshots and binding are checked again
after Lean finishes, before evidence can be written.

The temporary workspace's snapshotted sources, trusted checker and fixed
configuration are also checked around compilation. The harness is prepared
after the project build, so project compilation cannot silently replace an
already-staged checker program.

`lean-go-proof-evidence/v1` records contract bytes, logical project/tool input
hashes, the selected module and roots, manifest hash, theorem and expected names,
statements and axiom dependencies. It has no machine-specific absolute paths.
Changing a contract, proof, checker, runtime source or generated package requires
fresh evidence. The quota sidecar now uses this shared schema; its runtime and
Go API are unchanged.

Source elaboration is trusted, including macros, initializers and other
compile-time metaprograms. Workspace integrity checks detect input changes;
they do not sandbox hostile code or authenticate a compromised toolchain.
As with ordinary module extraction, the evidence
is source-level assurance, not a proof of semantics preservation through Lean's
compiler, the extractor, Go emission/runtime or the Go compiler. Normal module IR
theorem claims are unchanged by this separate workflow.

## Contributor checks

```sh
LEAN_GO_PROOFS=1 uv run --locked python scripts/test_proof_contracts.py
LEAN_GO_QUOTA=1 uv run --locked python scripts/test_quota_proofs.py
uv run --locked python scripts/check_quota.py
CGO_ENABLED=0 go test -count=1 ./...
```

The proof-contract suite checks malformed contracts, paths, snapshots, manifest
and output protection, rejected theorem/expectation forms, deterministic evidence
and an external project with a Go-only consumer. The quota suite retains its
library-specific source and theorem regression checks. The standalone verifiers
`check_clamp_demo.py`, `check_flags_demo.py` and `check_words_demo.py` additionally
regenerate artifacts with an installed CLI, check exact evidence and deploy only
Go sources. The installed-wheel smoke runs all three through both wheel builds;
see the [CLI guide](cli.md#verify-an-installation-build).
