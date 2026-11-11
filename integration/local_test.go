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
	"slices"
	"strings"
	"testing"

	local "github.com/franchb/lean-go/generated/local"
)

// Wire rows are enabled bit, nested bias Count, x Count, y Count.
type localCase [4]uint32

func localCases() []localCase {
	boundary := []uint32{0, 1, 2, 65535, 65536, 1<<31 - 1, 1 << 31, 1<<32 - 2, 1<<32 - 1}
	inputs := make([]localCase, 0, 2*len(boundary)*len(boundary)*len(boundary)+512)
	for _, bias := range boundary {
		for _, x := range boundary {
			for _, y := range boundary {
				for flag := range uint32(2) {
					inputs = append(inputs, localCase{flag, bias, x, y})
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(935317))
	for range 512 {
		inputs = append(inputs, localCase{uint32(rng.Intn(2)), rng.Uint32(), rng.Uint32(), rng.Uint32()})
	}
	return inputs
}

// Widen arithmetic and reduce once, independently of the helper's UInt32 chain.
func expectedLocal(input localCase) uint64 {
	total := 9 * (uint64(input[2]) + uint64(input[3]))
	if input[0] == 1 {
		total = uint64(input[2]) + uint64(input[3]) + 16*uint64(input[1])
	}
	return total % (uint64(1) << 32)
}

func localScalar(value uint32) *local.Input {
	return &local.Input{Type: "UInt32", UInt32: value}
}

func localNode(name string, fields ...*local.Input) *local.Input {
	return &local.Input{Type: "Local." + name, Constructor: "Local." + name + ".mk", Fields: fields}
}

func localInputs(input localCase) []*local.Input {
	flag := "Bool.false"
	if input[0] == 1 {
		flag = "Bool.true"
	}
	return []*local.Input{
		localNode("Config", localNode("Nested", localNode("Count", localScalar(input[1]))), &local.Input{Type: "Bool", Constructor: flag}),
		localNode("Count", localScalar(input[2])), localNode("Count", localScalar(input[3])),
	}
}

func importLocal(t *testing.T, inputs []*local.Input) []local.Value {
	t.Helper()
	args := make([]local.Value, len(inputs))
	for i, input := range inputs {
		var err error
		args[i], err = local.Import(input, local.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	return args
}

func readLocal(t *testing.T, value local.Value) uint64 {
	t.Helper()
	fields := value.Fields()
	if value.Type() != "Local.Count" || value.Constructor() != "Local.Count.mk" || len(fields) != 1 {
		t.Fatalf("expected nominal Count result, got %s %s (%d fields)", value.Type(), value.Constructor(), len(fields))
	}
	if _, scalar := value.UInt32(); scalar {
		t.Fatal("source wrapper exposed as scalar")
	}
	result, ok := fields[0].UInt32()
	if !ok || fields[0].Type() != "UInt32" || fields[0].Constructor() != "" || len(fields[0].Fields()) != 0 {
		t.Fatal("Count field is not UInt32")
	}
	return uint64(result)
}

func runLocal(t *testing.T, input localCase) uint64 {
	t.Helper()
	out, err := local.Run(importLocal(t, localInputs(input)), local.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return readLocal(t, out)
}

func TestLocalSemantics(t *testing.T) {
	for i, input := range localCases() {
		if got, want := runLocal(t, input), expectedLocal(input); got != want {
			t.Fatalf("local %d %v: generated %d, oracle %d", i, input, got, want)
		}
	}
}

func TestLocalOwnership(t *testing.T) {
	for flag := range uint32(2) {
		input := localCase{flag, 2, 4294967295, 9}
		source := localInputs(input)
		args := importLocal(t, source)
		source[0].Fields[0].Fields[0].Fields[0].UInt32 = 100
		source[0].Fields[0].Fields[0] = localNode("Count", localScalar(200))
		source[0].Fields[1].Constructor = "Bool.false"
		source[1].Fields[0].UInt32 = 300
		source[2].Fields[0] = localScalar(400)
		// Accessors at every captured record level must return detached slices.
		args[0].Fields()[0].Fields()[0].Fields()[0] = local.Value{}
		args[0].Fields()[0].Fields()[0] = local.Value{}
		args[0].Fields()[1] = local.Value{}
		for range 3 {
			out, err := local.Run(args, local.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			out.Fields()[0] = local.Value{}
			if got, want := readLocal(t, out), expectedLocal(input); got != want {
				t.Fatalf("mutable captured input/accessor changed result: %d, want %d", got, want)
			}
		}
		out, err := local.Run(args, local.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		args[1] = out
		input[2] = uint32(expectedLocal(input))
		out, err = local.Run(args, local.DefaultLimits())
		if err != nil || readLocal(t, out) != expectedLocal(input) {
			t.Fatalf("reused source wrapper: %v", err)
		}
	}
}

func invalidLocalValue(t *testing.T, out local.Value, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || out.Type() != "" || out.Constructor() != "" || len(out.Fields()) != 0 {
		t.Fatalf("expected invalid result and %v, got %s %s and %v", want, out.Type(), out.Constructor(), err)
	}
	if _, ok := out.UInt32(); ok {
		t.Fatal("invalid result exposed scalar")
	}
}

func TestLocalOriginalSourceAPI(t *testing.T) {
	for _, malformed := range []*local.Input{
		nil, localNode("Count"), localNode("Count", localScalar(1), localScalar(2)),
		localNode("Nested", localScalar(1)), localNode("Config", localNode("Nested", localNode("Count", localScalar(1)))),
		localNode("Config", localNode("Count", localScalar(1)), &local.Input{Type: "Bool", Constructor: "Bool.true"}),
		localNode("Config", localNode("Nested", localNode("Count", localScalar(1))), localScalar(0)),
		{Type: "Local.Count", Constructor: "Local.Nested.mk", Fields: []*local.Input{localScalar(1)}},
	} {
		out, err := local.Import(malformed, local.DefaultLimits())
		invalidLocalValue(t, out, err, local.ErrMalformed)
	}
	args := importLocal(t, localInputs(localCase{}))
	for _, wrong := range [][]local.Value{nil, args[:2], append(append([]local.Value{}, args...), args[1]), {args[1], args[0], args[2]}} {
		out, err := local.Run(wrong, local.DefaultLimits())
		invalidLocalValue(t, out, err, local.ErrMalformed)
	}
	scalar := importLocal(t, []*local.Input{localScalar(1)})[0]
	for i := range args {
		for _, bad := range []local.Value{{}, scalar} {
			wrong := append([]local.Value{}, args...)
			wrong[i] = bad
			out, err := local.Run(wrong, local.DefaultLimits())
			invalidLocalValue(t, out, err, local.ErrMalformed)
		}
	}
}

func TestLocalBudgets(t *testing.T) {
	for flag := range uint32(2) {
		input := localCase{flag, 2, 4294967295, 3}
		args := importLocal(t, localInputs(input))
		for _, tc := range []struct {
			name string
			get  func(local.Limits) uint64
			set  func(*local.Limits, uint64)
		}{
			{"input nodes", func(l local.Limits) uint64 { return l.MaxNodes }, func(l *local.Limits, n uint64) { l.MaxNodes = n }},
			{"allocations", func(l local.Limits) uint64 { return l.MaxAllocations }, func(l *local.Limits, n uint64) { l.MaxAllocations = n }},
			{"steps", func(l local.Limits) uint64 { return l.MaxSteps }, func(l *local.Limits, n uint64) { l.MaxSteps = n }},
			{"frames", func(l local.Limits) uint64 { return l.MaxFrames }, func(l *local.Limits, n uint64) { l.MaxFrames = n }},
			{"slots", func(l local.Limits) uint64 { return l.MaxSlots }, func(l *local.Limits, n uint64) { l.MaxSlots = n }},
			{"depth", func(l local.Limits) uint64 { return l.MaxDepth }, func(l *local.Limits, n uint64) { l.MaxDepth = n }},
			{"output nodes", func(l local.Limits) uint64 { return l.MaxOutputNodes }, func(l *local.Limits, n uint64) { l.MaxOutputNodes = n }},
		} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, flag), func(t *testing.T) {
				// Find the exact accepted boundary without depending on compiler binder IDs.
				low, high := uint64(1), tc.get(local.DefaultLimits())
				for low < high {
					mid := low + (high-low)/2
					limits := local.DefaultLimits()
					tc.set(&limits, mid)
					out, err := local.Run(args, limits)
					if err == nil {
						high = mid
					} else {
						invalidLocalValue(t, out, err, local.ErrResource)
						low = mid + 1
					}
				}
				if low <= 1 {
					t.Fatal("fixture did not exercise budget")
				}
				limits := local.DefaultLimits()
				tc.set(&limits, low)
				out, err := local.Run(args, limits)
				if err != nil || readLocal(t, out) != expectedLocal(input) {
					t.Fatalf("exact budget %d failed: %v", low, err)
				}
				t.Logf("exact %s budget: %d", tc.name, low)
				tc.set(&limits, low-1)
				out, err = local.Run(args, limits)
				invalidLocalValue(t, out, err, local.ErrResource)
			})
		}
	}
}

func TestLocalGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module localconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";l "localconsumer/extracted")
func scalar(n uint32)*l.Input{return &l.Input{Type:"UInt32",UInt32:n}}
func node(name string,fields ...*l.Input)*l.Input{return &l.Input{Type:"Local."+name,Constructor:"Local."+name+".mk",Fields:fields}}
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
source:=[]*l.Input{node("Config",node("Nested",node("Count",scalar(2))),&l.Input{Type:"Bool",Constructor:"Bool.true"}),node("Count",scalar(4294967295)),node("Count",scalar(9))}
args:=make([]l.Value,len(source));for i,p:=range source{var err error;args[i],err=l.Import(p,l.DefaultLimits());if err!=nil{panic(err)}}
source[0].Fields[0].Fields[0].Fields[0].UInt32=100
out,err:=l.Run(args,l.DefaultLimits());if err!=nil{panic(err)}
if out.Type()!="Local.Count"||out.Constructor()!="Local.Count.mk"||len(out.Fields())!=1{panic("wrapper shape")}
n,ok:=out.Fields()[0].UInt32();if !ok||n!=40{panic("result")}
out.Fields()[0]=l.Value{};n,ok=out.Fields()[0].UInt32();if !ok||n!=40{panic("field ownership")}
fmt.Println("local Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/local") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "local Go-only consumer: OK\n" {
		t.Fatalf("local consumer: %v\n%s", err, out)
	}
}

func buildLocalReference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"lakefile.toml": "name = \"local_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Local\"\n[[lean_exe]]\nname = \"local-reference\"\nroot = \"Reference\"\n"}
	for target, source := range map[string]string{"Local.lean": "../examples/modules/Local.lean", "Reference.lean": "../examples/reference/LocalReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "local-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native local build: %v\n%s", err, out)
	}
	return dir
}

func runLocalReference(t *testing.T, dir string, wire []byte) ([]byte, error) {
	t.Helper()
	input, output := filepath.Join(dir, "input.json"), filepath.Join(dir, "output.json")
	if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, wire, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/local-reference"), input, output)
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		if _, outputErr := os.Stat(output); !errors.Is(outputErr, os.ErrNotExist) {
			t.Fatalf("native local failure published output: %v", outputErr)
		}
		return nil, fmt.Errorf("%w: %s", err, text)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return data, nil
}

func TestLocalNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native local comparison")
	}
	dir := buildLocalReference(t)
	inputs := localCases()
	wire, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := runLocalReference(t, dir, wire)
	if err != nil {
		t.Fatal(err)
	}
	var results []uint64
	if err = json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(inputs) {
		t.Fatalf("native local count %d, want %d", len(results), len(inputs))
	}
	for i, input := range inputs {
		native, generated, want := results[i], runLocal(t, input), expectedLocal(input)
		if native != want || generated != want {
			t.Fatalf("local %d %v: native %d, generated %d, oracle %d", i, input, native, generated, want)
		}
	}
	t.Run("wire rejects malformed and oversized input", func(t *testing.T) {
		for _, invalid := range []string{
			"[[[0]]]", "[[0,4294967296,0,0]]", "[[2,0,0,0]]", "[[0,0,0]]", "[[0,0,0,0,0]]",
			"[[00000000000,0,0,0]]", "[[00,0,0,0]]", "[", "]", "{}", "[[-1,0,0,0]]", "[[0,1.5,0,0]]",
			"[[0,1e2,0,0]]", "[[true,0,0,0]]", "[[0,0,0,0],]", "[[0,0,0,0]] trailing", "[] []", "",
			"[" + strings.Repeat("[0,0,0,0],", 8192) + "[0,0,0,0]]", string([]byte{0xff}), strings.Repeat(" ", 4194305),
		} {
			if data, err := runLocalReference(t, dir, []byte(invalid)); err == nil {
				t.Fatalf("native codec accepted invalid wire %.80q: %s", invalid, data)
			}
		}
	})
	t.Run("wire accepts exact limits", func(t *testing.T) {
		batch := "[" + strings.Repeat("[1,4294967295,0,0],", 8191) + "[1,4294967295,0,0]]"
		wire := batch + strings.Repeat(" ", 4194304-len(batch))
		data, err := runLocalReference(t, dir, []byte(wire))
		if err != nil {
			t.Fatalf("native codec rejected exact row/byte limits: %v", err)
		}
		var values []uint64
		if err := json.Unmarshal(data, &values); err != nil || len(values) != 8192 {
			t.Fatalf("native exact-limit rows: %d, %v", len(values), err)
		}
		for _, value := range values {
			if value != 4294967280 {
				t.Fatalf("native exact-limit value: %d", value)
			}
		}
	})
}

