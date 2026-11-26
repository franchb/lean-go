# Proved binary header codecs from a JSON spec

`lean-go-format` turns a short JSON description of a fixed-size binary
header into:

1. ordinary first-order Lean (`source/<Module>.lean`): the header record, a
   `parse` function with an explicit rejection reason for every constraint,
   `serialize`, a `validate` predicate and, for formats with a payload
   length, a `classify` decision for the bytes after the header;
2. a proof contract (`proof/<Module>Contract.lean`, `proof-contract.json`) of
   expected propositions stated from the spec, and proofs of them
   (`proof/<Module>Proofs.lean`) produced by one fixed scheme;
3. a Go package over `[]byte` on top of the extracted code that never panics,
   never modifies or keeps the caller's slices, and returns typed errors;
4. generated Go property and fuzz tests, including a second, independent
   reading of the spec with `encoding/binary`.

In Go the point is not memory safety. It is that the parser never panics,
accepts exactly the specified language, and that parse and serialize are
mutually inverse, so every accepted header has exactly one encoding.

Three generated examples are tracked in [examples/formats](../examples/formats):
the [demo header](../examples/formats/header), the
[UDP header](../examples/formats/udp) (RFC 768) and the
[DNS message header](../examples/formats/dns) (RFC 1035 section 4.1.1).

## Write a spec

```json
{
  "name": "dns",
  "module": "DnsHeader",
  "description": "DNS message header, RFC 1035 section 4.1.1.",
  "fields": [
    {"name": "id", "type": "uint16"},
    {"bitfield": "uint16", "bits": [
      {"name": "qr", "width": 1},
      {"name": "opcode", "width": 4},
      {"name": "aa", "width": 1},
      {"name": "tc", "width": 1},
      {"name": "rd", "width": 1},
      {"name": "ra", "width": 1},
      {"name": "z", "width": 3, "reserved": true},
      {"name": "rcode", "width": 4}
    ]},
    {"name": "qdcount", "type": "uint16"},
    {"name": "ancount", "type": "uint16"},
    {"name": "nscount", "type": "uint16"},
    {"name": "arcount", "type": "uint16"}
  ]
}
```

Fields are listed in wire order and packed without gaps.

