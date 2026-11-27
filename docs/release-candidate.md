# Check source proofs, generated Go and deployment

Use the CLI and examples from the same checkout or release tag. This checkout
prepares lean-go `0.1.0a15` with generator v20 and defaults to stable Lean
4.36.0; its other supported compiler is 4.36.0-rc2.
[Alpha.15 release notes](releases/v0.1.0-alpha.15.md)

This walkthrough checks fresh source theorems, generated artifacts and isolated
Go consumers. APIs remain experimental. The [byte article](articles/proved-byte-codec.md)
explains the current application; the [clamp article](articles/lean-proofs-to-go.md)
is a historical walkthrough and retains its original release context.

## Check this development checkout

Use Go **1.27.4** and pinned Lean **4.36.0** at commit
`611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9`, with Go, Lean and Lake on `PATH`.
From the repository root:

```sh
git rev-parse HEAD
git status --short
go run ./cmd/lean-go doctor
go run ./cmd/lean-go-dev installed-cli
```

The installed-CLI check builds lean-go with `go build`, installs a second
binary with `go install` from a module archive of the tracked files (the route
of `go install ...@<tag>`), and runs each from a read-only directory outside the
checkout. For every standalone demo it copies the demo, generates fresh
artifacts through the binary, compares package, IR and evidence bytes with the
tracked ones, rechecks the source claims against an independent inventory, and
then deploys only Go sources and `go.mod`, running the tests and executable
offline with cgo and downloads disabled and producer tools hidden, and
comparing the program's output with the expected text.

| Demo | Source claims | Independent finite Go cases |
| --- | --- | --- |
| [Clamp](../examples/proofs/clamp) | 3 | 681 input pairs |
| [Flags](../examples/proofs/flags) | 8 | 4,752 input pairs |
| [Words](../examples/proofs/words) | 7 | 699 boundary and seeded triples |
| [Bytes](../examples/proofs/bytes) | 12 | 652 boundary, one-hot and seeded cases |
| [Header](../examples/proofs/header) | 9 | All 262,144 valid headers plus rejection and limit cases |
| [Wire16](../examples/proofs/wire16) | 13 | All 65,536 words and all 262,144 valid typed headers |
| [Adler-32](../examples/proofs/adler32) | 2 | Empty and multi-block input, checksum agreement with `hash/adler32` and raw/typed resource boundaries |
| [CRC-16/XMODEM](../examples/proofs/crc16) | 2 | Every byte from nine boundary states, the published check value and chunked input |
| [Normalizer](../examples/proofs/normalizer) | 2 | All 7,265 expressions up to depth two under nine valuations, 512 seeded deeper expressions and a 1,001-negation chain |
| [Clock](../examples/proofs/clock) | 3 | 17 boundary and 1,024 seeded second counts, plus wrapping and error cases |

Both binaries check all ten demos, freshly checking 61 source claims per
binary, and the two composition claims of the
[checksummed Wire16 frame](../examples/proofs/checkedframe): a v2 proof contract
checks those two propositions once and binds them to both component manifests,
for a total of 63 distinct source claims. Current regenerated manifests and
proof evidence use generator v20 and the single IR schema `lean-go-ir/v24`.

## Reproduce a published tag

Published tags up to `v0.1.0-alpha.15` predate the Go command: their
generation and proof checks are the Python package and verifier scripts of that
tag. Reproduce such a tag with the walkthrough tracked in it, for example the
[alpha.15 walkthrough](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.15/docs/release-candidate.md)
(eight standalone demos and their composition, 58 distinct source claims) or
the [alpha.7 walkthrough](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.7/docs/release-candidate.md)
(three demos, 18 source claims). Alpha.4 remains paired with Lean 4.35.0; use
its [tagged walkthrough](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.4/docs/release-candidate.md).
Local candidate results do not establish a public tag's availability, module
checksum or future workflow state.

## Run the complete source-to-Go check

```sh
go run ./cmd/lean-go-dev release-candidate
```

