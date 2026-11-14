# A copyable Lean-proved word-packing module in Go

This standalone module widens `UInt32` to `UInt64`, narrows modulo 2^32, packs
two 32-bit words into one 64-bit word, and reads its high and low halves. Copy
this directory anywhere and run it with Go **1.27.4** and `CGO_ENABLED=0`.
It has no external Go dependencies or `replace` directives.

This example is included in the [alpha.8 candidate](../../../docs/releases/v0.1.0-alpha.8.md),
Python package `0.1.0a8`. Before publication, regenerate with the CLI from this
same [checkout](../../../docs/cli.md#development-checkout). After
publication, pair its tagged example and CLI. Published alpha.7 also has this
demo, with generator v9; use its
[tagged example](https://github.com/franchb/lean-go/tree/v0.1.0-alpha.7/examples/proofs/words)
and [tagged guide](https://github.com/franchb/lean-go/blob/v0.1.0-alpha.7/docs/cli.md)
for historical reproduction. Current artifacts use generator v10. The checked-in
Go package needs no producer tools to run.

## Run with Go alone

From this directory:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
```

In PowerShell, set the environment first:

```powershell
$env:CGO_ENABLED = "0"
$env:GOTOOLCHAIN = "local"
$env:GOWORK = "off"
$env:GOPROXY = "off"
$env:GOSUMDB = "off"
go test -count=1 ./...
go run .
```

Expected output:

```text
widen(0x80000001) = 0x0000000080000001
pack(0x80000001, 0xffffffff) = 0x80000001ffffffff
high(0x80000001ffffffff) = 0x80000001
low(0x80000001ffffffff) = 0xffffffff
narrow(0x80000001ffffffff) = 0xffffffff
```

Packing places `high` in bits 32–63 and `low` in bits 0–31; all input values are
allowed. This is a numeric word operation, without a byte-order or serialization
contract. The runtime source uses ordinary `<<<` notation, which lowers to the
audited `UInt64.shiftLeft` primitive. Remainder appears only in the
proofs' specification and the independent Go tests; no runtime division or
remainder primitive is extracted. Other widths and generic collections remain
deferred.

## Generated Go API

```go
import words "example.com/lean-go-words-demo/words"

packed, err := words.RunPack(uint32(0x80000001), uint32(0xffffffff), words.DefaultLimits())
if err != nil {
    return err
}
high, err := words.RunHigh(packed, words.DefaultLimits())
if err != nil {
    return err
}
fmt.Printf("%08x\n", high) // 80000001
```

| Typed entrypoint | Input | Result |
| --- | --- | --- |
| `RunWiden` | `uint32` | `uint64` |
| `RunNarrow` | `uint64` | `uint32` |
| `RunPack` | high and low `uint32` | `uint64` |
| `RunHigh` | `uint64` | `uint32` |
| `RunLow` | `uint64` | `uint32` |

Each function takes `Limits` as its last argument and returns `(result, error)`.
Raw callers use `RunRoot` with the corresponding `Words.*` name and immutable
`Value` arguments. The five selected roots share `lean-go-ir/v11`,
`lean-go-manifest/v11`, profile `pure-module/v9` and generator
`lean-go-modulegen/v10` in this checkout. Only those roots are exported; proof helpers are absent
from the runtime package.

Every call starts fresh logical budgets. `widen`, `narrow` and `low` need one
allocation and two steps; `high` needs three/four; `pack` needs five/six. The tests
check these exact thresholds against this generated package, including failure
one step or allocation below the threshold. A zero budget is invalid. Invalid
limits return `ErrLimits`; exhausted valid budgets return `ErrResource`. Failed
typed calls return zero, and failed raw calls return an invalid value. Check the
error before consuming any result. Budget failures are operational behavior
outside the total Lean definitions' theorems.

## Seven source claims

The Lean kernel checks these statements universally, with no input assumptions:

| Claim | Statement | Recorded axioms |
| --- | --- | --- |
| `widen_preserves` | `(widen word).toNat = word.toNat` for every `UInt32` | None |
| `narrow_truncates` | `(narrow word).toNat = word.toNat % 2^32` for every `UInt64` | None |
| `narrow_widen` | `narrow (widen word) = word` for every `UInt32` | `propext` |
| `widen_narrow` | `widen (narrow word) = word % 4294967296` for every `UInt64` | `propext` |
| `low_pack` | `low (pack hi lo) = lo` for every pair of `UInt32` values | `propext`, `Quot.sound` |
| `high_pack` | `high (pack hi lo) = hi` for every pair of `UInt32` values | `propext`, `Quot.sound` |
| `pack_unpack` | `pack (high word) (low word) = word` for every `UInt64` | `propext`, `Quot.sound` |

The `widen_narrow` remainder is a `UInt64` operation in the proposition; the
`narrow_truncates` remainder is a natural-number operation. The proof-only
`pack_bits` helper connects packing to bit-vector concatenation. No Mathlib,
`sorry`, `native_decide` or additional axiom allowance is used.

| File or directory | Purpose |
| --- | --- |
| `source/Words.lean` | Five ordinary scalar Lean definitions |
| `proof/WordsContract.lean` | Seven independently authored closed expected propositions |
| `proof/WordsProofs.lean` | Universal source proofs, separate from runtime code and expectations |
| `proof-contract.json` | Five roots and seven theorem-to-expectation mappings |
| `words/`, `words.ir.json` | Generated Go, manifest and producer IR |
| `proof-evidence.json` | Fresh source-proof evidence bound to source and generated bytes |
| `main.go`, `main_test.go`, `go.mod` | Copyable consumer and independent Go tests |

The assurance is **proved Lean logic with tested, trusted extraction**. Runtime
code does not import proofs; the expected propositions do not import or reuse
them. The checker compares each theorem's full statement with its independent
expectation and records the axioms shown above. The manifest's `program.claims`
remains empty. These source proofs do not prove Lean's compiler, the extractor,
the generated Go runtime or Go's compiler correct.

The Go regression suite checks all five typed and raw operations against
identity, modulo, multiplication and division, and checks all seven laws over
**699 triples**: 169 boundary pairs, 18 wide boundary values and 512 seeded
full-width triples. Cases cover zero, maxima, bits 31/32/63, neighbors of 2^32,
and alternating bits. Additional tests exercise exact and exhausted budgets,
invalid limits, wrong widths/arity, zero results on error and immutable inputs.
These are finite extraction checks, distinct from the universal Lean proofs.

## Regenerate and verify the handoff

Use Python **3.15** through uv, Go **1.27.4** and Lean/Lake **4.36.0-rc2** at
commit `b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`. Before alpha.8 publication,
install this checkout's CLI, replacing the example absolute path:

```sh
uv tool install --force --python 3.15 /absolute/path/to/lean-go
lean-go doctor
```

From the Words example directory, generate into fresh candidate paths:

```sh
lean-go generate --source-dir source --module Words \
  --root Words.high --root Words.low --root Words.narrow --root Words.pack --root Words.widen \
  --out candidate/words --ir-output candidate/words.ir.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/words/manifest.json --output candidate/proof-evidence.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/words/manifest.json --evidence candidate/proof-evidence.json
```

Both proof commands rebuild all seven claims. Review and test fresh candidates
before replacing tracked artifacts; differing existing output is rejected and
preserved. Never hand-edit generated Go, hashes, IR or evidence. See the
[proof-contract guide](../../../docs/proof-contracts.md) for evidence binding.

From the repository root, verify the complete copied example with that installed
CLI. Capture its path before `uv run` adjusts `PATH`:

```sh
words_cli=$(command -v lean-go)
uv run --locked python scripts/check_words_demo.py --cli "$words_cli"
```

In PowerShell, first use `$words_cli = (Get-Command lean-go).Source`.

The verifier regenerates the IR, package, manifest and evidence in a fresh
external workspace, compares exact bytes, and freshly checks the tracked evidence.
It then deploys only `go.mod` and Go sources, hides producer tools and C compilers
from `PATH`, disables module/checksum downloads and cgo, and runs both tests and
the executable with isolated caches. Root `go test ./...` includes the same
Go-only copy check. Current installed-CLI CI runs clamp, flags, words and bytes
through both the direct wheel and a wheel rebuilt from the source distribution
on Linux and Windows, checking 30 source claims per wheel. Published alpha.7's
exact-tag workflow covers its three demos; local checkout checks do not validate
the published tag's artifact hashes.
