# A proved four-byte header in Go

A version-1 header has two flag bits and an unsigned 16-bit payload length. The
first byte must equal 1. The second byte stores the flags in bits 0 and 1, with
all six reserved bits clear. The last two bytes hold the length, high byte first.

Copy this directory and run the example using Go 1.27.4.

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
```

```text
[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]
[02 80 00 00] -> unsupported version
[01 04 00 00] -> reserved flags
```

`NewByteQuadFromBytes` accepts a Go `[4]byte` and limits. Pass the record to
`RunParse`. Check the Go error first, then use `AsParsed`,
`IsUnsupportedVersion` or `IsReservedFlags` on the result. A bad version takes
precedence over reserved flags. Successful headers expose `Flags()` and
`Length()`.

To encode, construct a `Header` with `NewHeader(flags, length, limits)` and call
`RunEncode`. The encoder checks flags before length. It returns `reservedFlags`
for any flag above bit 1 and `lengthOverflow` for lengths above 65,535 with
valid flags. On success, `AsEncoded` returns the record and its `Bytes()` method
returns the wire array. Both roots also accept raw calls through `RunRoot`.
The demonstration uses ordinary Lean records and two explicit result types.

## Source guarantees

Nine kernel-checked propositions characterize parser success, both parser
rejections, and the 16-bit bound on every parsed length. They also characterize
encoder success, both encoder rejections, and round trips in both directions
for all admissible inputs. The success propositions fix the byte order and the
values of every field. The proof contract states its expectations independently
of the proof module.

Extraction and Go execution remain tested, trusted components. The Go tests
compare typed and raw calls with `encoding/binary` for all 262,144 valid
flags/length combinations. They also exercise every version byte, reserved flags,
malformed records and resource limits.

## Reproduce the evidence

Use Python 3.15, Lean/Lake 4.36.0 and Go 1.27.4. From the repository root,
install the checkout CLI, activate its environment, and enter the example.

```sh
uv sync --locked --reinstall-package lean-go-tools
. .venv/bin/activate
cd examples/proofs/header
```

```sh
lean-go generate --source-dir source --module HeaderCodec \
  --root HeaderCodec.encode --root HeaderCodec.parse \
  --out candidate/header --ir-output candidate/header.ir.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/header/manifest.json --output candidate/proof-evidence.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/header/manifest.json --evidence candidate/proof-evidence.json
```

From the repository root, the following command compares the regenerated package
and evidence with the tracked bytes and runs a Go-only copy with downloads
disabled.

```sh
uv run --locked python scripts/check_header_demo.py --cli "$PWD/.venv/bin/lean-go"
```

The [CLI guide](../../../docs/cli.md) lists the supported producer identities.
The [proof contract guide](../../../docs/proof-contracts.md) describes the
evidence format.
