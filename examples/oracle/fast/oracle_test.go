package fast

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	adlermodel "example.com/lean-go-oracle/model/adler32"
	crcmodel "example.com/lean-go-oracle/model/crc16"
)

// The oracle harness. Each check runs a message, split into Update calls,
// through an implementation under test and through the model extracted from
// the proved Lean source, and compares the states after every chunk. The
// first difference is returned as an error; nil means they agree.

// adlerImpl is an Adler-32 implementation under test: its initial packed
// state and its streaming update.
type adlerImpl struct {
	init   uint32
	update func(state uint32, p []byte) uint32
}

// crcImpl is a CRC-16/XMODEM implementation under test.
type crcImpl struct {
	init   uint16
	update func(crc uint16, p []byte) uint16
}

var (
	fastAdler = adlerImpl{AdlerInit, UpdateAdler32}
	fastCRC   = crcImpl{CRC16Init, UpdateCRC16}
)

// modelCRCInit is the CRC-16/XMODEM initial register. The Lean model has no
// initial root: its theorems hold from every state, and the catalogue fixes
// the XMODEM initial value at zero.
const modelCRCInit uint16 = 0

func checkAdler(impl adlerImpl, chunks [][]byte) error {
	model := adlermodel.Initial()
	state := impl.init
	if want := adlermodel.Checksum(model); state != want {
		return fmt.Errorf("initial state: implementation %#08x, model %#08x", state, want)
	}
	for i, chunk := range chunks {
		model = adlermodel.Update(model, chunk)
		state = impl.update(state, chunk)
		if want := adlermodel.Checksum(model); state != want {
			return fmt.Errorf("after chunk %d of %d: implementation %#08x, model %#08x", i+1, len(chunks), state, want)
		}
	}
	return nil
}

func checkCRC(impl crcImpl, chunks [][]byte) error {
	model := modelCRCInit
	state := impl.init
	if state != model {
		return fmt.Errorf("initial state: implementation %#04x, model %#04x", state, model)
	}
	for i, chunk := range chunks {
		model = crcmodel.Update(model, chunk)
		state = impl.update(state, chunk)
		if state != model {
			return fmt.Errorf("after chunk %d of %d: implementation %#04x, model %#04x", i+1, len(chunks), state, model)
		}
	}
	return nil
}

// fuzzCase is the shape of one fuzz input. The message is data with a run of
// runLen copies of runByte inserted at runAt (modulo len(data)+1), so the
// fuzzer can reach long uniform runs without growing its inputs. cuts is read
// as little-endian uint16 chunk lengths; whatever the cuts do not cover is the
// last chunk. A cut may be zero, which exercises empty updates.
type fuzzCase struct {
	data    []byte
	cuts    []byte
	runLen  uint16
	runByte byte
	runAt   uint16
}

func (c fuzzCase) message() []byte {
	at := int(c.runAt) % (len(c.data) + 1)
	m := make([]byte, 0, len(c.data)+int(c.runLen))
	m = append(m, c.data[:at]...)
	m = append(m, bytes.Repeat([]byte{c.runByte}, int(c.runLen))...)
	return append(m, c.data[at:]...)
}

func (c fuzzCase) chunks() [][]byte {
	m := c.message()
	var out [][]byte
	for cuts := c.cuts; len(cuts) >= 2 && len(m) > 0; cuts = cuts[2:] {
		n := min(int(binary.LittleEndian.Uint16(cuts)), len(m))
		out = append(out, m[:n])
		m = m[n:]
	}
	if len(m) > 0 {
		out = append(out, m)
	}
	return out
}

func cutsOf(lengths ...int) []byte {
	var b []byte
	for _, n := range lengths {
		b = binary.LittleEndian.AppendUint16(b, uint16(n))
	}
	return b
}

// maxLowPrefix is the shortest message after which the Adler-32 low sum is
// 65520, its largest reduced value: 256 bytes of 0xff and one of 0xef. The
// sums are largest at the start of an Update call that follows it, which is
// where a deferred-reduction block starts.
func maxLowPrefix() []byte {
	return append(bytes.Repeat([]byte{0xff}, 256), 0xef)
}

