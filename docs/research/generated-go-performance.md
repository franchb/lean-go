# Generated Go performance

Reusing retired frame slots reduces heap allocation in recursive generated Go.
For the measured balanced tree, bytes per operation fell **41.2%** and allocations
fell **38.1%**, with an **8.6%** lower typed-API median runtime. For the skewed tree,
bytes fell **14.8%** and allocations fell **13.0%**; runtime was effectively unchanged.
The quota and numeric workloads retain their previous allocation counts.

These are local measurements of the compiled Go state machine, not throughput
promises or evidence of a formally verified compiler.

## Workloads

The maintained [`benchmarks`](../../benchmarks) package compares independently
written Go calculations with generated typed and raw APIs:

- **Quota:** immutable state `(capacity=1000, used=400)`, successful reserve and
  release of 7 units, and remaining capacity. Each operation has a separate timed
  loop. Calls do not carry the returned state into the next iteration.
- **Numbers:** `(123456789, 37)`, returning the full arithmetic/comparison record:
  wrapping addition, subtraction and multiplication; a masked shift; comparisons;
  constants; and a helper call.
- **Balanced tree:** depth 7, with 255 tree nodes and 128 color nodes.
- **Skewed tree:** 64 branches, with 129 tree nodes and 65 color nodes. Leaves in
  both shapes cycle red, green and blue. The operation mirrors and recolors the tree.

Every timed loop uses `b.Loop()` and a package-level result sink. Input creation,
raw import, and tree construction are outside timing. Generated errors are checked
inside the loop. Raw calls use previously imported immutable values. Typed numeric
arguments are wrapped on each call, so typed and raw allocation counts can differ.

The handwritten implementations have the same domain outputs but do not implement
the generated runtime's validation or logical resource budgets. The handwritten
numeric and quota outputs are plain Go records. Handwritten trees allocate fresh
immutable tree nodes and store colors as scalar enum fields; generated trees use
boxed color nodes. This is a comparison of representation and execution overhead,
not an equivalence claim about safety features. The sub-nanosecond handwritten
quota timings measure tiny inlined calculations, not application request latency.

Parity tests compare complete typed/raw output values against the handwritten
implementations. They cover quota acceptance, rejection, invalid raw state, zero
and maximum values; 24 numeric edge-case pairs; and the full shape and values of
both trees. Tree inputs are checked for immutability.

## Method and results

Host: Linux amd64, Intel Core i5-14600K. Toolchain: Go 1.27.4,
`CGO_ENABLED=0`, `GOTOOLCHAIN=local`, `GOWORK=off`. Each sample uses one Go execution
thread (`-cpu=1`); OS CPU affinity was not pinned. Benchmarks ran serially, with
other agent compilation and test work held during the timed runs.

