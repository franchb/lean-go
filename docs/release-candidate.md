# Check source proofs, generated Go and deployment

Published `v0.1.0-alpha.7` uses Python package `0.1.0a7` and module generator v9.
This development checkout retains that package version but adds the **unreleased**
UInt8 engine, byte codec and module generator v10. Use the CLI and examples from
the same checkout or tag. A version string alone does not identify the source
revision or prove that a receipt describes the published release.

This walkthrough checks fresh source theorems, generated artifacts and isolated
Go consumers. APIs remain experimental. The [byte article](articles/proved-byte-codec.md)
explains the current application; the [tagged alpha.7 walkthrough](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.7/docs/release-candidate.md)
records the published release's workflow. Alpha.4 remains paired with Lean 4.35.0;
use its [tagged walkthrough](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.4/docs/release-candidate.md)
when reproducing that version.

## Check this development checkout

Use Go **1.27.4**, pinned Lean **4.36.0-rc2** at commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`, and **uv** with Python **3.15**.
Make Go, Lean and Lake available on `PATH`. From the repository root:

```sh
git rev-parse HEAD
git status --short
uv python install 3.15
uv sync --locked --reinstall-package lean-go-tools
uv run --locked lean-go doctor
checkout_cli="$(pwd)/.venv/bin/lean-go"
uv run --locked python scripts/check_clamp_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_flags_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_words_demo.py --cli "$checkout_cli"
uv run --locked python scripts/check_bytes_demo.py --cli "$checkout_cli"
```

In PowerShell, set `$checkout_cli = (Resolve-Path .venv/Scripts/lean-go.exe).Path`
and put each continued command on one line. Reinstall the checkout CLI after
pipeline changes to refresh its bundled payload. See the
[installation guide](cli.md#development-checkout) for an isolated tool install.

Each verifier copies its standalone module, generates fresh artifacts through the
installed CLI, compares package, IR and evidence bytes, and rechecks the source
claims. All four then deploy only Go sources and `go.mod`, running the tests and
executable offline with cgo and downloads disabled and producer tools hidden.

| Demo | Source claims | Independent finite Go cases |
| --- | --- | --- |
| [Clamp](../examples/proofs/clamp) | 3 | 681 input pairs |
| [Flags](../examples/proofs/flags) | 8 | 4,752 input pairs |
| [Words](../examples/proofs/words) | 7 | 699 boundary and seeded triples |
| [Bytes](../examples/proofs/bytes) | 12 | 652 boundary, one-hot and seeded cases |

The full installation smoke, `uv run --locked python scripts/check_cli_install.py`,
runs these four verifiers for both a direct wheel and an sdist-rebuilt wheel,
freshly checking 30 source claims per wheel. Current regenerated manifests and
proof evidence use module generator v10; the earlier demos retain their existing
IR schemas. The [clamp article](articles/lean-proofs-to-go.md) is a historical
walkthrough and retains its original release context.

## Reproduce published alpha.7

Use a separate release checkout and install the CLI from that same tag:

```sh
git clone --branch v0.1.0-alpha.7 --depth 1 https://github.com/franchb/lean-go.git lean-go-alpha7
cd lean-go-alpha7
git rev-parse HEAD
uv tool install --force --python 3.15 "git+https://github.com/franchb/lean-go.git@v0.1.0-alpha.7"
lean-go doctor
release_cli=$(command -v lean-go)
uv run --locked python scripts/check_clamp_demo.py --cli "$release_cli"
uv run --locked python scripts/check_flags_demo.py --cli "$release_cli"
uv run --locked python scripts/check_words_demo.py --cli "$release_cli"
```

Capture the installed path before `uv run` changes `PATH`. In PowerShell, use
`$release_cli = (Get-Command lean-go).Source`. This release has three standalone
demos and 18 source claims; it has no UInt8 engine or byte codec. Run the
remaining sections from the checkout whose source you intend to verify.

## Run the complete source-to-Go check

```sh
uv run --locked python scripts/check_release_candidate.py
```

The command copies the ordinary Lean clamp example into a temporary external
project, extracts fresh Go, checks three source theorems, verifies the resulting
evidence, and builds a separate Go-only consumer. The consumer has only Go on its
tool path, uses `CGO_ENABLED=0`, and disables module network access. It compares
177 boundary and seeded cases with an independent integer model, including
values above 2^53 and `math.MaxUint64`.

Success prints a JSON receipt with `"status": "passed"`,
`"candidate": "0.1.0a7"`, tool identities,
checked claims, source/proof/tool hashes, the generated manifest and IR hashes,
and the consumer case count. It contains no machine-specific absolute paths.
The `candidate` field retains the package-version label `0.1.0a7`; it is not a
new release name or a claim that the source matches published alpha.7. Interpret
the source/proof/tool hashes together with the recorded commit and working-tree
changes. A receipt from this development checkout describes its current inputs,
including generator v10, rather than the published tag's inputs.
All temporary projects and caches owned by the verifier are removed. To retain
the receipt and commit identity outside the checkout:

```sh
git rev-parse HEAD > ../checkout-walkthrough.commit
git status --short > ../checkout-walkthrough.status
uv run --locked python scripts/check_release_candidate.py > ../checkout-walkthrough.json
```

A failed stage exits nonzero and emits no success receipt. This command checks
the selected source checkout. The following separate test checks the published
alpha.7 Go module, so its result does not cover unreleased UInt8 behavior:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  LEAN_GO_RELEASE_VERSION=v0.1.0-alpha.7 \
  go test -count=1 -run '^TestTaggedReleaseConsumer$' -v ./integration
```

