// Package fast holds hand-optimized checksum implementations that are tested
// differentially against models extracted from proved Lean sources.
package fast

// AdlerMod is the Adler-32 modulus, the largest prime below 2^16 (RFC 1950).
const AdlerMod = 65521

// AdlerBlock is the number of bytes summed between modular reductions. It is
// the largest n for which a block of n bytes of 0xff, starting from the largest
// reduced sums (65520, 65520), cannot overflow the 32-bit high sum:
//
//	(n+1)*65520 + 255*n*(n+1)/2 <= 2^32 - 1
//
// TestAdlerBlockBound recomputes it.
const AdlerBlock = 5552

// AdlerInit is the packed Adler-32 state of the empty message: high sum 0,
// low sum 1.
const AdlerInit uint32 = 1

// UpdateAdler32 continues an Adler-32 computation over p. The state is packed
// as (high << 16) | low, which is also the checksum, so the result of one call
// is passed to the next and the last result is the Adler-32 of the
// concatenated chunks. Start with AdlerInit.
func UpdateAdler32(state uint32, p []byte) uint32 {
	return adlerBlocks(state, p, AdlerBlock)
}

// adlerBlocks sums p in blocks of at most block bytes and reduces after each
// block. block must not exceed AdlerBlock for a correct result; the tests use
// larger values to show what the oracle catches.
func adlerBlocks(state uint32, p []byte, block int) uint32 {
	a, b := state&0xffff, state>>16
	for len(p) > 0 {
		q := p
		if len(q) > block {
			q = q[:block]
		}
		p = p[len(q):]
		// Eight bytes at a time: b gains 8*a plus each byte weighted by the
		// number of prefix sums it appears in, then a gains the bytes. This
		// is the same arithmetic as eight single steps, with a shorter
		// dependency chain.
		for len(q) >= 8 {
			x0, x1, x2, x3 := uint32(q[0]), uint32(q[1]), uint32(q[2]), uint32(q[3])
			x4, x5, x6, x7 := uint32(q[4]), uint32(q[5]), uint32(q[6]), uint32(q[7])
			b += 8*a + 8*x0 + 7*x1 + 6*x2 + 5*x3 + 4*x4 + 3*x5 + 2*x6 + x7
			a += x0 + x1 + x2 + x3 + x4 + x5 + x6 + x7
			q = q[8:]
		}
		for _, x := range q {
			a += uint32(x)
			b += a
		}
		a %= AdlerMod
		b %= AdlerMod
	}
	return b<<16 | a
}