Both measurements used exactly:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  go test -run '^$' -bench . -benchmem -count=5 -benchtime=300ms -cpu=1 ./benchmarks
```

The baseline uses generator `lean-go-modulegen/v6` from commit `1cfc39e`, with the
maintained benchmark files added before any generator edit. The after measurement
uses `lean-go-modulegen/v7`, with generator source digest
`6837785d8297ceff807188e18a7bf3b6d87e1d2be5bf9ebd93c07d5aafaa328d`. The IR, profiles, public APIs and workload inputs are unchanged.

The table shows medians of five samples. The raw [before](data/generated-go-before.txt)
and [after](data/generated-go-after.txt) logs retain every sample, byte count and
allocation count; ranges and other summaries can be recomputed from them.

| Workload / API | Before ns/op | After ns/op | Change | B/op before → after | allocs/op before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Numbers / Handwritten | 5.16 | 5.17 | +0.1% | 0 → 0 | 0 → 0 |
| Numbers / Typed | 530.70 | 525.40 | -1.0% | 1,808 → 1,808 | 22 → 22 |
| Numbers / Raw | 491.30 | 493.30 | +0.4% | 1,648 → 1,648 | 20 → 20 |
| Quota reserve / Handwritten | 0.64 | 0.64 | +0.1% | 0 → 0 | 0 → 0 |
| Quota reserve / Typed | 202.70 | 205.30 | +1.3% | 592 → 592 | 9 → 9 |
| Quota reserve / Raw | 185.00 | 187.00 | +1.1% | 512 → 512 | 8 → 8 |
| Quota release / Handwritten | 0.65 | 0.65 | -1.1% | 0 → 0 | 0 → 0 |
| Quota release / Typed | 181.40 | 183.70 | +1.3% | 512 → 512 | 8 → 8 |
| Quota release / Raw | 163.40 | 163.90 | +0.3% | 432 → 432 | 7 → 7 |
| Quota remaining / Handwritten | 0.40 | 0.40 | +0.6% | 0 → 0 | 0 → 0 |
| Quota remaining / Typed | 90.86 | 70.75 | -22.1% | 160 → 160 | 2 → 2 |
| Quota remaining / Raw | 72.85 | 72.88 | +0.0% | 160 → 160 | 2 → 2 |
| Balanced tree / Handwritten | 3,275.00 | 3,147.00 | -3.9% | 6,120 → 6,120 | 255 → 255 |
| Balanced tree / Typed | 25,837.00 | 23,611.00 | -8.6% | 55,296 → 32,488 | 982 → 608 |
| Balanced tree / Raw | 25,900.00 | 23,722.00 | -8.4% | 55,296 → 32,488 | 982 → 608 |
| Skewed tree / Handwritten | 1,623.00 | 1,592.00 | -1.9% | 3,096 → 3,096 | 129 → 129 |
| Skewed tree / Typed | 13,181.00 | 12,998.00 | -1.4% | 34,640 → 29,496 | 501 → 436 |
| Skewed tree / Raw | 13,099.00 | 13,492.00 | +3.0% | 34,640 → 29,496 | 501 → 436 |

Balanced typed runtime ranges were 25,453–26,073 ns before and 23,484–23,893 ns
after. Skewed typed ranges were 13,035–13,278 ns before and 12,954–13,101 ns after;
the median difference is within overlapping ranges. Numeric ranges also
overlap. These five sequential samples are not a statistical confidence interval,
and order, CPU frequency and background host activity may affect timings.

The quota-remaining baseline was unusually variable (78.52–105.50 ns for typed
calls). Its lower after median must **not** be attributed to frame reuse: that
nonrecursive generated function makes no callee frames and its allocation count
did not change. No broad quota or numeric speedup is claimed.

## Profile evidence

Before implementation, separate two-second CPU and allocation profiles of the
same tree and numeric inputs identified the generated executor's allocations.
The following percentages are sampled profile observations, not exact counters:

| Workload | Allocation evidence | CPU evidence |
| --- | --- | --- |
| Balanced tree / typed | `execute` directly accounts for 65.44% of allocation objects and 49.78% of bytes. The specific initial/callee slot-slice lines account for approximately 37.94% of all objects. Output nodes account for approximately 34.55% of objects and 50.14% of bytes. | `execute` is 19.50% flat; `runtime.makeslice` is 22.82% cumulative. Allocation and GC routines dominate the remaining hot entries. |
| Numbers / typed | Node construction is 72.48% of objects; direct executor allocations are 18.31%; typed scalar wrapping is 9.21%. | Node construction is 56.96% cumulative; `execute` is 7.17% flat. |
| Quota remaining / typed | All sampled objects are from node construction: one predicate Bool and one UInt64 result. No frame-slot heap allocation was sampled. | Node construction is 53.05% cumulative; limit checking is 9.32% flat. |

A balanced-tree operation enters 383 logical frames (255 mirror calls and 128
recolor calls), but only about nine are active at once. This supports reusing
storage at a previously visited depth. The numeric helper is called only once,
so it offers no repeated-depth reuse. Allocating output nodes and their field
slices remains a separate cost; this change does not optimize them.

To profile the maintained workload on the current revision:

```sh
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off \
  go test -run '^$' -bench '^BenchmarkTree/Balanced7/Typed$' -cpu=1 \
  -benchtime=2s -cpuprofile=/tmp/tree-cpu.pprof \
  -memprofile=/tmp/tree-mem.pprof -o /tmp/lean-go-bench.test ./benchmarks
go tool pprof -top /tmp/lean-go-bench.test /tmp/tree-cpu.pprof
go tool pprof -top -sample_index=alloc_objects /tmp/lean-go-bench.test /tmp/tree-mem.pprof
```

Profiles are separate from the five-sample timing runs. Profiling a newer revision
will not reproduce the old percentages exactly.

## Implementation and verification

The change is in [`internal/modulegen/emit.go`](../../internal/modulegen/emit.go).
The private `frameForCall` helper selects only retired backing capacity at the
next stack depth. It reuses the existing slot slice if capacity permits, clears
the **old length** before shrinking, and allocates only if the callee needs more
capacity. It returns a fresh frame value with reset function, continuation and
destination metadata. Argument copying and the parent continuation happen before
append, so no frame pointer is used across a possible stack relocation. The storage is local to one invocation; there is no
global or cross-invocation pool.

Generated constructor field slices contain copied node pointers and do not alias
frame-slot storage. Clearing retired slots releases references without mutating
nodes already returned to a parent. Each live depth owns distinct slot backing.
All original `budget.enter`, `budget.step` and `budget.make` charges remain in
place: `MaxSlots` still counts cumulative logical entered slots, even when their
physical storage is reused. `MaxFrames` still limits active depth. The logical
`MaxAllocations` budget is independent of Go's measured heap allocation count.

[`frames_test.go`](../../internal/modulegen/frames_test.go) exercises sibling calls
with slot counts 8 → 2 → 5 → 2 → 8 and an additional nested call. It checks old-tail
clearing, growth within and beyond capacity, retained output nodes, concurrent
invocations, typed/raw parity, and exact limits: 39 cumulative slots, 3 active
frames, 39 steps and 24 logical allocations. Reducing each applicable execution
or output limit by one must return the resource error and no partial result.

The focused generator, command, integration, quota and benchmark parity tests
passed, as did `go vet ./...`. All ten module packages were regenerated from the
checked-in IR into fresh directories before copying output. The quota proof
checker rechecked ten source theorems and rebound its evidence to the new
manifest; the source-proof claims did not change. Full native Lean regeneration
and differential checks remain separate release gates.
