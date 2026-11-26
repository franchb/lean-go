# Install the lean-go CLI

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

Existing differing package output is rejected and preserved. Package and IR
paths must not overlap. With no module arguments, `lean-go generate` runs the
audited normalizer profile and defaults to `./generated/normalizer` and
`./testdata/normalizer.ir.json` in the invoking directory.

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
source inputs and generated bytes; stale evidence fails. It belongs outside
source, proof and generated directories. These are Lean source proofs with
**tested, trusted extraction**, not an end-to-end compiler-correctness proof.

The package bundles only the audited pipeline scripts, producer sources and
pins, the Go emitter/validators/templates, and the audited pilot model.
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
regenerate and recheck the complete standalone clamp, flags, words, bytes, header, wire16, crc16 and adler32
demos, compare their tracked artifacts, freshly check all 56 source claims per
wheel, and test and run all eight copied modules with Go alone. Each wheel also checks the two frame composition claims against both component manifests. `--without-lean`
explicitly restricts this smoke test to packaging,
help/version and missing-tool errors; it does not claim producer validation. CI runs the full smoke on Linux
and Windows.

For editable development, refresh bundled assets after pipeline source changes
with `uv sync --reinstall-package lean-go-tools`. Legacy commands such as
`uv run --locked python scripts/generate.py` and
`uv run --locked python scripts/check_proofs.py` remain supported, including
their original path conventions.
