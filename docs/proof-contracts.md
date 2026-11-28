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
propositions. The checker builds them with pinned Lean, then has the Lean
kernel decide each claim in a separate process that runs none of your code,
and binds deterministic evidence to the sources and to a generated package
that it rebuilds from those sources. Reviewers still decide whether the
selected propositions express the intended requirements. This does not prove
correctness of the compiler or extractor. What is and is not established is
listed under [evidence and assurance](#evidence-and-assurance).

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
exactly `Prop`; the kernel checks that the theorem proves its value. The
checker never copies the theorem type to manufacture an expectation.

Proof and contract files must not use the Lean module system (a `module`
header); the checker refuses such a module by name, and the same applies to
runtime modules.

## Bind one proof to multiple generated packages

A v2 composition contract names two to 64 component contracts and a directory
containing the composition proofs. Each component is a v1 contract under the
same project root. Its runtime snapshot, selected roots and generated files are
checked against the manifest supplied for that name. Component and composition
Lean modules must have distinct names and share the selected compiler pin.
The 256-file and 8 MiB bounds apply to the combined project.

The checked-frame contract combines Wire16 and CRC-16. From the repository
root, with this checkout's CLI:

```sh
go run ./cmd/lean-go check-proofs \
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
metered one. The checker binds either. To verify existing evidence instead of
writing it:

```sh
lean-go check-proofs \
  --contract /path/to/my-library/proof-contract.json \
  --manifest /tmp/library-generated/manifest.json \
  --evidence /path/to/my-library/proof-evidence.json
```

Add `--require-validation X.validation.json` to either command to make it
fail unless a complete [translation-validation](research/translation-validation.md)
report exists for exactly the IR in the manifest
([rules](cli.md#require-the-validation-report)). Without it, the proof check
says nothing about whether the IR was validated.

Use the same lean-go checkout or release to regenerate artifacts and proof
evidence; reproducing a published release requires that release's examples
and CLI together ([installation](cli.md#install)).

Choose exactly one of `--output` and `--evidence`. Both freshly compile and check
the source proofs. `--output` publishes freshly checked evidence atomically;
`--evidence` requires the existing bytes to match the fresh result. Failed checks
preserve existing output. Evidence cannot overwrite inputs or live inside the
generated package. Use fresh candidate paths when changing extracted source.

The checker is embedded in the lean-go binary; your project does not need
copies of checker sources or Lean checking infrastructure. The
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

Quota demonstrates multiple roots; from the repository root:

```sh
go run ./cmd/lean-go check-proofs \
  --contract libraries/quota/proof-contract.json \
  --manifest generated/quota/manifest.json \
  --evidence libraries/quota/proof-evidence.json
```

Go consumers use only generated code and the standard library with
`CGO_ENABLED=0`. Lean is needed to regenerate and check evidence, not to
deploy the Go package.

The [standalone flag-mask example](../examples/proofs/flags) selects four
`UInt32` bitwise roots and checks eight universal laws against independent expected
propositions. It includes full-width Go regression tests and is regenerated and checked by
the installed-CLI check. Pair its tagged example and CLI for release reproduction, or install
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

### What the check does

1. **Build.** A fresh workspace holds the snapshotted runtime and proof
   sources, the pinned `lean-toolchain` and a fixed, dependency-free Lake
   configuration; project scripts, dependency settings and caches are not
   used. The runtime modules are built first and alone, and the digests of
   their `.olean` files are recorded. Then every module is built. The
   snapshotted sources and fixed configuration are compared after each step,
   and the runtime `.olean` files must be unchanged after the proof modules
   are built.
2. **Decide.** Only after the build, the checker program
   ([`producer/LeanGo/Proofs/Check.lean`](../producer/LeanGo/Proofs/Check.lean),
   embedded in the `lean-go` binary) is written to a new temporary directory
   outside the workspace and run there with `lean --run`. It does not
   elaborate or import any module of the workspace, so it runs no
   initializer, macro, elaborator, attribute handler, delaborator or
   unification hint from them. It reads the workspace `.olean` files as
   data. It imports the toolchain modules they depend on from the pinned
   toolchain's own library directory, never from the workspace, and without
   loading environment extensions. It then replays every declaration of every
   workspace module (runtime, contract and proof modules alike) through the
   kernel with `Lean.Kernel.Environment.replay`, one module at a time in
   import order. Any kernel rejection fails the check and names the
   declaration. The kernel refuses a name that is already declared, so no
   module can replace a declaration of the toolchain or of an earlier
   module. The one exception is `replay`'s own: a theorem with the name and
   type of one already checked is left out and the checked one is kept,
   because Lean generates the same equation lemmas for an imported function
   in every module that needs them. `replay` does not send `unsafe` or
   `partial` constants to the kernel; they are left out of the checked
   environment, so a claim that depends on one fails.
3. **Claims.** For each claim, the theorem must be a theorem without universe
   parameters and the expected statement a closed `def` of type `Prop`
   without universe parameters. The kernel then decides the claim: the
   checker adds a theorem whose type is the expected proposition and whose
   value is the claimed theorem, and the kernel must accept it. `Meta.isDefEq`
   is not used. All axioms that the claim and the expected proposition
   depend on are collected from the replayed environment; only `propext`,
   `Classical.choice` and `Quot.sound` are accepted, so `sorry`, the axiom
   that `native_decide` adds and declared axioms are rejected.
4. **Package.** The manifest must name the contract's module, roots, profile
   and Lean identity, list the runtime source hashes, and describe the package
   directory exactly (a metered package is the manifest and `api.go`,
   `control.go`, `runtime.go`, `schema.go`; a native package records
   `"mode": "native"` and the current native generator and lists its Go
   files). Then the module and roots are exported again from the hash-bound
   sources, which must give exactly the `program` in the manifest, and that
   program is emitted again with the manifest's mode and `max_depth`, which
   must give exactly the package's files, `manifest.json` included, byte for
   byte. A package from another lean-go version fails with a message that
   says so; regenerate it. The export reuses the producer cache.
5. **Recheck.** The contract, the sources and the package are read again
   before evidence is written or compared.

`lean-go-proof-evidence/v3` (and `v4` for compositions) records contract
bytes, logical project/tool input hashes, the selected module and roots,
manifest hash, theorem and expected names, statements and axiom
dependencies. Evidence for a native package also records
`"generated_mode": "native"`, as does a native component's entry in
composition evidence; metered evidence has no mode field. It has no
machine-specific absolute paths. Changing a contract, proof, checker, runtime
source or generated package requires fresh evidence. The quota sidecar uses
this shared schema.

The recorded `statement` is the expected proposition as the kernel checked
it, rendered by the checker itself from the kernel term rather than by Lean's
pretty printer, so no notation or delaborator from any module affects it.
It shows every argument, including implicit and instance arguments, uses no
notation and omits universe levels, for example
`∀ (value : UInt64) (limit : UInt64), LE.le UInt64 instLEUInt64 (Clamp.clamp value limit) limit`.
Every constant is named in full. A name component that is not a plain ASCII
identifier is wrapped in `«»` with each non-ASCII character written as
`\u{…}`, so a constant cannot be named to read as other syntax or as a
look-alike of another name (Greek letters are identifier characters in Lean);
string literals are escaped the same way. Bound variables are shown with
plain ASCII names (`x₁` as `x1`, `α` as `x`), since their names carry no
meaning. Universe levels and the structure a projection belongs to are not
shown.

This is harder to read than the earlier pretty-printed text (schema
`v1`/`v2`). Read the contract file for the statement as written and the
evidence for the term that was checked. The two say different things when the
contract module shadows a name, declares its own instance or notation, or
computes its definitions with meta code; the full names in the evidence show
that.

### What it guarantees, against whom

The proof and contract modules are untrusted as far as logic goes: they may
contain any Lean, including meta code that adds declarations by any route,
sets options such as `debug.skipKernelTC`, or registers macros,
elaborators, attributes, unification hints, delaborators or initializers.
For such inputs, `check-proofs` succeeds only if each claimed theorem is
accepted by the kernel as a proof of its expected proposition, in the
environment of the built workspace modules on top of the pinned toolchain,
using no axioms other than the three above; and only if the bound package is
exactly what this lean-go generates from the hash-bound runtime sources.

### What it does not guarantee

- **Hostile I/O during the build is not contained.** Lean code runs while the
  workspace is built, with your permissions; there is no sandbox. Such code
  can read and write files and start processes. The checks above take away
  the easy routes: it cannot replace the checker, which does not exist yet
  and is then written elsewhere; it cannot change the snapshotted sources or
  the runtime `.olean` files without failing the check; `.olean` files that it
  writes for toolchain module names are not read. It can still write crafted
  `.olean` files for proof or contract modules (they are replayed like any
  other, and the recorded statement shows what was checked, but the `.olean`
  reader is not hardened against malformed files), modify the toolchain
  installation or other files it can write, or leave a process running that
  changes files after a check has passed. Check untrusted proofs in a
  disposable container or virtual machine.
- **The runtime sources and the toolchain are trusted.** Runtime modules are
  elaborated with macros and initializers enabled, as for extraction, and
  are built in the same workspace before the proofs. The Lean kernel, the
  toolchain's own `.olean` files and the `.olean` reader are trusted.
- **Expected statements need review.** The gate shows that the theorem proves
  the expected definition as built. Whether that definition says what you
  need, and whether it is the one written in the contract file when the
  contract module uses meta code, is for a reviewer.
- **No resource bound.** A hostile proof can make the kernel or the build run
  for a long time; each Lean process is stopped after 300 seconds.
- As with ordinary module extraction, the evidence is source-level assurance,
  not a proof of semantics preservation through Lean's compiler, the
  extractor, Go emission/runtime or the Go compiler. Normal module IR theorem
  claims are unchanged by this separate workflow.

## Contributor checks

```sh
LEAN_GO_PROOFS=1 LEAN_GO_QUOTA=1 go test -count=1 -p 1 ./internal/pipeline
CGO_ENABLED=0 go test -count=1 ./...
go run ./cmd/lean-go-dev installed-cli
```

The proof-contract tests (`internal/pipeline/proof_contract_test.go`, and with
Lean `lean_proofs_test.go`) check malformed contracts, paths, snapshots, manifest
and output protection, rejected theorem/expectation forms, deterministic evidence
and an external project with a Go-only consumer. The Lean-gated attack tests in
`internal/pipeline/lean_proofgate_test.go` (`LEAN_GO_PROOFS=1 go test -p 1
-run TestProofGate ./internal/pipeline`) cover the threats above: a theorem
added with `debug.skipKernelTC`, declarations inserted without `addDecl`, a
forged unification hint, initializers, delaborators and a user-declared
`Lean.reduceBool` that would print a forged result, shadowing definitions,
claims through skipped `unsafe` and `partial` constants, unapproved axioms,
the module system, build-time writes to the checker, toolchain and runtime
`.olean` paths, and edited, re-emitted, other-mode and composition
packages. The quota tests (`lean_quota_test.go`) retain the library-specific
source and theorem regression checks and recheck the tracked quota evidence.
The installed-CLI check regenerates clamp, flags, words, bytes, header, wire16,
crc16, adler32, normalizer and clock with a built and a module-installed
binary, checks exact evidence against an independent claim inventory and
deploys only Go sources, freshly checking 61 source claims per binary; see the
[CLI guide](cli.md#verify-an-installation).