| Key | Where | Meaning |
| --- | --- | --- |
| `name` | top level | Go package and directory name: lower case letters and digits. |
| `module` | top level | Optional Lean module name, default `<Name>Header`. |
| `description` | top level | Optional; used in the package comment. |
| `fields` | top level | The fields in wire order. |
| `type` | integer field | `uint8`, `uint16`, `uint32` or `uint64`. |
| `endian` | integer field | `big` (default, network order) or `little`; not for `uint8`. |
| `bitfield` | bit-field word | `uint8`, `uint16` or `uint32`, always big-endian. |
| `bits` | bit-field word | Its members, most significant first, each with a `name` and a `width`. Optionally give every member an `lsb` (its least significant bit, 0 = the word's lowest bit) and list them in any order. |
| `const` | field or member | The value must equal this constant (a magic number or version). It is not stored in the Go header; serialization writes it. |
| `reserved` | field or member | `true`: the bits must be zero. Not stored. |
| `enum` | field or member | The value must be one of these (at most 32). |
| `min`, `max` | field or member | Inclusive bounds. |
| `payload_length` | integer field | `{}`: the field counts the payload bytes that follow the header. `{"includes_header": true}`: it counts the header too, so the payload is the value minus the header size and values below the header size are rejected. Use `max` to limit it. At most 32 bits. |

Numbers are JSON integers or strings such as `"0xCAFEBABE"`. Names start
with a lower-case letter and contain letters and digits; the Go field is the
name with its first letter in upper case (`srcPort` becomes `SrcPort`).

The spec is checked strictly. Unknown or duplicate keys, trailing data,
duplicate names (also names that differ only in case), Lean or Go keywords,
overlapping or uncovered bits of a bit-field word, constants and bounds that
do not fit their width, empty or repeated enums, `min > max`, enums with no
value inside `min`/`max`, bounds that are always satisfied, and a payload
length that can never hold the header are errors with the path of the
offending entry, for example
`spec: fields[1]: bits 0..0 of the 8-bit word are not covered; declare them, for example as reserved`.

## Generate

From a checkout with Go 1.27.4, the pinned Lean 4.36.0 and uv:

```sh
go run ./cmd/lean-go-format check -spec spec.json
go run ./cmd/lean-go-format generate -spec spec.json -out DIR
```

`generate` renders the sources into a fresh temporary directory, runs the
lean-go extractor and the proof checker there, and copies every file into
`DIR`. Files of `DIR` it does not produce, such as a README or hand-written
tests, are left alone. `-sources-only` writes the generated sources without
running Lean. `-repo` points at the lean-go checkout (default: the current
directory). One format takes about 20 to 30 seconds.

| Output | Produced by |
| --- | --- |
| `go.mod`, `proof-contract.json`, `source/`, `proof/`, `<name>/<name>.go`, `<name>/lean.go`, `<name>/<name>_gen_test.go` | the generator |
| `internal/extracted/` (manifest and four Go files), `<name>.ir.json` | the extractor |
| `proof-evidence.json` | the proof checker |

The result is a standalone Go module that builds with Go alone.

## Use the Go package

```go
import "example.com/lean-go-format-dns/dns"

h, n, err := dns.Parse(packet)          // n == dns.HeaderSize on success
if err != nil {
    var e *dns.Error                      // e.Reason, e.Reason.Field()
    if errors.As(err, &e) && e.Reason == dns.ReasonZNonzero { ... }
}
out, err := dns.AppendSerialize(buf, h) // validates first; buf unchanged on error
err = dns.Validate(h)
```

For a format with a payload length, `Parse` also returns the payload:
`h, n, payload, err := udp.Parse(datagram)`. The payload is a sub-slice of the
input with capacity equal to its length. A short header gives
`ReasonTruncated` and a short payload `ReasonPayloadTruncated`; both match
`io.ErrUnexpectedEOF` and set `Missing` to the number of bytes still needed.
`AppendFrame(dst, h, payload)` appends a header and its payload after
checking that the length field describes the payload, and `PayloadLength(h)`
returns the declared count.

Each call validates and converts its arguments; `ErrInternal` wraps a failure
of the extracted code (such as a resource limit) and indicates a bug.

## What is proved

For a format with checks `C₁ … Cₙ` on the wire bytes (in wire order) and
validity conditions `V₁ … Vₘ` on header values, the contract states, with
every multi-byte value written out in base 256 and every bit-field as
`value / 2^shift % 2^width` over natural numbers:

| Claim | Statement |
| --- | --- |
| `parse_ok_iff` | `parse w = .ok h ↔ (C₁ ∧ … ∧ Cₙ) ∧ (each field of h equals its formula over the bytes of w)` |
| `parse_<reason>_iff` | `parse w = .<reason>ᵢ ↔ C₁ ∧ … ∧ Cᵢ₋₁ ∧ ¬Cᵢ`: the first failing check is reported |
| `accepts_iff` | `(∃ h, parse w = .ok h) ↔ C₁ ∧ … ∧ Cₙ` |
| `validate_valid_iff` | `validate h = .valid ↔ V₁ ∧ … ∧ Vₘ` |
| `validate_<reason>_iff` | `validate h = .<reason>ⱼ ↔ V₁ ∧ … ∧ Vⱼ₋₁ ∧ ¬Vⱼ` |
| `parse_serialize` | `V₁ ∧ … ∧ Vₘ → parse (serialize h) = .ok h` |
| `serialize_parse` | `parse w = .ok h → serialize h = w` |
| `parse_validate` | `parse w = .ok h → validate h = .valid` |
| `payload_need_more_iff` | `classify h available = .needMore missing ↔ available < L ∧ missing = L - available` |
| `payload_complete_iff` | `classify h available = .complete consumed ↔ L ≤ available ∧ consumed = L` |

`L` is the length field, minus the header size when it includes the header.
With no checks, `accepts` states that every input is accepted; with no
validity conditions, `validate_valid` states that every header is valid.

The checks are, per field in wire order: the constant, the reserved bits,
the enum, the minimum and the maximum. The validity conditions are, per
field: that a bit-field fits its width (omitted when an enum or a maximum
already implies it), then the enum, minimum and maximum. The DNS contract,
for example, begins:

```lean
def parse_ok_iff : Prop :=
  ∀ (w : Wire) (h : Header),
    DnsHeader.parse w = .ok h ↔
      (w.b2.toNat * 256 + w.b3.toNat) / 16 % 8 = 0 ∧
      (h.id.toNat = w.b0.toNat * 256 + w.b1.toNat ∧
       h.qr.toNat = (w.b2.toNat * 256 + w.b3.toNat) / 32768 % 2 ∧
       ...
```

The checker accepts only the axioms `propext`, `Classical.choice` and
`Quot.sound` and rejects `sorry` and `native_decide`; the proofs use neither
`native_decide` nor `bv_decide`.

### How the proofs are built

Nothing is tuned per format. The proofs file contains:

- a fixed library: `extract`, `extract0` (a digit of a mixed-radix sum,
  proved from core `Nat` lemmas), and toNat lemmas for the runtime helpers
  `be16w`, `be32w`, `be64w`, `u16` and `to16`;
- for every word, lemmas over plain natural numbers: its bytes reassemble
  (a chain of `Nat.mod_mul` facts, then `omega` on generalized atoms), each
  member is extracted from a packed value (rewrite into the shape of
  `extract`, with `omega` proving only linear side goals), members reassemble,
  and the consequences "serialize then read gives the members back" and
  "equal members force equal bytes";
- for every member, toNat lemmas linking the runtime reader, the check, the
  header value and each serialized byte to those formulas;
- the claims, by one linear case chain over the runtime checks.
  `serialize_parse` follows from `parse_serialize`, `parse_validate` and the
  injectivity of `parse`.

## Supported features and limits

Supported with automatic proofs: unsigned 8-, 16-, 32- and 64-bit integers
in either byte order; bit-fields in big-endian 8-, 16- and 32-bit words
(members typed as the smallest of `uint8`/`uint16`/`uint32` that holds
them); constants and reserved bits or whole reserved integers; enums; minimum
and maximum; one payload-length field of at most 32 bits, optionally
including the header.

Limits:

- A header is at most 64 bytes with at most 64 stored fields: Lean record
  constructors are limited to 64 fields.
- A proof contract holds at most 64 claims, so a format may have at most 58
  checks and validity conditions in total (56 with a payload length).
- Bit-field words are big-endian and at most 32 bits. Little-endian
  bit-field words and bit-fields inside 64-bit words are not supported.
- Fields are byte-aligned words; there are no variable-length fields,
  options, checksums or cross-field constraints other than the payload
  length.
- Proof time grows with the number of members per word: the DNS header
  takes a few seconds; a 32-bit word of 32 one-bit fields about a minute.

### Extractor constraints

The runtime Lean stays inside the extractor's current integer matrix:
`UInt16` values are built from bytes with `|||` and `<<<` only and compared
after `UInt16.toUInt32`; arithmetic and comparisons happen in `UInt32` or
`UInt64`; there is no division. A 9- to 16-bit member read from a word is
converted to `UInt16` through two bytes (`to16`). Once `UInt16` comparison,
arithmetic and `UInt32.toUInt16` are available, `u16`, `to16` and the
widening in `validate` can be dropped. Bit-field serializers take only their
own fields, and readers do not take the whole `Wire`, which keeps the
extracted type closure well below the producer's limit for 64-byte headers.

## Tests

The generated `<name>_gen_test.go` checks, without Lean:

- round trips of thousands of random valid headers, biased towards limits;
- `Parse` against `specParse`, a direct `encoding/binary` reading of the spec,
  on valid encodings, single-bit mutations and random bytes, including the
  reported reason and `serialize (parse b) = b`;
- `Validate` against `specValidate` on arbitrary field values, and that
  `AppendSerialize` writes nothing on error;
- truncated input, payload deficits, payload aliasing and capacity, and
  overlapping `AppendFrame` arguments;
- fuzz targets `FuzzParse` (no panic, input untouched, agreement with the
  spec reading) and `FuzzRoundTrip`;
- every independent decoder a hand-written test file registers in
  `referenceDecoders`. The examples register decoders written from RFC 768,
  RFC 1035 and the original header demo.

Repository tests in `internal/wireformat` check spec validation, that the
tracked examples equal the generator's output, that each example passes its
tests with Go alone, and that the generated header package agrees with
[examples/proofs/header](../examples/proofs/header) on every version and
flags byte and every encodable header. With `LEAN_GO_FORMATS=1` they also
regenerate the examples through extraction and the proof checker and compare
every file byte for byte:

```sh
LEAN_GO_FORMATS=1 go test -count=1 -run TestEndToEnd ./internal/wireformat
```

`LEAN_GO_FORMATS_EXTRA=internal/wireformat/testdata/mix.json` adds further
specs to that run (paths relative to the package directory).
[mix.json](../internal/wireformat/testdata/mix.json) uses every feature.

## Assurance boundary

The theorems are about the Lean source. Compilation, extraction, the Go
emitter, the adapter and the Go toolchain are tested and trusted, as for
every lean-go package; see [proof contracts](proof-contracts.md). The
contract is generated from the spec by the same tool that generates the
proofs, so reviewers should read the contract and check that it says what
the format requires. The adapter and the backend glue in `lean.go` only copy
bytes and map results; the decisions come from the extracted functions.

## Code layout

- `internal/wireformat/spec.go`, `model.go`, `layout.go`: the JSON model,
  strict decoding and validation.
- `leanrt.go`: the runtime Lean. `leanproof.go`, `proofs.go`,
  `proofs_main.go`: the contract and the proofs.
- `goadapter.go`: the adapter. `gobackend.go`: the glue for the extracted
  package's typed API, the one file to replace for another emission mode.
  `gotests.go`: the generated tests.
- `pipeline.go`: `runPipeline`, the only function that calls the extractor
  (`scripts/generate.py`) and the proof checker (`scripts/check_proofs.py`).
