# Try the alpha.3 release candidate

`v0.1.0-alpha.3` is an **unpublished release candidate**. The published preview
is still `v0.1.0-alpha.2`; there is no alpha.3 tag to install yet. This candidate
brings together multi-root extraction, the quota library and reusable source-proof
contracts. Its API remains experimental.

## Start from a clean checkout

Use Go **1.27.4**, the pinned Lean **4.35.0**, and **uv** with Python **3.15**.
Make Go, Lean and Lake available on `PATH`. Clone into a new directory:

```sh
git clone --branch codex/alpha3-release-candidate --single-branch \
  https://github.com/franchb/lean-go.git lean-go-alpha3
cd lean-go-alpha3
git rev-parse HEAD
uv python install 3.15
```

The branch can advance during preparation. Record the full commit printed above
alongside your verification output. The verifier operates on its current source
checkout; it does not establish that a working tree is clean. CI runs it from a
fresh checkout, and release preparation also checks a clean tracked snapshot.

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
git rev-parse HEAD > ../alpha3-walkthrough.commit
uv run --locked python scripts/check_release_candidate.py > ../alpha3-walkthrough.json
```

A failed stage exits nonzero and emits no success receipt. This command checks
the source workflow; the candidate's exact-version module consumer is a separate
check:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  go test -count=1 -run '^TestReleaseCandidateConsumer$' -v ./integration
```

That test exercises candidate Go packages through a temporary module proxy,
including quota and multi-root composition, without substituting a local
`replace`. It is prepublication evidence. The public exact-tag consumer check
runs after an actual release tag exists.

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
