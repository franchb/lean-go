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
  correctness). Every tracked program has a checked-in report; coverage is 95
  of 95 functions and 61 of 61 roots. See
  [translation validation](docs/research/translation-validation.md).
  The proof automation now records every rewrite of its evaluation pass with
  an explicit proof, so the kernel no longer has to recheck those steps by
  reducing fixed-width arithmetic; this took the DNS header's `word1` and `serialize`
  obligations from beyond the 4096 MiB cap to well under a second each.
- Translation validation also checks that each established root returns.
  Partial correctness alone is satisfied by a program that never returns. On
  up to four generated inputs and with a fixed fuel, the kernel now computes
  that the IR program returns a value, which with the root's theorem gives
  `prog.run 64 "R" [encode c…] = some (encode (R c…))`. A root whose program
  returns nothing on such an input is reported with `failed` witnesses and
  fails `--validate require`; witnesses that could not be attempted are
  listed and do not fail the run. Reports use schema `lean-go-validation/v3`
  (`witnesses` per root, `roots_witnessed` in the summary).
- Proof evidence can be bound to native packages through their manifests
  (`"generated_mode": "native"`).
- `check-proofs` no longer trusts the theorems it finds in the built proof
  environment. A proof file could add a theorem without kernel checking
  (for example with `debug.skipKernelTC` and `addDecl`) and have a false
  statement certified, because the old checker compared types with
  `Meta.isDefEq` and never sent the proof to the kernel. The checker is now
  a separate program, staged outside the proof workspace after the build,
  that imports none of the user's modules and runs none of their code: it
  reads their `.olean` files as data, replays every declaration through
  `Lean.Kernel.Environment.replay`, lets the kernel decide each claim and
  collects axioms from the replayed environment. Proof and contract files
  using the Lean module system are refused. Code that runs while the proofs
  are built is still not sandboxed; see
  [what is not guaranteed](docs/proof-contracts.md#what-it-does-not-guarantee).
- `check-proofs` no longer binds evidence to a hand-edited package whose
  manifest was re-hashed. It exports the module again from the hash-bound
  sources and emits it again, and requires the manifest's program and every
  package file byte for byte. Packages from another lean-go version are
  refused with a hint to regenerate.
- Proof evidence moves to `lean-go-proof-evidence/v3` (`v4` for
  compositions): the recorded statement is now rendered by the checker from
  the kernel term, with every argument shown, no notation, every constant
  named in full and every name that is not a plain ASCII identifier escaped,
  instead of Lean's pretty printer. Earlier evidence must be regenerated.

**What it supports**

- The unsigned integer matrix is complete: `UInt8`, `UInt16`, `UInt32` and
  `UInt64` each admit the same fourteen operations (`+ - * / %`, the four
  bitwise operations, both shifts and the three comparisons) and the
  conversions to the other three widths, 68 primitives in all. Division
  and remainder are new at every width, and `UInt16` gains the arithmetic,
  comparisons and conversions it lacked. Both emitters test the divisor,
  so `x / 0 = 0` and `x % 0 = x` as in Lean. A program that divides
  declares the IR feature `division`. See the
  [support matrix](docs/support-matrix.md), the new
  [clock example](examples/proofs/clock) (three proved claims about
  splitting seconds with `/` and `%`) and the `Unsigned` module example.
- Translation validation follows the compiler's arithmetic rewrites that
  come with division: `x / 2^k` exported as a shift, a literal on the left
  of a multiplication, operands that are all literals folded to one, and
  the folds `x % 0`, `x / 1`, `x * 1`, `x + 0`, `x % 1`, `x * 0` and
  `0 / x`. Each step is an instance of a proved lemma or of core's
  evaluation of literals.
- Otherwise the admitted Lean subset is unchanged from alpha.15.

**Output modes**

- Native Go is the default output of `generate` and `emit`: plain types and
  functions with no resource budgets. The original budgeted output is
  `--mode metered`. See [docs/native-mode.md](docs/native-mode.md).
- The clamp example also tracks a native package with its own evidence.
- Metered fast path (`lean-go-modulegen/v20`): a metered root whose call
  graph has no recursion and whose types are not recursive runs the native
  emitter's code when the caller's limits cover its worst case, computed per
  limit from the state machine's own accounting; otherwise, and for any
  malformed argument, the unchanged state machine runs, so results and
  errors are the same. Measured in one binary on an idle machine, without
  and with the fast path: DNS parse plus serialize through the metered
  adapter 11,230 ns to 3,162 ns (186 to 46 allocations), UDP 6,968 ns to
  2,608 ns, the demo header 4,247 ns to 1,907 ns, Numbers 1,305 ns to
  372 ns, Quota reserve 397 ns to 172 ns. Against the previous generator
  (`lean-go-modulegen/v19`, commit `da2b025`) in a separate session, typed
  scalar entry points such as `Clamp.clamp` went from 37.1 ns to 4.9 ns.
  Byte folds and recursive roots are unchanged. The differential tests of
  the native emitter keep the state machine as their oracle
  (`modulegen.GenerateStateMachine`). See
  [docs/research/extraction-modules.md](docs/research/extraction-modules.md#metered-fast-path).

**Wire formats**

- `lean-go format` generates a Lean parser and serializer, a proof contract
  with generated proofs, a native extracted package and a `[]byte` Go API
  from a JSON header spec, with demo, UDP and DNS examples. See
  [docs/wire-formats.md](docs/wire-formats.md).
- Faster native wire-format adapters: headers travel by pointer inside the
  adapter, serialization appends straight to the destination, and native
  `As` methods no longer copy their receiver (native emitter
  `lean-go-nativegen/v2`). DNS parse plus serialize goes from 160.4 ns to
  106.6 ns (5.1 to 3.4 times a hand-written `encoding/binary` codec), UDP
  from 42.1 ns to 36.5 ns, the demo header from 22.8 ns to 19.5 ns. The
  remaining gap comes from by-value copies of records with more than four
  fields at the public API; see
  [docs/wire-formats.md](docs/wire-formats.md#native-against-metered).

**Examples**

- [examples/oracle](examples/oracle) uses extracted native packages as
  reference models: hand-optimized Adler-32 (deferred reduction, blocks of
  5,552 bytes) and CRC-16/XMODEM (two lookup tables) are tested against the
  packages generated from the proved sources, with seeded, random-chunking,
  exhaustive and fuzz tests, and three planted bugs that the harness finds
  and shrinks. The models are tracked artifacts. See
  [docs/oracle-testing.md](docs/oracle-testing.md).

**Install and tool**

- The extraction pipeline is a single Go command, `lean-go`, with the
  subcommands `generate`, `emit`, `check-proofs`, `validate`, `format`,
  `regenerate`, `doctor` and `version`. The Lean producer is embedded in the
  binary and its compiled form is kept in a content-addressed cache. See
  [docs/cli.md](docs/cli.md).
- The repository no longer contains Python. The `lean-go-tools` wheel and its
  `lean-go` command, `scripts/` (the wrappers, verifiers and test suites),
  `pyproject.toml` and `uv.lock` are removed, and CI no longer installs uv or
  Python. Install the command with `go install ./cmd/lean-go` from a checkout,
  or, from the first tag that contains it,
  `go install github.com/franchb/lean-go/cmd/lean-go@<tag>`. The tests are Go
  tests; the demo verifiers, the installed-CLI smoke and the release
  walkthrough are `go run ./cmd/lean-go-dev installed-cli` and
  `release-candidate`. The installed-CLI check installs the command from a
  module archive of the tracked files, as `go install ...@<tag>` does, and
  checks each demo against an independent list of its claims and its expected
  output.
- One IR schema, `lean-go-ir/v24`, replaces the per-feature versions; the IR
  lists the capability families it uses in `features`. IR and manifests from
  earlier releases are rejected and must be regenerated.
- `lean-go regenerate` regenerates every tracked artifact, wire-format
  example and validation report.
- The pilot pipeline is retired; the normalizer is now an ordinary module
  example.
- `generate` writes the IR file and the validation report next to their
  destinations before it swaps the package, so a destination that cannot be
  written no longer costs the previous package under `--replace`.
- `emit --native-max-depth` requires a whole positive integer (`7abc` was
  read as 7 and `0` as the default).
- Published packages are ordinary directories (they were mode 0700); a
  producer cache entry that does not verify is rebuilt and replaced instead
  of being rebuilt on every run; a leftover publication lock says what it is.
- The validation runner checks that a completed report covers every IR
  function and every requested root, and counts anything missing as unproved.

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
