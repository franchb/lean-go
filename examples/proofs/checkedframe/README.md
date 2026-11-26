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

From the repository root, use the checkout CLI and Python 3.15 via uv:

```sh
uv sync --locked --reinstall-package lean-go-tools
uv run --locked python scripts/check_checked_frame.py --cli "$PWD/.venv/bin/lean-go"
```

The checker takes Wire16 and CRC-16 sources from their original directories and rechecks both claims against each generated manifest. The two evidence files bind the same source theorems to both components. The compatibility workflow repeats the check with freshly generated packages on both supported Lean versions. Go tests exercise the I/O adapter and compare its bytes to a separate implementation. [Wire16 evidence](evidence/wire16.json) · [CRC-16 evidence](evidence/crc16.json)
