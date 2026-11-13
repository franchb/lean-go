# From Lean source proofs to Go with alpha.4

This walkthrough uses the `v0.1.0-alpha.4` experimental preview, which brings
together the installable CLI, frame-storage reuse and a standalone clamp demo
with multi-root extraction, the quota library and reusable source-proof contracts.
It checks source theorems, extracts fresh Go and exercises an isolated consumer.
Its API remains experimental.

## Start from a clean checkout

Use Go **1.27.4**, the pinned Lean **4.35.0**, and **uv** with Python **3.15**.
Make Go, Lean and Lake available on `PATH`. Clone into a new directory:

```sh
git clone --branch v0.1.0-alpha.4 --single-branch \
  https://github.com/franchb/lean-go.git lean-go-alpha4
cd lean-go-alpha4
git rev-parse HEAD
uv python install 3.15
```

Record the full commit printed above alongside your verification output. The tag
pins the release sources. The verifier operates on its current source
checkout; it does not establish that a working tree is clean. CI runs it from a
fresh checkout, and release preparation also checks a clean tracked snapshot.

## Check the installed CLI and standalone demo

Install the public tag and check the exact producer toolchain:

```sh
uv tool install --python 3.15 "git+https://github.com/franchb/lean-go.git@v0.1.0-alpha.4"
lean-go --version
lean-go doctor
clamp_cli=$(command -v lean-go)
uv run --locked python scripts/check_clamp_demo.py --cli "$clamp_cli"
```

The CLI reports `lean-go 0.1.0a4`. If it is not on `PATH`, follow the
[installation guide](cli.md). The POSIX commands above capture the installed CLI
path before `uv run` adds the checkout environment to `PATH`; in PowerShell,
use `$clamp_cli = (Get-Command lean-go).Source` before the verifier command.
The demo verifier copies the standalone module,
generates fresh artifacts through the installed CLI, compares package, IR and
evidence bytes, and rechecks three source claims. It then runs the copied Go-only
tests and executable offline, including 681 finite input pairs checked against
an independent model. See the [standalone demo](../examples/proofs/clamp) and
[article](articles/lean-proofs-to-go.md) for the claims and trust boundary.

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

Success prints a JSON receipt with `"status": "passed"`, tool identities,
checked claims, source/proof/tool hashes, the generated manifest and IR hashes,
and the consumer case count. It contains no machine-specific absolute paths.
All temporary projects and caches owned by the verifier are removed. To retain
the receipt and commit identity outside the checkout:

```sh
git rev-parse HEAD > ../alpha4-walkthrough.commit
uv run --locked python scripts/check_release_candidate.py > ../alpha4-walkthrough.json
```

A failed stage exits nonzero and emits no success receipt. This command checks
the source workflow; verify the public Go module separately:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  LEAN_GO_RELEASE_VERSION=v0.1.0-alpha.4 \
  go test -count=1 -run '^TestTaggedReleaseConsumer$' -v ./integration
```

That test downloads the exact tag through the public Go proxy and checksum
database, verifies its version without a local `replace`, then runs tree,
UInt64, quota and multi-root checks offline with only Go on `PATH`.

For checks before publishing a tag, the candidate test remains available:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  go test -count=1 -run '^TestReleaseCandidateConsumer$' -v ./integration
```

The candidate test exercises Go packages through a temporary module proxy,
including quota and multi-root composition, without substituting a local
`replace`. It is prepublication evidence; it does not establish public tag
availability or the checksum of the published module archive.

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
