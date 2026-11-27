# Install the lean-go CLI

## Go command (development checkout)

This checkout's extraction pipeline is a single Go binary. It needs only the
pinned Lean on `PATH`; Python and uv are not involved. Build it with Go 1.27.4:

```sh
go build -o ~/bin/lean-go ./cmd/lean-go
lean-go doctor
```

Published tags up to `v0.1.0-alpha.15` contain only the legacy
`lean-go -in FILE -out DIR` form of this command, so build it from a checkout.

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
Python wrappers and CI use it. The builder's Go version is not recorded in
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
| `--validate off\|report\|require` | `off` | `report` records the result and never fails because of an unproved obligation; `require` fails (stage `validation`, code `validation_failed`) after writing the report unless every function and every root is proved, and then publishes neither the package nor the IR |
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

**Reports.** The report (`lean-go-validation/v2`) is deterministic: two runs on
the same inputs and budgets write the same bytes. It records the module and
roots, the SHA-256 of the IR it validated, the compiler, the budget, the
digests of the validation library (`producer/LeanGo/Validate*` and the runner
`tool/lean-go/validate`), and, per function, its kind, specification, status,
reason class, theorem name and statement and axioms; per root, its status,
call closure and the whole-program statements. Timings and heartbeat counts go
only to the diagnostics file (`lean-go-validation-diagnostics/v1`). Validation
files never enter the IR `inputs` or the manifest.

**What is proved.** For each proved function, the kernel-checked statement
`Callable.Rel Eq (evalBody (Spec.env spec) prog.F) spec_F`; for each
established root `R`, `∀ n, Callable.Rel Approx (prog.run n "R") R` and its
explicit form: whenever the IR program returns a value for encoded arguments,
it is the encoding of the source function's result. Only the three standard
axioms are accepted.

**What is not proved.** Termination and progress: a program that never
returns satisfies the statement (partial correctness only). Which roots are
exported is still the producer's choice.

**What stays trusted.** The Lean kernel; the IR semantics in
`producer/LeanGo/Validate` (that it describes what the Go printer implements);
the JSON-to-Lean bridge; the IR-to-Go printer, the Go runtime and the Go
compiler. The compiler's LCNF-to-IR lowering is no longer trusted for the
functional behaviour of established roots.

The Python entry points `scripts/generate.py`, `scripts/check_proofs.py` and
`scripts/regenerate_all.py` are thin wrappers that build and run this command,
keeping their historical path conventions. `scripts/generate.py` and the
Python package's `lean-go generate` below keep metered as their default mode
and pass it explicitly; give them `--mode native` for native output.

## Python package

