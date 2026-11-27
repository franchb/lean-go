# Checksummed Wire16 frames

A frame consists of a four-byte Wire16 header, its declared payload, and a two-byte big-endian CRC-16/XMODEM. The checksum starts at zero and covers every header and payload byte. The 16-bit length counts only payload bytes. For flags 2 and the ASCII payload `123456789`, the header is `01 02 00 09` and the trailer is `57 44`.

`checkedframe.Encode` builds the body with Wire16 and appends the checksum. `checkedframe.ReadFrameFrom` reads one frame, verifies its trailer and returns an owned payload. It leaves the next frame on the reader. A complete mismatched trailer returns `ErrChecksum`. EOF at a frame boundary returns `io.EOF`, and EOF inside the frame returns `io.ErrUnexpectedEOF`.

## Run with Go alone

Keep the three sibling directories `checkedframe`, `crc16`, and `wire16` together. This example's local Go module replacements use the existing generated packages and adapters from those directories. From this directory run:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go run .
```

## Recheck the two Lean claims

For every valid flag value, every payload up to 65,535 bytes and every suffix, encoding followed by decoding recovers the flags and payload and preserves the suffix. For a parsed header and a complete payload of its declared length, the decoder accepts a trailer exactly when it equals the independent bit-by-bit CRC reference. [Model](proof/CheckedFrame.lean) · [Propositions](proof/CheckedFrameContract.lean) · [Proofs](proof/CheckedFrameProofs.lean)

From the repository root, with Go 1.27.4 and the default Lean 4.36.0 pin:

```sh
go run ./cmd/lean-go-dev installed-cli --demo checkedframe
```

It checks both claims with the checkout's lean-go against the tracked Wire16 and CRC-16 manifests, requires the fresh evidence to equal [the tracked evidence](proof-evidence.json) byte for byte, and tests and runs this module with Go alone. The same check with the checkout's CLI directly:

```sh
go run ./cmd/lean-go check-proofs \
  --contract examples/proofs/checkedframe-contract.json \
  --component-manifest wire16 examples/proofs/wire16/wire16/manifest.json \
  --component-manifest crc16 examples/proofs/crc16/crc16/manifest.json \
  --evidence examples/proofs/checkedframe/proof-evidence.json
```

The composition contract binds both theorems to the Wire16 and CRC-16 packages in one evidence file. The compatibility workflow repeats the check with freshly generated packages on both supported Lean versions. Go tests exercise the stream adapter against a separate implementation. [Composition contract](../checkedframe-contract.json) · [Bound evidence](proof-evidence.json)
