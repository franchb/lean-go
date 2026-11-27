package fast

import (
	"bytes"
	"testing"
)

// Deliberately wrong implementations. Each test runs the oracle harness over
// the same seed corpus that the fuzz targets start from, requires a
// counterexample, shrinks it and checks that the shrunk input is the
// smallest one by the argument given in the test. None of this depends on
// fuzzing luck: the seeds are fixed and the shrinker is deterministic.

// Off by one: 5553-byte blocks. Within one block the high sum grows by up to
// b0 + 5553*a0 + 255*5553*5554/2, which exceeds 2^32-1 only when the low sum
// a0 at the start of the block is at least 65303. Every other input agrees.
var adlerBlockTooLarge = adlerImpl{AdlerInit, func(s uint32, p []byte) uint32 {
	return adlerBlocks(s, p, AdlerBlock+1)
}}

// One wrong table entry: bit 0 of entry 0x5a of the eight-step table flipped.
var crcWrongEntry = func() crcImpl {
	t := *MakeCRC16Table(CRC16Poly)
	t[0][0x5a] ^= 1
	return crcImpl{CRC16Init, t.Update}
}()

// The CRC-16/CCITT-FALSE initial register (0xffff) used for XMODEM, whose
// initial register is zero: the two catalogue entries differ only there.
var crcWrongInit = crcImpl{0xffff, UpdateCRC16}

// firstFailing returns the first seed on which check fails.
func firstFailing(t *testing.T, check func([][]byte) error) ([][]byte, error) {
	t.Helper()
	for _, c := range seedCases() {
		chunks := c.chunks()
		if err := check(chunks); err != nil {
			return chunks, err
		}
	}
	t.Fatal("the oracle found no counterexample in the seed corpus")
	return nil, nil
}

func TestOracleCatchesAdlerBlockTooLarge(t *testing.T) {
	check := func(c [][]byte) error { return checkAdler(adlerBlockTooLarge, c) }
	found, err := firstFailing(t, check)
	t.Logf("seed counterexample (%d bytes): %s\n\t%v", totalLen(found), describe(found), err)
	small := shrink(found, func(c [][]byte) bool { return check(c) != nil })
	t.Logf("shrunk counterexample (%d bytes): %s\n\t%v", totalLen(small), describe(small), check(small))
	// No input shorter than 257 + 5553 bytes can fail. The overflow needs a
	// whole 5553-byte block in one call, starting from low sum >= 65303
	// (with the high sum at most 65520, below that the block fits). From the
	// initial low sum 1 that needs byte sum >= 65302 before the block,
	// which takes 257 bytes because 256*255 = 65280. The first block of a
	// call starts with the call, so those bytes form an earlier chunk.
	if len(small) != 2 || len(small[0]) != 257 || len(small[1]) != AdlerBlock+1 {
		t.Fatalf("shrunk counterexample has shape %d chunks, %d bytes; want [257 bytes] [5553 bytes]", len(small), totalLen(small))
	}
	// The same split passes with the correct block size.
	if err := checkAdler(fastAdler, small); err != nil {
		t.Fatal(err)
	}
	// The bug is invisible to a 0xff run of up to 64 KiB in one call from
	// the initial state: the low sum at block starts is 1 + 40074k mod
	// 65521, which stays below 65303 for k <= 11. Chunk boundaries are
	// what expose it.
	if err := checkAdler(adlerBlockTooLarge, [][]byte{bytes.Repeat([]byte{0xff}, 65535)}); err != nil {
		t.Fatalf("expected a single 64 KiB 0xff run to pass: %v", err)
	}
}

func TestOracleCatchesCRCWrongTableEntry(t *testing.T) {
	check := func(c [][]byte) error { return checkCRC(crcWrongEntry, c) }
	found, err := firstFailing(t, check)
	t.Logf("seed counterexample (%d bytes): %s\n\t%v", totalLen(found), describe(found), err)
	small := shrink(found, func(c [][]byte) bool { return check(c) != nil })
	t.Logf("shrunk counterexample (%d bytes): %s\n\t%v", totalLen(small), describe(small), check(small))
	// From the zero register the first byte indexes the table directly, so
	// the one-byte message 0x5a is a smallest counterexample.
	if len(small) != 1 || !bytes.Equal(small[0], []byte{0x5a}) {
		t.Fatalf("shrunk counterexample %s; want [5a]", describe(small))
	}
}

func TestOracleCatchesCRCWrongInit(t *testing.T) {
	check := func(c [][]byte) error { return checkCRC(crcWrongInit, c) }
	found, err := firstFailing(t, check)
	t.Logf("seed counterexample (%d bytes): %s\n\t%v", totalLen(found), describe(found), err)
	small := shrink(found, func(c [][]byte) bool { return check(c) != nil })
	t.Logf("shrunk counterexample (%d bytes): %s\n\t%v", totalLen(small), describe(small), check(small))
	if len(small) != 0 {
		t.Fatalf("shrunk counterexample %s; want the empty message", describe(small))
	}
}