`lean-go-tools` installs the `lean-go` command for generation and source-proof
checks. For development, follow the [checkout installation](#development-checkout).
Alpha.15 defaults to Lean/Lake **4.36.0** at commit
`611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9`. Install Go 1.27.4, then install
the compiler and the tagged CLI with Python 3.15 via uv.

```sh
elan toolchain install leanprover/lean4:v4.36.0
uv tool install --force --python 3.15 "git+https://github.com/franchb/lean-go.git@v0.1.0-alpha.15"
lean-go --version
lean-go doctor
```

The version command prints `lean-go 0.1.0a15`. To generate and check proofs with
rc2, install `leanprover/lean4:v4.36.0-rc2` and set `LEAN_GO_TOOLCHAIN=4.36.0-rc2`
for both commands. Alpha.15 records the selected compiler in the IR, manifest
and proof evidence. Use the default compiler to reproduce the tagged examples.
[Release notes](releases/v0.1.0-alpha.15.md)

When upgrading from alpha.14 to alpha.15, regenerate the Go package, IR and proof evidence
together with generator v18. Record byte folds use v22/v23 IR in alpha.15. This
development checkout accepts only `lean-go-ir/v24` IR and `lean-go-manifest/v24`
manifests from generator v19; regenerate earlier artifacts with its CLI. The Adler-32 example passes its two-word state directly through the generated Go loop.
[Tagged alpha.14 guide](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.14/docs/cli.md)

Use `uv tool update-shell` if your shell cannot find `lean-go`. The install
and doctor commands also work in PowerShell. For multiline POSIX shell examples
below, put each command on one line in PowerShell. Help and version work with
Python alone. The doctor command checks Go, Python, Lean and the compiler
selected by Lake, and returns a nonzero status if any check fails.

## Development checkout

Development builds default to Lean/Lake **4.36.0** at
`611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9`. Install
`elan toolchain install leanprover/lean4:v4.36.0` and use Go 1.27.4 and
Python 3.15 via uv. Install the CLI from the checkout that contains your
examples. That pairing reproduces the tracked IR, Go and source-proof evidence.
To target the other supported compiler, install it with
`elan toolchain install leanprover/lean4:v4.36.0-rc2` and set
`LEAN_GO_TOOLCHAIN=4.36.0-rc2` for both generation and proof checking.
Each selection checks the exact commit through Lean and Lake. Use separate output
directories for artifacts produced with different compilers.

```sh
uv python install 3.15
uv tool install --force --python 3.15 /absolute/path/to/lean-go
lean-go --version
lean-go doctor
```

This replaces the installed `lean-go` command with this checkout's version.
To reproduce alpha.7, use its tagged examples and [installation guide](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.7/docs/cli.md).
Pair alpha.8 examples with its tagged CLI. A released CLI
need not reproduce artifacts from a later development checkout.
For checkout-local use, run from the repository root:

```sh
uv sync --locked --reinstall-package lean-go-tools
uv run --locked lean-go --version
uv run --locked lean-go doctor
checkout_cli="$(pwd)/.venv/bin/lean-go"
uv run --locked python scripts/check_bytes_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_header_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_wire16_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_crc16_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_adler32_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_normalizer_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_checked_frame.py --cli "$checkout_cli"
```

On Windows, use the executable in `.venv/Scripts/lean-go.exe`. Reinstall after
pipeline changes so the installed payload matches the checkout. This checkout
reports package version `0.1.0a15`. Record the commit and source/tool hashes when
sharing results; the version string alone is not proof of publication or byte
identity. Pair generated artifacts with the same checkout and toolchain when
reproducing their hashes. Later compiler versions need explicit [compatibility qualification](compiler-compatibility.md);
the installed producer keeps its exact version and commit checks.

The [Words example](../examples/proofs/words), introduced in alpha.7, uses exact
`UInt32.toUInt64` and `UInt64.toUInt32` conversions, five roots and seven source
claims. This checkout's regenerated Words, clamp and flags artifacts use
`lean-go-ir/v24`, `pure-module/v22` and `lean-go-modulegen/v19`. Their manifests
and proof evidence require this checkout's CLI.
The [byte codec](../examples/proofs/bytes) additionally requires the
alpha.8 UInt8 capability for its four roots.
Its twelve proofs and Go-only application are explained in the
[byte walkthrough](articles/proved-byte-codec.md). See the
[UInt8 report](research/extraction-uint8.md) for the historical schema rules.
Published alpha.7's generator remains v9 and has no UInt8 support.

## Generate from your project

Run from any working directory; every relative path belongs to that directory.
Paths containing spaces are supported. The source directory must contain
ordinary `.lean` sources and cannot contain symlinks. Existing source admission,
import restrictions and resource limits are unchanged; see
[the supported subset](getting-started.md).

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

The Go command writes a native package here (`manifest.json`, `native.go`,
`types.go`, `functions.go`); the Python package's command writes a metered
package unless given `--mode native`. Add `--mode metered` to either for the
metered package (`manifest.json`, `api.go`, `control.go`, `runtime.go`,
`schema.go`), as the tracked demos do.

Existing differing package output is rejected and preserved. Package and IR
paths must not overlap. `--source-dir`, `--module`, at least one `--root`,
`--out` and `--ir-output` are all required; there are no default paths.

## Extraction diagnostics

Since alpha.5, the CLI includes readable extraction errors and
`--diagnostics=json` for `lean-go generate` and the generation/export scripts. Reports use the
`lean-go-diagnostics/v1` schema with stage/code, rejection reason, known compiler
form, source location and a next-step hint. See the
[diagnostics guide and schema](diagnostics.md) for fields and process behavior.
Alpha.6 classifies valid custom compiler rewrites as `admission` / `source_audit`,
retaining the source owner and location when known. Genuine Lean build and invalid
primitive failures remain `build` failures.
The [compiler compatibility harness](compiler-compatibility.md) uses these reports
from a Git checkout; it is not included in the installed CLI or wheel.

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

Both modes recheck source proofs freshly. Evidence binds theorem statements,
source inputs and generated bytes; stale evidence fails. The manifest may be
metered or native: its schema fixes the layout, and every file it lists must
be present with its recorded digest, with no other file in the package
directory. Native evidence records `"generated_mode": "native"`. It belongs outside
source, proof and generated directories. These are Lean source proofs with
**tested, trusted extraction**, not an end-to-end compiler-correctness proof.

The package bundles only the audited pipeline scripts, producer sources and
pins, and the Go emitters, validators and templates.
It copies them into a temporary writable workspace per invocation; installed
files remain unchanged. Go runs with `CGO_ENABLED=0`, `GOTOOLCHAIN=local`,
`GOWORK=off` and isolated caches. User Go flags and Lean/Lake path overrides do
not change the pinned producer. Generated consumers need only Go and its
standard library; they do not need Python, Lean, Lake, uv or a C compiler.

For a proof about multiple generated packages, repeat `--component-manifest NAME PATH` for each named component. [Composition contract and CLI example](proof-contracts.md#bind-one-proof-to-multiple-generated-packages)

## Verify an installation build

From a checkout, the standard build backend is managed by uv:

```sh
uv build
uv run --locked python scripts/test_cli.py
uv run --locked python scripts/check_cli_install.py
```

The install smoke test audits a direct wheel and an sdist-rebuilt wheel, installs
each in a fresh environment, verifies the actual installed imports, and runs
external generation, proof checking and a Go-only consumer. It also checks
read-only installed resources and paths containing spaces. Both installed wheels
regenerate and recheck the complete standalone clamp, flags, words, bytes, header, wire16, crc16, adler32 and
normalizer demos, compare their tracked artifacts, freshly check all 58 source claims per
wheel, and test and run all nine copied modules with Go alone. Each wheel also checks the two frame composition claims against both component manifests. `--without-lean`
explicitly restricts this smoke test to packaging,
help/version and missing-tool errors; it does not claim producer validation. CI runs the full smoke on Linux
and Windows.

For editable development, refresh bundled assets after pipeline source changes
with `uv sync --reinstall-package lean-go-tools`. Legacy commands such as
`uv run --locked python scripts/generate.py` and
`uv run --locked python scripts/check_proofs.py` remain supported with the
same required arguments.