// seedCases is the shared seed corpus: the empty message, every single byte,
// the two catalogue check strings, uniform runs of every length around one
// and two reduction blocks, the same runs after the sums have been driven to
// their maximum, and many tiny chunks.
func seedCases() []fuzzCase {
	cases := []fuzzCase{{}}
	for b := range 256 {
		cases = append(cases, fuzzCase{data: []byte{byte(b)}})
	}
	cases = append(cases,
		fuzzCase{data: []byte("123456789")},
		fuzzCase{data: []byte("Wikipedia"), cuts: cutsOf(4, 0, 1)},
	)
	prefix := maxLowPrefix()
	for _, n := range []int{1, AdlerBlock - 1, AdlerBlock, AdlerBlock + 1, AdlerBlock + 2,
		2*AdlerBlock - 1, 2 * AdlerBlock, 2*AdlerBlock + 1, 65535} {
		for _, fill := range []byte{0xff, 0x00} {
			cases = append(cases,
				fuzzCase{runLen: uint16(n), runByte: fill},
				fuzzCase{data: prefix, cuts: cutsOf(len(prefix)), runLen: uint16(n), runByte: fill, runAt: uint16(len(prefix))},
				fuzzCase{data: prefix, runLen: uint16(n), runByte: fill, runAt: uint16(len(prefix))},
			)
		}
	}
	tiny := make([]byte, 64)
	for i := range tiny {
		tiny[i] = byte(0xff - i)
	}
	cases = append(cases, fuzzCase{data: tiny, cuts: bytes.Repeat([]byte{1, 0}, 64)})
	return cases
}

// shrink returns a smaller failing input: it drops whole chunks, joins
// adjacent chunks, deletes spans inside chunks with halving span sizes, and
// lowers byte values, repeating until nothing changes. fails must report
// whether its argument still fails; shrink never calls it with an input
// that shares memory with the original.
func shrink(chunks [][]byte, fails func([][]byte) bool) [][]byte {
	cur := clone(chunks)
	try := func(next [][]byte) bool {
		if fails(next) {
			cur = next
			return true
		}
		return false
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(cur); i++ {
			next := clone(cur)
			if try(append(next[:i], next[i+1:]...)) {
				changed = true
				i--
			}
		}
		for i := 0; i+1 < len(cur); i++ {
			next := clone(cur)
			next[i] = append(next[i], next[i+1]...)
			if try(append(next[:i+1], next[i+2:]...)) {
				changed = true
				i--
			}
		}
		for i := range cur {
			for k := len(cur[i]) / 2; k >= 1; k /= 2 {
				for at := 0; at+k <= len(cur[i]); {
					next := clone(cur)
					next[i] = append(next[i][:at], next[i][at+k:]...)
					if try(next) {
						changed = true
					} else {
						at += k
					}
				}
			}
		}
		for i := range cur {
			for j := range cur[i] {
				for v := cur[i][j]; v > 0; {
					// Try zero first, then halve.
					lower := byte(0)
					if !tryByte(&cur, i, j, lower, try) {
						lower = v / 2
						if lower == 0 || !tryByte(&cur, i, j, lower, try) {
							break
						}
					}
					changed = true
					v = lower
				}
			}
		}
	}
	return cur
}

func tryByte(cur *[][]byte, i, j int, v byte, try func([][]byte) bool) bool {
	next := clone(*cur)
	next[i][j] = v
	return try(next)
}

func clone(chunks [][]byte) [][]byte {
	out := make([][]byte, len(chunks))
	for i, c := range chunks {
		out[i] = bytes.Clone(c)
		if out[i] == nil {
			out[i] = []byte{}
		}
	}
	return out
}

// describe renders chunks with runs collapsed, for example
// "[ff×256 ef] [ff×5553]".
func describe(chunks [][]byte) string {
	if len(chunks) == 0 {
		return "(empty message, no chunks)"
	}
	var parts []string
	for _, c := range chunks {
		var runs []string
		for i := 0; i < len(c); {
			j := i
			for j < len(c) && c[j] == c[i] {
				j++
			}
			if j-i == 1 {
				runs = append(runs, fmt.Sprintf("%02x", c[i]))
			} else {
				runs = append(runs, fmt.Sprintf("%02x×%d", c[i], j-i))
			}
			i = j
		}
		parts = append(parts, "["+strings.Join(runs, " ")+"]")
	}
	return strings.Join(parts, " ")
}

func totalLen(chunks [][]byte) int {
	n := 0
	for _, c := range chunks {
		n += len(c)
	}
	return n
}
