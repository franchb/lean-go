# Compiler compatibility research

`scripts/check_compatibility.py` qualifies an explicitly installed Lean compiler
against the checkout's production pin. It is research tooling, not a switch for
the installed `lean-go` CLI. Ordinary extraction still checks its exact Lean
version and commit. A successful research run does not change those checks or
promise compatibility with other releases.

The current supported target is Lean **4.36.0-rc2**, commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`. Later releases require a fresh
qualification run and review before promotion.

The baseline version is read from `producer/lean-toolchain`; its commit comes
from `scripts/export.py`. Both must match the observed compiler. Python 3.15 and
Go 1.27.4 are required. The runner currently targets Linux/POSIX installations.

## Run and replay

Use real installation or `bin` directories containing Lean's installed library,
not elan's shim directory. The runner downloads nothing, never selects a global
elan default, and rejects existing artifact directories and output paths that
overlap the checkout or either compiler installation.

```sh
# The baseline must match the current production pin exactly.
uv run --locked python scripts/check_compatibility.py \
  --baseline-bin /opt/lean-production/bin \
  --candidate-bin /opt/lean-candidate/bin \
  --artifacts /tmp/lean-compatibility-new-run

# Omit --candidate-bin for a baseline corpus check. For the full comparison
# success path, pass the production installation as both baseline and candidate.
uv run --locked python scripts/test_compatibility.py

# A retained run contains its source snapshot and a replay entry point.
# Supply a fresh output path; the recorded compiler installations must exist.
uv run --no-project --python 3.15 python \
  /tmp/lean-compatibility-new-run/replay.py /tmp/lean-compatibility-replay
