# Extracted Lean models as differential oracles

This example tests hand-optimized Go against plain Go models extracted from
proved Lean sources. It covers two checksums: Adler-32 with deferred modular
reduction, and CRC-16/XMODEM driven by lookup tables. The models are the
native-mode packages generated from the
[Adler-32](../proofs/adler32/source/Adler32.lean) and
[CRC-16](../proofs/crc16/source/Crc16.lean) sources of the proved examples.
The tests compare the fast code with the models on arbitrary data split at
arbitrary chunk boundaries, through ordinary `go test` and `go test -fuzz`.
The consumer needs no cgo and no Lean toolchain. The module uses only the
standard library.

The [guide](../../docs/oracle-testing.md) explains the method and when to use it.

## Layout

| Path | Contents |
| --- | --- |
| `model/adler32/`, `model/crc16/` | Generated native-mode model packages (package `extracted`) and their manifests. Do not edit them. |
| [`../proofs/adler32/adler32.ir.json`](../proofs/adler32/adler32.ir.json), [`../proofs/crc16/crc16.ir.json`](../proofs/crc16/crc16.ir.json) | The producer IR the models were emitted from: the proved examples' own IR files, shared with their metered packages |
| `fast/adler32.go` | `UpdateAdler32`: sums in blocks of up to `AdlerBlock` = 5,552 bytes, eight bytes per iteration, reduces once per block |
| `fast/crc16.go` | `UpdateCRC16`: two 256-entry tables, two bytes per lookup pair |
| `fast/oracle_test.go` | The harness (`checkAdler`, `checkCRC`), fuzz-input decoding, the shared seed corpus, a shrinker |
| `fast/differential_test.go` | Seed, random-chunking, exhaustive CRC-step and known-value tests; `FuzzAdler32`, `FuzzCRC16` |
| `fast/bugs_test.go` | Three deliberately wrong variants, each with a test that the harness finds and shrinks a counterexample |
| `fast/bound_test.go` | Derives the largest safe Adler-32 block size and checks it against `AdlerBlock` |
| `fast/bench_test.go` | Fast, model and `hash/adler32` benchmarks |

Both fast APIs stream. Pass each call's result to the next call. Start
Adler-32 from `AdlerInit` and CRC-16 from `CRC16Init`. The packed Adler-32
state `high<<16 | low` is also the checksum.

## What is proved, tested and trusted

**Proved in Lean, against independent references.** The
[Adler-32 contract](../proofs/adler32/proof/Adler32Contract.lean) states
that one `step` from any reduced state equals the RFC 1950 natural-number
reference: both sums are taken modulo 65521 and start at (1, 0). It also
states that folding `update` over any list of chunks from `initial` equals
the reference over the concatenation. The
[CRC-16 contract](../proofs/crc16/proof/Crc16Contract.lean) states that
`updateByte` equals eight steps of a bit-at-a-time register for every state
and byte. It also states that folding `update` over any chunks equals the
reference over the concatenation, from any state. The
[Adler-32 proofs](../proofs/adler32/proof/Adler32Proofs.lean) and the
[CRC-16 proofs](../proofs/crc16/proof/Crc16Proofs.lean) discharge both
contracts. The proof contracts
([Adler-32](../proofs/adler32/proof-contract.json),
[CRC-16](../proofs/crc16/proof-contract.json)) bind those theorems to the
same source files used here. The proofs concern the Lean source, not the
generated Go.

**Tested, not proved.** The fast code agrees with the models on everything
the tests run.

- `TestSeedsAgreeWithModel` runs the 314 seeds. They include the empty
  message, every single byte, the check strings, runs of 0xff and 0x00 at
  lengths around one and two blocks and at 65,535 bytes, the same runs
  after a prefix that drives the low sum to 65520, and 64 one-byte chunks.
- `TestRandomChunkingAgreesWithModel` runs 400 random messages of up to
  64 KiB, plus eight of 1 MiB. Their chunk lengths are mixed: small, around
  the block size and large.
- `TestCRCStepsExhaustive` compares both CRC steps on every 16-bit register.
- `FuzzAdler32` and `FuzzCRC16` check that the chunk states agree with the
  models after each chunk, and that the input is unchanged.
- `TestKnownValues` is an independent check. It covers CRC-16/XMODEM
  `"123456789"` = `0x31C3`, Adler-32 `"Wikipedia"` = `0x11E60398`, and
  agreement with `hash/adler32`.

