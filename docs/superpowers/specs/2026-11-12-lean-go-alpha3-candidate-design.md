# Alpha.3 release candidate

Prepare an installable-preview candidate containing the completed differential
testing, multi-root extraction, quota and reusable proof-contract work. The
published preview remains `v0.1.0-alpha.2` until a separate release is authorized.
This milestone prepares and validates a feature branch; it does not merge main,
create a tag or publish a release.

## Consumer evidence

One shared Go consumer program exercises the existing tree and UInt64 APIs plus
quota creation/reservation/release/rejection/immutability and composed multi-root
counter operations. It runs with `CGO_ENABLED=0`, only Go on PATH and network
module resolution disabled after download. The selected module must have the
expected path and exact version without a `replace` directive.

Before publication, a default Go test builds a local module-proxy artifact from
the checkout's public Go library sources (`generated/`, `quota/`, `go.mod` and
license) at `v0.1.0-alpha.3`. This exercises versioned dependency resolution and
the public APIs on Linux and Windows without requiring a nonexistent tag. It is
candidate evidence, not a claim that the public tag exists or that a future public
module archive has the same checksum.

The existing tagged consumer uses the same API checks but continues to download
the requested published version from the public Go proxy with checksum database
verification. It has no local candidate fallback. Its exact-tag result can only
be established after publication.

## Producer walkthrough

A dependency-free Python 3.15 verifier, invoked through `uv`, copies the ordinary
clamp runtime/proofs/contract into a temporary external project. It generates Go,
publishes and freshly verifies source-proof evidence, then runs a Go-only
consumer against boundary cases including the complete UInt64 range. It prints
a JSON receipt only after all stages succeed and leaves the checkout untouched.

Run this workflow from a clean archive of the final tracked candidate, as well
as in extraction CI. The human walkthrough includes the candidate clone,
recording its commit, the verifier and explicit source/proof/Go stages. All
commands distinguish the untagged candidate from the still-published alpha.2.

## Compatibility and assurance

No compiler/runtime, Lean source semantics, generated Go or proof-evidence
changes are needed. Single-root v6/profile v4 and multi-root v7/profile v5 remain
as documented. Go stays on 1.27.4, Lean on pinned 4.35.0 and Python on 3.15 through
`uv`; consumers remain `CGO_ENABLED=0`. Broad generic collections stay deferred.

The release notes describe source theorems and the fixed axiom checks separately
from tested, trusted extraction. Lean elaboration, including compile-time
metaprograms, remains trusted. Neither the release nor its receipt is a proof of
compiler correctness or a sandbox guarantee.

## Candidate acceptance

The final candidate commit must have green Linux and Windows Go jobs, including
candidate-version consumer execution, and green Lean extraction/proof/admission/
differential/walkthrough CI. Local checks and independent review cover the new
test and verifier paths. Documentation and the final handoff identify exactly
what was tested before publication; public-tag validation remains a later step.
