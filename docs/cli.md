# Install the lean-go CLI

## Install

<a id="development-checkout"></a><a id="go-command-development-checkout"></a>

lean-go is a single Go binary. Building it needs Go 1.27.4; running it needs
only the pinned Lean and Lake on `PATH`. The producer's Lean sources are
embedded in the binary, so it runs from any directory without a checkout.

Install Lean/Lake **4.36.0** (commit `611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9`)
with elan, then install lean-go from a checkout:

```sh
elan toolchain install leanprover/lean4:v4.36.0
go install ./cmd/lean-go
lean-go version
lean-go doctor
```

`go install` puts the binary in `$(go env GOBIN)`, or `$(go env GOPATH)/bin`
when `GOBIN` is unset; `go build -o DIR/lean-go ./cmd/lean-go` writes it
elsewhere. `lean-go version` prints the tool version (`0.1.0a15` in this
checkout), the Go release that built it and the digest of its pipeline
sources. `lean-go doctor` checks the Go release, Lean, Lake and the producer
cache, and exits nonzero if a check fails.

A release tag that contains this command can be installed without a checkout:

```sh
go install github.com/franchb/lean-go/cmd/lean-go@<tag>
```

No published tag contains it yet: tags up to `v0.1.0-alpha.15` have only the
legacy `lean-go -in FILE -out DIR` form of this command, and their
generation and proof commands are the Python package of that tag (see its
[tagged guide](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.15/docs/cli.md)).
For this checkout, `go run ./cmd/lean-go-dev installed-cli` serves a module
archive of the tracked files from a local proxy and installs it with exactly
this `go install` form; the archive contains the embedded producer and the
installed binary passes the same checks as a checkout build (see
[Verify an installation](#verify-an-installation)). Only a published tag can
show that the public module proxy serves it.

To target the other supported compiler, install it with
`elan toolchain install leanprover/lean4:v4.36.0-rc2` and set
`LEAN_GO_TOOLCHAIN=4.36.0-rc2` for both generation and proof checking. Each
selection checks the exact commit through Lean and Lake, and the IR, manifest
and proof evidence record it. Use separate output directories for artifacts
produced with different compilers, and the default compiler to reproduce the
tracked examples. Later compiler versions need explicit
[compatibility qualification](compiler-compatibility.md).

This checkout accepts only `lean-go-ir/v24` IR and `lean-go-manifest/v24`
manifests from generator v20; regenerate artifacts of earlier versions with
it. Pair generated artifacts with the same checkout and toolchain when
reproducing their hashes, and record the commit and the source and tool hashes
when sharing results: the version string alone is not proof of publication or
byte identity.

## Commands

| Command | Purpose |
| --- | --- |
| `lean-go generate` | Lean sources → IR → Go package (`--source-dir`, `--module`, repeated `--root`, `--out`, `--ir-output`, `--mode native\|metered`, `--diagnostics text\|json`, `--replace`, `--no-cache`, `--strict-go`, and the [validation options](#translation-validation)) |
| `lean-go validate` | translation-validate an existing IR file against its sources (`--source-dir`, `--module`, repeated `--root`, `--ir`, validation options) |
| `lean-go emit` | IR file → Go package (`--in FILE --out DIR [--mode ...] [--native-max-depth N] [--replace]`) |
| `lean-go check-proofs` | proof contracts and composition contracts, as described below |
| `lean-go format check\|generate` | [wire formats](wire-formats.md); `lean-go-format` remains as an alias |
| `lean-go regenerate` | contributor command: regenerate every tracked artifact, wire-format example and validation report; `--coverage` prints the validation coverage table |
| `lean-go doctor`, `lean-go version` | check Lean, Lake, the build toolchain and the producer cache; print the tool identity |

`generate` and `emit` emit [native Go](native-mode.md) by default: plain Go
types and functions with no resource budgets. `--mode metered` selects the
original metered package (an explicit-stack interpreter with seven resource
budgets per call), for callers who need budgets on untrusted recursive input.
The IR is the same in both modes, and `check-proofs` accepts both.

`generate` keeps the safety properties of the original pipeline: every run uses
a fresh private Lake workspace, source snapshots are limited to 256 files and
8 MiB without symlinks or reserved module names, Lean and Lake must report the
exact pinned version and commit, and diagnostics follow
[diagnostics.md](diagnostics.md). Relative paths belong to the working
directory. Existing differing package output is preserved and rejected unless
`--replace` is given: then the existing directory is replaced only after
generation fully succeeded and the IR file and validation report have been
written next to their destinations, and only when it is recognizably a
package that lean-go generated (a lean-go manifest and no files other than
generated ones). The IR file is rewritten atomically. Generated Go is formatted by the
`go/format` package compiled into the binary, so byte-exact reproduction of
tracked artifacts requires a binary built with Go 1.27.4. A binary built by
another Go release still generates, printing a one-line warning on stderr that
names both versions (in `--diagnostics=json` mode, after success only, so a
failure report stays the only stderr document). `--strict-go` turns the warning
into a `toolchain_mismatch` failure; `lean-go regenerate`, the checkout's
integration tests and CI use it. The builder's Go version is not recorded in
manifests.

For `go generate`, add a directive next to the package that consumes the output:

```go
//go:generate lean-go generate --source-dir ../lean --module Clamp --root Clamp.clamp --out ./clamp --ir-output ./clamp.ir.json --replace
```

The producer Lean sources are embedded in the binary (package `producer`) and
written into the private workspace at run time; `--source-dir` holds only your
code. The pipeline is the `internal/pipeline` package, which tests and the
other commands call in-process.

### Producer build cache

Compiling the embedded producer is most of an extraction's time. `generate`
therefore keeps the compiled producer in a content-addressed cache, by default
under the user cache directory (`$XDG_CACHE_HOME/lean-go/producer` or
`~/.cache/lean-go/producer` on Linux, `~/Library/Caches/lean-go/producer` on
macOS, `%LocalAppData%\lean-go\producer` on Windows). The key is a SHA-256 of
the cache format, the exact `lean --version` text (release, commit and target),
the producer's Lake library stanza and every embedded producer file; a run
with translation validation uses a separate entry that also contains the
compiled validation library, keyed additionally by its files. Entries
are built only in a producer-only workspace, never next to user sources, are
published atomically, and are verified file by file against their recorded
digests before use; the directory must be owned by you and closed to other
users (mode `0700` on POSIX systems), otherwise the cache is ignored. A cache
hit produces byte-identical IR and Go to a cold build; the parity tests check
this.

**Trust consequence.** Compiled files in the cache are trusted like any local
build cache, such as Go's `GOCACHE`: anyone who can write your cache directory
can change what the producer does, and therefore what Go it emits. The digest
check detects corruption and partial writes, not a deliberate edit by your own
account. Use `--no-cache` (or `LEAN_GO_NO_CACHE=1`) to force a fresh producer
build, for example when producing release artifacts; `LEAN_GO_CACHE_DIR` moves
the cache. The proof checker never uses the cache: proofs are always rebuilt.

### Provenance inventory

The IR `inputs` map names everything the producer consumed, without machine
paths: `source/<path>` for each snapshotted source file,
`producer/LeanGo/Module/<File>.lean` for each embedded producer file, and
`tool/lean-go/pipeline`, the SHA-256 of the extraction pipeline's non-test Go
sources (`internal/pipeline` except `proof_*.go` and the validation runner
`validate_*.go`, plus the shared compiler policy), framed like the validator digest in `internal/moduleir/identity.go`.
Proof evidence `tool_inputs` records the embedded `producer/lean-toolchain`
and `producer/LeanGo/Proofs/Check.lean` plus `tool/lean-go/pipeline` and
`tool/lean-go/proofcheck` (the `proof_*.go` sources). The manifest already
records the emitter (`generator_source_sha256`) and validator digests.
Changing any of these sources changes the inventory; regenerate the tracked
artifacts with `lean-go regenerate`.

### Translation validation

`generate --validate report|require` (default `off`) proves, for the exported
program, that the IR agrees with the Lean source functions, using the
`#lean_go_validate` command described in
[translation-validation.md](research/translation-validation.md).

| Option | Default | Meaning |
| --- | --- | --- |
| `--validate off\|report\|require` | `off` | `report` records the result and never fails because of an unproved obligation; `require` fails (stage `validation`, code `validation_failed`) after writing the report unless every function and every root is proved and no root's witnesses failed, and then publishes neither the package nor the IR |
| `--validation-report FILE` | next to the IR: `X.ir.json` → `X.validation.json` | the deterministic report |
| `--validation-diagnostics FILE` | not written | timings, heartbeat counts, full failure messages and the process attempts |
| `--validate-memory-mb N` | 4096 | memory limit of the validation process, in MiB |
| `--validate-heartbeats N` | 200000 | heartbeats per obligation, in thousands (Lean's `maxHeartbeats` unit and default) |
| `--validate-timeout-ms N` | 120000 | wall-clock limit per obligation |
| `--validate-process-timeout S` | 3600 | wall-clock limit of one validation process |

`lean-go validate --source-dir DIR --module M --root R… --ir FILE` does the
same for an IR file that already exists: it exports again in a fresh
workspace, requires the result to be byte-identical to `FILE`, and writes the
report (`--validate` defaults to `report` there).

**How it runs.** The export is unchanged. After the complete IR has been
computed, a second Lean process runs the validation harness in the same
private workspace; nothing it does, including being killed, can change the IR
or the generated Go, which are byte-identical with and without validation.
The validation library and command do not depend on your sources and are
compiled once into the producer cache; per extraction only the harness runs.
Lean is started directly (not through `lake env`) with `-M <memory>`, Lean's
own memory limit, which its runtime enforces on Linux, macOS and Windows, and
with `Elab.async` off so that a kernel check that exceeds the limit fails in
place. An obligation that hits a limit (the memory cap, a heartbeat or time
budget, or the process timeout) is reported `failed` with a reason class that
names the limit (`memory_limit`, `heartbeat_limit`, `obligation_time_limit`,
`process_time_limit`); the process is rerun with that obligation skipped, so
the remaining obligations still run. Every tracked example validates
under the default cap (a whole program peaks at about 1.7 GB, most of it the
imported environment); an obligation whose kernel check outgrows it is
reported `failed: memory limit` instead of exhausting the machine.

**Reports.** The report (`lean-go-validation/v3`) is deterministic: two runs on
the same inputs and budgets write the same bytes. It records the module and
roots, the SHA-256 of the IR it validated, the compiler, the budget, the
digests of the validation library (`producer/LeanGo/Validate*` and the runner
`tool/lean-go/validate`), and, per function, its kind, specification, status,
reason class, theorem name and statement and axioms; per root, its status,
call closure, the whole-program statements and its non-vacuity witnesses
(`witnesses`: status, reason class, fuel and the theorem and statement of each
case). Timings and heartbeat counts go only to the diagnostics file
(`lean-go-validation-diagnostics/v1`). Validation files never enter the IR
`inputs` or the manifest.

The commands print one summary line, `validation: P/F functions proved, E/R
roots established, W/R roots witnessed; report in FILE`, followed by one line
per function or root that is not proved, per root whose witnesses failed, and
per established root whose witnesses were not attempted (with the reason).

**What is proved.** For each proved function, the kernel-checked statement
`Callable.Rel Eq (evalBody (Spec.env spec) prog.F) spec_F`; for each
established root `R`, `∀ n, Callable.Rel Approx (prog.run n "R") R` and its
explicit form: whenever the IR program returns a value for encoded arguments,
it is the encoding of the source function's result. Only the three standard
axioms are accepted.

**Non-vacuity witnesses.** For each established root, a few concrete inputs
built from its parameter types (at most four cases, fuel 64) give the
kernel-checked theorem `prog.run 64 "R" [encode c…] = some (encode (R c…))`.
A root's witnesses are `checked` (every case returned), `failed` (the kernel
showed that the program returns `none` on a generated input; `require` fails)
or `not_attempted` with a reason class, for example `root_not_proved`,
`no_samples` or a budget (`require` does not fail; the summary line counts
it). A witness that kills the validation process is skipped in a rerun, as an
obligation is.

**What is not proved.** Termination and progress: a program that never
returns satisfies the statement (partial correctness only); the witnesses
cover only their generated inputs. Which roots are exported is still the
producer's choice.

**What stays trusted.** The Lean kernel; the IR semantics in
`producer/LeanGo/Validate` (that it describes what the Go printer implements);
the JSON-to-Lean bridge; the IR-to-Go printer, the Go runtime and the Go
compiler. The compiler's LCNF-to-IR lowering is no longer trusted for the
functional behaviour of established roots.

## Generate from your project

Run from any working directory; every relative path belongs to that directory.
Paths containing spaces are supported. The source directory must contain
ordinary `.lean` sources and cannot contain symlinks. Existing source admission,
import restrictions and resource limits are unchanged; see
[the supported subset](getting-started.md).

The symlink rule covers paths as well as files. No existing component of the
source directory, of the package output directory, or of the contract,
manifest and evidence paths given to `check-proofs` may be a symbolic link, so
a project reached through a linked directory is rejected; give the real path
instead. On macOS, `/tmp` and `/var` are links into `/private`: name
`/private/tmp/...` or work below your home directory. CI covers Linux and
Windows only; the macOS consequence follows from the rule and has not been run
there.

```sh
lean-go generate \
  --source-dir ./project/source --module Clamp --root Clamp.clamp \
  --out ./generated/clamp --ir-output ./clamp.ir.json
```

The module, source directory and roots must be supplied together. Module
extraction requires explicit package and IR output paths. Repeat `--root` to
expose multiple functions in one package with shared generated types:

```sh
lean-go generate \
  --source-dir ./project/source --module Clamp \
  --root Clamp.clamp --root Clamp.Bounds.cap \
  --out ./generated/clamp-multi --ir-output ./clamp-multi.ir.json
```

These commands write a native package (`manifest.json`, `native.go`,
`types.go`, `functions.go`). Add `--mode metered` for the
metered package (`manifest.json`, `api.go`, `control.go`, `runtime.go`,
`schema.go`), as the tracked demos do.

Existing differing package output is rejected and preserved. Package and IR
paths must not overlap. `--source-dir`, `--module`, at least one `--root`,
`--out` and `--ir-output` are all required; there are no default paths.

## Extraction diagnostics

Since alpha.5, the CLI includes readable extraction errors and
`--diagnostics=json` for `lean-go generate`. Reports use the
`lean-go-diagnostics/v1` schema with stage/code, rejection reason, known compiler
form, source location and a next-step hint. See the
[diagnostics guide and schema](diagnostics.md) for fields and process behavior.
Alpha.6 classifies valid custom compiler rewrites as `admission` / `source_audit`,
retaining the source owner and location when known. Genuine Lean build and invalid
primitive failures remain `build` failures.
The [compiler compatibility harness](compiler-compatibility.md) uses these reports
from a Git checkout (`go run ./cmd/lean-go-dev compatibility`); it is not part of
the installed CLI.

## Check proofs

Use a [proof contract](proof-contracts.md) selecting the same module and roots
as the generated manifest. The reusable
[standalone clamp example](../examples/proofs/clamp) includes source, proofs,
a contract, generated artifacts and a copyable Go module. The
[article](articles/lean-proofs-to-go.md) walks through the complete handoff.

```sh
lean-go check-proofs \
  --contract ./project/proof-contract.json \
  --manifest ./generated/clamp/manifest.json --output ./proof-evidence.json
lean-go check-proofs \
  --contract ./project/proof-contract.json \
  --manifest ./generated/clamp/manifest.json --evidence ./proof-evidence.json
```

Both modes recheck source proofs freshly: the proof workspace is built, then
a checker staged outside it after the build replays every workspace
declaration through the Lean kernel and has the kernel decide each claim,
without importing or running any of your modules
([what this guarantees and what it does not](proof-contracts.md#evidence-and-assurance)).
Evidence (`lean-go-proof-evidence/v3`, or `v4` for compositions) binds
theorem statements, source inputs and generated bytes; stale evidence fails.
The manifest may be metered or native: its schema fixes the layout, and every
file it lists must be present with its recorded digest, with no other file in
the package directory. The package must also be exactly what this lean-go
generates: the module is exported again from the hash-bound sources (reusing
the producer cache) and must give the manifest's program, and emitting that
program with the manifest's mode and options must give the package's files
byte for byte. A package from another lean-go version is refused with a hint
to regenerate it. Native evidence records `"generated_mode": "native"`. It belongs outside
source, proof and generated directories. These are Lean source proofs with
**tested, trusted extraction**, not an end-to-end compiler-correctness proof.
The build of the proof workspace runs your Lean code without a sandbox; check
untrusted proofs in a disposable container or virtual machine.

The binary embeds the producer sources and the compiler pin, and the Go
emitters, validators and templates are compiled into it. Each invocation
stages the producer into a private temporary workspace; nothing next to the
binary is read or written. Lean/Lake path overrides (`LEAN_PATH`, `LAKE_HOME`
and the like) are removed from the child environment, so they cannot change
the pinned producer. Generated consumers need only Go and its standard
library; they do not need Lean, Lake or a C compiler.

For a proof about multiple generated packages, repeat `--component-manifest NAME PATH` for each named component. [Composition contract and CLI example](proof-contracts.md#bind-one-proof-to-multiple-generated-packages)

## Verify an installation

<a id="verify-an-installation-build"></a>

From a checkout, with the pinned Go and Lean on `PATH`:

```sh
go test -count=1 ./internal/cli/ ./internal/smoke/
go run ./cmd/lean-go-dev installed-cli
```

The first command tests the command boundary and the smoke harness itself.
The second builds lean-go with `go build` and installs a second binary with
`go install github.com/franchb/lean-go/cmd/lean-go@v0.0.0-smoke` from a module
archive of the tracked files served by a local proxy, which is the route of
`go install ...@<tag>`. Each binary is copied into a read-only directory and
run from directories outside the checkout: help and version with no tools on
`PATH`, `doctor` reporting the missing tools, then `doctor`, `generate` and
`check-proofs` on a copied clamp project through relative paths containing
spaces, a Go-only consumer of 177 boundary and seeded cases, a two-root
package, and a rejected source that must leave the earlier package, IR and
evidence unchanged. Both binaries then regenerate every standalone demo (clamp,
flags, words, bytes, header, wire16, crc16, adler32, normalizer and clock) from
a copy, compare package, IR and evidence bytes with the tracked ones, freshly
check all 61 source claims against an independent inventory, test and run each
copied consumer with Go alone and compare its output with the expected text,
and check the two checked-frame composition claims against both component
manifests. `--without-lean` restricts the run to the binaries, help, version
and missing-tool errors; it does not claim producer validation. `--cli PATH`
checks an installed binary instead of building one, and `--expect-tag TAG`
requires it to report that module version. CI runs the full check on Linux and
Windows.
