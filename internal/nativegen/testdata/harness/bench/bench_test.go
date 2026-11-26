// Package bench compares native and metered generated Go with line-for-line
// handwritten transliterations of the same Lean sources. Run through
// LEAN_GO_NATIVE_BENCH=. go test -run TestDifferential ./internal/nativegen.
package bench

import (
	"hash/adler32"
	"testing"

	madler "difftest/Adler32/met"
	nadler "difftest/Adler32/nat"
	mcrc "difftest/Crc16/met"
	ncrc "difftest/Crc16/nat"
	mnum "difftest/Numbers/met"
	nnum "difftest/Numbers/nat"
	mquota "difftest/Quota/met"
	nquota "difftest/Quota/nat"
	mtree "difftest/Trees/met"
	ntree "difftest/Trees/nat"
)

// Adler32.lean, line for line.
type adlerState struct{ low, high uint32 }

func adlerReduce(sum uint32) uint32 {
	if sum < 65521 {
		return sum
	}
	return sum - 65521
}
func adlerStep(state adlerState, b uint8) adlerState {
	low := adlerReduce(state.low + uint32(b))
	return adlerState{low, adlerReduce(state.high + low)}
}
func adlerUpdate(state adlerState, data []byte) adlerState {
	for _, b := range data {
		state = adlerStep(state, b)
	}
	return state
}
func adlerChecksum(state adlerState) uint32 { return state.high<<16 | state.low }

// Crc16.lean, line for line.
func crcStep(state uint16) uint16 {
	shifted := state << 1
	if uint32(state) < 0x8000 {
		return shifted
	}
	return shifted ^ 0x1021
}
func crcUpdateByte(state uint16, b uint8) uint16 {
	loaded := state ^ uint16(b)<<8
	a := crcStep(crcStep(loaded))
	c := crcStep(crcStep(a))
	d := crcStep(crcStep(c))
	return crcStep(crcStep(d))
}
func crcUpdate(state uint16, data []byte) uint16 {
	for _, b := range data {
		state = crcUpdateByte(state, b)
	}
	return state
}

// Numbers, Quota and Trees: the handwritten versions in benchmarks/.
type numbersResult struct {
	sum, difference, product, shifted    uint32
	equal, less, lessEq                  bool
	twice, zero, maximum, wrapped, mixed uint32
}

func compute(a, b uint32) numbersResult {
	return numbersResult{a + b, a - b, a * b, a << (b & 31), a == b, a < b, a <= b, a * 2, 0, ^uint32(0), 1, (a + 1) * (b - 1)}
}

type quotaState struct{ capacity, used uint64 }
type quotaOutcome struct {
	state    quotaState
	accepted bool
}

func reserve(s quotaState, amount uint64) quotaOutcome {
	if s.used <= s.capacity && amount <= s.capacity-s.used {
		return quotaOutcome{quotaState{s.capacity, s.used + amount}, true}
	}
	return quotaOutcome{s, false}
}
func release(s quotaState, amount uint64) quotaOutcome {
	if s.used <= s.capacity && amount <= s.used {
		return quotaOutcome{quotaState{s.capacity, s.used - amount}, true}
	}
	return quotaOutcome{s, false}
}
func remaining(s quotaState) uint64 {
	if s.used <= s.capacity {
		return s.capacity - s.used
	}
	return 0
}

type tree struct {
	left, right *tree
	color       uint8
}

func mirror(t *tree) *tree {
	if t.left == nil {
		c := t.color
		if c == 0 {
			c = 2
		} else if c == 2 {
			c = 0
		}
		return &tree{color: c}
	}
	return &tree{left: mirror(t.right), right: mirror(t.left)}
}
func balanced(depth int, seed *uint8) *tree {
	if depth == 0 {
		c := *seed % 3
		*seed++
		return &tree{color: c}
	}
	return &tree{left: balanced(depth-1, seed), right: balanced(depth-1, seed)}
}
func skew(depth int, seed *uint8) *tree {
	t := balanced(0, seed)
	for range depth {
		t = &tree{left: t, right: balanced(0, seed)}
	}
	return t
}

