# A proved four-byte codec you can run in Go

A four-byte field in a file or network protocol needs an exact byte order and a
reliable round trip. This example defines that small library in ordinary Lean,
proves twelve source properties and generates a Go package with an independent
`encoding/binary` test oracle. Its Go application converts `0x12345678` in both
orders and reconstructs the original word.

The byte codec and UInt8 support are part of the [alpha.8 candidate](../releases/v0.1.0-alpha.8.md),
Python package `0.1.0a8`. Before publication, use this checkout's CLI and generator
v10. Published alpha.7 has generator v9 and no
UInt8 support. After alpha.8 publication, pair its tagged example and CLI.

## Run the application with Go alone

From the repository root, use Go **1.27.4**:

```sh
(
  cd examples/proofs/bytes
  CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
  CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
)
```

```text
LE(0x12345678) = [78 56 34 12] -> 0x12345678
BE(0x12345678) = [12 34 56 78] -> 0x12345678
```

Copy [the example directory](../../examples/proofs/bytes) into another location to
run the same commands there. It is a standalone Go module. Deployment needs its
`go.mod`, `main.go` and four generated Go files under `bytes/`; keep `main_test.go`
to retain the independent regression tests. Lean, Python, uv and a C compiler
are not needed to build or run the generated application.

The complete, tested [main.go](../../examples/proofs/bytes/main.go) encodes and
decodes both orders, checks every returned error, and prints the result. Its
`octets` helper turns the immutable generated record into a Go `[4]byte`:

```go
func octets(q codec.ByteQuad) ([4]byte, error) {
    var out [4]byte
    for i, field := range []func() (uint8, bool){q.B0, q.B1, q.B2, q.B3} {
        value, ok := field()
        if !ok {
            return [4]byte{}, fmt.Errorf("read byte %d: invalid ByteQuad", i)
        }
        out[i] = value
    }
    return out, nil
}
```

This is an excerpt from the application, whose imports include `fmt` and
`codec "example.com/lean-go-bytes-demo/bytes"`. Each accessor returns a validity
flag because a zero `ByteQuad` wrapper is invalid. The Go array belongs to the
consumer; the extracted Lean runtime uses a concrete record with four fields.

| Generated entry point | Input → successful result |
| --- | --- |
| `RunEncodeLE`, `RunEncodeBE` | `uint32` → `ByteQuad` |
| `RunDecodeLE`, `RunDecodeBE` | `ByteQuad` → `uint32` |
| `NewByteQuad` | Four `uint8` arguments → `ByteQuad` |
| `ByteQuad.B0` through `B3` | No arguments → `(uint8, bool)` |

Each constructor and root takes `Limits` last and returns `(result, error)`.
The application uses `codec.DefaultLimits()`. To decode bytes supplied by another
Go API, pass them in field order to `NewByteQuad`, check its error, then call the
appropriate decoder. Raw callers use `RunRoot` with `ByteCodec.encodeLE`,
`ByteCodec.decodeLE`, `ByteCodec.encodeBE` or `ByteCodec.decodeBE`.

## The small runtime definition

The [runtime source](../../examples/proofs/bytes/source/ByteCodec.lean) defines
global `ByteQuad` with fields `b0` through `b3`, each a `UInt8`. For little endian:

```lean
def encodeLE (w : UInt32) : ByteQuad :=
  ⟨w.toUInt8, (w >>> (8 : UInt32)).toUInt8,
    (w >>> (16 : UInt32)).toUInt8, (w >>> (24 : UInt32)).toUInt8⟩
```

This definition is inside `namespace ByteCodec`. Narrowing to `UInt8` keeps the
value modulo 256. Logical shifts select the next eight bits. The big-endian
encoder reverses those field positions. Each decoder widens the four bytes to
`UInt32`, shifts them into position and combines them with bitwise OR.
The runtime uses no Lean arrays or generic collections. Natural numbers and bit
vectors appear only in the proof modules.

## Twelve precise source claims

The [contract](../../examples/proofs/bytes/proof-contract.json) maps twelve named
[theorems](../../examples/proofs/bytes/proof/ByteCodecProofs.lean) to independently
authored [expected propositions](../../examples/proofs/bytes/proof/ByteCodecContract.lean).
The expected module imports the runtime source, without importing the proof
implementation. The checker compares each theorem's type with its selected
expectation and asks Lean to rebuild the proofs.

Four claims establish both inverse directions for both orders, universally over
every `UInt32` word and every four-byte record:

```text
decodeLE (encodeLE word) = word
decodeBE (encodeBE word) = word
encodeLE (decodeLE bytes) = bytes
encodeBE (decodeBE bytes) = bytes
```

