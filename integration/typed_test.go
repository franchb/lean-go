package integration_test

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	local "github.com/franchb/lean-go/generated/local"
	numbers "github.com/franchb/lean-go/generated/numbers"
	records "github.com/franchb/lean-go/generated/records"
	trees "github.com/franchb/lean-go/generated/trees"
)

// This adapter keeps constructor failures at the caller's test location while
// accepting the constructor's value/error pair directly.
func requireTyped[T any](t *testing.T) func(T, error) T {
	t.Helper()
	return func(value T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}

func typedLocalArguments(t *testing.T, input localCase) (local.Config, local.Count, local.Count) {
	t.Helper()
	l := local.DefaultLimits()
	count := requireTyped[local.Count](t)
	bias := requireTyped[local.Nested](t)(local.NewNested(count(local.NewCount(input[1], l)), l))
	config := requireTyped[local.Config](t)(local.NewConfig(bias, input[0] == 1, l))
	return config, count(local.NewCount(input[2], l)), count(local.NewCount(input[3], l))
}

func TestTypedLocalSemantics(t *testing.T) {
	for i, input := range localCases() {
		config, x, y := typedLocalArguments(t, input)
		bias, biasOK := config.Bias()
		count, countOK := bias.Count()
		value, valueOK := count.Value()
		enabled, enabledOK := config.Enabled()
		if !biasOK || !countOK || !valueOK || !enabledOK || value != input[1] || enabled != (input[0] == 1) {
			t.Fatalf("local %d: named input getters differ", i)
		}
		out := requireTyped[local.Count](t)(local.RunTyped(config, x, y, local.DefaultLimits()))
		got, ok := out.Value()
		if !ok || uint64(got) != expectedLocal(input) || uint64(got) != runLocal(t, input) {
			t.Fatalf("local %d %v: typed result %d, valid %v", i, input, got, ok)
		}
		// Replacing a returned wrapper cannot mutate either the config or its child.
		count = local.Count{}
		bias, biasOK = config.Bias()
		count, countOK = bias.Count()
		value, valueOK = count.Value()
		if !biasOK || !countOK || !valueOK || value != input[1] {
			t.Fatalf("local %d: replacing a getter result mutated the config", i)
		}
		value, ok = x.Value()
		if !ok || value != input[2] {
			t.Fatalf("local %d: execution mutated an input", i)
		}
	}
}

func typedRecordArguments(t *testing.T, input recordCase) (records.State, records.Count) {
	t.Helper()
	l := records.DefaultLimits()
	count := requireTyped[records.Count](t)
	nested := requireTyped[records.Nested](t)(records.NewNested(count(records.NewCount(input[1], l)), l))
	enabled := requireTyped[records.Enabled](t)(records.NewEnabled(input[2] == 1, l))
	state := requireTyped[records.State](t)(records.NewState(count(records.NewCount(input[0], l)), nested, enabled, input[3], l))
	return state, count(records.NewCount(input[4], l))
}

func readTypedRecords(t *testing.T, state records.State) recordResults {
	t.Helper()
	count, countOK := state.Count()
	nested, nestedOK := state.Nested()
	inner, innerOK := nested.Count()
	enabled, enabledOK := state.Enabled()
	raw, rawOK := state.Raw()
	c, cOK := count.Value()
	n, nOK := inner.Value()
	e, eOK := enabled.Value()
	if !countOK || !nestedOK || !innerOK || !enabledOK || !rawOK || !cOK || !nOK || !eOK {
		t.Fatal("typed record getter rejected a valid field")
	}
	result := recordResults{uint64(c), uint64(n), 0, uint64(raw), (uint64(c) + uint64(n) + uint64(raw)) % (uint64(1) << 32)}
	if e {
		result[2] = 1
	}
	return result
}

func TestTypedRecordsSemantics(t *testing.T) {
	for i, input := range recordCases() {
		state, delta := typedRecordArguments(t, input)
		before := readTypedRecords(t, state)
		out := requireTyped[records.State](t)(records.RunTyped(state, delta, records.DefaultLimits()))
		if got := readTypedRecords(t, out); got != expectedRecords(input) || got != runRecords(t, input) {
			t.Fatalf("records %d %v: typed result %v", i, input, got)
		}
		if got := readTypedRecords(t, state); got != before {
			t.Fatalf("records %d: execution mutated the input: %v -> %v", i, before, got)
		}
	}
}

func readTypedNumbers(t *testing.T, result numbers.Results) numberResults {
	t.Helper()
	sum, difference, product, shifted, equal, less, lessEq, twice, zero, maximum, wrapped, mixed, ok := result.AsMk()
	if !ok {
		t.Fatal("typed numeric result rejected its constructor")
	}
	out := numberResults{uint64(sum), uint64(difference), uint64(product), uint64(shifted), 0, 0, 0,
		uint64(twice), uint64(zero), uint64(maximum), uint64(wrapped), uint64(mixed)}
	for i, bit := range []bool{equal, less, lessEq} {
		if bit {
			out[4+i] = 1
		}
	}
	return out
}

func TestTypedNumbersSemantics(t *testing.T) {
	for i, pair := range numberPairs() {
		out := requireTyped[numbers.Results](t)(numbers.RunTyped(pair[0], pair[1], numbers.DefaultLimits()))
		if got := readTypedNumbers(t, out); got != expectedNumbers(pair) || got != runNumbers(t, pair) {
			t.Fatalf("numbers %d %v: typed result %v", i, pair, got)
		}
	}
	l := numbers.DefaultLimits()
	out := requireTyped[numbers.Results](t)(numbers.NewResultsMk(1, 2, 3, 4, true, false, true, 5, 6, 7, 8, 9, l))
	if got, want := readTypedNumbers(t, out), (numberResults{1, 2, 3, 4, 1, 0, 1, 5, 6, 7, 8, 9}); got != want {
		t.Fatalf("typed sum construction changed fields: %v", got)
	}
}

func typedTreeInput(t *testing.T, input *trees.Input) trees.Tree {
	t.Helper()
	l := trees.DefaultLimits()
	tree := requireTyped[trees.Tree](t)
	if input.Constructor == "Trees.Tree.branch" {
		return tree(trees.NewTreeBranch(typedTreeInput(t, input.Fields[0]), typedTreeInput(t, input.Fields[1]), l))
	}
	var color trees.Color
	switch input.Fields[0].Constructor {
	case "Trees.Color.red":
		color = requireTyped[trees.Color](t)(trees.NewColorRed(l))
	case "Trees.Color.green":
		color = requireTyped[trees.Color](t)(trees.NewColorGreen(l))
	case "Trees.Color.blue":
		color = requireTyped[trees.Color](t)(trees.NewColorBlue(l))
	default:
		t.Fatal("unknown tree fixture color")
	}
	return tree(trees.NewTreeLeaf(color, l))
}

func encodeTypedTree(t *testing.T, tree trees.Tree) any {
	t.Helper()
	if color, ok := tree.AsLeaf(); ok {
		switch {
		case color.AsRed():
			return []any{0}
		case color.AsGreen():
			return []any{1}
		case color.AsBlue():
			return []any{2}
		default:
			t.Fatal("typed leaf has no color alternative")
		}
	}
	left, right, ok := tree.AsBranch()
	if !ok {
		t.Fatal("typed tree has no alternative")
	}
	return []any{encodeTypedTree(t, left), encodeTypedTree(t, right)}
}

func TestTypedTreesSemantics(t *testing.T) {
	for i, input := range moduleTrees() {
		tree := typedTreeInput(t, input)
		out := requireTyped[trees.Tree](t)(trees.RunTyped(tree, trees.DefaultLimits()))
		got := encodeTypedTree(t, out)
		if !reflect.DeepEqual(got, expectedModuleTree(input)) || !reflect.DeepEqual(got, encodeModuleValue(runModuleTree(t, input))) {
			t.Fatalf("tree %d: typed result differs", i)
		}
		if !reflect.DeepEqual(encodeTypedTree(t, tree), encodeModuleInput(input)) {
			t.Fatalf("tree %d: execution mutated input", i)
		}
	}
}

func TestTypedInvalidHandlesAndAlternatives(t *testing.T) {
	l := trees.DefaultLimits()
	red := requireTyped[trees.Color](t)(trees.NewColorRed(l))
	leaf := requireTyped[trees.Tree](t)(trees.NewTreeLeaf(red, l))
	branch := requireTyped[trees.Tree](t)(trees.NewTreeBranch(leaf, leaf, l))
	if red.AsGreen() || red.AsBlue() || (trees.Color{}).AsRed() {
		t.Fatal("wrong or invalid color alternative accepted")
	}
	if left, right, ok := leaf.AsBranch(); ok || left != (trees.Tree{}) || right != (trees.Tree{}) {
		t.Fatal("leaf exposed branch fields")
	}
	if color, ok := branch.AsLeaf(); ok || color != (trees.Color{}) {
		t.Fatal("branch exposed a leaf field")
	}
	if left, right, ok := (trees.Tree{}).AsBranch(); ok || left != (trees.Tree{}) || right != (trees.Tree{}) {
		t.Fatal("invalid tree exposed branch fields")
	}
	for name, construct := range map[string]func() (trees.Tree, error){
		"color": func() (trees.Tree, error) { return trees.NewTreeLeaf(trees.Color{}, l) },
		"left":  func() (trees.Tree, error) { return trees.NewTreeBranch(trees.Tree{}, leaf, l) },
		"right": func() (trees.Tree, error) { return trees.NewTreeBranch(leaf, trees.Tree{}, l) },
		"root":  func() (trees.Tree, error) { return trees.RunTyped(trees.Tree{}, l) },
	} {
		t.Run(name, func(t *testing.T) {
			out, err := construct()
			if !errors.Is(err, trees.ErrMalformed) || out != (trees.Tree{}) {
				t.Fatalf("invalid handle returned %v, %v", out, err)
			}
		})
	}
	if n, ok := (local.Count{}).Value(); ok || n != 0 {
		t.Fatal("invalid Count exposed a value")
	}
	if count, ok := (local.Nested{}).Count(); ok || count != (local.Count{}) {
		t.Fatal("invalid Nested exposed a child")
	}
	if enabled, ok := (local.Config{}).Enabled(); ok || enabled {
		t.Fatal("invalid Config exposed a Boolean")
	}
	if out, err := local.NewNested(local.Count{}, local.DefaultLimits()); !errors.Is(err, local.ErrMalformed) || out != (local.Nested{}) {
		t.Fatalf("invalid record child returned %v, %v", out, err)
	}
	config, x, y := typedLocalArguments(t, localCase{1, 2, 3, 4})
	for i, args := range []struct {
		config local.Config
		x, y   local.Count
	}{{local.Config{}, x, y}, {config, local.Count{}, y}, {config, x, local.Count{}}} {
		if out, err := local.RunTyped(args.config, args.x, args.y, local.DefaultLimits()); !errors.Is(err, local.ErrMalformed) || out != (local.Count{}) {
			t.Fatalf("invalid argument %d returned %v, %v", i, out, err)
		}
	}
}

func TestTypedConvertedHandles(t *testing.T) {
	t.Run("sums", func(t *testing.T) {
		l := trees.DefaultLimits()
		red := requireTyped[trees.Color](t)(trees.NewColorRed(l))
		leaf := requireTyped[trees.Tree](t)(trees.NewTreeLeaf(red, l))
		// Go permits explicit conversions between these wrappers because their
		// underlying struct shapes match. The owned node keeps its original type.
		wrongTree, wrongColor := trees.Tree(red), trees.Color(leaf)
		if wrongTree.IsLeaf() || wrongTree.IsBranch() {
			t.Fatal("converted Color reported a Tree alternative")
		}
		if color, ok := wrongTree.AsLeaf(); ok || color != (trees.Color{}) {
			t.Fatal("converted Color exposed a leaf field")
		}
		if left, right, ok := wrongTree.AsBranch(); ok || left != (trees.Tree{}) || right != (trees.Tree{}) {
			t.Fatal("converted Color exposed branch fields")
		}
		if wrongColor.IsRed() || wrongColor.IsGreen() || wrongColor.IsBlue() ||
			wrongColor.AsRed() || wrongColor.AsGreen() || wrongColor.AsBlue() {
			t.Fatal("converted Tree reported a Color alternative")
		}
		for name, call := range map[string]func() (trees.Tree, error){
			"leaf":  func() (trees.Tree, error) { return trees.NewTreeLeaf(wrongColor, l) },
			"left":  func() (trees.Tree, error) { return trees.NewTreeBranch(wrongTree, leaf, l) },
			"right": func() (trees.Tree, error) { return trees.NewTreeBranch(leaf, wrongTree, l) },
			"root":  func() (trees.Tree, error) { return trees.RunTyped(wrongTree, l) },
		} {
			t.Run(name, func(t *testing.T) {
				if out, err := call(); !errors.Is(err, trees.ErrMalformed) || out != (trees.Tree{}) {
					t.Fatalf("converted handle returned %v, %v", out, err)
				}
			})
		}
	})
	t.Run("records", func(t *testing.T) {
		l := local.DefaultLimits()
		config, x, y := typedLocalArguments(t, localCase{1, 2, 3, 4})
		wrongConfig, wrongCount := local.Config(x), local.Count(config)
		if bias, ok := wrongConfig.Bias(); ok || bias != (local.Nested{}) {
			t.Fatal("converted Count exposed a Config child")
		}
		if enabled, ok := wrongConfig.Enabled(); ok || enabled {
			t.Fatal("converted Count exposed a Config Boolean")
		}
		if value, ok := wrongCount.Value(); ok || value != 0 {
			t.Fatal("converted Config exposed a Count scalar")
		}
		if out, err := local.NewNested(wrongCount, l); !errors.Is(err, local.ErrMalformed) || out != (local.Nested{}) {
			t.Fatalf("converted record child returned %v, %v", out, err)
		}
		if out, err := local.RunTyped(wrongConfig, x, y, l); !errors.Is(err, local.ErrMalformed) || out != (local.Count{}) {
			t.Fatalf("converted Config argument returned %v, %v", out, err)
		}
		if out, err := local.RunTyped(config, wrongCount, y, l); !errors.Is(err, local.ErrMalformed) || out != (local.Count{}) {
			t.Fatalf("converted Count argument returned %v, %v", out, err)
		}
	})
}

func TestTypedSharedTreeLimitsAndOwnership(t *testing.T) {
	l := trees.DefaultLimits()
	red := requireTyped[trees.Color](t)(trees.NewColorRed(l))
	leaf := requireTyped[trees.Tree](t)(trees.NewTreeLeaf(red, l))
	branch := requireTyped[trees.Tree](t)(trees.NewTreeBranch(leaf, leaf, l))
	// Each shared branch occurrence has five nodes and depth three. The new
	// root therefore needs eleven nodes and depth four, irrespective of sharing.
	l.MaxNodes, l.MaxDepth = 11, 4
	root := requireTyped[trees.Tree](t)(trees.NewTreeBranch(branch, branch, l))
	for _, limits := range []trees.Limits{
		{MaxNodes: 10, MaxDepth: 4, MaxOutputNodes: l.MaxOutputNodes, MaxAllocations: l.MaxAllocations, MaxSteps: l.MaxSteps, MaxFrames: l.MaxFrames, MaxSlots: l.MaxSlots},
		{MaxNodes: 11, MaxDepth: 3, MaxOutputNodes: l.MaxOutputNodes, MaxAllocations: l.MaxAllocations, MaxSteps: l.MaxSteps, MaxFrames: l.MaxFrames, MaxSlots: l.MaxSlots},
	} {
		if out, err := trees.NewTreeBranch(branch, branch, limits); !errors.Is(err, trees.ErrResource) || out != (trees.Tree{}) {
			t.Fatalf("shared construction accepted insufficient budget: %v, %v", out, err)
		}
	}
	out := requireTyped[trees.Tree](t)(trees.RunTyped(root, l))
	if !reflect.DeepEqual(encodeTypedTree(t, out), []any{[]any{[]any{2}, []any{2}}, []any{[]any{2}, []any{2}}}) {
		t.Fatal("shared tree result differs")
	}
	left, right, ok := root.AsBranch()
	if !ok {
		t.Fatal("shared tree lost its branch")
	}
	left = trees.Tree{}
	branch = trees.Tree{}
	leaf = trees.Tree{}
	red = trees.Color{}
	if !reflect.DeepEqual(encodeTypedTree(t, right), []any{[]any{0}, []any{0}}) ||
		!reflect.DeepEqual(encodeTypedTree(t, root), []any{[]any{[]any{0}, []any{0}}, []any{[]any{0}, []any{0}}}) {
		t.Fatal("replacing aliases or running the tree changed its shared children")
	}
	if _, _, ok := left.AsBranch(); ok {
		t.Fatal("cleared alias retained a branch")
	}
}

var typedLocalLimits = []struct {
	name  string
	field func(*local.Limits) *uint64
}{
	{"input nodes", func(l *local.Limits) *uint64 { return &l.MaxNodes }},
	{"depth", func(l *local.Limits) *uint64 { return &l.MaxDepth }},
	{"output nodes", func(l *local.Limits) *uint64 { return &l.MaxOutputNodes }},
	{"allocations", func(l *local.Limits) *uint64 { return &l.MaxAllocations }},
	{"steps", func(l *local.Limits) *uint64 { return &l.MaxSteps }},
	{"frames", func(l *local.Limits) *uint64 { return &l.MaxFrames }},
	{"slots", func(l *local.Limits) *uint64 { return &l.MaxSlots }},
}

func TestTypedConstructorLimits(t *testing.T) {
	config, x, y := typedLocalArguments(t, localCase{1, 2, 3, 4})
	for _, tc := range typedLocalLimits {
		t.Run(tc.name, func(t *testing.T) {
			l := local.DefaultLimits()
			for _, invalid := range []uint64{0, *tc.field(&l) + 1} {
				*tc.field(&l) = invalid
				out, err := local.NewCount(7, l)
				if !errors.Is(err, local.ErrLimits) || out != (local.Count{}) {
					t.Fatalf("invalid limit %d returned %v, %v", invalid, out, err)
				}
				out, err = local.RunTyped(config, x, y, l)
				if !errors.Is(err, local.ErrLimits) || out != (local.Count{}) {
					t.Fatalf("RunTyped with invalid limit %d returned %v, %v", invalid, out, err)
				}
			}
		})
	}
	// Import-style construction charges nodes and depth, not execution budgets.
	l := local.Limits{MaxNodes: 2, MaxDepth: 2, MaxOutputNodes: 1, MaxAllocations: 1, MaxSteps: 1, MaxFrames: 1, MaxSlots: 1}
	count := requireTyped[local.Count](t)(local.NewCount(7, l))
	if value, ok := count.Value(); !ok || value != 7 {
		t.Fatal("exact Count construction budget failed")
	}
	for _, limits := range []local.Limits{
		{MaxNodes: 1, MaxDepth: 2, MaxOutputNodes: 1, MaxAllocations: 1, MaxSteps: 1, MaxFrames: 1, MaxSlots: 1},
		{MaxNodes: 2, MaxDepth: 1, MaxOutputNodes: 1, MaxAllocations: 1, MaxSteps: 1, MaxFrames: 1, MaxSlots: 1},
	} {
		if out, err := local.NewCount(7, limits); !errors.Is(err, local.ErrResource) || out != (local.Count{}) {
			t.Fatalf("insufficient Count construction budget returned %v, %v", out, err)
		}
	}
}

func compareTypedLocalRun(t *testing.T, input localCase, limits local.Limits) bool {
	t.Helper()
	config, x, y := typedLocalArguments(t, input)
	raw, rawErr := local.Run(importLocal(t, localInputs(input)), limits)
	typed, typedErr := local.RunTyped(config, x, y, limits)
	if !errors.Is(typedErr, rawErr) {
		t.Fatalf("typed/raw error mismatch: %v / %v", typedErr, rawErr)
	}
	if rawErr != nil {
		if !errors.Is(rawErr, local.ErrResource) || typed != (local.Count{}) || raw.Type() != "" {
			t.Fatalf("resource failure exposed a result: typed %v, raw %v, error %v", typed, raw, rawErr)
		}
		return false
	}
	value, ok := typed.Value()
	if !ok || uint64(value) != readLocal(t, raw) || uint64(value) != expectedLocal(input) {
		t.Fatalf("typed/raw successful result mismatch: %d, valid %v", value, ok)
	}
	return true
}

func TestTypedRunResourceParity(t *testing.T) {
	for flag := range uint32(2) {
		input := localCase{flag, 2, ^uint32(0), 3}
		for _, tc := range typedLocalLimits {
			t.Run(fmt.Sprintf("%s/%d", tc.name, flag), func(t *testing.T) {
				l := local.DefaultLimits()
				low, high := uint64(1), *tc.field(&l)
				for low < high {
					mid := low + (high-low)/2
					*tc.field(&l) = mid
					if compareTypedLocalRun(t, input, l) {
						high = mid
					} else {
						low = mid + 1
					}
				}
				if low <= 1 {
					t.Fatal("fixture did not exercise the budget")
				}
				// Config has five nodes; each Count argument has two. Run must
				// charge all three arguments against one nine-node input budget.
				if tc.name == "input nodes" && low != 9 {
					t.Fatalf("aggregate input-node boundary = %d, want 9", low)
				}
				if tc.name == "depth" && low != 4 {
					t.Fatalf("input-depth boundary = %d, want 4", low)
				}
				*tc.field(&l) = low
				if !compareTypedLocalRun(t, input, l) {
					t.Fatalf("exact budget %d rejected", low)
				}
				*tc.field(&l) = low - 1
				if compareTypedLocalRun(t, input, l) {
					t.Fatalf("insufficient budget %d accepted", low-1)
				}
			})
		}
	}
}

func TestTypedGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module typedconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import (
 "fmt"
 "os/exec"
 l "typedconsumer/local"
 n "typedconsumer/numbers"
 r "typedconsumer/records"
 t "typedconsumer/trees"
)
func must[T any](value T, err error) T { if err != nil { panic(err) }; return value }
func main() {
 for _, name := range []string{"lean", "lake", "python", "python3", "uv", "cc", "gcc"} {
  if _, err := exec.LookPath(name); err == nil { panic("producer available") }
 }
 ll := l.DefaultLimits()
 bias := must(l.NewNested(must(l.NewCount(2, ll)), ll))
 config := must(l.NewConfig(bias, true, ll))
 localResult := must(l.RunTyped(config, must(l.NewCount(4294967295, ll)), must(l.NewCount(9, ll)), ll))
 if value, ok := localResult.Value(); !ok || value != 40 { panic("local result") }
 rl := r.DefaultLimits()
 count := must(r.NewCount(5, rl))
 state := must(r.NewState(count, must(r.NewNested(count, rl)), must(r.NewEnabled(true, rl)), 10, rl))
 updated := must(r.RunTyped(state, count, rl))
 updatedCount, ok := updated.Count(); if !ok { panic("record count") }
 if value, ok := updatedCount.Value(); !ok || value != 10 { panic("record result") }
 enabled, ok := updated.Enabled(); if !ok { panic("record enabled") }
 if value, ok := enabled.Value(); !ok || value { panic("record bool") }
 if value, ok := updated.Raw(); !ok || value != 15 { panic("record scalar") }
 tl := t.DefaultLimits()
 leaf := must(t.NewTreeLeaf(must(t.NewColorRed(tl)), tl))
 tree := must(t.NewTreeBranch(leaf, leaf, tl))
 mirrored := must(t.RunTyped(tree, tl))
 left, right, ok := mirrored.AsBranch(); if !ok { panic("tree result") }
 for _, child := range []t.Tree{left, right} {
  color, ok := child.AsLeaf(); if !ok || !color.AsBlue() { panic("tree color") }
 }
 results := must(n.RunTyped(4294967295, 33, n.DefaultLimits()))
 sum, _, _, _, equal, less, lessEq, _, _, _, _, _, ok := results.AsMk()
 if !ok || sum != 32 || equal || less || lessEq { panic("numeric result") }
 fmt.Println("typed Go-only consumer: OK")
}`,
	}
	for _, module := range []string{"local", "records", "trees", "numbers"} {
		for name, data := range directoryFiles(t, filepath.Join("../generated", module)) {
			if strings.HasSuffix(name, ".go") {
				files[filepath.Join(module, name)] = data
			}
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "typed Go-only consumer: OK\n" {
		t.Fatalf("typed consumer: %v\n%s", err, out)
	}
	// Reuse the successfully compiled module, changing only the typed caller.
	// The resulting diagnostic must name the incompatible nominal types.
	writeNumbersFiles(t, dir, map[string]string{"main.go": `package main
import l "typedconsumer/local"
func main() {
 limits := l.DefaultLimits()
 count, _ := l.NewCount(7, limits)
 _, _ = l.RunTyped(count, count, count, limits)
}`})
	cmd = exec.Command(goBinary(), "build", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "cannot use count") ||
		!strings.Contains(string(out), "Count") || !strings.Contains(string(out), "Config") ||
		!strings.Contains(string(out), "argument to l.RunTyped") {
		t.Fatalf("wrong typed argument did not fail type checking: %v\n%s", err, out)
	}
}