var colors = []ntree.Color{ntree.ColorRed(), ntree.ColorGreen(), ntree.ColorBlue()}

func nativeTree(t *tree) ntree.Tree {
	if t.left == nil {
		return ntree.TreeLeaf(colors[t.color])
	}
	return ntree.TreeBranch(nativeTree(t.left), nativeTree(t.right))
}
func meteredTree(tb testing.TB, t *tree) mtree.Tree {
	l := mtree.DefaultLimits()
	if t.left == nil {
		var c mtree.Color
		var err error
		switch t.color {
		case 0:
			c, err = mtree.NewColorRed(l)
		case 1:
			c, err = mtree.NewColorGreen(l)
		default:
			c, err = mtree.NewColorBlue(l)
		}
		if err != nil {
			tb.Fatal(err)
		}
		v, err := mtree.NewTreeLeaf(c, l)
		if err != nil {
			tb.Fatal(err)
		}
		return v
	}
	v, err := mtree.NewTreeBranch(meteredTree(tb, t.left), meteredTree(tb, t.right), l)
	if err != nil {
		tb.Fatal(err)
	}
	return v
}
func sameTree(n ntree.Tree, t *tree) bool {
	if c, ok := n.AsLeaf(); ok {
		return t.left == nil && colors[t.color] == c
	}
	l, r, _ := n.AsBranch()
	return t.left != nil && sameTree(l, t.left) && sameTree(r, t.right)
}

func payload() []byte {
	data := make([]byte, 64<<10)
	for i := range data {
		data[i] = byte(i*7 + i>>8)
	}
	return data
}

var sink32 uint32
var sink16 uint16
var sink64 uint64

func meteredAdler(tb testing.TB, data []byte) uint32 {
	l := madler.DefaultLimits()
	s, err := madler.RunInitial(l)
	if err != nil {
		tb.Fatal(err)
	}
	for len(data) > 0 {
		n := min(len(data), 4095)
		if s, err = madler.RunUpdate(s, data[:n], l); err != nil {
			tb.Fatal(err)
		}
		data = data[n:]
	}
	sum, err := madler.RunChecksum(s, l)
	if err != nil {
		tb.Fatal(err)
	}
	return sum
}

func meteredCRC(tb testing.TB, data []byte) uint16 {
	l := mcrc.DefaultLimits()
	var state uint16
	var err error
	for len(data) > 0 {
		n := min(len(data), 256)
		if state, err = mcrc.RunUpdate(state, data[:n], l); err != nil {
			tb.Fatal(err)
		}
		data = data[n:]
	}
	return state
}

func TestParity(t *testing.T) {
	data := payload()
	want := adler32.Checksum(data)
	if nadler.Checksum(nadler.Update(nadler.Initial(), data)) != want || adlerChecksum(adlerUpdate(adlerState{1, 0}, data)) != want || meteredAdler(t, data) != want {
		t.Fatal("Adler-32 implementations disagree")
	}
	if c := crcUpdate(0, data); ncrc.Update(0, data) != c || meteredCRC(t, data) != c {
		t.Fatal("CRC-16 implementations disagree")
	}
	if n, h := nnum.Compute(123456789, 37), compute(123456789, 37); n.Field0 != h.sum || n.Field3 != h.shifted || n.Field11 != h.mixed || n.Field5 != h.less {
		t.Fatal("Numbers disagree")
	}
	s := nquota.State{Capacity: 1000, Used: 400}
	if r := nquota.Reserve(s, 7); r.State.Used != reserve(quotaState{1000, 400}, 7).state.used || !r.Accepted {
		t.Fatal("Quota disagrees")
	}
	seed := uint8(0)
	h := balanced(7, &seed)
	got, err := ntree.Mirror(nativeTree(h))
	if err != nil || !sameTree(got, mirror(h)) {
		t.Fatal("Trees disagree")
	}
}

