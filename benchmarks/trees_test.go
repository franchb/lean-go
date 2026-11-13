package benchmarks_test

import (
	nt "github.com/franchb/lean-go/generated/trees"
	"testing"
)

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
func typedTree(t testing.TB, n *tree) nt.Tree {
	t.Helper()
	l := nt.DefaultLimits()
	if n.left == nil {
		var c nt.Color
		var err error
		switch n.color {
		case 0:
			c, err = nt.NewColorRed(l)
		case 1:
			c, err = nt.NewColorGreen(l)
		case 2:
			c, err = nt.NewColorBlue(l)
		}
		if err != nil {
			t.Fatal(err)
		}
		v, err := nt.NewTreeLeaf(c, l)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v, err := nt.NewTreeBranch(typedTree(t, n.left), typedTree(t, n.right), l)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func rawTree(n *tree) *nt.Input {
	if n.left == nil {
		return &nt.Input{Type: "Trees.Tree", Constructor: "Trees.Tree.leaf", Fields: []*nt.Input{{Type: "Trees.Color", Constructor: []string{"Trees.Color.red", "Trees.Color.green", "Trees.Color.blue"}[n.color]}}}
	}
	return &nt.Input{Type: "Trees.Tree", Constructor: "Trees.Tree.branch", Fields: []*nt.Input{rawTree(n.left), rawTree(n.right)}}
}
func readTree(t testing.TB, n nt.Tree) *tree {
	t.Helper()
	if c, ok := n.AsLeaf(); ok {
		var color uint8
		switch {
		case c.IsRed():
			color = 0
		case c.IsGreen():
			color = 1
		case c.IsBlue():
			color = 2
		default:
			t.Fatal("unknown color")
		}
		return &tree{color: color}
	}
	a, b, ok := n.AsBranch()
	if !ok {
		t.Fatal("unknown tree")
	}
	return &tree{left: readTree(t, a), right: readTree(t, b)}
}

var treeHandSink *tree
var treeTypedSink nt.Tree
var treeRawSink nt.Value

func BenchmarkTree(b *testing.B) {
	for _, shape := range []string{"Balanced7", "Skew64"} {
		b.Run(shape, func(b *testing.B) {
			seed := uint8(0)
			var n *tree
			if shape == "Balanced7" {
				n = balanced(7, &seed)
			} else {
				n = skew(64, &seed)
			}
			typed := typedTree(b, n)
			l := nt.DefaultLimits()
			raw, err := nt.Import(rawTree(n), l)
			if err != nil {
				b.Fatal(err)
			}
			args := []nt.Value{raw}
			b.Run("Handwritten", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					treeHandSink = mirror(n)
				}
			})
			b.Run("Typed", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					v, err := nt.RunTyped(typed, l)
					if err != nil {
						b.Fatal(err)
					}
					treeTypedSink = v
				}
			})
			b.Run("Raw", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					v, err := nt.Run(args, l)
					if err != nil {
						b.Fatal(err)
					}
					treeRawSink = v
				}
			})
		})
	}
}
