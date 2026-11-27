package fast

import (
	"bytes"
	"hash/adler32"
	"math/rand/v2"
	"testing"

	crcmodel "example.com/lean-go-oracle/model/crc16"
)

func TestSeedsAgreeWithModel(t *testing.T) {
	for i, c := range seedCases() {
		chunks := c.chunks()
		if err := checkAdler(fastAdler, chunks); err != nil {
			t.Errorf("seed %d Adler-32 %s: %v", i, describe(chunks), err)
		}
		if err := checkCRC(fastCRC, chunks); err != nil {
			t.Errorf("seed %d CRC-16 %s: %v", i, describe(chunks), err)
		}
	}
}

// randomChunks splits a random message into random chunks. Chunk lengths are
// drawn from a mix of small sizes and sizes around the reduction block.
func randomChunks(r *rand.Rand, size int) [][]byte {
	m := make([]byte, size)
	switch r.IntN(4) {
	case 0:
		for i := range m {
			m[i] = byte(r.Uint32())
		}
	case 1:
		for i := range m {
			m[i] = 0xff
		}
	case 2:
		for i := range m {
			m[i] = 0xff - byte(r.IntN(4))
		}
	default:
		b := byte(r.Uint32())
		for i := range m {
			if r.IntN(64) == 0 {
				b = byte(r.Uint32())
			}
			m[i] = b
		}
	}
	var chunks [][]byte
	for len(m) > 0 {
		var n int
		switch r.IntN(3) {
		case 0:
			n = r.IntN(17)
		case 1:
			n = AdlerBlock - 8 + r.IntN(17)
		default:
			n = r.IntN(3 * AdlerBlock)
		}
		n = min(n, len(m))
		chunks = append(chunks, m[:n])
		m = m[n:]
	}
	return chunks
}

func TestRandomChunkingAgreesWithModel(t *testing.T) {
	r := rand.New(rand.NewPCG(1950, 0x1021))
	rounds := 400
	if testing.Short() {
		rounds = 40
	}
	for i := range rounds {
		size := r.IntN(64 << 10)
		if i%50 == 0 {
			size = 1 << 20
		}
		chunks := randomChunks(r, size)
		if err := checkAdler(fastAdler, chunks); err != nil {
			t.Fatalf("round %d Adler-32 (%d bytes, %d chunks): %v", i, totalLen(chunks), len(chunks), err)
		}
		if err := checkCRC(fastCRC, chunks); err != nil {
			t.Fatalf("round %d CRC-16 (%d bytes, %d chunks): %v", i, totalLen(chunks), len(chunks), err)
		}
	}
}

// The CRC state space is small enough to check both fast steps exhaustively.
// The fast Update is a fold of a two-byte step followed by at most one
// one-byte step. The one-byte step is compared with the model on every
// (register, byte) pair. The two-byte step reads the register and the bytes
// only through x = register ^ (b0<<8 | b1), so comparing it on every
// register with two zero bytes covers every x; it is also compared on every
// (register, b0) pair with a b1 derived from both. Adler-32 has no such
// shortcut: the fast version's blocks are not a byte fold and its state
// space has 2^32 reachable values.
func TestCRCStepsExhaustive(t *testing.T) {
	var one [1]byte
	var two [2]byte
	for s := range 1 << 16 {
		for b := range 256 {
			one[0] = byte(b)
			if got, want := UpdateCRC16(uint16(s), one[:]), crcmodel.UpdateByte(uint16(s), byte(b)); got != want {
				t.Fatalf("state %#04x byte %#02x: fast %#04x, model %#04x", s, b, got, want)
			}
			two[0], two[1] = byte(b), byte(s*7+b*13)
			if got, want := UpdateCRC16(uint16(s), two[:]), crcmodel.Update(uint16(s), two[:]); got != want {
				t.Fatalf("state %#04x bytes %x: fast %#04x, model %#04x", s, two, got, want)
			}
		}
		two = [2]byte{}
		if got, want := UpdateCRC16(uint16(s), two[:]), crcmodel.Update(uint16(s), two[:]); got != want {
			t.Fatalf("state %#04x bytes 0000: fast %#04x, model %#04x", s, got, want)
		}
	}
}

// Independent checks that do not involve the model: the published check
// values and the standard library.
func TestKnownValues(t *testing.T) {
	if got := UpdateCRC16(CRC16Init, []byte("123456789")); got != 0x31C3 {
		t.Errorf("CRC-16/XMODEM check value %#04x, want 0x31C3", got)
	}
	if got := UpdateAdler32(AdlerInit, []byte("Wikipedia")); got != 0x11E60398 {
		t.Errorf("Adler-32(Wikipedia) %#08x, want 0x11E60398", got)
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		chunks := randomChunks(r, r.IntN(200<<10))
		if got, want := UpdateAdler32(AdlerInit, bytes.Join(chunks, nil)), adler32.Checksum(bytes.Join(chunks, nil)); got != want {
			t.Fatalf("hash/adler32 %#08x, fast %#08x", want, got)
		}
	}
}

func FuzzAdler32(f *testing.F) {
	for _, c := range seedCases() {
		f.Add(c.data, c.cuts, c.runLen, c.runByte, c.runAt)
	}
	f.Fuzz(func(t *testing.T, data, cuts []byte, runLen uint16, runByte byte, runAt uint16) {
		c := fuzzCase{data, cuts, runLen, runByte, runAt}
		chunks := c.chunks()
		before := bytes.Clone(c.message())
		if err := checkAdler(fastAdler, chunks); err != nil {
			t.Fatalf("%s: %v", describe(chunks), err)
		}
		if !bytes.Equal(bytes.Join(chunks, nil), before) {
			t.Fatal("an update modified its input")
		}
	})
}

func FuzzCRC16(f *testing.F) {
	for _, c := range seedCases() {
		f.Add(c.data, c.cuts, c.runLen, c.runByte, c.runAt)
	}
	f.Fuzz(func(t *testing.T, data, cuts []byte, runLen uint16, runByte byte, runAt uint16) {
		c := fuzzCase{data, cuts, runLen, runByte, runAt}
		chunks := c.chunks()
		before := bytes.Clone(c.message())
		if err := checkCRC(fastCRC, chunks); err != nil {
			t.Fatalf("%s: %v", describe(chunks), err)
		}
		if !bytes.Equal(bytes.Join(chunks, nil), before) {
			t.Fatal("an update modified its input")
		}
	})
}
