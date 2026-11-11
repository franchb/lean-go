# lean-go

Generate pure Go packages from a supported subset of ordinary Lean 4.

The intended workflow is to write executable definitions and proofs in Lean,
then generate Go source that consumers can build with `CGO_ENABLED=0`.
Lean is needed to prove and regenerate components, not to build the consuming
Go application.

This project is at the design stage. No extractor implementation or release
exists yet. The initial assurance target is **proved Lean logic with tested,
trusted extraction**; generated Go is not yet covered by an end-to-end
compiler-correctness proof.

See the [initial design](docs/superpowers/specs/2026-11-11-lean-go-extraction-design.md)
for scope, architecture, proof boundaries, and release acceptance criteria.

Maintainer: [franchb](https://github.com/franchb) · hello@franchb.com