Inverse laws alone allow any consistent ordering. Eight additional claims fix
the intended byte positions with arithmetic expectations. Each encoded field's
`toNat` equals `word.toNat / divisor % 256`:

| Field | Little-endian divisor | Big-endian divisor |
| --- | --- | --- |
| `b0` | 1 | 16777216 |
| `b1` | 256 | 65536 |
| `b2` | 65536 | 256 |
| `b3` | 16777216 | 1 |

The end positions use `word.toNat % 256` directly. The division and remainder
are specification arithmetic and do not broaden runtime primitive admission.
Fresh evidence records `propext` for the eight position claims, and `propext`
plus `Quot.sound` for the four inverses. The existing axiom allowlist is
unchanged; `sorry`, custom axioms and `native_decide` do not satisfy the checker.

## Regenerate and recheck the same artifacts

Regeneration uses Go **1.27.4**, Python **3.15** through **uv**, and Lean/Lake
**4.36.0-rc2** at exact commit
`b35cb10cf2ced51a2fd1880cfdbf75c71ab7a35a`. The Python runtime dependencies use
only the standard library. Install the CLI from this same checkout, from its
root:

```sh
uv python install 3.15
uv sync --locked --reinstall-package lean-go-tools
uv run --locked lean-go doctor
checkout_cli="$(pwd)/.venv/bin/lean-go"
uv run --locked python scripts/check_bytes_demo.py --cli "$checkout_cli"
```

On Windows, the executable is `.venv/Scripts/lean-go.exe`. The verifier generates
fresh artifacts, compares the tracked Go, IR, manifest and evidence bytes,
rechecks all twelve proofs, then tests and runs a copy with only Go sources and
`go.mod`. It hides producer tools and disables cgo and downloads.

For a reviewable regeneration, choose a new output directory. The following
commands run from the repository root and leave the tracked artifacts intact:

```sh
uv run --locked lean-go generate \
  --source-dir examples/proofs/bytes/source --module ByteCodec \
  --root ByteCodec.decodeBE --root ByteCodec.decodeLE \
  --root ByteCodec.encodeBE --root ByteCodec.encodeLE \
  --out candidate/bytes --ir-output candidate/bytes.ir.json
uv run --locked lean-go check-proofs \
  --contract examples/proofs/bytes/proof-contract.json \
  --manifest candidate/bytes/manifest.json --output candidate/proof-evidence.json
uv run --locked lean-go check-proofs \
  --contract examples/proofs/bytes/proof-contract.json \
  --manifest candidate/bytes/manifest.json --evidence candidate/proof-evidence.json
```

Both proof commands freshly rebuild the source proofs. The first writes bound
evidence; the second requires the existing bytes to match the fresh result.
Review the runtime source, expected propositions, proofs, generated Go, IR,
manifest and evidence together. Record the source commit and any local changes:
a receipt from this candidate describes these inputs and does not establish
public tag availability. See [proof contracts](../proof-contracts.md) for the
binding rules.

## What the Go tests establish

The [standalone tests](../../examples/proofs/bytes/main_test.go) compare all four
roots through typed and raw APIs with Go's `encoding/binary`. They check the
twelve laws over 652 finite cases: 84 word/distinct-byte boundary combinations,
32 one-hot cases, 24 byte-position boundaries and 512 seeded cases. Arbitrary
input records exercise decode/encode as well as encode/decode.

The tests also check immutable ownership, malformed inputs, invalid zero wrappers
and exact resource thresholds. Each encoder needs 11 allocations and 12 steps;
each decoder needs 13 allocations and 15 steps. Calls with valid but exhausted
budgets return `ErrResource`; invalid limit settings return `ErrLimits`.
Malformed inputs return `ErrMalformed`. Failed record calls return invalid zero
wrappers, failed scalar calls return zero, and failed raw calls return invalid
values. Check errors before reading results. Each call starts fresh budgets.

The Lean kernel establishes the selected universal **source** statements. The
Lean compiler, extraction adapter and lowering, Go emitter, generated runtime,
Go compiler and execution environment remain tested and trusted. Resource limits
can reject a call even though the Lean function is total. Lean elaboration,
including macros and initializers, also requires trusted source; this tool is
not a sandbox. The finite Go tests supply independent implementation evidence,
not a universal proof of the Go library or a compiler-correctness theorem.

The [UInt8 report](../research/extraction-uint8.md) describes the fourteen exact
new runtime leaves, selective schemas and focused validation. The useful unit
of contribution here is the complete small library: explicit source requirements,
checked proofs, reproducible generated bytes and an application a Go consumer
can build and run independently.
