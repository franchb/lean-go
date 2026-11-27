# Changelog

All releases so far are alpha prereleases. Dates are the dates of the Git
tags. Each entry links to the full release notes in
[docs/releases](docs/releases). Artifacts (IR, manifests, proof evidence) are
tied to the version that produced them; pair examples and the tool from the
same tag or commit.

## Unreleased

Changes on the development branch since `v0.1.0-alpha.15`.

**What is guaranteed**

- Translation validation: `lean-go generate --validate report|require` and
  `lean-go validate` prove, per extraction, that the IR program returns the
  encoding of the Lean function's result whenever it returns (partial
  correctness). Every tracked program has a checked-in report; coverage is 88
  of 88 functions and 54 of 54 roots. See
  [translation validation](docs/research/translation-validation.md).
  The proof automation now records its generic evaluation steps with
  explicit proofs, so the kernel no longer reduces fixed-width arithmetic to
  recheck them; this took the DNS header's `word1` and `serialize`
  obligations from beyond the 4096 MiB cap to well under a second each.
- Proof evidence can be bound to native packages through their manifests
  (`"generated_mode": "native"`).

**What it supports**

- The admitted Lean subset and the integer primitive table are unchanged
  from alpha.15.

**Output modes**

- Native Go is the default output of `generate` and `emit`: plain types and
  functions with no resource budgets. The original budgeted output is
  `--mode metered`. See [docs/native-mode.md](docs/native-mode.md).
- The clamp example also tracks a native package with its own evidence.

**Wire formats**

- `lean-go format` generates a Lean parser and serializer, a proof contract
  with generated proofs, a native extracted package and a `[]byte` Go API
  from a JSON header spec, with demo, UDP and DNS examples. See
  [docs/wire-formats.md](docs/wire-formats.md).

**Install and tool**

- The extraction pipeline is a single Go command, `lean-go`, with the
  subcommands `generate`, `emit`, `check-proofs`, `validate`, `format`,
  `regenerate`, `doctor` and `version`. The Lean producer is embedded in the
  binary and its compiled form is kept in a content-addressed cache. The
  Python entry points (`scripts/generate.py`, `scripts/check_proofs.py`,
  `scripts/regenerate_all.py` and the `lean-go-tools` wheel) remain as thin
  wrappers. See [docs/cli.md](docs/cli.md).
- One IR schema, `lean-go-ir/v24`, replaces the per-feature versions; the IR
  lists the capability families it uses in `features`. IR and manifests from
  earlier releases are rejected and must be regenerated.
- `lean-go regenerate` regenerates every tracked artifact, wire-format
  example and validation report.
- The pilot pipeline is retired; the normalizer is now an ordinary module
  example.

## v0.1.0-alpha.15 (2026-11-26)

Byte folds whose accumulator is a structure of two `UInt32` fields, with the
Adler-32 example. [Notes](docs/releases/v0.1.0-alpha.15.md)

## v0.1.0-alpha.14 (2026-11-26)

Byte folds over `List UInt8` extracted to a Go `[]byte`; one composition
contract binds the checksummed frame's theorems to two packages.
[Notes](docs/releases/v0.1.0-alpha.14.md)

## v0.1.0-alpha.13 (2026-11-26)

Generator v15 and a checksummed Wire16 frame built from two proved
components. [Notes](docs/releases/v0.1.0-alpha.13.md)

## v0.1.0-alpha.12 (2026-11-25)

`UInt16` XOR and a streaming CRC-16/XMODEM example.
[Notes](docs/releases/v0.1.0-alpha.12.md)

## v0.1.0-alpha.11 (2026-11-25)

`UInt16.toUInt32`; Wire16 frames over byte slices, `io.Reader` and
`io.Writer`. [Notes](docs/releases/v0.1.0-alpha.11.md)

## v0.1.0-alpha.10 (2026-11-23)

First `UInt16` support (byte casts, OR, shifts) and the Wire16 codec.
[Notes](docs/releases/v0.1.0-alpha.10.md)

## v0.1.0-alpha.9 (2026-11-23)

Lean 4.36.0 support, checked Go `[4]byte` conversions and a proved four-byte
header codec. [Notes](docs/releases/v0.1.0-alpha.9.md)

## v0.1.0-alpha.8 (2026-11-23)

Scalar `UInt8` with byte/word conversions and a proved byte codec.
[Notes](docs/releases/v0.1.0-alpha.8.md)

## v0.1.0-alpha.7 (2026-11-13)

Exact `UInt32.toUInt64` and `UInt64.toUInt32` conversions, `<<<` notation,
and the Words example. [Notes](docs/releases/v0.1.0-alpha.7.md)

## v0.1.0-alpha.6 (2026-11-13)

Bitwise AND, OR, XOR, complement and logical right shift for `UInt32` and
`UInt64`; precise rejection of custom compiler rewrites.
[Notes](docs/releases/v0.1.0-alpha.6.md)

## v0.1.0-alpha.5 (2026-11-13)

Lean 4.36.0-rc2 and structured extraction diagnostics.
[Notes](docs/releases/v0.1.0-alpha.5.md)

## v0.1.0-alpha.4 (2026-11-13)

Installable CLI, recursive frame-storage reuse and the copyable clamp example.
[Notes](docs/releases/v0.1.0-alpha.4.md)

## v0.1.0-alpha.3 (2026-11-12)

Multi-root extraction, the quota library, reusable proof contracts and
generated-program differential testing. [Notes](docs/releases/v0.1.0-alpha.3.md)

## v0.1.0-alpha.2 (2026-11-11)

First preview: a pure, monomorphic subset of Lean 4 extracted to standalone Go.
It replaces the `v0.1.0-alpha.1` tag of the same day, which had a release-test
cleanup issue. [Notes](docs/releases/v0.1.0-alpha.2.md)
