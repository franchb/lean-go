import Adler32

namespace Adler32Contract

def observe (s : Adler32.State) : Nat × Nat := (s.low.toNat, s.high.toNat)

def reduced (s : Adler32.State) : Prop := s.low.toNat < 65521 ∧ s.high.toNat < 65521

/-- RFC 1950 computes both sums modulo 65521, starting at (1, 0). -/
def referenceByte (s : Nat × Nat) (b : UInt8) : Nat × Nat :=
  let low := (s.1 + b.toNat) % 65521
  (low, (s.2 + low) % 65521)

def step_spec : Prop := ∀ s b, reduced s →
  observe (Adler32.step s b) = referenceByte (observe s) b

def chunks_spec : Prop := ∀ (chunks : List (List UInt8)),
  observe (chunks.foldl Adler32.update Adler32.initial) =
    chunks.flatten.foldl referenceByte (1, 0)

/-- Combining independently seeded checksums gives the checksum of concatenation.
    The machine argument represents the full byte length of the right operand. -/
def combine_spec : Prop := ∀ (xs ys : List UInt8) (n : UInt64), n.toNat = ys.length →
  Adler32.combine (Adler32.checksum (Adler32.update Adler32.initial xs))
    (Adler32.checksum (Adler32.update Adler32.initial ys)) n =
  Adler32.checksum (Adler32.update Adler32.initial (xs ++ ys))

end Adler32Contract
