# Write two frames and read them back in Go

Wire16 encodes a header as four bytes: version, flags, and a big-endian payload
length. In this example the first payload is the two bytes in `"Go"`, with
flags 3. An empty frame with flags 0 follows it.

```text
01 03 00 02 47 6f   01 00 00 00
```

From a checkout containing the writer, run the [executable example](../../examples/proofs/wire16/frame/roundtrip_test.go):

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

## What the checks establish

Lean checks eleven [Wire16 propositions](../../examples/proofs/wire16/proof/Wire16Contract.lean).
For example, `encode_success_iff` specifies the version, allowed flags and each
length byte. `parse_encode_roundtrip` establishes that encoding a valid Lean
header produces bytes that parse back to that header.

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

Alpha.10 ships the header codec and frame reader. The append example uses the
post-alpha.10 checkout. [Alpha.10 release](https://github.com/franchb/lean-go/releases/tag/v0.1.0-alpha.10)
