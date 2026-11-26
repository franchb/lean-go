package diff

import (
	"errors"
	"testing"

	met "difftest/Trees/met"
	nat "difftest/Trees/nat"
	small "difftest/Trees/natsmall"
)

// left builds a branch chain of the given height whose recursion descends
// on the left at every level.
func left(height int) nat.Tree {
	t := nat.Tree{}
	for range height {
		t = nat.TreeBranch(t, nat.TreeLeaf(nat.ColorGreen()))
	}
	return t
}

func height(t nat.Tree) int {
	h := 0
	for {
		l, r, ok := t.AsBranch()
		if !ok {
			return h
		}
		// Mirror moves the deep side to the right.
		if _, _, deep := r.AsBranch(); deep {
			t = r
		} else {
			t = l
		}
		h++
	}
}

// A chain of height h makes h+1 nested calls; MaxDepth nested calls succeed.
func TestDepthGuardBoundary(t *testing.T) {
	if small.MaxDepth != 64 {
		t.Fatalf("MaxDepth = %d", small.MaxDepth)
	}
	chain := small.Tree{}
	for h := 0; h <= 70; h++ {
		_, err := small.Mirror(chain)
		if ok := h+1 <= small.MaxDepth; ok != (err == nil) || (!ok && !errors.Is(err, small.ErrDepth)) {
			t.Fatalf("height %d: %v", h, err)
		}
		chain = small.TreeBranch(chain, small.Tree{})
	}
	got, err := nat.Mirror(left(nat.MaxDepth - 1))
	if err != nil || height(got) != nat.MaxDepth-1 {
		t.Fatalf("height %d at the default limit: %v", height(got), err)
	}
	if _, err := nat.Mirror(left(nat.MaxDepth)); !errors.Is(err, nat.ErrDepth) {
		t.Fatalf("one past the default limit: %v", err)
	}
}

// A hostile million-level value yields ErrDepth rather than a stack overflow.
func TestHostileDeepValue(t *testing.T) {
	deep := left(1_000_000)
	got, err := nat.Mirror(deep)
	if !errors.Is(err, nat.ErrDepth) || !got.IsLeaf() {
		t.Fatalf("got %v", err)
	}
}

// The zero Tree is Trees.Tree.leaf Trees.Color.red, wherever it appears.
func TestZeroTree(t *testing.T) {
	got, err := nat.Mirror(nat.TreeBranch(nat.Tree{}, nat.TreeLeaf(nat.Color{})))
	if err != nil {
		t.Fatal(err)
	}
	l, r, ok := got.AsBranch()
	lc, lok := l.AsLeaf()
	rc, rok := r.AsLeaf()
	if !ok || !lok || !rok || !lc.IsBlue() || !rc.IsBlue() {
		t.Fatalf("mirror of zero leaves: %v %v", lc, rc)
	}
}

func decodeTree(data []byte, depth int) (nat.Tree, []byte) {
	if len(data) == 0 {
		return nat.Tree{}, nil
	}
	b := data[0]
	data = data[1:]
	switch {
	case b == 0xff:
		return nat.Tree{}, data
	case b < 0x80 || depth == 0:
		return nat.TreeLeaf([]nat.Color{nat.ColorRed(), nat.ColorGreen(), nat.ColorBlue(), {}}[b%4]), data
	}
	l, data := decodeTree(data, depth-1)
	r, data := decodeTree(data, depth-1)
	return nat.TreeBranch(l, r), data
}

// FuzzMirror decodes arbitrary bytes into a tree, including zero handles,
// and requires agreement with the metered package and no panic.
func FuzzMirror(f *testing.F) {
	f.Add([]byte{0x80, 0x00, 0x81, 0x01, 0x02})
	f.Add([]byte{0xff})
	f.Add([]byte{0x90, 0x90, 0x90, 0x90, 0x03, 0xff, 0x02})
	f.Fuzz(func(t *testing.T, data []byte) {
		// Stay within the metered package's default node budget.
		tree, _ := decodeTree(data[:min(len(data), 16000)], 200)
		got, err := nat.Mirror(tree)
		if err != nil {
			t.Fatal(err)
		}
		want, err := met.RunTyped(met_t1(tree), limits)
		if err != nil {
			t.Fatal(err)
		}
		if !eq_t1(got, want) {
			t.Fatal("native and metered results differ")
		}
	})
}
