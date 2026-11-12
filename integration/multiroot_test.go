package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	counters "github.com/franchb/lean-go/generated/counters"
)

type counterSample struct {
	value, delta uint32
	total        uint64
	enabled      bool
}
type counterResult [4]uint64

func counterSamples() []counterSample {
	boundary := []uint32{0, 1, 31, 32, 1 << 31, 1<<32 - 1}
	wide := []uint64{0, 1, 1<<32 - 1, 1 << 32, 1<<53 + 1, 1 << 63, 1<<64 - 1}
	var samples []counterSample
	for _, a := range boundary {
		for _, b := range boundary {
			for _, enabled := range []bool{false, true} {
				samples = append(samples, counterSample{a, b, wide[len(samples)%len(wide)], enabled})
			}
		}
	}
	rng := rand.New(rand.NewSource(73264))
	for range 128 {
		samples = append(samples, counterSample{rng.Uint32(), rng.Uint32(), rng.Uint64(), rng.Intn(2) == 1})
	}
	return samples
}

func expectedCounter(sample counterSample) counterResult {
	first, second := uint64(sample.value), uint64(sample.value)
	if sample.enabled {
		first = (first + uint64(sample.delta)) % (1 << 32)
		second = (first + uint64(sample.delta)) % (1 << 32)
	}
	return counterResult{uint64(sample.value), first, second, sample.total}
}

func typedCounter(t *testing.T, sample counterSample) counterResult {
	t.Helper()
	l := counters.DefaultLimits()
	initial := requireTyped[counters.State](t)(counters.RunInitial(sample.value, sample.total, sample.enabled, l))
	delta := requireTyped[counters.Count](t)(counters.NewCount(sample.delta, l))
	first := requireTyped[counters.State](t)(counters.RunAdvance(initial, delta, l))
	second := requireTyped[counters.State](t)(counters.RunAdvance(first, delta, l))
	return counterResult{
		uint64(requireTyped[uint32](t)(counters.RunRead(initial, l))),
		uint64(requireTyped[uint32](t)(counters.RunRead(first, l))),
		uint64(requireTyped[uint32](t)(counters.RunRead(second, l))),
		requireTyped[uint64](t)(counters.RunTotal(second, l)),
	}
}

func rawCounter(t *testing.T, sample counterSample) counterResult {
	t.Helper()
	l := counters.DefaultLimits()
	importValue := func(input *counters.Input) counters.Value {
		return requireTyped[counters.Value](t)(counters.Import(input, l))
	}
	flag := "Bool.false"
	if sample.enabled {
		flag = "Bool.true"
	}
	args := []counters.Value{
		importValue(&counters.Input{Type: "UInt32", UInt32: sample.value}),
		importValue(&counters.Input{Type: "UInt64", UInt64: sample.total}),
		importValue(&counters.Input{Type: "Bool", Constructor: flag}),
	}
	run := func(name string, args ...counters.Value) counters.Value {
		return requireTyped[counters.Value](t)(counters.RunRoot("Counters."+name, args, l))
	}
	delta := importValue(&counters.Input{Type: "Counters.Count", Constructor: "Counters.Count.mk",
		Fields: []*counters.Input{{Type: "UInt32", UInt32: sample.delta}}})
	initial := run("initial", args...)
	first := run("advance", initial, delta)
	second := run("advance", first, delta)
	var result counterResult
	for i, value := range []counters.Value{initial, first, second} {
		n, ok := run("read", value).UInt32()
		if !ok {
			t.Fatal("read result is not UInt32")
		}
		result[i] = uint64(n)
	}
	var ok bool
	result[3], ok = run("total", second).UInt64()
	if !ok {
		t.Fatal("total result is not UInt64")
	}
	return result
}

func TestMultiRootComposedSemantics(t *testing.T) {
	for i, sample := range counterSamples() {
		want := expectedCounter(sample)
		if raw, typed := rawCounter(t, sample), typedCounter(t, sample); raw != want || typed != want {
			t.Fatalf("sample %d: raw %v typed %v want %v", i, raw, typed, want)
		}
	}
}

func TestMultiRootBoundaries(t *testing.T) {
	l := counters.DefaultLimits()
	for _, name := range []string{"", "Counters.step", "Counters.missing"} {
		value, err := counters.RunRoot(name, nil, l)
		if !errors.Is(err, counters.ErrMalformed) || value.Type() != "" {
			t.Fatalf("root %q: %v", name, err)
		}
	}
	if _, err := counters.RunRead(counters.State{}, l); !errors.Is(err, counters.ErrMalformed) {
		t.Fatalf("zero wrapper: %v", err)
	}
	state := requireTyped[counters.State](t)(counters.RunInitial(1, 1<<63, true, l))
	delta := requireTyped[counters.Count](t)(counters.NewCount(2, l))
	limited := l
	limited.MaxSlots = 1
	if _, err := counters.RunAdvance(state, delta, limited); !errors.Is(err, counters.ErrResource) {
		t.Fatalf("root frame budget: %v", err)
	}
	if value := requireTyped[uint32](t)(counters.RunRead(state, l)); value != 1 {
		t.Fatal("failed call changed state or leaked budget")
	}
}

func TestMultiRootGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module counterconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";x "counterconsumer/extracted")
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
l:=x.DefaultLimits()
s,err:=x.RunInitial(4294967295,18446744073709551615,true,l);if err!=nil{panic(err)}
d,err:=x.NewCount(2,l);if err!=nil{panic(err)}
next,err:=x.RunAdvance(s,d,l);if err!=nil{panic(err)}
v,err:=x.RunRead(next,l);if err!=nil||v!=1{panic("shared typed value")}
old,err:=x.RunRead(s,l);if err!=nil||old!=4294967295{panic("immutability")}
wide,err:=x.RunTotal(next,l);if err!=nil||wide!=18446744073709551615{panic("wide root")}
fmt.Println("multi-root Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/counters") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", "-buildvcs=false", ".")
	cmd.Dir, cmd.Env = dir, commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "multi-root Go-only consumer: OK\n" {
		t.Fatalf("consumer: %v\n%s", err, out)
	}
}

func TestMultiRootNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native multi-root comparison")
	}
	dir := t.TempDir()
	var source strings.Builder
	source.WriteString("import Counters\nstructure Sample where\n  value : UInt32\n  total : UInt64\n  enabled : Bool\n  delta : UInt32\ndef samples : List Sample := [\n")
	for i, sample := range counterSamples() {
		if i > 0 {
			source.WriteString(",\n")
		}
		fmt.Fprintf(&source, "{ value := %d, total := %d, enabled := %t, delta := %d }", sample.value, sample.total, sample.enabled, sample.delta)
	}
	source.WriteString(`]
def encode (sample : Sample) : String := Id.run do
  let original := Counters.initial sample.value sample.total sample.enabled
  let first := Counters.advance original ⟨sample.delta⟩
  let second := Counters.advance first ⟨sample.delta⟩
  let values := [toString (Counters.read original).toNat, toString (Counters.read first).toNat,
    toString (Counters.read second).toNat, toString (Counters.total second).toNat]
  return "[" ++ String.intercalate "," values ++ "]"
def main : IO Unit := IO.println ("[" ++ String.intercalate "," (samples.map encode) ++ "]")
`)
	files := map[string]string{"Reference.lean": source.String(), "lakefile.toml": "name = \"counter_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Counters\"\n[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n"}
	for target, path := range map[string]string{"Counters.lean": "../examples/modules/Counters.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "reference")
	cmd.Dir, cmd.Env = dir, commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v\n%s", err, out)
	}
	cmd = exec.Command(filepath.Join(dir, ".lake/build/bin/reference"))
	cmd.Dir, cmd.Env = dir, commandEnv(nil)
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var actual []counterResult
	if err = json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	samples := counterSamples()
	if len(actual) != len(samples) {
		t.Fatal("native result count mismatch")
	}
	for i, sample := range samples {
		if want := expectedCounter(sample); actual[i] != want || typedCounter(t, sample) != want || rawCounter(t, sample) != want {
			t.Fatalf("native sample %d: %v want %v", i, actual[i], want)
		}
	}
}

func TestMultiRootRegenerationOrder(t *testing.T) {
	if os.Getenv("LEAN_GO_REGENERATE") != "1" {
		t.Skip("set LEAN_GO_REGENERATE=1 for fresh multi-root extraction")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{"Counters.initial", "Counters.advance", "Counters.read", "Counters.total"}
	dir := t.TempDir()
	for pass := range 2 {
		out, ir := filepath.Join(dir, fmt.Sprintf("package%d", pass)), filepath.Join(dir, fmt.Sprintf("ir%d.json", pass))
		args := []string{"run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Counters", "--out", out, "--ir-output", ir}
		for i := range roots {
			index := i
			if pass == 1 {
				index = len(roots) - 1 - i
			}
			args = append(args, "--root", roots[index])
		}
		cmd := exec.Command("uv", args...)
		cmd.Dir, cmd.Env = root, commandEnv(nil)
		if log, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("regeneration: %v\n%s", err, log)
		}
		if !reflect.DeepEqual(directoryFiles(t, out), directoryFiles(t, "../generated/counters")) {
			t.Fatal("root ordering or regeneration changed package")
		}
		actual, err := os.ReadFile(ir)
		if err != nil {
			t.Fatal(err)
		}
		tracked, err := os.ReadFile("../testdata/counters.ir.json")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(actual, tracked) {
			t.Fatal("root ordering or regeneration changed IR")
		}
	}
}
