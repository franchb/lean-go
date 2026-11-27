# Unsigned integer support matrix

The module extractor admits exactly 68 runtime primitives on the four unsigned
widths: fourteen operations per width and the twelve conversions between
distinct widths. Every entry is an exact pinned Lean declaration with an audited
extern symbol and axiom set (`producer/LeanGo/Module/Primitives.lean`); the IR
validator (`internal/moduleir`), the metered emitter (`internal/modulegen`, boxed
executor and scalar-native path), the native emitter (`internal/nativegen`) and
the Lean validation semantics (`producer/LeanGo/Validate/Prim.lean`) use the
same names and meanings.

Semantics are Lean's, exactly. `w` is the width in bits and `m = 2^w`.

| Lean source | IR primitive | `UInt8` | `UInt16` | `UInt32` | `UInt64` | Result |
| --- | --- | :-: | :-: | :-: | :-: | --- |
| `a + b` | `add` | yes | yes | yes | yes | `(a + b) mod m` |
| `a - b` | `sub` | yes | yes | yes | yes | `(a - b) mod m`, wrapping below zero |
| `a * b` | `mul` | yes | yes | yes | yes | `(a * b) mod m` |
| `a / b` | `div` | yes | yes | yes | yes | `floor(a / b)`; **`a / 0 = 0`** |
| `a % b` | `mod` | yes | yes | yes | yes | `a mod b`; **`a % 0 = a`** |
| `a &&& b` | `land` | yes | yes | yes | yes | bitwise AND |
| `a \|\|\| b` | `lor` | yes | yes | yes | yes | bitwise OR |
| `a ^^^ b` | `xor` | yes | yes | yes | yes | bitwise XOR |
| `~~~a` | `complement` | yes | yes | yes | yes | `(m - 1) - a` |
| `a <<< b` | `shiftLeft` | yes | yes | yes | yes | `(a * 2^(b mod w)) mod m`: the count is reduced modulo the width |
| `a >>> b` | `shiftRight` | yes | yes | yes | yes | `floor(a / 2^(b mod w))`, logical |
| `a == b`, `decide (a = b)` | `decEq` | yes | yes | yes | yes | `Bool` |
| `a < b`, `decide (a < b)` | `decLt` | yes | yes | yes | yes | unsigned `Bool` |
| `a ≤ b`, `decide (a ≤ b)` | `decLe` | yes | yes | yes | yes | unsigned `Bool` |
| literals `(n : UIntW)` | `uintW` value | yes | yes | yes | yes | `n mod m`, folded by Lean before export |

Conversions `a.toUIntV` exist for every ordered pair of distinct widths (12):

| From \ to | `UInt8` | `UInt16` | `UInt32` | `UInt64` |
| --- | :-: | :-: | :-: | :-: |
| `UInt8` | | zero-extend | zero-extend | zero-extend |
| `UInt16` | `a mod 2^8` | | zero-extend | zero-extend |
| `UInt32` | `a mod 2^8` | `a mod 2^16` | | zero-extend |
| `UInt64` | `a mod 2^8` | `a mod 2^16` | `a mod 2^32` | |

Shift counts at or above the width do not shift everything out, as plain Go
would: `(1 : UInt8) <<< 9 = 2`. Go panics on an integer division by zero, so
every emitter tests the divisor first; division and remainder by a literal zero
are also emitted correctly, because the pinned compiler keeps `x / 0` as a
division (it folds `x % 0` to `x`).

## Compiler rewrites

The pinned compiler's constant folder changes some source forms before export;
every form it produces is an admitted primitive or a literal:

| Source | Exported IR |
| --- | --- |
| `x * 2^k`, `2^k * x` | `shiftLeft x k` |
| `x / 2^k` | `shiftRight x k` |
| `x % 2^k` | `mod x 2^k` (no mask) |
| `x / 0`, and `x / 2^w` (the literal wraps to 0) | `div x 0` |
| `x % 0`, `x / 1`, `x * 1`, `x + 0` | `x` |
| `x % 1`, `x * 0`, `0 / y` | the literal `0` |
| constant operands, such as `100 / 7 + 100 % 0` | the exact literal (`114`) |

[Translation validation](research/translation-validation.md) proves the
exported IR equal to the source across each of these rewrites: it brings
both sides to one form with proved lemmas (a shift by a literal is a
multiplication or division by the power of two, a literal factor stands on
the right) and with core's lemmas and literal evaluation for the folds.

## IR features

A program declares the capability families it uses: `uint8`, `uint16`, `uint32`
and `uint64` for each width in an executable type; `bitwise` for AND, OR, XOR,
complement and shifts; `division` for division or remainder; `width_conversion`
for conversions. Arithmetic and comparisons need only the width feature.

## Resource accounting

Each primitive is one IR step and one logical allocation in both the boxed
executor and the scalar-native path, including the guarded division and
remainder.

## Still unsupported

- Signed integers (`Int8`, `Int16`, `Int32`, `Int64`), `Nat` and `Int` at
  runtime, `USize`, `Float` and `Float32`: rejected during source admission
  (`source_audit` or `unsupported_type`). Proof modules may still use `Nat`.
- Conversions to or from those types, such as `UInt32.toNat`, `UInt64.toUSize`
  or `UInt16.toFloat`, and aliases such as `abbrev Byte := UInt8` in executable
  types.
- Other unsigned operations without a listed primitive, for example `log2` or
  `toBitVec`, unless the compiler lowers them to the listed leaves.
- Replacing, re-implementing or rewriting any of these declarations or their
  standard instances (`csimp`, `implemented_by`, `extern`, initializers): the
  audit compares them with the pristine toolchain and rejects changes.

## Evidence

- `internal/pipeline/lean_unsigned_test.go` (with `LEAN_GO_MODULES=1`): every
  primitive of every width from ordinary notation and direct names, the
  compiler rewrites above and the rejections; `integration/unsigned_native_test.go`
  (with `LEAN_GO_MODULES=1`): native Lean against both Go APIs and an
  arithmetic model on boundary values (0, 1, maxima, powers of two, zero divisors, shift counts at
  and above the width).
- `internal/moduleir` and `internal/modulegen` unit tests over the whole matrix,
  `internal/nativegen` agreement between the native matrix and the validator.
- The differential campaign (`go run ./cmd/lean-go-dev differential`) on all
  four widths,
  generating division and remainder and zero-divisor inputs.
- The compatibility corpus cases `division`, `uint16_land`, `uint32_narrow16`
  and `folded_numeric`, and the [clock example](../examples/proofs/clock).
