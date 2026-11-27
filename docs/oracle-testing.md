# Extracted models as test oracles

Extracted Go is usually slower than hand-optimized Go, and the Lean subset
that can be extracted is small. For many teams, then, extracted code is
most useful today as an oracle rather than as the code they ship. You
prove properties of a small Lean model, extract it to plain Go, and fuzz
the production Go against it inside `go test`. The consumer needs no cgo, no
Lean toolchain in CI, no subprocess and no serialization.

Cedar uses the same method. Its authors write models in Lean, prove
properties of them, and use differential random testing (DRT) against the
Rust production code. In their words, "we randomly generate millions of
inputs ... and send them to both the Lean model and the corresponding Rust
production implementation". They report that proofs found 4 bugs in the
policy validator and that DRT and property-based testing found 21 more
([Disselkoen et al., *How We Built Cedar: A Verification-Guided Approach*,
arXiv:2407.01688](https://arxiv.org/abs/2407.01688)). Here the oracle is
ordinary Go in the same process as the code under test.

The worked example is [`examples/oracle`](../examples/oracle). It covers
Adler-32 with deferred reduction and table-driven CRC-16/XMODEM, both tested
against [native-mode](native-mode.md) models of the proved
[Adler-32](../examples/proofs/adler32) and [CRC-16](../examples/proofs/crc16)
sources.

## Oracle or production code?

| Use the extracted code in production when | Use it as an oracle when |
| --- | --- |
| it is fast enough. Native mode is within about 1.1x of a line-for-line Go transliteration of the Lean source ([benchmarks](native-mode.md#benchmarks)) | the production code relies on optimizations that the Lean source does not express, such as deferred reduction, lookup tables, unrolling or SIMD. Our Adler-32 model is 3.5x slower than the fast code. |
| the algorithm fits the extractable subset | the production code must keep an API, allocation behaviour or dependencies that the extractor does not emit |
| you want proof to cover the code that runs, with only the extractor and the compiler trusted | you accept a tested, rather than proved, link between production and model, in exchange for keeping the production code |
| the inputs need resource limits; then use metered mode | the model only needs to be correct and fast enough to fuzz |

The two uses combine. A team can start with an oracle and later ship the
extracted code for the cases where it is fast enough.

What the oracle guarantees is narrower than a proof. The fast code agrees
with a proved model on every input the tests ran. Coverage of the input
space, and of the harness itself, is what decides how much that is worth.

## Structuring a differential harness

Write one check function that takes the implementation under test as a value
and returns the first disagreement, or `nil`. Then the unit tests, the fuzz
targets and the planted-bug tests all share it. In the example,
`checkAdler(impl, chunks)` starts both sides from their own initial states,
compares those states, and then compares the states after every chunk
([`oracle_test.go`](../examples/oracle/fast/oracle_test.go)).

**Compare intermediate state, not just the final answer.** With streaming
code, comparing after each chunk localizes a failure to one call. It also
catches a wrong initial value on the empty message.

**Make chunk boundaries part of the input.** Streaming code has a second
input besides the bytes: where the caller splits them. Deferred reduction
is a good example. A block size one byte too large passes every single-call
run of 0xff up to 64 KiB, because at each block start the low sum is
1 + 40074k mod 65521, which never reaches the danger zone. It fails at
once when an earlier chunk leaves the low sum high. The example decodes the
fuzz input as data, a list of little-endian `uint16` chunk lengths (zero is
allowed, so empty updates are exercised), and one run (length, byte and
position) inserted into the data. The run lets the fuzzer produce 64 KiB of
0xff with a five-byte input. Mutating that input by hand would take many
steps.

**Seed the corpus with the adversarial cases you can name.** Seeds run on
every `go test`, whether or not fuzzing is enabled, so they are free
regression tests ([Go fuzzing](https://go.dev/doc/security/fuzz/); seeds
come from `F.Add` and from `testdata/fuzz/<Name>`,
[package testing](https://pkg.go.dev/testing#hdr-Fuzzing)). For
arithmetic with deferred reduction, name the worst case: the largest
reduced state, followed by the largest bytes, in a block of exactly the
limit, one less and one more. Add the empty message, every single byte, the
published check values, and many tiny chunks. One seed list in the example
feeds `FuzzAdler32`, `FuzzCRC16` and the planted-bug tests.

**Keep fuzz targets fast and deterministic.** The Go documentation asks
for this: by default an execution that takes more than one second fails,
and targets run in parallel workers in no fixed order. Keep inputs
bounded. A `uint16` run length keeps the inserted run under 64 KiB, which
the Adler-32 model checks in well under a millisecond.

**Exhaust what is small.** The CRC register has 2^16 states. Comparing the
fast one-byte step on every (register, byte) pair, and the two-byte step on
every value of `register ^ (b0<<8 | b1)`, covers every input of both steps.
Because the fast code is a fold of those steps, that leaves nothing for
fuzzing to find in the steps themselves. Adler-32 has no such shortcut,
because its fast form is not a byte fold.

**Plant bugs to test the harness.** A harness that compares the wrong
values passes everything. Keep deliberately wrong variants in test files,
run each through the harness over the fixed seed corpus, and require a
counterexample. Do not leave this to fuzzing luck. The example's three
variants and the counterexamples they produce are tabulated in
[its README](../examples/oracle/README.md#the-method-has-teeth).

**Shrink, and say why the result is minimal.** The Go fuzzer minimizes a
failing input before it writes it to `testdata/fuzz/<Name>`
(`-fuzzminimizetime`, default 60 s). For deterministic tests, a small
shrinker of your own works: drop chunks, join chunks, delete spans, lower
bytes. Where you can, check the shrunk size against an argument. The
block-size counterexample needs 257 bytes to drive the low sum from 1 to
at least 65,303, then a full 5,553-byte block in a later call: 5,810 bytes
in two chunks.

## Fuzzing in CI and corpus handling

- Plain `go test` runs every seed. That is the always-on gate.
- For a fuzzing job, use a short fixed budget per target:
  `go test -run '^$' -fuzz '^FuzzAdler32$' -fuzztime 30s ./fast/`. `-fuzz`
  accepts a pattern that matches one target, so run one command per target.
  `-fuzztime` also accepts an execution count such as `-fuzztime 100000x`,
  which makes the job length independent of machine load.
- The generated corpus lives in `$GOCACHE/fuzz`. Cache that directory
  between CI runs if you want campaigns to build on one another.
- When fuzzing fails, the engine writes the minimized input to
  `testdata/fuzz/<Name>/` and prints a `go test -run=<Name>/<hash>`
  command. Commit that file with the fix. It then runs as a seed on every
  `go test`.
- Report fuzz runs with their execution counts, not only their durations. On
  a shared 4-core host, 30 s gave about 1.4 million Adler-32 executions and
  2.0 million CRC-16 executions. Throughput swung by more than 5x over the
  run because of other load.

## Regenerating the model

The model packages are generated output. Regenerate them with the commands
in the [example README](../examples/oracle/README.md#regenerating-the-models)
rather than editing them. Generate from the same source directory and roots
as the proof contract, so that the theorems are about the code you
extracted. Two generations are byte-identical. Review the manifest diff when
the generator or Lean version changes. Rerun the oracle tests after
regenerating, because a model change is a change to the specification.

## The Adler-32 block bound

RFC 1950 defines Adler-32 with two sums: s1 is the sum of all bytes and
s2 is the sum of all s1 values. Both are taken modulo 65521, and s1 starts
at 1 and s2 at 0
([RFC 1950, section 2.2](https://www.rfc-editor.org/rfc/rfc1950#section-2.2)).
A fast implementation adds bytes into 32-bit sums and reduces only once
every n bytes. The largest safe n follows from the worst case. Both sums
grow with the starting sums and with each byte, so the worst case starts
from the largest reduced values a0 = b0 = 65520 and feeds n bytes of 0xff:

```text
a_i = a0 + 255 i
b_n = b0 + sum_{i=1..n} a_i = b0 + n a0 + 255 n (n+1) / 2
    = (n+1) 65520 + 255 n (n+1) / 2
```

The low sum a_n = 65520 + 255n is the smaller of the two, so the high sum
sets the bound: the largest n with b_n ≤ 2^32 − 1 is **5,552**. At that n
the worst high sum is 4,294,690,200. At 5,553 it would be 4,296,171,735,
which exceeds 2^32 − 1 = 4,294,967,295. zlib states the same bound as
`NMAX`: "the largest n such that 255n(n+1)/2 + (n+1)(BASE-1) <= 2^32-1"
([zlib `adler32.c`](https://github.com/madler/zlib/blob/develop/adler32.c)).

[`TestAdlerBlockBound`](../examples/oracle/fast/bound_test.go) checks the
bound in four ways:

- It searches for the largest n with the closed form in 64-bit arithmetic
  and requires 5,552, equal to `AdlerBlock`.
- It checks the closed form against a step-by-step 64-bit simulation.
- It checks that the low sum never binds.
- It requires the 32-bit block loop to agree with exact arithmetic from the
  worst state for a 5,552-byte block, and to disagree for 5,553 bytes.

Note that the starting state is the bottleneck, not the data alone. From
the initial state (1, 0), even 5,553 bytes of 0xff fit. That is why the
planted bug needs a chunk boundary to show.

## Limits

- **Tested, not proved.** Agreement covers only the inputs that ran.
  Seeds, exhaustive checks on small state spaces and arguments about worst
  cases narrow the gap; they do not close it.
- **Trusted base.** The extractor and the Go compiler are trusted: the link
  from the generated Go to the Lean source is tested, not proved. So is
  the harness itself; planted bugs are the check on it.
- **The model must be fast enough to fuzz.** Native folds run at hundreds of
  MB/s. A model that is quadratic, or recursive over deep values, limits the
  input sizes a fuzz campaign can reach.
- **The model must be extractable.** It must stay within the subset that
  native mode accepts ([native mode](native-mode.md#limits)). Model in Lean
  what can be extracted, and keep the optimizations on the Go side.
- **State conventions must line up.** The harness maps between the two
  representations. The example compares the packed Adler-32 state through
  the model's `checksum` root. The CRC model has no initial root, so the
  harness takes the XMODEM initial value 0 from the specification, and the
  proof covers every starting state. Mistakes in that mapping are harness
  bugs.
- **Fuzz budgets in CI are small.** Thirty seconds will not reach rare
  states by chance. Put the states you can reason about into seeds.
