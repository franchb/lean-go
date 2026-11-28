# Streaming Adler-32 from Lean

The Lean state holds two `UInt32` sums. Each byte updates the low sum and then adds that result to the high sum, reducing both modulo 65521. The initial sums are 1 and 0. The exported checksum packs the high sum into bits 16–31 and the low sum into bits 0–15. [RFC 1950, section 2.2](https://www.rfc-editor.org/rfc/rfc1950#section-2.2)

Run the checked-in example with Go 1.27.4 from this directory.

```sh
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go run .
# Adler-32("Wikipedia") = 0x11E60398
```

`RunInitial` creates the immutable seed. `RunUpdate(state, data, limits)` borrows a Go byte slice for the call and returns the next state. Carry that result between chunks. `RunChecksum` returns the packed `uint32`. The generated update loops over the slice with both state fields in Go locals. It charges the equivalent Lean list and call tree to the resource limits. The example processes up to 256 bytes per call, so callers can supply input larger than a single call's budget. [Lean implementation](source/Adler32.lean) · [Go example](main.go)

The source proof checks one step from any state with both sums below 65521 against a natural-number reference. From the standard seed, any list of chunks gives the same pair of sums as processing its concatenation with that reference. Two further claims cover the exported checksum: for a state with both sums below 65521 it is the high sum times 65536 plus the low sum, the value RFC 1950 stores, and so the checksum of any list of chunks from the standard seed is that value for the reference sums of the concatenation. Go tests compare the packed checksum and streaming prefixes with `hash/adler32` and compare the raw and slice APIs at each of the seven resource limits. [Contract](proof/Adler32Contract.lean) · [Proofs](proof/Adler32Proofs.lean) · [Go tests](main_test.go)

To regenerate the package and verify the four proof claims, use the development checkout with Go 1.27.4 and the default Lean 4.36.0 pin. Run this command from the repository root:

```sh
go run ./cmd/lean-go-dev installed-cli --demo adler32
```

It builds the checkout's lean-go (with `go build`, and with `go install` from a module archive), regenerates a copy of this example with each binary, compares the package, IR and evidence bytes with the tracked ones, freshly checks all four claims, and tests and runs the copied Go module with Go alone. Add `--cli "$(command -v lean-go)"` to check an installed lean-go instead.
