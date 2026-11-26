# DNS message header (RFC 1035, section 4.1.1)

The 12-byte header of every DNS message, from
[RFC 1035 section 4.1.1](https://www.rfc-editor.org/rfc/rfc1035#section-4.1.1),
generated from [spec.json](spec.json) by
[lean-go-format](../../../docs/wire-formats.md).

```text
                                1  1  1  1  1  1
  0  1  2  3  4  5  6  7  8  9  0  1  2  3  4  5
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                      ID                       |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|QR|   Opcode  |AA|TC|RD|RA|   Z    |   RCODE   |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
|                    QDCOUNT                    |
|                    ANCOUNT                    |
|                    NSCOUNT                    |
|                    ARCOUNT                    |
+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
```

The second word is a big-endian bit-field word; the RFC numbers its bits
from the most significant, which is the order the spec lists them in. Z is
"Reserved for future use. Must be zero in all queries and responses", so the
spec marks it `reserved` and `Parse` rejects a nonzero Z with
`ReasonZNonzero`. Later RFCs give two of these bits a meaning (AD and CD in
RFC 4035); this example follows RFC 1035 and rejects them. Opcode and RCODE
values the RFC reserves for future use are accepted, since later RFCs assign
them.

```go
h, n, err := dns.Parse(message)  // n == 12; the question section is not read
// h.Id, h.Qr, h.Opcode, h.Aa, h.Tc, h.Rd, h.Ra, h.Rcode, h.Qdcount, ...
out, err := dns.AppendSerialize(nil, dns.Header{Id: 0xabcd, Rd: 1, Qdcount: 1})
// ab cd 01 00 00 01 00 00 00 00 00 00
err = dns.Validate(dns.Header{Opcode: 16}) // ReasonOpcodeTooWide
```

## Guarantees

Fourteen kernel-checked claims ([contract](proof/DnsHeaderContract.lean),
[proofs](proof/DnsHeaderProofs.lean), [evidence](proof-evidence.json)):
parsing accepts exactly the headers whose Z bits are zero, every field is the
stated bit range of the big-endian words, the validity predicate bounds each
bit-field by its width (seven validation reasons), and both round trips hold.

[rfc1035_test.go](dns/rfc1035_test.go) is a decoder written from the RFC with
`encoding/binary`. The generated tests compare it with `Parse` on 20,000
valid, mutated and random headers; it also checks a query, its response and
an NXDOMAIN answer byte for byte.

## Run and regenerate

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOPROXY=off go test -count=1 ./...
go test -fuzz=FuzzParse ./dns
```

From the repository root:

```sh
go run ./cmd/lean-go-format generate -spec examples/formats/dns/spec.json -out examples/formats/dns
```
