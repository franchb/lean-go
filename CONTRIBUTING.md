# Contributing

The preview focuses on a small, explicit subset of ordinary Lean and generated
Go packages that work with `CGO_ENABLED=0`. Reproducible unsupported examples,
compiler-version investigations and focused tests are useful contributions.

Use Go 1.27.4, Lean 4.35.0 and Python 3.15 through uv. Start with the
[walkthrough](docs/getting-started.md) and the [README](README.md). Run:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
LEAN_GO_MODULES=1 uv run --locked python -m unittest discover -s scripts -p 'test_*.py'
CGO_ENABLED=0 GOTOOLCHAIN=local LEAN_GO_NATIVE=1 LEAN_GO_REGENERATE=1 LEAN_GO_MODULES=1 go test -count=1 ./...
uv run --locked python scripts/differential.py
```

For an extraction change, include a minimal accepted example, meaningful rejection
cases and comparison with native Lean. Preserve exact source provenance and
resource accounting. Do not broaden trusted primitives by namespace or silently
accept compiler forms whose meaning has not been checked.

Edit generator and producer sources rather than generated Go files. Generate into
a fresh output path: differing existing artifacts are intentionally preserved and
rejected. Review the resulting source, IR and manifest together. Source bytes are
hashed; keep repository text in LF form.

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
