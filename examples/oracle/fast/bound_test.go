package fast

import (
	"bytes"
	"testing"
)

// worstHigh is the largest high sum after n unreduced steps. Both sums only
// grow with the starting sums and with each byte, so the worst case starts
// from the largest reduced sums, a0 = b0 = 65520, and feeds n bytes of 0xff:
//
//	a_i = a0 + 255*i
//	b_n = b0 + sum_{i=1..n} a_i = b0 + n*a0 + 255*n*(n+1)/2
//
// With a0 = b0 = 65520 this is (n+1)*65520 + 255*n*(n+1)/2. The low sum
// a_n = 65520 + 255*n is far smaller, so the high sum sets the bound.
func worstHigh(n uint64) uint64 {
	return (n+1)*(AdlerMod-1) + 255*n*(n+1)/2
}

func TestAdlerBlockBound(t *testing.T) {
	const limit = 1<<32 - 1
	n := uint64(0)
	for worstHigh(n+1) <= limit {
		n++
	}
	t.Logf("largest safe block: %d bytes (worst high sum %d, next %d, limit %d)", n, worstHigh(n), worstHigh(n+1), uint64(limit))
	if n != AdlerBlock {
		t.Fatalf("largest safe block is %d, AdlerBlock is %d", n, AdlerBlock)
	}
	// Check the closed form against a step-by-step simulation in 64 bits,
	// and that the low sum is never the binding constraint.
	a, b := uint64(AdlerMod-1), uint64(AdlerMod-1)
	for i := uint64(1); i <= AdlerBlock+1; i++ {
		a += 0xff
		b += a
		if b != worstHigh(i) {
			t.Fatalf("step %d: simulated high sum %d, closed form %d", i, b, worstHigh(i))
		}
		if a > limit {
			t.Fatalf("low sum overflows first, at step %d", i)
		}
	}
	// The 32-bit block loop agrees with 64-bit arithmetic on the worst case
	// for AdlerBlock bytes and disagrees for one byte more.
	worst := uint32(AdlerMod-1)<<16 | (AdlerMod - 1)
	for _, size := range []int{AdlerBlock, AdlerBlock + 1} {
		run := bytes.Repeat([]byte{0xff}, size)
		got := adlerBlocks(worst, run, size)
		want := uint32(worstHigh(uint64(size))%AdlerMod)<<16 | uint32((AdlerMod-1+255*uint64(size))%AdlerMod)
		if (got == want) != (size == AdlerBlock) {
			t.Errorf("block of %d bytes from the worst state: 32-bit %#08x, exact %#08x", size, got, want)
		}
	}
}
