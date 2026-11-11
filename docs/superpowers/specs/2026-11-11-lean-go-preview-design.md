# Experimental public preview

Publish the completed typed API and UInt64 work as `v0.1.0-alpha.1` in
`github.com/franchb/lean-go`. This is experimental software for the documented
subset of ordinary Lean, with a trusted extraction pipeline rather than a
formally verified compiler. Keep public attribution to franchb and
`hello@franchb.com`.

The repository carries the Apache License 2.0, a runnable consumer walkthrough,
release notes, and contributor instructions. Generated consumers need only Go
1.27.4 with `CGO_ENABLED=0`. Extraction retains pinned Lean 4.35.0 and Python 3.15
through uv. Unsupported forms and all resource-limit behavior remain explicit.

CI runs regular Go tests and vet on Ubuntu 24.04 and Windows Server 2025. A
separate Ubuntu job runs source admission and native/regeneration gates with
pinned tools. Workflow actions use verified immutable commits, permissions are
read-only, and checkout does not retain credentials. Normalize repository text to
LF because provenance hashes depend on bytes.

A tag-only integration gate fetches the exact module tag into an independent
temporary Go project with a fresh module cache and no local replacements. It
verifies the selected version, then runs typed tree and UInt64 consumers with
only Go on PATH and network module fetching disabled. Run that gate on both
Linux and Windows. Native Lean validation remains Linux evidence.

Publication is ordered: validate the candidate and scan public history, create
the public repository, push the reviewed default branch, wait for actual CI
success, tag that exact commit, verify tagged consumers, then publish a GitHub
prerelease. Never rewrite a public version tag. Preserve the evidence in the
release record. No unrelated announcements or messages are part of this work.