The command copies the ordinary Lean clamp example into a temporary external
project, extracts fresh metered Go with the checkout's pipeline, checks three
source theorems, verifies the resulting evidence, and builds a separate
Go-only consumer. The consumer has only Go on its tool path, uses
`CGO_ENABLED=0`, and disables module network access. It compares 177 boundary
and seeded cases with an independent integer model, including values above
2^53 and `math.MaxUint64`.

Success prints a JSON receipt with `"status": "passed"`,
`"candidate": "0.1.0a15"`, tool identities (the Go release, the Lean version
and commit, and `lean_go_pipeline`, the digest of the pipeline sources),
checked claims, source/proof/tool hashes, the generated manifest and IR hashes,
the digest of the walkthrough's own source and the consumer case count. It
contains no machine-specific absolute paths. The candidate label describes this
checkout, including generator v20; it does not establish publication. Interpret
the hashes together with the recorded commit and working-tree changes. All
temporary projects and caches owned by the walkthrough are removed. To retain
the receipt and commit identity outside the checkout:

```sh
git rev-parse HEAD > ../checkout-walkthrough.commit
git status --short > ../checkout-walkthrough.status
go run ./cmd/lean-go-dev release-candidate > ../checkout-walkthrough.json
```

A failed stage exits nonzero and prints no receipt. This command checks the
selected source checkout. The following separate test checks the public Go
module:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  LEAN_GO_RELEASE_VERSION=v0.1.0-alpha.15 \
  go test -count=1 -run '^TestTaggedReleaseConsumer$' -v ./integration
```

That test downloads the exact tag through the public Go proxy and checksum
database, verifies its version without a local `replace`, then runs tree,
UInt64, quota and multi-root checks offline with only Go on `PATH`. It covers the
existing root-module packages. On a tag, the "Installed CLI" workflow also
installs the tagged command with `go install github.com/franchb/lean-go/cmd/lean-go@<tag>`
and runs `go run ./cmd/lean-go-dev installed-cli --cli <binary> --expect-tag <tag>`
on Linux and Windows. Those are configured coverage expectations, not claims
that a future tag run passed. The public Go consumer test alone does not cover
every byte, bitwise or width conversion operation.

To check the current checkout through a local module proxy, the candidate test
remains available:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  go test -count=1 -run '^TestReleaseCandidateConsumer$' -v ./integration
```

The candidate test exercises Go packages through a temporary module proxy,
including quota and multi-root composition, without substituting a local
`replace`. The candidate checkout serves a synthetic archive as
`v0.1.0-alpha.15` from that local proxy. This checks the local archive's Go
behavior; it does not establish public tag availability or the checksum of a
future published module.

## Inspect the individual stages

The [clamp runtime](../examples/proofs/clamp/source/Clamp.lean) delegates to a
[local helper](../examples/proofs/clamp/source/Clamp/Bounds.lean): return the input
when it is within the limit, otherwise return the limit. Its
[proofs](../examples/proofs/clamp/proof/ClampProofs.lean) establish the bound,
preservation of already-bounded inputs and idempotence. The independently written
[expected propositions](../examples/proofs/clamp/proof/ClampContract.lean) and
[JSON contract](../examples/proofs/clamp/proof-contract.json) select those claims.

On a POSIX shell, keep an external project for inspection. From the repository
root:

```sh
candidate_work=$(mktemp -d)
cp -R examples/proofs/clamp "$candidate_work/project"

go run ./cmd/lean-go generate \
  --source-dir "$candidate_work/project/source" \
  --module Clamp --root Clamp.clamp --mode metered \
  --out "$candidate_work/clamp" --ir-output "$candidate_work/clamp.ir.json"

go run ./cmd/lean-go check-proofs \
  --contract "$candidate_work/project/proof-contract.json" \
  --manifest "$candidate_work/clamp/manifest.json" \
  --output "$candidate_work/proof-evidence.json"

go run ./cmd/lean-go check-proofs \
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
package; it does not need the source project, Lean or a C compiler.

The Lean kernel checks the selected source statements. Compilation, extraction,
the generated runtime and the Go compiler remain tested and trusted. Lean
elaboration, including compile-time metaprograms, is also trusted; this workflow
is not a sandbox. Read [proof contracts](proof-contracts.md) for the exact evidence
and validation rules, or [quota](quota.md) for a multi-root library API.
