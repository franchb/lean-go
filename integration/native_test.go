package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	n "github.com/franchb/lean-go/generated/normalizer"
)

type nativeTree struct {
	Tag         int     `json:"tag"`
	Tri         *uint8  `json:"tri,omitempty"`
	ID          *uint32 `json:"id,omitempty"`
	Left, Right *nativeTree
}

// Explicit codec tags avoid Go field-name defaults in the producer protocol.
func (e nativeTree) MarshalJSON() ([]byte, error) {
	m := map[string]any{"tag": e.Tag}
	if e.Tri != nil {
		m["tri"] = *e.Tri
	}
	if e.ID != nil {
		m["id"] = *e.ID
	}
	if e.Left != nil {
		m["left"] = e.Left
	}
	if e.Right != nil {
		m["right"] = e.Right
	}
	return json.Marshal(m)
}
func nativeExpr(e *n.Input) *nativeTree {
	out := &nativeTree{Tag: int(e.Kind) - 1}
	switch e.Kind {
	case n.Literal:
		v := uint8(e.Value)
		out.Tri = &v
	case n.Variable:
		v := e.ID
		out.ID = &v
	case n.Not:
		out.Left = nativeExpr(e.Left)
	case n.And, n.Or:
		out.Left = nativeExpr(e.Left)
		out.Right = nativeExpr(e.Right)
	}
	return out
}
func TestNativeLeanReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 to build the pinned native Lean reference")
	}
	corpus := expressions(2)
	inputs := make([]*n.Input, 512)
	for i := range inputs {
		inputs[i] = corpus[(i*7919)%len(corpus)]
	}
	inputs = append(inputs, unary(variable(^uint32(0))), binary(n.And, variable(0), variable(1)), unary(unary(unary(leaf(n.Unknown)))))
	wire := make([]*nativeTree, len(inputs))
	for i, e := range inputs {
		wire[i] = nativeExpr(e)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	outputPath := filepath.Join(dir, "output.json")
	irPath := filepath.Join(dir, "program.json")
	if err = os.WriteFile(inputPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", filepath.Join(root, "scripts/export.py"), "--output", irPath, "--reference-input", inputPath, "--reference-output", outputPath)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native producer failed: %v\n%s", err, output)
	}
	resultBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	var results []struct {
		Normal *nativeTree `json:"normal"`
		Values []uint8     `json:"values"`
	}
	if err = json.Unmarshal(resultBytes, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(inputs) {
		t.Fatalf("native batch length %d", len(results))
	}
	for i, r := range results {
		actual := checkExpression(t, inputs[i])
		if !reflect.DeepEqual(r.Normal, nativeExpr(actual)) {
			t.Fatalf("native normal form mismatch at %d", i)
		}
		want := []uint8{}
		for a := n.False; a <= n.True; a++ {
			for b := n.False; b <= n.True; b++ {
				want = append(want, uint8(evalInput(inputs[i], a, b)))
			}
		}
		if !reflect.DeepEqual(r.Values, want) {
			t.Fatalf("native evaluator mismatch at %d: %v != %v", i, r.Values, want)
		}
	}
}
