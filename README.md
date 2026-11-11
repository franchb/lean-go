# lean-go

Generate pure Go packages from a supported subset of ordinary Lean 4.

The intended workflow is to write executable definitions and proofs in Lean,
then generate Go source that consumers can build with `CGO_ENABLED=0`.
Lean is needed to prove and regenerate components, not to build the consuming
Go application.

This project is at the **Lean producer prototype and feasibility-evidence
stage**. The public three-valued normalizer is proved, and pinned compiler
probes, source audits, and rejection controls have been executed. The
[feasibility report](docs/research/extraction-feasibility.md) selects **pure mono
LCNF for the controlled normalizer subset**, with an original adapter.
No Go emitter, generated Go package, or release exists yet.

The initial assurance target is **proved Lean logic with tested, trusted
extraction**. The source proofs and bounded producer evidence are complete;
the Go extraction tests remain future work. Generated Go will not initially
have an end-to-end compiler-correctness proof.

See the [approved design](docs/superpowers/specs/2026-11-11-lean-go-extraction-design.md)
for scope, architecture, proof boundaries, and future release acceptance, and
the [executed feasibility plan](docs/superpowers/plans/2026-11-11-lean-go-feasibility.md)
for implementation responsibilities and reproduction commands.

Maintainer: [franchb](https://github.com/franchb) · hello@franchb.com
