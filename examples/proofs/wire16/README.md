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

## Read frames from a stream

`frame.ReadFrameFrom(r, limits)` reads one frame from an `io.Reader`.
Each successful result owns its payload. A call returns `io.EOF` at a frame
boundary or `io.ErrUnexpectedEOF` if the stream ends within a header or a
declared payload. A complete invalid header is rejected before reading its
payload. If the reader supplies the last bytes of a complete valid frame
together with an error, the call returns that frame. If more payload bytes
are required, an error delivered with the last header byte applies to this
frame. Bytes already read from the stream remain consumed when the call fails.

Both readers use `RunClassify(header, available, limits)`. For example, a
header declaring five bytes with two available yields `needMore 3`. With
seven available it yields `complete 5`, leaving the next two bytes for the caller.

## Append a frame

`frame.AppendFrame(dst, flags, payload, limits)` derives the length from the
payload and calls the generated header encoder. A payload of 65,536 bytes or
more returns `frame.ErrPayloadTooLarge`. Check the error before using the
result. On success, use the returned slice for the next append. On error the
destination and its backing array retain their original contents.

The [frame writing walkthrough](../../../docs/articles/wire16-frame-roundtrip.md)
follows two frames through the writer and reader, then runs the Go fuzz target.
The writer is available in the post-alpha.10 checkout.

## Write a frame to a stream

`frame.WriteFrameTo(w, flags, payload, limits)` validates and stages one
complete frame, then calls `w.Write` once. The encoded frame is at most
65,539 bytes, including the four-byte header. Invalid flags, an oversized
payload, or a codec limit error return zero before the writer sees any bytes.

The returned count includes header and payload bytes. A short write with a
nil error becomes `io.ErrShortWrite`. Other write errors are returned with
the count the writer reported, even if it accepted the entire frame. Check
both results before retrying. Resending a partially written frame on the same
stream would duplicate its prefix.

Run the [stream writer example](frame/stream_write_test.go) to write a frame
and read it back with `ReadFrameFrom`.

```sh
CGO_ENABLED=0 go test -run '^ExampleWriteFrameTo$' -v ./frame
```

## Proofs and Go checks

Thirteen kernel-checked propositions cover the two-byte codec, header and
payload completeness. Two characterize the exact deficit and the consumed
length for every possible available-byte count.
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
[native Lean code](native/Reference.lean) and compares encoding, decoding and UInt32 widening for all 65,536 inputs directly with Go. These checks exercise the generated code. The Lean theorems concern the source.

## Regenerate

With the checkout CLI installed, run these commands here.

```sh
lean-go generate --source-dir source --module Wire16 \
  --root Wire16.classify --root Wire16.decodeBE --root Wire16.encode --root Wire16.encodeBE --root Wire16.parse \
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