func generateLocal(t *testing.T, out string) []byte {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Local", "--root", "Local.run", "--out", out, "--ir-output", out+".json")
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("local generation: %v\n%s", err, text)
	}
	data, err := os.ReadFile(out + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireLocalIR(t *testing.T, data []byte) {
	t.Helper()
	var program struct {
		Schema, Profile, Root string
		Functions             []reducedFunction
		Provenance            []reducedProvenance
	}
	if err := json.Unmarshal(data, &program); err != nil {
		t.Fatal(err)
	}
	if program.Schema != "lean-go-ir/v5" || program.Profile != "pure-module/v3" || program.Root != "Local.run" {
		t.Fatalf("unexpected local schema/profile/root: %s/%s/%s", program.Schema, program.Profile, program.Root)
	}
	functions := make(map[string]reducedFunction)
	for _, function := range program.Functions {
		functions[function.Name] = function
	}
	requireLocalFunctions(t, functions)
	requireLocalProvenance(t, program.Provenance)
}

func requireLocalFunctions(t *testing.T, functions map[string]reducedFunction) {
	t.Helper()
	root := functions["Local.run"]
	params := make([]string, len(root.Params))
	for i, param := range root.Params {
		params[i] = param.Type
	}
	if !reflect.DeepEqual(params, []string{"Local.Config", "Local.Count", "Local.Count"}) || root.Result != "Local.Count" {
		t.Fatalf("source root signature changed: %v -> %s", params, root.Result)
	}
	const name = "Local.run._lam_0"
	helper, ok := functions[name]
	if !ok || len(helper.Params) != 2 {
		t.Fatalf("missing actual lifted helper with capture and explicit argument: %+v", helper)
	}
	if reducedCalls(root.Body, name) != 2 || reducedCalls(helper.Body, name) != 0 || reducedCalls(helper.Body, "Local.add") != 8 {
		t.Fatal("fixture lost its two direct local calls or eight nonrecursive arithmetic calls")
	}
}

func requireLocalProvenance(t *testing.T, records []reducedProvenance) {
	t.Helper()
	ownerFound, derivedFound := false, false
	for _, record := range records {
		switch record.Name {
		case "Local.run":
			ownerFound = len(record.Dependencies) > 0 && len(record.Rewrites) == 0
			for _, dependency := range []string{"Local.Config.bias", "Local.Config.enabled", "Local.Nested.count", "Local.Count.value", "Local.add"} {
				if !slices.Contains(record.Dependencies, dependency) {
					t.Fatalf("local source owner lost original dependency %s", dependency)
				}
			}
			for _, dependency := range record.Dependencies {
				if strings.Contains(dependency, "._lam_") {
					t.Fatalf("compiler helper entered original source dependencies: %s", dependency)
				}
			}
		case "Local.run._lam_0":
			derivedFound = reflect.DeepEqual(record.Dependencies, []string{"Local.run"}) && len(record.Rewrites) == 0
		}
	}
	if !ownerFound || !derivedFound {
		t.Fatal("missing unchanged source owner or exact local-helper derivation")
	}
}

func TestLocalCompilerEvidence(t *testing.T) {
	data, err := os.ReadFile("../testdata/local.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	requireLocalIR(t, data)
}

func TestLocalRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh local producer builds")
	}
	dir := t.TempDir()
	tracked, err := os.ReadFile("../testdata/local.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var prior []byte
	for i := range 2 {
		out := filepath.Join(dir, fmt.Sprintf("local%d", i))
		data := generateLocal(t, out)
		requireLocalIR(t, data)
		sameFiles(t, directoryFiles(t, out), directoryFiles(t, "../generated/local"))
		if !bytes.Equal(data, tracked) {
			t.Fatal("tracked local IR is stale")
		}
		if i > 0 && !bytes.Equal(data, prior) {
			t.Fatal("fresh local IR differs")
		}
		prior = data
	}
}
