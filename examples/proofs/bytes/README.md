# A copyable Lean-proved byte codec in Go

This standalone module encodes every `UInt32` as four `UInt8` fields in a
`ByteQuad` record and decodes that record in little-endian or big-endian order.
It is available in this development checkout and is **unreleased**; published
alpha.7 has no UInt8 engine or byte codec, even though this checkout's package
version still reads `0.1.0a7`. The [walkthrough](../../../docs/articles/proved-byte-codec.md)
explains the application and source claims. Copy this directory and run it with Go **1.27.4** and `CGO_ENABLED=0`:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
```

```text
LE(0x12345678) = [78 56 34 12] -> 0x12345678
BE(0x12345678) = [12 34 56 78] -> 0x12345678
```

The generated package exposes `RunEncodeLE` and `RunEncodeBE`
(`uint32` to `ByteQuad`), `RunDecodeLE` and `RunDecodeBE` (`ByteQuad` to `uint32`),
`NewByteQuad`, and the `B0`–`B3` accessors. Every constructor or root call takes
`Limits` last and returns `(result, error)`; accessors return `(uint8, bool)`.
Always check errors before using a result. Raw callers use the four corresponding
`ByteCodec.*` root names with immutable `Value` arguments.

The twelve source claims prove both inverse directions for each byte order and
all eight encoder-byte positions using natural-number division and remainder.
The expected propositions import only the runtime source, independently of the
proofs. Positions record `propext`; inverse laws record `propext` and `Quot.sound`.
The assurance is **proved Lean logic with tested, trusted extraction**. These
proofs do not prove the compiler, extractor or generated runtime correct, and do
not describe resource-limit failures.

Runtime source uses only byte/word casts, `UInt32` shifts and OR. The generated
bundle uses `lean-go-ir/v13`, `lean-go-manifest/v13`, `pure-module/v11`, and
`lean-go-modulegen/v10`. Tests independently compare all four typed and raw roots
with `encoding/binary`, check the twelve laws over 652 boundary, distinct-byte,
one-hot and seeded cases, and exercise malformed inputs, ownership and budgets.
Each encoder requires 11 allocations and 12 steps; each decoder requires 13
allocations and 15 steps. Valid exhausted budgets return `ErrResource`; invalid
limits return `ErrLimits`. Failed typed calls return zero/invalid records; failed
raw calls return invalid values.

To regenerate, use Python **3.15** through uv, Go **1.27.4**, Lean/Lake
**4.36.0-rc2** at commit `b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`, and the
installed CLI from the same checkout. Install it from the repository root with
`uv sync --locked --reinstall-package lean-go-tools`, then use
`uv run --locked lean-go` there or the absolute `.venv/bin/lean-go` path from
another directory (`.venv/Scripts/lean-go.exe` on Windows). The commands below
assume that checkout CLI is available as `lean-go`. From this example directory:

```sh
lean-go generate --source-dir source --module ByteCodec \
  --root ByteCodec.decodeBE --root ByteCodec.decodeLE \
  --root ByteCodec.encodeBE --root ByteCodec.encodeLE \
  --out candidate/bytes --ir-output candidate/bytes.ir.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/bytes/manifest.json --output candidate/proof-evidence.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/bytes/manifest.json --evidence candidate/proof-evidence.json
```

Generate into fresh paths and review the Go, IR, manifest and bound proof evidence
together. From the repository root, run the complete verifier with the absolute
path of that installed CLI:

```sh
uv run --locked python scripts/check_bytes_demo.py --cli /absolute/path/to/lean-go
```

It checks fresh generation and all twelve proofs, compares exact tracked artifact
bytes, then tests and runs a copy containing only Go sources and `go.mod`, with
producer tools hidden and cgo and downloads disabled. Root Go tests exercise the
same isolated deployment. See the [proof contract guide](../../../docs/proof-contracts.md)
for evidence binding and the [CLI guide](../../../docs/cli.md) for installation.