**Trusted.** The extractor and the Go compiler are trusted. Nothing proves
that the generated Go implements the Lean source. That link is tested in the
repository (see [native mode](../../docs/native-mode.md#evidence)) but not
proved. The test harness is trusted as well. A harness that never compares,
or compares the wrong values, passes vacuously; `bugs_test.go` is the check
against that.

## The method has teeth

Each wrong variant runs against the seed corpus that the fuzz targets start
from. The test requires a counterexample, shrinks it deterministically, logs
it and asserts its size. Run `go test -v -run Oracle ./fast/` to see the logs.

| Variant | First failing seed | Shrunk | Why that is smallest |
| --- | --- | --- | --- |
| Adler-32 block of 5,553 bytes, one too many | `[ff×256 ef] [ff×5553]` (two chunks, 5,810 bytes): implementation `0x69e59c89`, model `0x6ac69c89` | still 2 chunks and 5,810 bytes; the shrinker then lowers byte values that still overflow | The overflow needs a whole 5,553-byte block in one call starting from a low sum ≥ 65,303. Reaching that from 1 takes a byte sum ≥ 65,302, so at least 257 bytes in an earlier chunk. |
| CRC-16 table entry `0x5a` with bit 0 flipped | `[5a]`: `0xfbbe` vs `0xfbbf` | `[5a]` | From the zero register the first byte indexes the table directly. |
| CRC-16 initial register `0xffff` (the CCITT-FALSE value) | the empty message | the empty message | — |

The block-size bug survives a single 64 KiB run of 0xff from the initial
state, and the test asserts that it does. At block starts the low sum is
1 + 40074k mod 65521, which stays below 65,303 for every block in 64 KiB.
Chunk boundaries are what expose the bug.

## Benchmarks

The benchmarks hash 64 KiB of random data with Go 1.27.4,
`CGO_ENABLED=0`, `-cpu 1` and `-benchtime 300ms`. Each figure is the median
of five runs on a shared 4-core Xeon @ 2.10GHz that was busy with other
work, so the ratios mean more than the absolute times.

| Workload | Median | Throughput | Five samples (µs) |
| --- | ---: | ---: | --- |
| Adler-32 fast | 40.0 µs | 1.64 GB/s | 49.9 32.0 38.7 40.0 45.6 |
| Adler-32 model | 140.2 µs | 0.47 GB/s | 140.2 144.3 146.7 138.4 134.0 |
| `hash/adler32` | 37.6 µs | 1.74 GB/s | 30.5 33.3 37.6 40.6 39.0 |
| CRC-16 fast | 181.7 µs | 0.36 GB/s | 164.1 222.4 181.7 196.9 163.6 |
| CRC-16 model | 817.1 µs | 0.08 GB/s | 985.4 1111.9 794.5 757.8 817.1 |

| Ratio of medians | Value |
| --- | ---: |
| Adler-32 model / fast | 3.5 |
| Adler-32 fast / `hash/adler32` | 1.06 |
| CRC-16 model / fast | 4.5 |

The models run at hundreds of MB/s for Adler-32 and tens of MB/s for CRC-16,
which is fast enough for fuzzing: a 30-second run made about 1.4 million
Adler-32 and 2.0 million CRC-16 executions on this host.

## Commands

From this directory, with Go 1.27.4:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go vet ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go test -run '^$' -fuzz '^FuzzAdler32$' -fuzztime 30s ./fast/
CGO_ENABLED=0 GOTOOLCHAIN=local go test -run '^$' -fuzz '^FuzzCRC16$' -fuzztime 30s ./fast/
CGO_ENABLED=0 GOTOOLCHAIN=local go test -run '^$' -bench . -cpu 1 -count 5 -benchtime 300ms ./fast/
```

The root test `TestStandaloneOracleExample` in
[`integration/oracle_demo_test.go`](../../integration/oracle_demo_test.go)
copies this directory into an isolated workspace and runs `go vet` and
`go test` with only Go on `PATH` and `GOPROXY=off`.

## Regenerating the models

The model packages are generated output; do not edit them by hand. They are
tracked artifacts of the repository: `lean-go regenerate` rebuilds them
together with every other tracked package, and the root test suite fails
when they no longer match the generator. From the repository root, with
Go 1.27.4 and Lean 4.36.0:

```sh
go run ./cmd/lean-go regenerate
```

To generate one model by hand, into a fresh path:

```sh
go run ./cmd/lean-go generate --mode native \
  --source-dir examples/proofs/adler32/source --module Adler32 \
  --root Adler32.checksum --root Adler32.initial --root Adler32.update \
  --out /tmp/adler32-model --ir-output /tmp/adler32.ir.json
```

The roots are the ones listed in each proof contract. After regenerating,
rerun this module's tests.
