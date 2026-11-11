# UInt32 arithmetic and comparisons

Extend ordinary module extraction with literals, unsigned addition, subtraction,
multiplication, equality and order comparisons. This follows the user's selected
numeric milestone. Go consumers continue to use `CGO_ENABLED=0`; producer tooling
continues to use Python 3.15 through uv. Publication remains outside this local
implementation task.

## Semantics and versioning

The new exact pair is `lean-go-ir/v3` with `pure-module/v2`. Existing v2 IR with
`pure-module/v1` remains accepted with its original restrictions, and the audited
normalizer v1 implementation remains separate. New source module exports use v3.
Reuse the existing module validator and emitter rather than copying a third backend.

All arithmetic is modulo 2^32: maximum plus one is zero; zero minus one is the
maximum. Comparisons are unsigned. The admitted pure-mono leaves are:

| Lean declaration | Parameters | Result | Meaning |
| --- | --- | --- | --- |
| UInt32.add | UInt32, UInt32 | UInt32 | addition modulo 2^32 |
| UInt32.sub | UInt32, UInt32 | UInt32 | subtraction modulo 2^32 |
| UInt32.mul | UInt32, UInt32 | UInt32 | multiplication modulo 2^32 |
| UInt32.shiftLeft | UInt32, UInt32 | UInt32 | left shift by second argument modulo 32 |
| UInt32.decEq | UInt32, UInt32 | Bool | equality |
| UInt32.decLt | UInt32, UInt32 | Bool | unsigned less-than |
| UInt32.decLe | UInt32, UInt32 | Bool | unsigned less-or-equal |

Shift-left is needed because Lean folds multiplication by a power of two into it.
It must use `a << (b & 31)`, including counts 32, 33 and the maximum UInt32. Other
bit operations, division/remainder, other integer widths, arbitrary Nat arithmetic
and new control-flow forms remain outside this milestone. Existing supported
module structure, self recursion, data and immutable boundary remain available.

The source equality/ordering interfaces can contain dependent Decidable results;
their checked pure-mono signatures return Bool. The exporter must use a pinned
primitive signature table for these leaves, not infer their runtime signature
from the source theorem/proposition interface. Ordinary `==` and `decide` forms
are exercised. Unsupported lowering forms continue to fail explicitly.

## IR and runtime

The existing exact value object remains `{kind,name,args}`. A literal uses kind
`uint32`, a canonical decimal name in 0..4294967295, and a non-null empty argument
array. Negative values, leading zeros, signs, whitespace and overflow fail IR
validation. Source literals are normalized by Lean before export; a source literal
4294967297 therefore has the admitted runtime value 1.

A primitive uses kind `primitive`, one exact declaration name from the table, and
two scoped UInt32 argument identifiers. The validator independently checks arity,
types, result, and profile gating. In v3, any Bool schema must be exactly false tag
0 and true tag 1, both without fields. Comparisons require this schema. Primitive
leaves are not user functions; their signatures are fixed rather than supplied by
input JSON. Normal function/type closure remains exact.

The emitter generates direct Go expressions on private uint32 fields. Every
literal and primitive result allocates one fresh owned node through the existing
allocation/output/depth budget check, then enters the destination slot. Comparison
results use ordinary canonical Bool nodes. Each let state costs its existing
step. Failure returns an invalid value; no operand is mutated or returned as a
partial result. No runtime template interpreter, C linkage or new dependency is
introduced. The manifest includes a sorted exact catalog of used primitives,
their signatures and fixed semantics, derived from validated code.

## Source admission

Extend the pinned source boundary with a dedicated numeric primitive/support
catalog. Check exact declaration names, defining toolchain modules, safe source
definitions, expected native extern symbol, expected implemented-by/CSimp absence,
and the per-declaration axiom set. The seven runtime leaves map only to their
audited Lean runtime symbols. UInt32.shiftLeft has the audited axiom set [propext];
the other six have none. Source literal conversion wrappers and the fixed UInt32
order-instance package are source-only support, never arbitrary runtime leaves.

The private pinned UInt32 order package contains [propext, Classical.choice,
Quot.sound]; that exact named package is an explicit trusted boundary. This does
not relax the rejection of user axioms or arbitrary source proofs. The same
declaration checks apply during historical capture and final-environment audit.
User-added externs, replacements, compiler rules and compiler passes remain
rejected, including modifications to admitted primitive/support names. Preserve
historical local/scoped records and modern imported-definition handling.

The new catalog's bytes become a recorded producer input. Source and runtime
primitive dependencies are explicitly documented. The assurance remains tested,
trusted extraction, without an end-to-end compiler-correctness proof.

## Acceptance

Use ordinary Lean arithmetic examples and a separately compiled native reference.
Compare boundary Cartesian products and seeded pairs across all seven operations;
include 0, 1, 31, 32, 33, 2^31, maximum, wraparound, equal/unequal and both ordering
directions. Exercise mixed expressions, literal normalization and optimized
multiplication. Test malformed IR, counterfeit Bool, unknown primitives, wrong
arguments, profile mismatches, budget exhaustion and invalid partial results.
Test custom source instances and late/scoped attribute mutations rather than
assuming operator spelling fixes a primitive's meaning. Regeneration must be
reproducible, existing consumers must pass, and a numeric consumer must run with
Go-only PATH and cgo disabled. Independent specification and quality reviews are
required. Windows evidence remains cross-compilation until actual execution occurs.
