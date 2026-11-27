# Contributing

Useful contributions include reproducible examples of Lean code the extractor
rejects or mistranslates, focused tests, and changes to the producer, the
emitters or the validation library that come with evidence.

## Tools

- Go 1.27.4. Tracked artifacts are byte-exact only when `lean-go` is built
  with this release; `lean-go regenerate` refuses any other.
- Lean 4.36.0 at commit `611c117bfaf7b7ae1cfcf246ce6fba31e8f509e9`, with
  `lean` and `lake` on `PATH`.

Check them with `go run ./cmd/lean-go doctor`.

## Before sending a change

From the repository root:

```sh
go vet ./...
go test -count=1 ./...
```

`go test` uses the tracked generated packages and needs no Lean; it took
about 3 minutes on a shared 4-core machine, most of it in `integration` and
`internal/modulegen`.
Tests that run Lean are enabled by environment variables (`LEAN_GO_MODULES`,
`LEAN_GO_NATIVE`, `LEAN_GO_REGENERATE`, `LEAN_GO_PROOFS`, `LEAN_GO_QUOTA`,
`LEAN_GO_FORMATS`, `LEAN_GO_DIFFERENTIAL`, `LEAN_GO_VALIDATE`,
`LEAN_GO_PARITY`); the Linux extraction job in
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) sets all of them and
shows the exact steps. With every variable set, run `go test -p 1` so that
one Lean build runs at a time; the whole suite then took about 40 minutes on
a 4-core machine with a warm producer cache. Run the ones that cover your
change and report in the pull request which checks you ran and which you
could not.

The developer harnesses are subcommands of `go run ./cmd/lean-go-dev` (`help`
lists them). After a change to the command line or the proof demos, run the
installed-CLI check that the "Installed CLI" workflow runs on Linux and
Windows:

```sh
go run ./cmd/lean-go-dev installed-cli
```

## Regenerate tracked artifacts

Generated Go, IR, proof evidence, wire-format examples and validation reports
are tracked. Never edit them by hand. After a change to the producer, an
emitter, the validator or the validation library, run:

```sh
go run ./cmd/lean-go regenerate
```

It produces every artifact in a fresh temporary tree, replaces tracked copies
only after all of them succeeded, and ends with `N of M artifacts changed`; a
second run reports `0 of M`. A run on an unchanged checkout (120 artifacts,
two jobs, warm producer cache) took about 3 minutes on a shared 4-core
machine. Each validation process is capped at 4096 MiB; the largest tracked
program peaks at about 1.7 GB. On a machine short of memory, use `--jobs 1`.
`go run ./cmd/lean-go regenerate --coverage` prints the validation coverage
table, which must match the one in
[translation-validation.md](docs/research/translation-validation.md#9-coverage).
Review the changed source, IR, manifest and evidence together. Source bytes
are hashed, so keep repository text in LF form.

## Extraction changes

Include a minimal accepted example, rejection cases for the forms you do not
admit, and a comparison with Lean's own evaluation. Keep source provenance and
resource accounting exact. Do not admit primitives by namespace or accept
compiler forms whose meaning has not been checked; the admitted numeric
primitives are the explicit table in
[`Primitives.lean`](producer/LeanGo/Module/Primitives.lean). A new primitive
also needs the IR semantics in `producer/LeanGo/Validate` and the emitters to
agree with it.

For a library with source proofs, use a [proof contract](docs/proof-contracts.md)
with independently written expected statements, and review the requirements,
proofs, generated package and evidence together.

Contributions are licensed under the project's
[Apache License 2.0](LICENSE).