```

`--case helpers` focuses a replay on one case. Such a run only qualifies that
case; `selected_cases` records the scope. `--timeout 300` bounds each command;
POSIX timeout cleanup kills its process group, including child compilers.

The opt-in **Compiler compatibility research** GitHub Actions workflow installs
reviewed, checksum-verified release archives into job-local directories, then
passes those directories explicitly. Its `production` selection runs the
same-pin success path. New archive versions need a reviewed checksum in that
workflow. Differences deliberately make the research job fail, with evidence
uploaded even on failure. Ordinary extraction CI also runs the complete same-pin comparison corpus
using its already installed production compiler and preserves the report.

## What is checked

Each side observes `lean --version`, `lake env lean --version`, `lake --version`
and `lean --print-prefix`. Direct Lean and Lake's Lean must agree on the full
version and commit, and the prefix must match the explicit installation. The
report includes executable hashes and selected Lean library hashes, plus Python
and Go identities. These hashes are evidence, not binary authentication.

The harness copies the reviewed wheel payload closure from `pyproject.toml` into
a run-owned workspace. It records original producer, generator, harness and
corpus SHA-256 fingerprints. A candidate may receive only exact, counted
substitutions of compiler identity literals in the copied toolchain file,
Python exporters and Go IR validator. Before/after digests and a unified patch
are retained. Missing or duplicated anchors fail closed. A same-pin run has an
empty patch. Lean producer code, numeric audit policy and compiler semantics
are never adapted by this runner.

Producer compilation is a separate first gate. A failure there is
`producer_build_failed`; every candidate corpus case remains `not_run`.
Successful producer compilation enables eight accepted cases and six required
rejection cases (14 total):

| Case | Independent expectation |
| --- | --- |
| `numeric` | UInt32 and UInt64 wrap arithmetic and comparisons, including exact values above 2^53 |
| `bitwise` | UInt32/UInt64 AND, OR, XOR, complement and masked logical right shift |
| `left_shift` | UInt32/UInt64 ordinary `<<<` and direct controls, checked with exact exponent/multiply/modulo arithmetic |
| `width_conversions` | UInt32/UInt64 widening, narrowing and packing/unpacking with arithmetic consumer checks |
| `bytes` | UInt8 wrapping arithmetic, comparisons, bitwise operations, modulo-8 shifts and UInt8/UInt32 conversions with independent Go checks |
| `records` | Imported modern modules, nominal nested records, record updates, and shared types across two roots |
| `helpers` | Reduced-argument and captured local helpers with independent scalar results |
| `joins` | Both branches of nested joins/cases and wrapping continuation arithmetic |
| `polymorphic` | `admission` / `unsupported_type` |
| `extern` | `admission` / `source_audit` |
| `unsafe` | `admission` / `source_audit` |
| `folded_numeric` | `admission` / `source_audit`, even for division by one |
| `custom_pass` | `admission` / `source_audit` for a no-op custom compiler pass |
| `custom_csimp` | `admission` / `source_audit` for a valid custom compiler rewrite |

Accepted source must produce valid IR and a Go package, then pass a separate
checked-in Go consumer with independently authored expected constants or an
arithmetic per-bit model. Fixture files use
`consumer_test.go.txt` so ordinary repository Go test discovery skips them; the
runner copies each into its isolated consumer module as `consumer_test.go`. The consumer is
compiled with `CGO_ENABLED=0`, `GOPROXY=off`, `GOSUMDB=off`, `GOWORK=off` and
`GOTOOLCHAIN=local`; its test binary runs with no compiler tools on `PATH`.
Helpers must survive as called `._redArg` and `._lam_0` functions, and control
fixtures must retain `join`, `cases` and `jump`. Shape changes are reported even
when consumer behavior still passes.

Rejections require a single well-formed `lean-go-diagnostics/v1` document,
exit code 1, `success: false`, `stage: admission`, and the case's explicit code.
Messages and locations are retained as evidence; matching uses stage and code.
The runner does not classify English error text. Toolchain, build and emission
failures, signals, timeouts, malformed reports, and unexpected acceptances are
failures rather than successful negative cases.

## Results and evidence

| Exit | Meaning |
| --- | --- |
| `0` | The selected baseline corpus passed; if a candidate was supplied, all outcomes and canonical IR matched |
| `1` | Candidate difference: build, admission, behavior, shape, protocol, or canonical IR drift |
| `2` | Harness/precondition error or baseline failure; no candidate compatibility conclusion |

A baseline failure invalidates the experiment. A candidate `producer_build_failed`
concludes only that the unchanged producer cannot build with that compiler;
it makes no claim about individual candidate fixtures.

`report.json` summarizes versions, outcomes, statuses and differences.
`inputs.json` fingerprints the retained `source/` snapshot. Each compiler has
`identity.json`, `adaptations.json`, `adaptation.patch`, `result.json` and
`commands.jsonl`. Command logs include argv, working directory, bounded timeout,
stdout/stderr, return code and a fixed allowlist of relevant environment values;
they do not dump the process environment. Cases retain sources, consumers,
generated packages, raw IR and canonical IR.

Canonical IR comparison removes exactly the top-level `inputs`, `lean_version`
and `lean_commit` fields. It retains schema, roots, names, types, code,
provenance, claims and rewrites. JSON key ordering is normalized; arrays and
compiler-generated names are preserved. IR-only drift produces exit 1 and a
unified diff, even if Go behavior passes: it requires review, not automatic
acceptance.

Each difference creates a small `differences/<case>/replay.zip` containing the
source snapshot, relevant results/logs and a replay command focused on that
case. Producer failures use `differences/producer/replay.zip` and retain the
numeric-boundary reproducer. Build caches and consumer executables are excluded
from these bundles. Compiler installations remain external; replay records
fresh identities, which must be compared to the original evidence if an
installation has changed. CI artifacts can contain absolute local paths and
fixture source code, so review evidence before sharing it.

## Confirmed compiler-boundary finding

The initial Lean 4.36.0-rc2 investigation used commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a` against the producer originally pinned to
Lean 4.35.0 commit `eb6cafb7290e8bd5f2937522d8d804f58f02bafc`. That producer failed
while initializing its numeric audit: it expected
`_private.Init.Data.UInt.Package.0.instLinearOrderPackageUInt32` to be a definition.

The independent [NumericBoundaryName.lean](../compatibility/reproducers/NumericBoundaryName.lean)
probe imports only Lean and `Init.Data.UInt.Package`. It reports the private name
as a definition and the public name as missing on 4.35.0; 4.36.0-rc2 reverses those
results. This identifies a lean-go portability assumption about compiler library
names, not an upstream compiler bug. The production compiler upgrade uses the exact public declarations
`instLinearOrderPackageUInt32` and `instLinearOrderPackageUInt64`, retaining
the primitive, signature, axiom, source-closure and replacement checks. The
research runner deliberately does not patch the audit to hide differences.
Consult the current production pin and a fresh report for current support.

The [CustomCsimpDiagnostic.lean](../compatibility/reproducers/CustomCsimpDiagnostic.lean)
probe originally exposed a diagnostic coverage gap: a valid custom `csimp`
rewrite reported an unstructured historical-rewrite error as
`build` / `producer_failed`. The producer now emits `admission` / `source_audit`
at that explicit rejection site, with `compiler_form: csimp`, the rewritten
source declaration and its location when available. The `custom_csimp` corpus
case checks this rejection. Malformed Lean and invalid built-in rewrite
witnesses remain build failures; the harness does not relabel compiler errors.

## Trust scope

This is a small curated regression corpus, not a proof of compiler equivalence
or a broad collections profile. Arbitrary later releases need explicit
qualification and review. Compiler installations and Lean elaboration remain
trusted local code: a source can execute elaboration-time code, so copied
workspaces and process timeouts are not a security sandbox. No external issue,
message, release or publication is created by the harness.
