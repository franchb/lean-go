# A proved CRC-16/XMODEM byte update

CRC-16/XMODEM starts at zero and consumes bytes most significant bit first.
It uses the polynomial `x^16 + x^12 + x^5 + 1` (`0x1021` with the leading
term omitted), with no reflection or final XOR. The ASCII bytes of
`123456789` give `0x31C3`. [CRC catalogue and check value](https://reveng.sourceforge.io/crc-catalogue/16.htm#crc.cat.crc-16-xmodem)

From this directory, run the standalone example with Go 1.27.4.

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go test -count=1 ./...
CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off go run .
```

```text
CRC-16/XMODEM("123456789") = 0x31C3
```

To process a stream, call `checksum.Update(0, first)`, check the error,
then pass the returned `uint16` to `checksum.Update(state, next)` for each
subsequent chunk. An empty chunk preserves the incoming state. The Go wrapper
passes up to 256 bytes per call to the generated fold with default limits. On an
extraction error it returns the state at the beginning of the chunk.
[Go streaming wrapper](checksum/checksum.go)

## What Lean proves

The runtime function XORs the byte into the high half of the register and
then shifts the register eight times, reducing by the polynomial on
overflow. Its interface uses two scalars, `UInt16` and `UInt8`.
[Lean implementation](source/Crc16.lean)

The reference model takes one message bit at a time. It computes feedback
from that bit and the top register bit, then shifts and conditionally XORs
the polynomial. Two kernel-checked statements connect these descriptions.

- `update_byte_spec` proves equality to eight reference steps for every
  incoming state and byte.
- `chunks_spec` proves that folding the byte update over any list of chunks
  equals running the reference over the flattened message, from any state.

The generated fold borrows the input slice and iterates in wire order.
The Go tests exercise the generated code against a separate bit-at-a-time
implementation, the published check value, zero residue after appending the
check bytes, empty input and chunk splits. [Specification](proof/Crc16Contract.lean) · [Proofs](proof/Crc16Proofs.lean) · [Go tests](main_test.go)

## Reproduce the evidence

Use the CLI built from this checkout. The byte and chunk roots use generator v16 and v21 multi-root IR. From the repository root, with Go 1.27.4 and
Lean 4.36.0 available, run

```sh
uv sync --locked --reinstall-package lean-go-tools
uv run --locked python scripts/check_crc16_demo.py --cli "$PWD/.venv/bin/lean-go"
```

The verifier regenerates the package, IR and proof evidence, checks their
bytes against the tracked files, rechecks both theorems and then runs the
consumer with cgo disabled and only Go on its executable path. The compiler
compatibility workflow repeats generation and both theorem checks on Lean
4.36.0-rc2. [Bound proof evidence](proof-evidence.json)
