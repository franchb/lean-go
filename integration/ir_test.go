package integration_test

import (
	"os"
	"reflect"
	"testing"

	n "github.com/franchb/lean-go/generated/normalizer"
	"github.com/franchb/lean-go/internal/ir"
	"github.com/franchb/lean-go/internal/irtest"
)

func interpreterTree(e *n.Input) *irtest.Tree {
	if e == nil {
		return nil
	}
	return &irtest.Tree{Tag: int(e.Kind) - 1, Tri: uint8(e.Value), ID: e.ID, Left: interpreterTree(e.Left), Right: interpreterTree(e.Right)}
}
func TestIRMatchesGenerated(t *testing.T) {
	f, err := os.Open("../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	program, err := ir.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := irtest.New(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range expressions(2) {
		expected, err := evaluator.Evaluate(interpreterTree(input))
		if err != nil {
			t.Fatal(err)
		}
		owned, err := n.Import(input, n.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		actual, err := n.Normalize(owned, n.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(expected, interpreterTree(snapshot(actual))) {
			t.Fatal("generated Go differs from reference IR execution")
		}
	}
}
