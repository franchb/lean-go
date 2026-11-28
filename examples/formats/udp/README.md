# UDP header (RFC 768)

The User Datagram Protocol header, from the "Format" section of
[RFC 768](https://www.rfc-editor.org/rfc/rfc768), generated from
[spec.json](spec.json) by [`lean-go format`](../../../docs/wire-formats.md).

```text
 0      7 8     15 16    23 24    31
+--------+--------+--------+--------+
|     Source      |   Destination   |
|      Port       |      Port       |
+--------+--------+--------+--------+
|     Length      |    Checksum     |
+--------+--------+--------+--------+
```

All fields are 16-bit and in network byte order. RFC 768: "Length is the
length in octets of this user datagram including this header and the data.
(This means the minimum value of the length is eight.)" The spec marks the
length as a `payload_length` that includes the header, which makes values
below eight a rejection (`ReasonLengthTooSmall`) and the payload the length
minus eight.

```go
h, n, payload, err := udp.Parse(datagram)
// h.SrcPort, h.DstPort, h.Length, h.Checksum; n = h.Length; payload = datagram[8:n]
frame, err := udp.AppendFrame(nil, h, payload) // checks h.Length == 8 + len(payload)
```

A datagram shorter than its length gives `ReasonPayloadTruncated` with the
number of missing bytes; bytes after the datagram are not consumed. The
checksum is carried as a value: computing and checking it needs the IP
pseudo-header and is outside this fixed-size header format.

## Guarantees

Ten kernel-checked claims ([contract](proof/UdpHeaderContract.lean),
[proofs](proof/UdpHeaderProofs.lean), [evidence](proof-evidence.json)): parsing
accepts exactly the headers with length at least eight, every field is the
big-endian value of its two bytes, both round trips hold, and the payload
decision reports the exact deficit or `length - 8` bytes to consume.

[rfc768_test.go](udp/rfc768_test.go) is a decoder written from the RFC with
`encoding/binary`. The generated tests compare it with `Parse` on 20,000
valid, mutated and random headers, and check a captured DNS query datagram.

## Run and regenerate

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off go test -count=1 ./...
go test -fuzz=FuzzParse ./udp
```

From the repository root:

```sh
go run ./cmd/lean-go format generate -spec examples/formats/udp/spec.json -out examples/formats/udp
```