func BenchmarkAdler32(b *testing.B) {
	data := payload()
	b.Run("64KiB", func(b *testing.B) {
		b.Run("Native", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink32 = nadler.Checksum(nadler.Update(nadler.Initial(), data))
			}
		})
		b.Run("Handwritten", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink32 = adlerChecksum(adlerUpdate(adlerState{1, 0}, data))
			}
		})
		b.Run("HashAdler32", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink32 = adler32.Checksum(data)
			}
		})
		b.Run("Metered", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink32 = meteredAdler(b, data)
			}
		})
	})
}

func BenchmarkCRC16(b *testing.B) {
	data := payload()
	b.Run("64KiB", func(b *testing.B) {
		b.Run("Native", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink16 = ncrc.Update(0, data)
			}
		})
		b.Run("Handwritten", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink16 = crcUpdate(0, data)
			}
		})
		b.Run("Metered", func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				sink16 = meteredCRC(b, data)
			}
		})
	})
}

var numbersHand numbersResult
var numbersNative nnum.Results
var numbersMetered mnum.Results

func BenchmarkNumbers(b *testing.B) {
	a, c := uint32(123456789), uint32(37)
	b.Run("Native", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			numbersNative = nnum.Compute(a, c)
		}
	})
	b.Run("Handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			numbersHand = compute(a, c)
		}
	})
	b.Run("Metered", func(b *testing.B) {
		l := mnum.DefaultLimits()
		b.ReportAllocs()
		for b.Loop() {
			v, err := mnum.RunTyped(a, c, l)
			if err != nil {
				b.Fatal(err)
			}
			numbersMetered = v
		}
	})
}

var quotaHand quotaOutcome
var quotaNative nquota.Outcome
var quotaMetered mquota.Outcome

func BenchmarkQuota(b *testing.B) {
	hand := quotaState{1000, 400}
	native := nquota.State{Capacity: 1000, Used: 400}
	l := mquota.DefaultLimits()
	metered, err := mquota.NewState(1000, 400, l)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Reserve/Native", func(b *testing.B) {
		for b.Loop() {
			quotaNative = nquota.Reserve(native, 7)
		}
	})
	b.Run("Reserve/Handwritten", func(b *testing.B) {
		for b.Loop() {
			quotaHand = reserve(hand, 7)
		}
	})
	b.Run("Reserve/Metered", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if quotaMetered, err = mquota.RunReserve(metered, 7, l); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Release/Native", func(b *testing.B) {
		for b.Loop() {
			quotaNative = nquota.Release(native, 7)
		}
	})
	b.Run("Release/Handwritten", func(b *testing.B) {
		for b.Loop() {
			quotaHand = release(hand, 7)
		}
	})
	b.Run("Release/Metered", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if quotaMetered, err = mquota.RunRelease(metered, 7, l); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Remaining/Native", func(b *testing.B) {
		for b.Loop() {
			sink64 = nquota.Remaining(native)
		}
	})
	b.Run("Remaining/Handwritten", func(b *testing.B) {
		for b.Loop() {
			sink64 = remaining(hand)
		}
	})
	b.Run("Remaining/Metered", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if sink64, err = mquota.RunRemaining(metered, l); err != nil {
				b.Fatal(err)
			}
		}
	})
}

var treeHand *tree
var treeNative ntree.Tree
var treeMetered mtree.Tree

func BenchmarkTree(b *testing.B) {
	for _, shape := range []string{"Balanced7", "Skew64"} {
		seed := uint8(0)
		h := skew(64, &seed)
		if shape == "Balanced7" {
			seed = 0
			h = balanced(7, &seed)
		}
		native, metered := nativeTree(h), meteredTree(b, h)
		l := mtree.DefaultLimits()
		b.Run(shape+"/Native", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v, err := ntree.Mirror(native)
				if err != nil {
					b.Fatal(err)
				}
				treeNative = v
			}
		})
		b.Run(shape+"/Handwritten", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				treeHand = mirror(h)
			}
		})
		b.Run(shape+"/Metered", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v, err := mtree.RunTyped(metered, l)
				if err != nil {
					b.Fatal(err)
				}
				treeMetered = v
			}
		})
	}
}