That test downloads the exact tag through the public Go proxy and checksum
database, verifies its version without a local `replace`, then runs tree,
UInt64, quota and multi-root checks offline with only Go on `PATH`. It covers the
existing root-module packages. The exact-tag installed-CLI workflow separately
regenerates and checks all three standalone demos on Linux and Windows; the
public Go consumer test alone does not cover every bitwise operation or width
conversion.

To check the current checkout through a local module proxy, the candidate test
remains available:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  go test -count=1 -run '^TestReleaseCandidateConsumer$' -v ./integration
```

The candidate test exercises Go packages through a temporary module proxy,
including quota and multi-root composition, without substituting a local
`replace`. The candidate checkout serves a synthetic archive as
`v0.1.0-alpha.7` from that local proxy, using the retained version label. This
checks the local archive's Go behavior; it does not identify that synthetic
archive with the already-published alpha.7 module or its checksum.

## Inspect the individual stages

The [clamp runtime](../examples/proofs/clamp/source/Clamp.lean) delegates to a
[local helper](../examples/proofs/clamp/source/Clamp/Bounds.lean): return the input
when it is within the limit, otherwise return the limit. Its
[proofs](../examples/proofs/clamp/proof/ClampProofs.lean) establish the bound,
preservation of already-bounded inputs and idempotence. The independently written
[expected propositions](../examples/proofs/clamp/proof/ClampContract.lean) and
[JSON contract](../examples/proofs/clamp/proof-contract.json) select those claims.

On a POSIX shell, keep an external project for inspection:

```sh
candidate_work=$(mktemp -d)
cp -R examples/proofs/clamp "$candidate_work/project"

uv run --locked python scripts/generate.py \
  --source-dir "$candidate_work/project/source" \
  --module Clamp --root Clamp.clamp \
  --out "$candidate_work/clamp" --ir-output "$candidate_work/clamp.ir.json"

uv run --locked python scripts/check_proofs.py \
  --contract "$candidate_work/project/proof-contract.json" \
  --manifest "$candidate_work/clamp/manifest.json" \
  --output "$candidate_work/proof-evidence.json"

uv run --locked python scripts/check_proofs.py \
  --contract "$candidate_work/project/proof-contract.json" \
  --manifest "$candidate_work/clamp/manifest.json" \
  --evidence "$candidate_work/proof-evidence.json"
```

Save this as `$candidate_work/main.go`:

```go
package main

import (
    "fmt"
    clamp "example.com/proved-clamp/clamp"
)

func main() {
    result, err := clamp.RunTyped(9007199254740993, 9007199254740992, clamp.DefaultLimits())
    if err != nil { panic(err) }
    fmt.Println(result)
}
```

Then run the generated Go code:

```sh
cd "$candidate_work"
go mod init example.com/proved-clamp
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off go run -buildvcs=false .
```

The output is exactly `9007199254740992`. Deployment needs Go and the generated
package; it does not need the source project, Lean, Python, uv or a C compiler.

The Lean kernel checks the selected source statements. Compilation, extraction,
the generated runtime and the Go compiler remain tested and trusted. Lean
elaboration, including compile-time metaprograms, is also trusted; this workflow
is not a sandbox. Read [proof contracts](proof-contracts.md) for the exact evidence
and validation rules, or [quota](quota.md) for a multi-root library API.
