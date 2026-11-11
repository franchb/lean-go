package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	numbers "github.com/franchb/lean-go/generated/numbers"
)

type numberPair [2]uint32
type numberResults [12]uint64

func numberPairs() []numberPair {
	boundary := []uint32{0, 1, 2, 3, 31, 32, 33, 65535, 65536, 1<<31 - 1, 1 << 31, 1<<32 - 2, 1<<32 - 1}
	pairs := make([]numberPair, 0, len(boundary)*len(boundary)+1024)
	for _, a := range boundary {
		for _, b := range boundary {
			pairs = append(pairs, numberPair{a, b})
		}
	}
	rng := rand.New(rand.NewSource(741532))
	for range 1024 {
		pairs = append(pairs, numberPair{rng.Uint32(), rng.Uint32()})
	}
	return pairs
}

// Use widened arithmetic and an explicit modulus rather than uint32 overflow.
// A left shift is multiplication by a power of two in this independent oracle.
func expectedNumbers(pair numberPair) numberResults {
	const modulus uint64 = 1 << 32
	a, b := uint64(pair[0]), uint64(pair[1])
	bit := func(p bool) uint64 {
		if p {
			return 1
		}
		return 0
	}
	return numberResults{
		(a + b) % modulus, (a + modulus - b) % modulus, (a * b) % modulus,
		(a * (uint64(1) << (b % 32))) % modulus,
		bit(a == b), bit(a < b), bit(a <= b),
		(a * 2) % modulus, 0, modulus - 1, 1,
		(((a + 1) % modulus) * ((b + modulus - 1) % modulus)) % modulus,
	}
}

func numberArguments(t *testing.T, pair numberPair) []numbers.Value {
	t.Helper()
	args := make([]numbers.Value, len(pair))
	for i, value := range pair {
		var err error
		args[i], err = numbers.Import(&numbers.Input{Type: "UInt32", UInt32: value}, numbers.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	return args
}

func readNumberResults(t *testing.T, out numbers.Value) numberResults {
	t.Helper()
	var result numberResults
	fields := out.Fields()
	if out.Type() != "Numbers.Results" || out.Constructor() != "Numbers.Results.mk" || len(fields) != len(result) {
		t.Fatalf("unexpected numeric result shape: %s %s (%d fields)", out.Type(), out.Constructor(), len(fields))
	}
	for i, field := range fields {
		if i >= 4 && i <= 6 {
			if field.Type() != "Bool" || len(field.Fields()) != 0 {
				t.Fatalf("field %d is not a nullary Bool", i)
			}
			switch field.Constructor() {
			case "Bool.false":
			case "Bool.true":
				result[i] = 1
			default:
				t.Fatalf("field %d has unknown Bool constructor %q", i, field.Constructor())
			}
		} else {
			scalar, ok := field.UInt32()
			if !ok || len(field.Fields()) != 0 {
				t.Fatalf("field %d is not UInt32", i)
			}
			result[i] = uint64(scalar)
		}
	}
	return result
}

func runNumbers(t *testing.T, pair numberPair) numberResults {
	t.Helper()
	out, err := numbers.Run(numberArguments(t, pair), numbers.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return readNumberResults(t, out)
}

func TestNumbersSemantics(t *testing.T) {
	for i, pair := range numberPairs() {
		if got, want := runNumbers(t, pair), expectedNumbers(pair); got != want {
			t.Fatalf("pair %d %v: generated %v, oracle %v", i, pair, got, want)
		}
	}
}

func writeNumbersFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNumbersGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module numbersconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";n "numbersconsumer/extracted")
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
input:=&n.Input{Type:"UInt32",UInt32:4294967295}
a,err:=n.Import(input,n.DefaultLimits());if err!=nil{panic(err)}
input.UInt32=0
b,err:=n.Import(&n.Input{Type:"UInt32",UInt32:33},n.DefaultLimits());if err!=nil{panic(err)}
out,err:=n.Run([]n.Value{a,b},n.DefaultLimits());if err!=nil{panic(err)}
fields:=out.Fields();if len(fields)!=12{panic("wrong arity")}
want:=[]uint32{32,4294967262,4294967263,4294967294,0,0,0,4294967294,0,4294967295,1,0}
for i,f:=range fields{if i>=4&&i<=6{if f.Constructor()!="Bool.false"{panic("comparison")}}else{v,ok:=f.UInt32();if !ok||v!=want[i]{panic("arithmetic")}}}
fields[0]=n.Value{}
again,err:=n.Run([]n.Value{a,b},n.DefaultLimits());if err!=nil{panic(err)}
sum,ok:=again.Fields()[0].UInt32();if !ok||sum!=32{panic("ownership")}
fmt.Println("numeric Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/numbers") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "numeric Go-only consumer: OK\n" {
		t.Fatalf("consumer: %v\n%s", err, out)
	}
}

func buildNumbersReference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"lakefile.toml": "name = \"numbers_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Numbers\"\n[[lean_exe]]\nname = \"numbers-reference\"\nroot = \"Reference\"\n"}
	for target, source := range map[string]string{"Numbers.lean": "../examples/modules/Numbers.lean", "Reference.lean": "../examples/reference/NumbersReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "numbers-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v\n%s", err, out)
	}
	return dir
}

func TestNumbersNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native numeric comparison")
	}
	dir := buildNumbersReference(t)
	pairs := numberPairs()
	data, err := json.Marshal(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/numbers-reference"), "input.json", "output.json")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native run: %v\n%s", err, out)
	}
	data, err = os.ReadFile(filepath.Join(dir, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	var results [][]uint64
	if err = json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(pairs) {
		t.Fatalf("native result count %d, want %d", len(results), len(pairs))
	}
	for i, pair := range pairs {
		if len(results[i]) != len(numberResults{}) {
			t.Fatalf("native pair %d has %d results", i, len(results[i]))
		}
		native := numberResults(results[i])
		want, generated := expectedNumbers(pair), runNumbers(t, pair)
		if native != want || generated != want {
			t.Fatalf("pair %d %v: native %v, generated %v, oracle %v", i, pair, native, generated, want)
		}
	}
}

func generateNumbers(t *testing.T, out, rootFunction string) []byte {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Numbers", "--root", rootFunction, "--out", out, "--ir-output", out+".json")
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("numeric generation: %v\n%s", err, text)
	}
	data, err := os.ReadFile(out + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestNumbersRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh numeric producer builds")
	}
	dir := t.TempDir()
	tracked, err := os.ReadFile("../testdata/numbers.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var prior []byte
	for i := range 2 {
		out := filepath.Join(dir, fmt.Sprintf("numbers%d", i))
		data := generateNumbers(t, out, "Numbers.compute")
		sameFiles(t, directoryFiles(t, out), directoryFiles(t, "../generated/numbers"))
		if !bytes.Equal(data, tracked) {
			t.Fatal("tracked numeric IR is stale")
		}
		if i > 0 && !bytes.Equal(data, prior) {
			t.Fatal("fresh numeric IR differs")
		}
		prior = data
	}
}

func TestNumbersScalarRoot(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for numeric scalar-root extraction")
	}
	out := filepath.Join(t.TempDir(), "scalar")
	generateNumbers(t, out, "Numbers.scalar")
	files := map[string]string{
		"go.mod": "module numbersscalar\n\ngo 1.27.4\n",
		"scalar_test.go": `package extracted
import "testing"
func TestScalar(t *testing.T){
for _,tc:=range [][3]uint32{{0,0,4294967295},{4294967295,0,0},{1,3,4},{2147483647,3,0}}{
args:=make([]Value,2);for i,n:=range tc[:2]{v,err:=Import(&Input{Type:"UInt32",UInt32:n},DefaultLimits());if err!=nil{t.Fatal(err)};args[i]=v}
out,err:=Run(args,DefaultLimits());if err!=nil{t.Fatal(err)}
got,ok:=out.UInt32();if !ok||got!=tc[2]{t.Fatalf("%v: got %v",tc,got)}
}}
`,
	}
	writeNumbersFiles(t, out, files)
	cmd := exec.Command(goBinary(), "test", "-count=1", ".")
	cmd.Dir = out
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scalar consumer: %v\n%s", err, text)
	}
}
