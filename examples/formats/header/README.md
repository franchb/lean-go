# The demo header, generated from a spec

This is the four-byte version-1 header of
[examples/proofs/header](../../proofs/header), re-expressed as a
[spec](spec.json) for [`lean-go format`](../../../docs/wire-formats.md) instead
of hand-written Lean, proofs and Go.

| Byte | Meaning |
| --- | --- |
| 0 | Version, always 1 |
| 1 | Flags in bits 0 and 1; the six bits above them are reserved and must be zero |
| 2..3 | Payload length, high byte first |

```go
h, n, payload, err := header.Parse([]byte{0x01, 0x03, 0x00, 0x02, 'h', 'i'})
// h = {Flags: 3, Length: 2}, n = 6, payload = "hi"
_, _, _, err = header.Parse([]byte{0x02, 0x80, 0x00, 0x00}) // ReasonVersionMismatch
_, _, _, err = header.Parse([]byte{0x01, 0x04, 0x00, 0x00}) // ReasonReservedNonzero
out, err := header.AppendSerialize(nil, header.Header{Flags: 3, Length: 4660}) // 01 03 12 34
```

As in the original, an unsupported version takes precedence over reserved
bits. Unlike the original, which only decodes the four bytes, `Parse` also
returns the payload the length declares, and the Go header uses a `uint16`
length, so the original's `lengthOverflow` result has no counterpart.

## Guarantees

Eleven kernel-checked claims ([contract](proof/DemoHeaderContract.lean),
[proofs](proof/DemoHeaderProofs.lean), [evidence](proof-evidence.json)) state
exactly when parsing succeeds and which rejection it reports, the validity
predicate (flags below 4), both round trips, and the exact payload deficit or
consumed count.

The repository test `TestHeaderMatchesProofsHeader` in
[internal/wireformat](../../../internal/wireformat/examples_test.go) runs the
extracted package of the original demo next to this one: they agree on every
version and flags byte with three lengths, and on the encoding of every flags
value below 8 with every 16-bit length. [reference_test.go](header/reference_test.go)
restates the original rules and checks all 65,536 version and flags bytes.

## Run and regenerate

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off go test -count=1 ./...
```

From the repository root:

```sh
go run ./cmd/lean-go format generate -spec examples/formats/header/spec.json -out examples/formats/header
```
