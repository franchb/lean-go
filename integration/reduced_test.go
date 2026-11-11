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

	reduced "github.com/franchb/lean-go/generated/reduced"
)

// Wire rows are flag, unused Count, left Count, right Count, bias Count, depth.
type reducedCase [6]uint32
type reducedResults [3]uint64

func reducedCases() []reducedCase {
	boundary := []uint32{0, 1, 2, 65535, 65536, 1<<31 - 1, 1 << 31, 1<<32 - 2, 1<<32 - 1}
	inputs := make([]reducedCase, 0, 2*len(boundary)*len(boundary)*len(boundary)+512)
	for i, left := range boundary {
		for j, right := range boundary {
			for k, bias := range boundary {
				for flag := range uint32(2) {
					inputs = append(inputs, reducedCase{flag, boundary[(i+j+k)%len(boundary)], left, right, bias, uint32((i + j + k) % 9)})
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(863911))
	for range 512 {
		inputs = append(inputs, reducedCase{uint32(rng.Intn(2)), rng.Uint32(), rng.Uint32(), rng.Uint32(), rng.Uint32(), uint32(rng.Intn(65))})
	}
	return inputs
}

// Widen before arithmetic and reduce explicitly, independently of UInt32 overflow.
func expectedReduced(input reducedCase) reducedResults {
	const modulus uint64 = 1 << 32
	selected := input[3]
	if input[0] == 1 {
		selected = input[2]
	}
	return reducedResults{(uint64(selected) + 4*uint64(input[4])) % modulus, 30, ((uint64(input[5]) + 1) * uint64(input[2])) % modulus}
}

func reducedScalar(value uint32) *reduced.Input {
	return &reduced.Input{Type: "UInt32", UInt32: value}
}

func reducedCount(value uint32) *reduced.Input {
	return &reduced.Input{Type: "Reduced.Count", Constructor: "Reduced.Count.mk", Fields: []*reduced.Input{reducedScalar(value)}}
}

func reducedBool(bit uint32) *reduced.Input {
	name := "Bool.false"
	if bit == 1 {
		name = "Bool.true"
	}
	return &reduced.Input{Type: "Bool", Constructor: name}
}

func reducedInputs(input reducedCase) []*reduced.Input {
	return []*reduced.Input{reducedBool(input[0]), reducedCount(input[1]), reducedCount(input[2]), reducedCount(input[3]), reducedCount(input[4])}
}

func importReduced(t *testing.T, inputs []*reduced.Input) []reduced.Value {
	t.Helper()
	args := make([]reduced.Value, len(inputs))
	for i, input := range inputs {
		var err error
		args[i], err = reduced.Import(input, reduced.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	return args
}

func readReduced(t *testing.T, value reduced.Value) uint64 {
	t.Helper()
	fields := value.Fields()
	if value.Type() != "Reduced.Count" || value.Constructor() != "Reduced.Count.mk" || len(fields) != 1 {
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

func runReduced(t *testing.T, input reducedCase) uint64 {
	t.Helper()
	out, err := reduced.Run(importReduced(t, reducedInputs(input)), reduced.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return readReduced(t, out)
}

func TestReducedSemantics(t *testing.T) {
	for i, input := range reducedCases() {
		if got, want := runReduced(t, input), expectedReduced(input)[0]; got != want {
			t.Fatalf("reduced %d %v: generated %d, oracle %d", i, input, got, want)
		}
	}
}

func TestReducedOwnership(t *testing.T) {
	input := reducedCase{1, 7, 4294967295, 9, 2, 3}
	source := reducedInputs(input)
	args := importReduced(t, source)
	source[0].Constructor = "Bool.false"
	for _, count := range source[1:] {
		count.Fields[0].UInt32 = 100
		count.Fields[0] = reducedScalar(200)
	}
	for range 3 {
		out, err := reduced.Run(args, reduced.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		out.Fields()[0] = reduced.Value{}
		if got, want := readReduced(t, out), expectedReduced(input)[0]; got != want {
			t.Fatalf("mutable input/accessor changed result: %d, want %d", got, want)
		}
	}
	out, err := reduced.Run(args, reduced.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	args[2] = out
	input[2] = uint32(expectedReduced(input)[0])
	out, err = reduced.Run(args, reduced.DefaultLimits())
	if err != nil || readReduced(t, out) != expectedReduced(input)[0] {
		t.Fatalf("reused source wrapper: %v", err)
	}
}

func invalidReducedValue(t *testing.T, out reduced.Value, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || out.Type() != "" || out.Constructor() != "" || len(out.Fields()) != 0 {
		t.Fatalf("expected invalid result and %v, got %s %s and %v", want, out.Type(), out.Constructor(), err)
	}
	if _, ok := out.UInt32(); ok {
		t.Fatal("invalid result exposed scalar")
	}
}

func TestReducedFullSourceAPI(t *testing.T) {
	for _, malformed := range []*reduced.Input{
		nil, {Type: "Reduced.Count", Constructor: "Reduced.Count.mk"},
		{Type: "Reduced.Count", Constructor: "Reduced.Count.mk", Fields: []*reduced.Input{reducedBool(0)}},
		{Type: "Reduced.Count", Constructor: "Reduced.Count.mk", Fields: []*reduced.Input{reducedScalar(1), reducedScalar(2)}},
		{Type: "Reduced.Count", Constructor: "Bool.true"},
	} {
		out, err := reduced.Import(malformed, reduced.DefaultLimits())
		invalidReducedValue(t, out, err, reduced.ErrMalformed)
	}
	args := importReduced(t, reducedInputs(reducedCase{}))
	for _, wrong := range [][]reduced.Value{nil, args[:4], append(append([]reduced.Value{}, args...), args[1])} {
		out, err := reduced.Run(wrong, reduced.DefaultLimits())
		invalidReducedValue(t, out, err, reduced.ErrMalformed)
	}
	scalar := importReduced(t, []*reduced.Input{reducedScalar(1)})[0]
	// Every original position is checked, including the discarded Count argument.
	for i := range args {
		for _, bad := range []reduced.Value{{}, scalar} {
			wrong := append([]reduced.Value{}, args...)
			wrong[i] = bad
			out, err := reduced.Run(wrong, reduced.DefaultLimits())
			invalidReducedValue(t, out, err, reduced.ErrMalformed)
		}
	}
	wrong := append([]reduced.Value{}, args...)
	wrong[1] = args[0]
	out, err := reduced.Run(wrong, reduced.DefaultLimits())
	invalidReducedValue(t, out, err, reduced.ErrMalformed)
}

func TestReducedBudgets(t *testing.T) {
	for _, input := range []reducedCase{{0, 7, 4294967295, 3, 2, 0}, {1, 7, 4294967295, 3, 2, 0}} {
		args := importReduced(t, reducedInputs(input))
		for _, tc := range []struct {
			name string
			get  func(reduced.Limits) uint64
			set  func(*reduced.Limits, uint64)
		}{
			{"allocations", func(l reduced.Limits) uint64 { return l.MaxAllocations }, func(l *reduced.Limits, n uint64) { l.MaxAllocations = n }},
			{"steps", func(l reduced.Limits) uint64 { return l.MaxSteps }, func(l *reduced.Limits, n uint64) { l.MaxSteps = n }},
			{"slots", func(l reduced.Limits) uint64 { return l.MaxSlots }, func(l *reduced.Limits, n uint64) { l.MaxSlots = n }},
			{"depth", func(l reduced.Limits) uint64 { return l.MaxDepth }, func(l *reduced.Limits, n uint64) { l.MaxDepth = n }},
			{"output nodes", func(l reduced.Limits) uint64 { return l.MaxOutputNodes }, func(l *reduced.Limits, n uint64) { l.MaxOutputNodes = n }},
		} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, input[0]), func(t *testing.T) {
				low, high := uint64(1), tc.get(reduced.DefaultLimits())
				for low < high {
					mid := low + (high-low)/2
					limits := reduced.DefaultLimits()
					tc.set(&limits, mid)
					out, err := reduced.Run(args, limits)
					if err == nil {
						high = mid
					} else {
						invalidReducedValue(t, out, err, reduced.ErrResource)
						low = mid + 1
					}
				}
				if low <= 1 {
					t.Fatal("fixture did not exercise budget")
				}
				limits := reduced.DefaultLimits()
				tc.set(&limits, low)
				out, err := reduced.Run(args, limits)
				if err != nil || readReduced(t, out) != expectedReduced(input)[0] {
					t.Fatalf("exact budget %d failed: %v", low, err)
				}
				tc.set(&limits, low-1)
				out, err = reduced.Run(args, limits)
				invalidReducedValue(t, out, err, reduced.ErrResource)
			})
		}
	}
}

func TestReducedGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module reducedconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";r "reducedconsumer/extracted")
func count(n uint32)*r.Input{return &r.Input{Type:"Reduced.Count",Constructor:"Reduced.Count.mk",Fields:[]*r.Input{{Type:"UInt32",UInt32:n}}}}
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
source:=[]*r.Input{{Type:"Bool",Constructor:"Bool.true"},count(7),count(4294967295),count(9),count(2)}
args:=make([]r.Value,len(source));for i,p:=range source{var err error;args[i],err=r.Import(p,r.DefaultLimits());if err!=nil{panic(err)}}
source[2].Fields[0].UInt32=100
out,err:=r.Run(args,r.DefaultLimits());if err!=nil{panic(err)}
if out.Type()!="Reduced.Count"||out.Constructor()!="Reduced.Count.mk"||len(out.Fields())!=1{panic("wrapper shape")}
n,ok:=out.Fields()[0].UInt32();if !ok||n!=7{panic("result")}
out.Fields()[0]=r.Value{};n,ok=out.Fields()[0].UInt32();if !ok||n!=7{panic("field ownership")}
fmt.Println("reduced Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/reduced") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "reduced Go-only consumer: OK\n" {
		t.Fatalf("reduced consumer: %v\n%s", err, out)
	}
}

func buildReducedReference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"lakefile.toml": "name = \"reduced_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Reduced\"\n[[lean_exe]]\nname = \"reduced-reference\"\nroot = \"Reference\"\n"}
	for target, source := range map[string]string{"Reduced.lean": "../examples/modules/Reduced.lean", "Reference.lean": "../examples/reference/ReducedReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "reduced-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native reduced build: %v\n%s", err, out)
	}
	return dir
}

func nativeReduced(t *testing.T, dir string, inputs []reducedCase) []reducedResults {
	t.Helper()
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/reduced-reference"), "input.json", "output.json")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native reduced run: %v\n%s", err, out)
	}
	data, err = os.ReadFile(filepath.Join(dir, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]uint64
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(inputs) {
		t.Fatalf("native reduced count %d, want %d", len(rows), len(inputs))
	}
	results := make([]reducedResults, len(rows))
	for i, row := range rows {
		if len(row) != len(reducedResults{}) {
			t.Fatalf("native reduced %d has %d fields", i, len(row))
		}
		results[i] = reducedResults(row)
	}
	return results
}

func TestReducedNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native reduced comparison")
	}
	dir := buildReducedReference(t)
	inputs := reducedCases()
	results := nativeReduced(t, dir, inputs)
	for i, input := range inputs {
		native, generated, want := results[i], runReduced(t, input), expectedReduced(input)
		if native != want || generated != want[0] {
			t.Fatalf("reduced %d %v: native %v, generated %d, oracle %v", i, input, native, generated, want)
		}
	}
	for _, wire := range []string{"[[[0]]]", "[[0,4294967296,0,0,0,0]]", "[[2,0,0,0,0,0]]", "[[0,0,0,0,0,65]]",
		"[[0,0,0,0,0]]", "[[0,0,0,0,0,0,0]]", "[[00000000000]]", "[", "]", "{}", "[[-1,0,0,0,0,0]]",
		"[" + strings.Repeat("[],", 8192) + "[]]", string([]byte{0xff}), strings.Repeat(" ", 4194305)} {
		if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(wire), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/reduced-reference"), "invalid.json", "invalid-output.json")
		cmd.Dir = dir
		cmd.Env = commandEnv(nil)
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("native codec accepted invalid wire: %s", out)
		}
		if _, err := os.Stat(filepath.Join(dir, "invalid-output.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("native codec produced output for invalid input: %v", err)
		}
	}
}

func generateReduced(t *testing.T, out, rootFunction string) []byte {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Reduced", "--root", rootFunction, "--out", out, "--ir-output", out+".json")
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reduced generation: %v\n%s", err, text)
	}
	data, err := os.ReadFile(out + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func reducedCalls(value any, target string) int {
	count := 0
	switch value := value.(type) {
	case map[string]any:
		if value["kind"] == "call" && value["name"] == target {
			count++
		}
		for _, child := range value {
			count += reducedCalls(child, target)
		}
	case []any:
		for _, child := range value {
			count += reducedCalls(child, target)
		}
	}
	return count
}

type reducedFunction struct {
	Name, Result string
	Params       []struct{ Type string }
	Body         any
}

type reducedProvenance struct {
	Name         string
	Dependencies []string
	Rewrites     []any
}

func requireReducedSignature(t *testing.T, function reducedFunction, want []string) {
	t.Helper()
	got := make([]string, len(function.Params))
	for i, param := range function.Params {
		got[i] = param.Type
	}
	if !reflect.DeepEqual(got, want) || function.Result != "Reduced.Count" {
		t.Fatalf("%s signature: %v -> %s, want %v -> Reduced.Count", function.Name, got, function.Result, want)
	}
}

func requireReducedOwner(t *testing.T, records []reducedProvenance, owner string) {
	t.Helper()
	ownerFound, derivedFound := false, false
	for _, record := range records {
		switch record.Name {
		case owner:
			ownerFound = len(record.Dependencies) > 0 && len(record.Rewrites) == 0
			for _, dependency := range record.Dependencies {
				if strings.HasSuffix(dependency, "._redArg") {
					t.Fatalf("generated helper entered original source dependencies: %s", dependency)
				}
			}
		case owner + "._redArg":
			derivedFound = reflect.DeepEqual(record.Dependencies, []string{owner}) && len(record.Rewrites) == 0
		}
	}
	if !ownerFound || !derivedFound {
		t.Fatalf("missing unchanged source owner or exact compiler derivation for %s", owner)
	}
}

// Assert actual emitted functions and call edges, not just a generated-name string.
func requireReducedIR(t *testing.T, data []byte, root string, rootParams, retained []string) {
	t.Helper()
	var program struct {
		Schema, Profile, Root string
		Functions             []reducedFunction
		Provenance            []reducedProvenance
	}
	if err := json.Unmarshal(data, &program); err != nil {
		t.Fatal(err)
	}
	if program.Schema != "lean-go-ir/v4" || program.Profile != "pure-module/v3" || program.Root != root {
		t.Fatalf("unexpected reduced schema/profile/root: %s/%s/%s", program.Schema, program.Profile, program.Root)
	}
	functions := make(map[string]reducedFunction)
	for _, function := range program.Functions {
		functions[function.Name] = function
	}
	for name, want := range map[string][]string{root: rootParams, root + "._redArg": retained, "Reduced.add._redArg": {"Reduced.Count", "Reduced.Count"}} {
		function, ok := functions[name]
		if !ok {
			t.Fatalf("missing actual runtime function %s", name)
		}
		requireReducedSignature(t, function, want)
	}
	if reducedCalls(functions[root].Body, root+"._redArg") != 1 {
		t.Fatal("source root lost its exact helper call")
	}
	helper := functions[root+"._redArg"]
	minimumAddCalls := 4
	if root == "Reduced.recursive" {
		minimumAddCalls = 1
		if reducedCalls(helper.Body, root+"._redArg") != 1 {
			t.Fatal("recursive helper lost its actual self-call")
		}
	}
	if reducedCalls(helper.Body, "Reduced.add._redArg") < minimumAddCalls {
		t.Fatal("fixture lost direct calls to the reduced arithmetic helper")
	}
	for _, owner := range []string{root, "Reduced.add"} {
		requireReducedOwner(t, program.Provenance, owner)
	}
}

func TestReducedRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh reduced producer builds")
	}
	dir := t.TempDir()
	tracked, err := os.ReadFile("../testdata/reduced.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var prior []byte
	for i := range 2 {
		out := filepath.Join(dir, fmt.Sprintf("reduced%d", i))
		data := generateReduced(t, out, "Reduced.run")
		requireReducedIR(t, data, "Reduced.run", []string{"Bool", "Reduced.Count", "Reduced.Count", "Reduced.Count", "Reduced.Count"}, []string{"Bool", "Reduced.Count", "Reduced.Count", "Reduced.Count"})
		sameFiles(t, directoryFiles(t, out), directoryFiles(t, "../generated/reduced"))
		if !bytes.Equal(data, tracked) {
			t.Fatal("tracked reduced IR is stale")
		}
		if i > 0 && !bytes.Equal(data, prior) {
			t.Fatal("fresh reduced IR differs")
		}
		prior = data
	}
}

func TestReducedSpecialRoots(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for all-unused and recursive extraction")
	}
	inputs := reducedCases()
	inputData, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	var nativeData []byte
	if os.Getenv("LEAN_GO_NATIVE") == "1" {
		nativeData, err = json.Marshal(nativeReduced(t, buildReducedReference(t), inputs))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		root             string
		params, retained []string
		column           int
	}{
		{"Reduced.allUnused", []string{"Reduced.Count", "Bool"}, []string{}, 1},
		{"Reduced.recursive", []string{"Reduced.Chain", "Reduced.Count", "Reduced.Count"}, []string{"Reduced.Chain", "Reduced.Count"}, 2},
	} {
		t.Run(tc.root, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "extracted")
			data := generateReduced(t, out, tc.root)
			requireReducedIR(t, data, tc.root, tc.params, tc.retained)
			files := map[string]string{"go.mod": "module reducedspecial\n\ngo 1.27.4\n", "inputs.json": string(inputData),
				"special_test.go": fmt.Sprintf(`package extracted
import("encoding/json";"errors";"os";"os/exec";"testing")
func count(n uint32)*Input{return &Input{Type:"Reduced.Count",Constructor:"Reduced.Count.mk",Fields:[]*Input{{Type:"UInt32",UInt32:n}}}}
func boolean(n uint32)*Input{name:="Bool.false";if n==1{name="Bool.true"};return &Input{Type:"Bool",Constructor:name}}
func chain(n uint32)*Input{out:=&Input{Type:"Reduced.Chain",Constructor:"Reduced.Chain.nil"};for ;n>0;n--{out=&Input{Type:"Reduced.Chain",Constructor:"Reduced.Chain.cons",Fields:[]*Input{out}}};return out}
func TestSpecial(t *testing.T){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{t.Fatal("producer available")}}
encoded,err:=os.ReadFile("inputs.json");if err!=nil{t.Fatal(err)}
var inputs [][6]uint32;if err=json.Unmarshal(encoded,&inputs);err!=nil{t.Fatal(err)}
var native [][3]uint64
if encoded,err=os.ReadFile("native.json");err==nil{if err=json.Unmarshal(encoded,&native);err!=nil{t.Fatal(err)};if len(native)!=len(inputs){t.Fatal("native count")}}else if !errors.Is(err,os.ErrNotExist){t.Fatal(err)}
for i,row:=range inputs{
source:=[]*Input{count(row[1]),boolean(row[0])};want:=uint64(30);unused:=0
if %d==2{source=[]*Input{chain(row[5]),count(row[1]),count(row[2])};want=((uint64(row[5])+1)*uint64(row[2]))%%(1<<32);unused=1}
args:=make([]Value,len(source));for j,p:=range source{args[j],err=Import(p,DefaultLimits());if err!=nil{t.Fatal(err)}}
source[unused].Fields[0].UInt32++
out,err:=Run(args,DefaultLimits());if err!=nil{t.Fatal(err)}
if out.Type()!="Reduced.Count"||out.Constructor()!="Reduced.Count.mk"||len(out.Fields())!=1{t.Fatal("nominal result")}
if _,ok:=out.UInt32();ok{t.Fatal("wrapper erased at API")}
got,ok:=out.Fields()[0].UInt32();if !ok||uint64(got)!=want{t.Fatal(row,got,want)}
if native!=nil&&uint64(got)!=native[i][%d]{t.Fatal("native result",row,got,native[i])}
out.Fields()[0]=Value{};got,ok=out.Fields()[0].UInt32();if !ok||uint64(got)!=want{t.Fatal("result ownership")}
wrong:=append([]Value{},args...);wrong[unused]=Value{}
bad,err:=Run(wrong,DefaultLimits());if !errors.Is(err,ErrMalformed)||bad.Type()!=""{t.Fatal("unused argument unchecked",err)}
wrong[unused],err=Import(&Input{Type:"UInt32",UInt32:row[1]},DefaultLimits());if err!=nil{t.Fatal(err)}
bad,err=Run(wrong,DefaultLimits());if !errors.Is(err,ErrMalformed)||bad.Type()!=""{t.Fatal("unused nominal type unchecked",err)}
bad,err=Run(args[:len(args)-1],DefaultLimits());if !errors.Is(err,ErrMalformed)||bad.Type()!=""{t.Fatal("root signature reduced",err)}
limits:=DefaultLimits();limits.MaxDepth=1
bad,err=Run(args,limits);if !errors.Is(err,ErrResource)||bad.Type()!=""{t.Fatal("helper frame escaped depth budget",err)}
}
}
`, tc.column, tc.column)}
			if nativeData != nil {
				files["native.json"] = string(nativeData)
			}
			writeNumbersFiles(t, out, files)
			cmd := exec.Command(goBinary(), "test", "-count=1", ".")
			cmd.Dir = out
			cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
			if text, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("special reduced consumer: %v\n%s", err, text)
			}
		})
	}
}
