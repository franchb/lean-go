# A proved big-endian UInt16 codec and four-byte header

`Wire16.encodeBE` splits an unsigned 16-bit word into two bytes, high byte
first. `Wire16.decodeBE` combines those bytes. The header parser and encoder
reuse this codec for their length field.

| Byte | Meaning |
| --- | --- |
| 0 | Version, always 1 |
| 1 | Flags in bits 0 and 1, with all six reserved bits clear |
| 2 | High byte of the payload length |
| 3 | Low byte of the payload length |

Copy this directory and run the executable with Go 1.27.4.

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off go run .
```

```text
0xabcd -> [ab cd] -> 0xabcd
[01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]
[02 80 00 00] -> unsupported version
[01 04 00 00] -> reserved flags
```

The Go façade accepts a `uint16` in `RunEncodeBE`. Read the returned pair
with `Hi()` and `Lo()`. Construct a pair with `NewBytePair(hi, lo, limits)`
and pass it to `RunDecodeBE` to recover a `uint16`.

For a header, pass a `[4]byte` to `NewByteQuadFromBytes` and call `RunParse`.
Check the Go error, then inspect `AsParsed`, `IsUnsupportedVersion` or
`IsReservedFlags`. An unsupported version takes precedence even when reserved
bits are set. A parsed header exposes `Flags()` and `Length()`.
Use `NewHeader(flags, length, limits)` with a Go `uint16` and call `RunEncode`.
Reserved flags are the encoder's sole domain error. The encoded record's
`Bytes()` method returns a `[4]byte`. The [wider-input header
example](../header/README.md) accepts a `uint32` and checks for overflow.

## Read frames from a Go byte slice

`frame.ReadFrame(input, limits)` returns the parsed header, its payload, and
the remaining input. Pass the remainder to the next call for concatenated
frames. A short header or payload returns `io.ErrUnexpectedEOF` and the original
slice, so the caller can append more bytes and retry. A complete header with
an unsupported version or reserved flags returns the corresponding protocol
error immediately. Every error consumes zero bytes.

The payload and remainder borrow the caller's buffer. The payload's capacity
equals its length, so appending cannot overwrite the next frame. Copy the
payload if it must outlive reuse of the input buffer. The [executable
example](frame/frame_test.go) reads two consecutive frames, including an empty
one. This slice handling is implemented and tested in Go.

## Proofs and Go checks

Eleven kernel-checked propositions cover the two-byte codec and its header.
The expected encoder bytes are the quotient and remainder upon division by
256. A separate proposition specifies the decoded value as `hi * 256 + lo`.
Both round trips hold for every word and every pair. The header propositions
characterize every success and rejection, and both round trips hold for all
valid inputs. See the [independent propositions](proof/Wire16Contract.lean)
and [proofs](proof/Wire16Proofs.lean).

The Go tests compare both raw and typed calls with `encoding/binary` across
all 65,536 words and byte pairs and all 262,144 valid headers. They also test
the version and flag rejection branches, malformed inputs and resource limits.
On each supported Lean compiler, the compatibility workflow builds
[native Lean code](native/Reference.lean) and compares all 65,536 encoder and
decoder outputs directly with Go. These checks exercise the generated code. The Lean theorems concern the source.

## Regenerate

With the checkout CLI installed, run these commands here.

```sh
lean-go generate --source-dir source --module Wire16 \
  --root Wire16.decodeBE --root Wire16.encode --root Wire16.encodeBE --root Wire16.parse \
  --out candidate/wire16 --ir-output candidate/wire16.ir.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/wire16/manifest.json --output candidate/proof-evidence.json
lean-go check-proofs --contract proof-contract.json \
  --manifest candidate/wire16/manifest.json --evidence candidate/proof-evidence.json
```

From the repository root, compare regenerated artifacts to the tracked package
and run the Go-only deployment.

```sh
uv run --locked python scripts/check_wire16_demo.py --cli "$PWD/.venv/bin/lean-go"
```
