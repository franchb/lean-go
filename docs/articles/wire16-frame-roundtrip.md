# Write two frames and read them back in Go

Wire16 encodes a header as four bytes: version, flags, and a big-endian payload
length. In this example the first payload is the two bytes in `"Go"`, with
flags 3. An empty frame with flags 0 follows it.

```text
01 03 00 02 47 6f   01 00 00 00
```

From the alpha.11 checkout, run the [executable example](../../examples/proofs/wire16/frame/roundtrip_test.go):

```sh
cd examples/proofs/wire16
CGO_ENABLED=0 go test -run '^ExampleAppendFrame$' -v ./frame
```

It calls `frame.AppendFrame(nil, 3, []byte("Go"), limits)`, then passes the
returned slice to `frame.AppendFrame(buf, 0, nil, limits)`. A call to
`frame.ReadFrame(buf, limits)` returns the first payload and the four bytes
`01 00 00 00`. Pass that remainder to the next read to get the empty frame.

Once a writer has a payload, it derives the length from `len(payload)`.
65,535 bytes fit. At 65,536 it returns `frame.ErrPayloadTooLarge` before
converting the length to `uint16`. Check every returned error. On an append
error the caller retains its original destination, including any spare capacity.
On a short read, keep the slice returned with `io.ErrUnexpectedEOF`, append
the next chunk, and retry. A successful read borrows its payload from the input
buffer, so copy the payload before reusing that buffer.

For a file or stream, call `frame.ReadFrameFrom(r, limits)` once per frame.
Run the [stream example](../../examples/proofs/wire16/frame/stream_test.go) with
`CGO_ENABLED=0 go test -run '^ExampleReadFrameFrom$' -v ./frame`.
It returns `io.EOF` when the stream ends between frames and
`io.ErrUnexpectedEOF` for an unfinished header or payload. Each successful
call returns an owned payload and reads exactly to the end of that frame.

To send the same frame to an `io.Writer`, call
`frame.WriteFrameTo(w, 3, []byte("Go"), limits)`. Success reports six wire
bytes. If the transport accepts only the four header bytes before failing,
the call returns four and the transport error. The
[executable example](../../examples/proofs/wire16/frame/stream_write_test.go)
pairs the stream writer with `ReadFrameFrom`.

## What the checks establish

Lean checks thirteen [Wire16 propositions](../../examples/proofs/wire16/proof/Wire16Contract.lean).
For example, `encode_success_iff` specifies the version, allowed flags and each
length byte. `parse_encode_roundtrip` establishes that encoding a valid Lean
header produces bytes that parse back to that header. The completeness claims
specify exactly how many more bytes a short frame needs, or how many bytes
a complete frame consumes. Go handles reading, allocation and slicing.

The Go writer calls that generated encoder. The Go tests check the buffer
operations: preserving the destination on errors, copying overlapping
payloads, keeping the second frame intact, and retrying at every split of a
short two-frame stream. The fuzz target varies two payloads, their flags and
two chunk boundaries. It checks the wire bytes independently with
`encoding/binary` and compares the decoded flags, payloads and remainder.

```sh
CGO_ENABLED=0 go test -count=1 ./frame
CGO_ENABLED=0 go test -run '^$' -fuzz '^FuzzFrameRoundTrip$' -fuzztime=30s ./frame
```

Alpha.10 introduced the header codec and slice reader. Alpha.11 adds frame
append, the payload completeness proofs and the stream adapters used here.
[Alpha.11 release notes](../releases/v0.1.0-alpha.11.md)
