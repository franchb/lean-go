package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	choice "github.com/franchb/lean-go/generated/choice"
	trees "github.com/franchb/lean-go/generated/trees"
)

var colorNames = []string{"Trees.Color.red", "Trees.Color.green", "Trees.Color.blue"}

func colorLeaf(n int) *trees.Input {
	return &trees.Input{Type: "Trees.Tree", Constructor: "Trees.Tree.leaf", Fields: []*trees.Input{
		{Type: "Trees.Color", Constructor: colorNames[n]},
	}}
}
func treeBranch(a, b *trees.Input) *trees.Input {
	return &trees.Input{Type: "Trees.Tree", Constructor: "Trees.Tree.branch", Fields: []*trees.Input{a, b}}
}
func moduleTrees() []*trees.Input {
	values := []*trees.Input{colorLeaf(0), colorLeaf(1), colorLeaf(2)}
	for range 2 {
		previous := append([]*trees.Input(nil), values...)
		values = []*trees.Input{colorLeaf(0), colorLeaf(1), colorLeaf(2)}
		for _, a := range previous {
			for _, b := range previous {
				values = append(values, treeBranch(a, b))
			}
		}
	}
	rng := rand.New(rand.NewSource(741512))
	var random func(int) *trees.Input
	random = func(depth int) *trees.Input {
		if depth == 0 || rng.Intn(3) == 0 {
			return colorLeaf(rng.Intn(3))
		}
		return treeBranch(random(depth-1), random(depth-1))
	}
	for range 500 {
		values = append(values, random(6))
	}
	return values
}
func encodeModuleInput(v *trees.Input) any {
	if v.Constructor == "Trees.Tree.leaf" {
		for i, n := range colorNames {
			if v.Fields[0].Constructor == n {
				return []any{i}
			}
		}
		panic("test fixture color")
	}
	return []any{encodeModuleInput(v.Fields[0]), encodeModuleInput(v.Fields[1])}
}
func encodeModuleValue(v trees.Value) any {
	fields := v.Fields()
	if v.Constructor() == "Trees.Tree.leaf" {
		for i, n := range colorNames {
			if fields[0].Constructor() == n {
				return []any{i}
			}
		}
		panic("generated color")
	}
	return []any{encodeModuleValue(fields[0]), encodeModuleValue(fields[1])}
}
func expectedModuleTree(v *trees.Input) any {
	if v.Constructor == "Trees.Tree.leaf" {
		for i, n := range colorNames {
			if v.Fields[0].Constructor == n {
				return []any{2 - i}
			}
		}
		panic("test fixture color")
	}
	return []any{expectedModuleTree(v.Fields[1]), expectedModuleTree(v.Fields[0])}
}
func runModuleTree(t *testing.T, input *trees.Input) trees.Value {
	t.Helper()
	v, err := trees.Import(input, trees.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	out, err := trees.Run([]trees.Value{v}, trees.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestIndependentModuleSemantics(t *testing.T) {
	for i, input := range moduleTrees() {
		actual := encodeModuleValue(runModuleTree(t, input))
		if !reflect.DeepEqual(actual, expectedModuleTree(input)) {
			t.Fatalf("tree %d differs", i)
		}
	}
	for i, name := range []string{"stop", "wait", "go"} {
		v, err := choice.Import(&choice.Input{Type: "Choice.Signal", Constructor: "Choice.Signal." + name}, choice.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		out, err := choice.Run([]choice.Value{v}, choice.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if out.Constructor() != "Choice.Signal."+[]string{"wait", "go", "stop"}[i] {
			t.Fatal("choice rotation differs")
		}
	}
}

func TestModuleGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "extracted")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range directoryFiles(t, "../generated/trees") {
		if strings.HasSuffix(name, ".go") {
			if err := os.WriteFile(filepath.Join(pkg, name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	files := map[string]string{
		"go.mod": "module moduleconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";x "moduleconsumer/extracted")
func main(){
for _,n:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(n);err==nil{panic("producer available")}}
input:=&x.Input{Type:"Trees.Tree",Constructor:"Trees.Tree.leaf",Fields:[]*x.Input{{Type:"Trees.Color",Constructor:"Trees.Color.red"}}}
v,err:=x.Import(input,x.DefaultLimits());if err!=nil{panic(err)}
input.Fields[0].Constructor="Trees.Color.green"
out,err:=x.Run([]x.Value{v},x.DefaultLimits());if err!=nil{panic(err)}
if out.Constructor()!="Trees.Tree.leaf"||out.Fields()[0].Constructor()!="Trees.Color.blue"{panic("wrong output")}
fmt.Println("module Go-only consumer: OK")
}`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "module Go-only consumer: OK\n" {
		t.Fatalf("consumer: %v\n%s", err, out)
	}
}

func TestModuleNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native module comparison")
	}
	dir := t.TempDir()
	for target, source := range map[string]string{"Trees.lean": "../examples/modules/Trees.lean", "Reference.lean": "../examples/reference/TreesReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, target), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := "name = \"module_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Trees\"\n[[lean_exe]]\nname = \"module-reference\"\nroot = \"Reference\"\n"
	if err := os.WriteFile(filepath.Join(dir, "lakefile.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("lake", "build", "module-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v\n%s", err, out)
	}
	inputs, expected := []any{}, []any{}
	for _, input := range moduleTrees() {
		inputs = append(inputs, encodeModuleInput(input))
		expected = append(expected, encodeModuleValue(runModuleTree(t, input)))
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(filepath.Join(dir, ".lake/build/bin/module-reference"), "input.json", "output.json")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native run: %v\n%s", err, out)
	}
	actual, err := os.ReadFile(filepath.Join(dir, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err = json.Unmarshal(actual, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("native Lean and generated Go differ")
	}
}

func TestModuleRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh module producer builds")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, example := range []struct{ name, module, root string }{{"trees", "Trees", "Trees.mirror"}, {"choice", "Choice.Transform", "Choice.rotate"}} {
		t.Run(example.name, func(t *testing.T) {
			var prior []byte
			for i := range 2 {
				out := filepath.Join(dir, fmt.Sprintf("%s%d", example.name, i))
				irPath := out + ".json"
				cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", example.module, "--root", example.root, "--out", out, "--ir-output", irPath)
				cmd.Dir = root
				cmd.Env = commandEnv(nil)
				if text, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("generate: %v\n%s", err, text)
				}
				sameFiles(t, directoryFiles(t, out), directoryFiles(t, filepath.Join(root, "generated", example.name)))
				data, err := os.ReadFile(irPath)
				if err != nil {
					t.Fatal(err)
				}
				tracked, err := os.ReadFile(filepath.Join(root, "testdata", example.name+".ir.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, tracked) {
					t.Fatal("tracked module IR is stale")
				}
				if i > 0 && !bytes.Equal(data, prior) {
					t.Fatal("fresh module IR differs")
				}
				prior = data
			}
		})
	}
}

func TestChangedModuleAndScalarRoot(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for changed-source extraction")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sources := filepath.Join(dir, "source")
	if err = os.Mkdir(sources, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../examples/modules/Trees.lean")
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.ReplaceAll(string(data), "Trees", "Renamed")
	if !strings.Contains(changed, ".red => .blue") {
		t.Fatal("missing source mutation target")
	}
	changed = strings.Replace(changed, ".red => .blue", ".red => .green", 1)
	if err = os.WriteFile(filepath.Join(sources, "Renamed.lean"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, fn, test string }{
		{"changed", "Renamed.mirror", `package extracted
import "testing"
func TestChangedSource(t *testing.T){
v,err:=Import(&Input{Type:"Renamed.Tree",Constructor:"Renamed.Tree.leaf",Fields:[]*Input{{Type:"Renamed.Color",Constructor:"Renamed.Color.red"}}},DefaultLimits());if err!=nil{t.Fatal(err)}
out,err:=Run([]Value{v},DefaultLimits());if err!=nil{t.Fatal(err)}
if out.Fields()[0].Constructor()!="Renamed.Color.green"{t.Fatal("changed source behavior missing")}
}`},
		{"scalar", "Renamed.scalar", `package extracted
import "testing"
func TestScalarRoot(t *testing.T){for _,n:=range []uint32{0,1,^uint32(0)}{
v,err:=Import(&Input{Type:"UInt32",UInt32:n},DefaultLimits());if err!=nil{t.Fatal(err)}
out,err:=Run([]Value{v},DefaultLimits());if err!=nil{t.Fatal(err)}
got,ok:=out.UInt32();if !ok||got!=n{t.Fatal("scalar changed")}
}}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(dir, tc.name)
			cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", sources, "--module", "Renamed", "--root", tc.fn, "--out", out, "--ir-output", out+".json")
			cmd.Dir = root
			cmd.Env = commandEnv(nil)
			if text, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("changed generation: %v\n%s", err, text)
			}
			for name, body := range map[string]string{"go.mod": "module changedconsumer\n\ngo 1.27.4\n", "source_test.go": tc.test} {
				if err := os.WriteFile(filepath.Join(out, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd = exec.Command(goBinary(), "test", "-count=1", ".")
			cmd.Dir = out
			cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
			if text, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("changed consumer: %v\n%s", err, text)
			}
		})
	}
}
