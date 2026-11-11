# Public preview implementation plan

**Goal:** Publish a reproducible experimental preview with actual Windows and
tagged Go-only consumer evidence.

**Architecture:** Keep extraction and generated packages unchanged. Add release
documentation, pinned CI and an opt-in integration test around the public module
boundary. The task tracker owns status.

## 1. Prepare the public project

Add the canonical Apache 2.0 license, project copyright notice, LF attributes,
and contribution instructions. Update README status and link a walkthrough that
starts from a fresh Go module using an exact prerelease tag. Explain trusted
source elaboration, supported forms, immutable APIs and resource errors.

## 2. Add executable publication gates

Add `integration/release_test.go`: validate the requested tag, create an isolated
module/cache, fetch the exact release, reject replacement/version drift, and run
public typed tree and UInt64 APIs with Go alone. Reuse existing environment and
tool helpers. Test guard behavior locally; the real network path is exercised
after tag creation.

Add `.github/workflows/ci.yml` with Linux/Windows Go tests and vet plus Linux
Python admission/native/regeneration. Pin action commits and tool versions.
Tag runs exercise the isolated consumer on both operating systems; extraction
need not repeat for an unchanged commit already verified on its branch.

## 3. Review and validate locally

Run standard Go tests and vet with CGO disabled, cross-compile integration tests,
check workflow syntax and pinned downloads, and check documentation links and
identity/privacy across all public history. Obtain independent review of the
workflow, release test and publication claims. Run Ripwire contract/quality checks
for changed code. Commit as franchb and transfer to the requested local checkout.

## 4. Publish and verify

Create `franchb/lean-go` publicly and push the reviewed candidate to `main` over
HTTPS. Observe Linux, Windows and extraction jobs to completion; fix any real
failure before tagging. Tag the verified commit `v0.1.0-alpha.1`, then run the
isolated exact-tag consumer locally and in both CI operating systems. Publish a
GitHub prerelease only after those gates pass, with links to the observed runs.
Record completed evidence and synchronize task status. Preserve a clean local
checkout and report the repository/release links with concrete limitations.
