# Reusable source-proof contracts

A proof contract lets a Lean library use the same checked-evidence workflow as
the [quota library](quota.md). The development checker accepts metered
packages (manifest `lean-go-manifest/v24`) and [native](native-mode.md)
packages (manifest `lean-go-native-manifest/v1`, the default output of
`lean-go generate`) whose program uses IR `lean-go-ir/v24` and profile
`pure-module/v22`, with one or more roots in its `roots` array.
Manifests from earlier releases are rejected; regenerate them with the current
tool. Use the CLI and examples from the same tag to reproduce a release's proof
evidence. The checker is included starting with the `v0.1.0-alpha.3` source release.

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

## Bind one proof to multiple generated packages

A v2 composition contract names two to 64 component contracts and a directory
containing the composition proofs. Each component is a v1 contract under the
same project root. Its runtime snapshot, selected roots and generated files are
checked against the manifest supplied for that name. Component and composition
Lean modules must have distinct names and share the selected compiler pin.
The 256-file and 8 MiB bounds apply to the combined project.

The checked-frame contract combines Wire16 and CRC-16. From the repository
root, use the CLI installed from this checkout:

```sh
uv run --locked lean-go check-proofs \
  --contract examples/proofs/checkedframe-contract.json \
  --component-manifest wire16 examples/proofs/wire16/wire16/manifest.json \
  --component-manifest crc16 examples/proofs/crc16/crc16/manifest.json \
  --evidence examples/proofs/checkedframe/proof-evidence.json
```

Use `--output` to write fresh evidence. The checker proves the declared claims
once and rechecks the inputs before publishing. The evidence records each
component contract and manifest digest, all project source hashes, tool inputs
and both theorems. A change to either manifest invalidates the evidence.
[Example contract](../examples/proofs/checkedframe-contract.json) · [Combined evidence](../examples/proofs/checkedframe/proof-evidence.json)

## Generate and check an external project

With the Go command, from any directory (it needs only Go 1.27.4 to build and
the pinned Lean 4.36.0 to run; see the [CLI guide](cli.md)):

```sh
lean-go generate \
  --source-dir /path/to/my-library/source \
  --module Library --root Library.clamp \
  --out /tmp/library-generated --ir-output /tmp/library.ir.json

lean-go check-proofs \
  --contract /path/to/my-library/proof-contract.json \
  --manifest /tmp/library-generated/manifest.json \
  --output /path/to/my-library/proof-evidence.json
```

This generates a native package; add `--mode metered` to `generate` for a
metered one. The checker binds either.

The Python wrappers below do the same and generate a metered package unless
given `--mode native`. Run these commands from the lean-go checkout, using Go
1.27.4, pinned Lean 4.36.0 and Python 3.15 through `uv`. Use the same checkout to regenerate artifacts and
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
three source claims and the tested extraction boundary. The example tracks the
metered package `clamp/` with `proof-evidence.json` and the native package
`clampnative/` with `native-proof-evidence.json`; both bind the same three
claims:

```sh
cd examples/proofs/clamp
lean-go check-proofs --contract proof-contract.json \
  --manifest clampnative/manifest.json --evidence native-proof-evidence.json
```

Quota demonstrates
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
roots. Seven
independent expected propositions cover widening, modulo narrowing, both
conversion round trips, low/high packing identities and full unpack/repack.
The fresh checker records no axioms for the two `toNat` claims, `propext` for
the conversion round trips, and `propext` plus `Quot.sound` for the three packing
laws. Proof-only remainder and bit-vector helpers do not enter the runtime IR.
Pair examples and CLI from the same tag or the same checkout.
See the [verification walkthrough](release-candidate.md).

The [byte codec](../examples/proofs/bytes) adds four roots. Since alpha.9 the
generator adds checked Go `[4]byte` conversions. Its contract maps
twelve theorems to independent expected propositions: both inverse directions
for each byte order and all eight encoder-byte positions. The positions use
natural-number division by 1, 256, 65536 or 16777216 followed by remainder modulo
256, in reverse order for big endian. Fresh evidence records `propext` for the
eight position claims and `propext` plus `Quot.sound` for the four inverses. The
axiom allowlist is unchanged. Runtime code uses a concrete `ByteQuad` record;
proof-only natural numbers and bit vectors do not enter the generated runtime.
The [byte walkthrough](articles/proved-byte-codec.md) shows the Go application,
reproduction commands and assurance boundary. For development, this
example requires the CLI from this checkout. It is not in published alpha.7.

The [four-byte header](../examples/proofs/header) has nine source claims. They
specify each success and rejection condition, the decoded length bound, and
both round trips on valid inputs. Its consumer checks the wire layout against
Go's `encoding/binary` and exercises the raw and typed APIs.

The [Wire16 codec and typed header](../examples/proofs/wire16) add thirteen source
claims. The expected propositions specify each byte by division and remainder,
the decoded value by arithmetic, the header rejection order, and both codec
and header round trips. Two further claims characterize each payload decision by
the exact missing or consumed count in natural-number arithmetic.

The [CRC-16/XMODEM example](../examples/proofs/crc16) proves that one UInt16
byte update agrees with eight independent message-bit steps, then lifts the
result to arbitrary chunk boundaries in the proof module.

## Evidence and assurance

The fresh workspace uses fixed, dependency-free Lake scaffolding and the pinned
Lean identity. Each claim must name a theorem. Its type must match the selected
expected proposition, and all transitive axioms must be among `propext`,
`Classical.choice` and `Quot.sound`. `sorry`, custom axioms and compiler-trust
shortcuts such as `native_decide` are rejected.

Manifest checks require the module, roots, profile and Lean identity to match.
The full runtime source hash inventory must match the extraction inputs. The
manifest schema fixes the package layout and the manifest lists its files:
a metered package is the manifest and its four generated Go files
(`api.go`, `control.go`, `runtime.go`, `schema.go`); a native package must
record `"mode": "native"` and the current native generator, and its
`generated_files` names its Go files (`functions.go`, `native.go`,
`types.go` today; only plain lower-case non-test `.go` names are accepted).
The generated directory must contain exactly the manifest and those files,
and every file hash must match, so a missing, extra or changed file is
rejected. Snapshots and binding are checked again
after Lean finishes, before evidence can be written.

The temporary workspace's snapshotted sources, trusted checker and fixed
configuration are also checked around compilation. The harness is prepared
after the project build, so project compilation cannot silently replace an
already-staged checker program.

`lean-go-proof-evidence/v1` records contract bytes, logical project/tool input
hashes, the selected module and roots, manifest hash, theorem and expected names,
statements and axiom dependencies. Evidence for a native package also records
`"generated_mode": "native"`, as does a native component's entry in composition
evidence; metered evidence has no mode field. It has no machine-specific absolute paths.
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
library-specific source and theorem regression checks. The verifiers for clamp, flags, words, bytes, header, wire16 and crc16 regenerate artifacts with an installed CLI,
check exact evidence and deploy only Go sources. The installed-wheel smoke runs
all seven through both the direct and sdist-rebuilt wheels, freshly checking 54
source claims per wheel; see the [CLI guide](cli.md#verify-an-installation-build).
