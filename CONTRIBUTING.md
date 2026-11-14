# Contributing

The preview focuses on a small, explicit subset of ordinary Lean and generated
Go packages that work with `CGO_ENABLED=0`. Reproducible unsupported examples,
compiler-version investigations and focused tests are useful contributions.
The [compatibility harness](docs/compiler-compatibility.md) qualifies later Lean
releases before any production pin change.

Use Go 1.27.4, Lean 4.36.0-rc2 and Python 3.15 through uv. Start with the
[walkthrough](docs/getting-started.md) and the [README](README.md). Run:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
LEAN_GO_MODULES=1 LEAN_GO_DIFFERENTIAL=1 LEAN_GO_QUOTA=1 LEAN_GO_PROOFS=1 \
  uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
uv run --locked python scripts/differential.py
uv run --locked python scripts/check_quota.py
```

For an extraction change, include a minimal accepted example, meaningful rejection
cases and comparison with native Lean. Preserve exact source provenance and
resource accounting. Do not broaden trusted primitives by namespace or silently
accept compiler forms whose meaning has not been checked.

Edit generator and producer sources rather than generated Go files. Generate into
a fresh output path: differing existing artifacts are intentionally preserved and
rejected. Review the resulting source, IR and manifest together. Source bytes are
hashed; keep repository text in LF form.

For a library with source proofs, use a [proof contract](docs/proof-contracts.md)
and independently authored expected propositions. Review the requirements,
proofs, generated package and bound evidence together. The checker verifies the
selected Lean statements; compilation and extraction remain tested and trusted.

The [alpha.8 candidate](docs/releases/v0.1.0-alpha.8.md), Python package
`0.1.0a8`, includes clamp (three claims), flags (eight),
[words](examples/proofs/words) (seven) and the [byte codec](examples/proofs/bytes)
(twelve), with a runnable Go application for each. Before publication, validate
with this checkout's CLI. Published alpha.7 has the first three demos and no UInt8
support. Current artifacts
use `lean-go-modulegen/v10`; byte exports use v12/v13 IR and `pure-module/v10` or
`pure-module/v11`. Earlier IR schemas retain their restrictions, including Words'
v11 IR and `pure-module/v9`.
Pair tagged examples with the CLI from the same tag. For current artifacts,
[install the CLI from this same checkout](docs/cli.md#development-checkout);
published alpha.7's v9 generator does not reproduce them.
After changing a demo or CLI handoff, run the relevant verifier unit tests and
the complete installed-wheel smoke:

```sh
uv run --locked python scripts/test_clamp_demo.py
uv run --locked python scripts/test_flags_demo.py
uv run --locked python scripts/test_words_demo.py
uv run --locked python scripts/test_bytes_demo.py
uv run --locked python scripts/check_cli_install.py
```

The smoke audits both a direct wheel and an sdist-rebuilt wheel. For each wheel,
it regenerates all four demos, compares exact bytes, freshly checks all 30 source
claims and deploys each using only Go sources and `go.mod`. Root Go tests also
exercise all four nested modules by copying them into isolated Go-only
environments. Tests inside a nested module
can be run directly; they are otherwise excluded from root package discovery.
The [byte walkthrough](docs/articles/proved-byte-codec.md) explains how to review
a concrete contribution: four roots, twelve independent expected propositions,
fresh proof evidence and Go checks against `encoding/binary`. For changes to the
UInt8 engine, use the focused admission and differential tests in the
[UInt8 report](docs/research/extraction-uint8.md). Broad generic collections,
UInt16 and direct UInt8/UInt64 casts remain deferred.

CI executes ordinary Go tests on Linux and Windows, and extraction gates on Linux.
Tag workflows also fetch the exact published version into separate Go-only
consumers. A cross-compiled Windows binary alone is not Windows runtime evidence.

The Linux extraction job also generates small programs and compares an independent
source model with native Lean and both Go APIs. To investigate an extraction change,
add program seeds or replay a saved failure as described in the
[differential-testing guide](docs/research/differential-testing.md). Report rejected
programs and tool failures separately from semantic mismatches; do not discard them
to make a campaign pass.

Describe the concrete behavior change and actual validation in pull requests.
Report checks you could not run. Contributions use the project's
[Apache License 2.0](LICENSE).
