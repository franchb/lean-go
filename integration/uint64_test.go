package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	wide "github.com/franchb/lean-go/generated/wide"
	"github.com/franchb/lean-go/internal/moduleir"
)

type widePair [2]uint64
type wideResults [13]uint64

func widePairs() []widePair {
	boundary := []uint64{0, 1, 2, 3, 31, 32, 33, 63, 64, 65,
		1<<32 - 1, 1 << 32, 1<<32 + 1, 1<<53 - 1, 1 << 53, 1<<53 + 1,
		1<<63 - 1, 1 << 63, 1<<63 + 1, 1<<64 - 2, 1<<64 - 1}
	pairs := make([]widePair, 0, len(boundary)*len(boundary)+512)
	for _, a := range boundary {
		for _, b := range boundary {
			pairs = append(pairs, widePair{a, b})
		}
	}
	rng := rand.New(rand.NewSource(640053))
	for range 512 {
		pairs = append(pairs, widePair{rng.Uint64(), rng.Uint64()})
	}
	return pairs
}

// Arbitrary-precision arithmetic followed by reduction avoids repeating the
// emitter's native uint64 overflow and masked shift expressions.
func expectedWide(pair widePair) wideResults {
	a, b := new(big.Int).SetUint64(pair[0]), new(big.Int).SetUint64(pair[1])
	one := big.NewInt(1)
	modulus := new(big.Int).Lsh(new(big.Int).Set(one), 64)
	wrap := func(n *big.Int) uint64 { return new(big.Int).Mod(n, modulus).Uint64() }
	bit := func(p bool) uint64 {
		if p {
			return 1
		}
		return 0
	}
	shift := new(big.Int).Mod(b, big.NewInt(64)).Uint64()
	power := new(big.Int).Exp(big.NewInt(2), new(big.Int).SetUint64(shift), modulus)
	return wideResults{
		wrap(new(big.Int).Add(a, b)), wrap(new(big.Int).Sub(a, b)), wrap(new(big.Int).Mul(a, b)),
		wrap(new(big.Int).Mul(a, power)), bit(a.Cmp(b) == 0), bit(a.Cmp(b) < 0), bit(a.Cmp(b) <= 0),
		wrap(new(big.Int).Mul(a, big.NewInt(2))), 0, 18446744073709551615, 1, 9007199254740993,
		wrap(new(big.Int).Mul(new(big.Int).Add(a, one), new(big.Int).Sub(b, one))),
	}
}

func wideArguments(t *testing.T, pair widePair) []wide.Value {
	t.Helper()
	args := make([]wide.Value, len(pair))
	for i, value := range pair {
		args[i] = requireTyped[wide.Value](t)(wide.Import(&wide.Input{Type: "UInt64", UInt64: value}, wide.DefaultLimits()))
	}
	return args
}

func readWide(t *testing.T, out wide.Value) wideResults {
	t.Helper()
	var result wideResults
	fields := out.Fields()
	if out.Type() != "Wide.Results" || out.Constructor() != "Wide.Results.mk" || len(fields) != len(result) {
		t.Fatalf("unexpected wide result shape: %s %s (%d fields)", out.Type(), out.Constructor(), len(fields))
	}
	for i, field := range fields {
		if len(field.Fields()) != 0 {
			t.Fatalf("field %d is not a leaf", i)
		}
		if _, ok := field.UInt32(); ok {
			t.Fatalf("field %d exposed a UInt32 payload", i)
		}
		if i >= 4 && i <= 6 {
			if _, ok := field.UInt64(); ok || field.Type() != "Bool" {
				t.Fatalf("field %d is not Bool", i)
			}
			switch field.Constructor() {
			case "Bool.false":
			case "Bool.true":
				result[i] = 1
			default:
				t.Fatalf("field %d has invalid Bool constructor", i)
			}
		} else {
			value, ok := field.UInt64()
			if !ok || field.Type() != "UInt64" || field.Constructor() != "" {
				t.Fatalf("field %d is not UInt64", i)
			}
			result[i] = value
		}
	}
	return result
}

func wideGetters(result wide.Results) []func() (uint64, bool) {
	return []func() (uint64, bool){result.Sum, result.Difference, result.Product, result.Shifted,
		result.Twice, result.Zero, result.Maximum, result.Wrapped, result.Above53, result.Mixed}
}

func readTypedWide(t *testing.T, result wide.Results) wideResults {
	t.Helper()
	var out wideResults
	for i, get := range wideGetters(result) {
		value, ok := get()
		if !ok {
			t.Fatalf("wide numeric getter %d rejected a valid record", i)
		}
		index := i
		if i >= 4 {
			index += 3
		}
		out[index] = value
	}
	for i, get := range []func() (bool, bool){result.Equal, result.Less, result.LessEq} {
		value, ok := get()
		if !ok {
			t.Fatalf("wide Bool getter %d rejected a valid record", i)
		}
		if value {
			out[i+4] = 1
		}
	}
	return out
}

func runWide(t *testing.T, pair widePair) wideResults {
	t.Helper()
	out := requireTyped[wide.Value](t)(wide.Run(wideArguments(t, pair), wide.DefaultLimits()))
	return readWide(t, out)
}

func TestUInt64Semantics(t *testing.T) {
	pairs := widePairs()
	if len(pairs) != 953 {
		t.Fatalf("wide sample count changed: %d", len(pairs))
	}
	for i, pair := range pairs {
		typed := requireTyped[wide.Results](t)(wide.RunTyped(pair[0], pair[1], wide.DefaultLimits()))
		want, raw, got := expectedWide(pair), runWide(t, pair), readTypedWide(t, typed)
		if raw != want || got != want {
			t.Fatalf("pair %d %v: raw %v, typed %v, oracle %v", i, pair, raw, got, want)
		}
	}
}

func newWideResults(values wideResults, limits wide.Limits) (wide.Results, error) {
	return wide.NewResults(values[0], values[1], values[2], values[3], values[4] == 1,
		values[5] == 1, values[6] == 1, values[7], values[8], values[9], values[10], values[11], values[12], limits)
}

func wideResultInput(values wideResults) *wide.Input {
	fields := make([]*wide.Input, len(values))
	for i, value := range values {
		fields[i] = &wide.Input{Type: "UInt64", UInt64: value}
		if i >= 4 && i <= 6 {
			name := "Bool.false"
			if value == 1 {
				name = "Bool.true"
			}
			fields[i] = &wide.Input{Type: "Bool", Constructor: name}
		}
	}
	return &wide.Input{Type: "Wide.Results", Constructor: "Wide.Results.mk", Fields: fields}
}

func invalidWide(t *testing.T, out wide.Value, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || out != (wide.Value{}) {
		t.Fatalf("invalid execution returned %v, %v; want zero value, %v", out, err, want)
	}
}

func TestUInt64OwnershipAndHandles(t *testing.T) {
	want := expectedWide(widePair{18446744073709551615, 65})
	input := wideResultInput(want)
	owned := requireTyped[wide.Value](t)(wide.Import(input, wide.DefaultLimits()))
	input.Fields[0].UInt64 = 0
	input.Fields[4].Constructor = "Bool.true"
	input.Fields = nil
	fields := owned.Fields()
	fields[0] = wide.Value{}
	if got := readWide(t, owned); got != want {
		t.Fatalf("mutable input or returned fields changed owned record: %v", got)
	}
	typed := requireTyped[wide.Results](t)(newWideResults(want, wide.DefaultLimits()))
	copy := typed
	typed = wide.Results{}
	if got := readTypedWide(t, copy); got != want {
		t.Fatalf("replacing a wrapper changed its copy: %v", got)
	}
	for i, get := range wideGetters(typed) {
		if n, ok := get(); ok || n != 0 {
			t.Fatalf("zero record numeric getter %d exposed a value", i)
		}
	}
	for _, get := range []func() (bool, bool){typed.Equal, typed.Less, typed.LessEq} {
		if value, ok := get(); ok || value {
			t.Fatal("zero record exposed a Bool")
		}
	}
	if n, ok := (wide.Value{}).UInt64(); ok || n != 0 {
		t.Fatal("zero raw handle exposed UInt64")
	}
	args := wideArguments(t, widePair{18446744073709551615, 65})
	for range 2 {
		out := requireTyped[wide.Value](t)(wide.Run(args, wide.DefaultLimits()))
		if got := readWide(t, out); got != want {
			t.Fatal("repeated execution changed result")
		}
	}
	if value, ok := args[0].UInt64(); !ok || value != 18446744073709551615 {
		t.Fatal("execution changed its input")
	}
}

func TestUInt64Malformed(t *testing.T) {
	for name, input := range map[string]*wide.Input{
		"nil":                  nil,
		"wide inactive narrow": {Type: "UInt64", UInt64: 1, UInt32: 1},
		"narrow inactive wide": {Type: "UInt32", UInt32: 1, UInt64: 1},
		"wide constructor":     {Type: "UInt64", Constructor: "Wide.Results.mk"},
		"wide fields":          {Type: "UInt64", Fields: []*wide.Input{{Type: "UInt64"}}},
		"Bool inactive wide":   {Type: "Bool", Constructor: "Bool.false", UInt64: 1},
		"Bool inactive narrow": {Type: "Bool", Constructor: "Bool.true", UInt32: 1},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := wide.Import(input, wide.DefaultLimits())
			invalidWide(t, out, err, wide.ErrMalformed)
		})
	}
	for _, change := range []func(*wide.Input){
		func(v *wide.Input) { v.UInt64 = 1 },
		func(v *wide.Input) { v.UInt32 = 1 },
		func(v *wide.Input) { v.Fields[0] = &wide.Input{Type: "UInt32", UInt32: 1} },
		func(v *wide.Input) { v.Fields[4] = &wide.Input{Type: "UInt64", UInt64: 1} },
	} {
		input := wideResultInput(expectedWide(widePair{1, 2}))
		change(input)
		out, err := wide.Import(input, wide.DefaultLimits())
		invalidWide(t, out, err, wide.ErrMalformed)
	}
	args := wideArguments(t, widePair{1, 2})
	narrow := requireTyped[wide.Value](t)(wide.Import(&wide.Input{Type: "UInt32", UInt32: 1}, wide.DefaultLimits()))
	if value, ok := narrow.UInt64(); ok || value != 0 {
		t.Fatal("UInt32 exposed a UInt64 payload")
	}
	for _, wrong := range [][]wide.Value{nil, args[:1], {args[0], args[1], args[0]}, {{}, args[1]}, {args[0], {}}, {narrow, args[1]}, {args[0], narrow}} {
		out, err := wide.Run(wrong, wide.DefaultLimits())
		invalidWide(t, out, err, wide.ErrMalformed)
	}
}

func TestUInt64Budgets(t *testing.T) {
	pair := widePair{18446744073709551615, 65}
	args, want := wideArguments(t, pair), expectedWide(pair)
	for _, tc := range []struct {
		name string
		get  func(wide.Limits) uint64
		set  func(*wide.Limits, uint64)
	}{
		{"input nodes", func(l wide.Limits) uint64 { return l.MaxNodes }, func(l *wide.Limits, n uint64) { l.MaxNodes = n }},
		{"allocations", func(l wide.Limits) uint64 { return l.MaxAllocations }, func(l *wide.Limits, n uint64) { l.MaxAllocations = n }},
		{"steps", func(l wide.Limits) uint64 { return l.MaxSteps }, func(l *wide.Limits, n uint64) { l.MaxSteps = n }},
		{"frames", func(l wide.Limits) uint64 { return l.MaxFrames }, func(l *wide.Limits, n uint64) { l.MaxFrames = n }},
		{"slots", func(l wide.Limits) uint64 { return l.MaxSlots }, func(l *wide.Limits, n uint64) { l.MaxSlots = n }},
		{"depth", func(l wide.Limits) uint64 { return l.MaxDepth }, func(l *wide.Limits, n uint64) { l.MaxDepth = n }},
		{"output nodes", func(l wide.Limits) uint64 { return l.MaxOutputNodes }, func(l *wide.Limits, n uint64) { l.MaxOutputNodes = n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			low, high := uint64(1), tc.get(wide.DefaultLimits())
			for low < high {
				mid := low + (high-low)/2
				limits := wide.DefaultLimits()
				tc.set(&limits, mid)
				out, err := wide.Run(args, limits)
				if err == nil {
					high = mid
				} else {
					invalidWide(t, out, err, wide.ErrResource)
					low = mid + 1
				}
			}
			if low <= 1 {
				t.Fatal("fixture did not exercise budget")
			}
			limits := wide.DefaultLimits()
			tc.set(&limits, low)
			out := requireTyped[wide.Value](t)(wide.Run(args, limits))
			typed := requireTyped[wide.Results](t)(wide.RunTyped(pair[0], pair[1], limits))
			if readWide(t, out) != want || readTypedWide(t, typed) != want {
				t.Fatalf("exact %s budget %d changed result", tc.name, low)
			}
			t.Logf("exact %s budget: %d", tc.name, low)
			tc.set(&limits, low-1)
			out, err := wide.Run(args, limits)
			invalidWide(t, out, err, wide.ErrResource)
			typed, err = wide.RunTyped(pair[0], pair[1], limits)
			if !errors.Is(err, wide.ErrResource) || typed != (wide.Results{}) {
				t.Fatalf("typed lower budget returned %v, %v", typed, err)
			}
			for _, invalid := range []uint64{0, tc.get(wide.DefaultLimits()) + 1} {
				tc.set(&limits, invalid)
				out, err = wide.Run(args, limits)
				invalidWide(t, out, err, wide.ErrLimits)
				typed, err = newWideResults(want, limits)
				if !errors.Is(err, wide.ErrLimits) || typed != (wide.Results{}) {
					t.Fatalf("typed constructor invalid limit returned %v, %v", typed, err)
				}
			}
		})
	}
	for _, limits := range []wide.Limits{
		{MaxNodes: 14, MaxDepth: 2}, {MaxNodes: 13, MaxDepth: 2}, {MaxNodes: 14, MaxDepth: 1},
	} {
		l := wide.DefaultLimits()
		l.MaxNodes, l.MaxDepth = limits.MaxNodes, limits.MaxDepth
		raw, rawErr := wide.Import(wideResultInput(want), l)
		typed, typedErr := newWideResults(want, l)
		if l.MaxNodes == 14 && l.MaxDepth == 2 {
			if rawErr != nil || typedErr != nil || readWide(t, raw) != want || readTypedWide(t, typed) != want {
				t.Fatal("exact record constructor budget failed")
			}
		} else {
			invalidWide(t, raw, rawErr, wide.ErrResource)
			if !errors.Is(typedErr, wide.ErrResource) || typed != (wide.Results{}) {
				t.Fatal("record constructor exceeded node or depth limit")
			}
		}
	}
}

func TestUInt64GoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module wideconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";w "wideconsumer/extracted")
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
const maximum uint64=18446744073709551615
input:=&w.Input{Type:"UInt64",UInt64:maximum}
a,err:=w.Import(input,w.DefaultLimits());if err!=nil{panic(err)};input.UInt64=0
b,err:=w.Import(&w.Input{Type:"UInt64",UInt64:65},w.DefaultLimits());if err!=nil{panic(err)}
raw,err:=w.Run([]w.Value{a,b},w.DefaultLimits());if err!=nil{panic(err)}
sum,ok:=raw.Fields()[0].UInt64();if !ok||sum!=64{panic("raw sum")}
typed,err:=w.RunTyped(maximum,65,w.DefaultLimits());if err!=nil{panic(err)}
for _,tc:=range []struct{get func()(uint64,bool);want uint64}{
{typed.Sum,64},{typed.Difference,18446744073709551550},{typed.Product,18446744073709551551},
{typed.Shifted,18446744073709551614},{typed.Maximum,maximum},{typed.Above53,9007199254740993},{typed.Wrapped,1},
}{got,ok:=tc.get();if !ok||got!=tc.want{panic("typed arithmetic")}}
for _,get:=range []func()(bool,bool){typed.Equal,typed.Less,typed.LessEq}{value,ok:=get();if !ok||value{panic("comparison")}}
copy,err:=w.NewResults(maximum,0,1,2,true,false,true,3,0,maximum,1,9007199254740993,4,w.DefaultLimits());if err!=nil{panic(err)}
v,ok:=copy.Sum();if !ok||v!=maximum{panic("constructor")}
fmt.Println("wide Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/wide") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "wide Go-only consumer: OK\n" {
		t.Fatalf("consumer: %v\n%s", err, out)
	}
}

func buildWideReference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"lakefile.toml": "name = \"wide_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Wide\"\n[[lean_exe]]\nname = \"wide-reference\"\nroot = \"Reference\"\n"}
	for target, source := range map[string]string{"Wide.lean": "../examples/modules/Wide.lean", "Reference.lean": "../examples/reference/WideReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "wide-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native wide build: %v\n%s", err, out)
	}
	return dir
}

func runWideReference(t *testing.T, dir string, wire []byte) ([]byte, error) {
	t.Helper()
	input, output := filepath.Join(dir, "input.json"), filepath.Join(dir, "output.json")
	if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, wire, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/wide-reference"), input, output)
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		if _, outputErr := os.Stat(output); !errors.Is(outputErr, os.ErrNotExist) {
			t.Fatalf("failed native wide run published output: %v", outputErr)
		}
		return nil, fmt.Errorf("%w: %s", err, text)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return data, nil
}

func TestUInt64NativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native wide comparison")
	}
	dir := buildWideReference(t)
	pairs := widePairs()
	wire, err := json.Marshal(pairs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := runWideReference(t, dir, wire)
	if err != nil {
		t.Fatal(err)
	}
	// Decode exact integers directly: no interface{} or floating-point transit.
	var results [][]uint64
	if err := json.Unmarshal(data, &results); err != nil || len(results) != len(pairs) {
		t.Fatalf("native result count %d, want %d: %v", len(results), len(pairs), err)
	}
	for i, pair := range pairs {
		if len(results[i]) != len(wideResults{}) {
			t.Fatalf("native pair %d has %d fields", i, len(results[i]))
		}
		native, raw, want := wideResults(results[i]), runWide(t, pair), expectedWide(pair)
		typed := requireTyped[wide.Results](t)(wide.RunTyped(pair[0], pair[1], wide.DefaultLimits()))
		if native != want || raw != want || readTypedWide(t, typed) != want {
			t.Fatalf("pair %d %v: native %v, raw %v, oracle %v", i, pair, native, raw, want)
		}
	}
	t.Run("reject malformed and oversized wire", func(t *testing.T) {
		for _, invalid := range []string{
			"[[[0]]]", "[[18446744073709551616,0]]", "[[0,18446744073709551616]]", "[[0]]", "[[0,0,0]]",
			"[[000000000000000000000,0]]", "[[00,0]]", "[", "]", "{}", "[[-1,0]]", "[[0,1.5]]",
			"[[0,1e2]]", "[[true,0]]", "[[0,0],]", "[[0,0]] trailing", "[] []", "",
			"[" + strings.Repeat("[0,0],", 8192) + "[0,0]]", string([]byte{0xff}), strings.Repeat(" ", 4194305),
		} {
			if data, err := runWideReference(t, dir, []byte(invalid)); err == nil {
				t.Fatalf("native codec accepted invalid wire %.80q: %s", invalid, data)
			}
		}
	})
	t.Run("empty batch", func(t *testing.T) {
		data, err := runWideReference(t, dir, []byte("[]"))
		if err != nil || string(data) != "[]\n" {
			t.Fatalf("empty native batch: %s, %v", data, err)
		}
	})
	t.Run("exact row and byte limits", func(t *testing.T) {
		batch := "[" + strings.Repeat("[18446744073709551615,65],", 8191) + "[18446744073709551615,65]]"
		wire := batch + strings.Repeat(" ", 4194304-len(batch))
		data, err := runWideReference(t, dir, []byte(wire))
		if err != nil {
			t.Fatal(err)
		}
		var values [][]uint64
		if err := json.Unmarshal(data, &values); err != nil || len(values) != 8192 {
			t.Fatalf("native exact-limit rows: %d, %v", len(values), err)
		}
		want := expectedWide(widePair{18446744073709551615, 65})
		for i, row := range values {
			if len(row) != len(want) || wideResults(row) != want {
				t.Fatalf("native exact-limit row %d: %v", i, row)
			}
		}
	})
}

func generateWide(t *testing.T, out string) []byte {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Wide", "--root", "Wide.compute", "--out", out, "--ir-output", out+".json")
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wide generation: %v\n%s", err, text)
	}
	data, err := os.ReadFile(out + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestUInt64CompilerEvidence(t *testing.T) {
	data, err := os.ReadFile("../testdata/wide.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var program moduleir.Program
	if err := json.Unmarshal(data, &program); err != nil {
		t.Fatal(err)
	}
	for _, function := range program.Functions {
		if function.Name != "Wide.compute" {
			continue
		}
		literals := map[int]string{}
		for code := function.Body; code != nil; code = code.Next {
			if code.Value == nil || code.Bind == nil {
				continue
			}
			if code.Value.Kind == "uint64" {
				literals[code.Bind.ID] = code.Value.Name
			}
			if code.Value.Kind == "primitive" && code.Value.Name == "UInt64.shiftLeft" &&
				len(code.Value.Args) == 2 && literals[code.Value.Args[1]] == "1" {
				return // Pinned Lean lowers source multiplication by two to shift-left.
			}
		}
	}
	t.Fatal("wide fixture no longer exercises the power-of-two compiler rewrite")
}

func TestUInt64Regeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh wide producer builds")
	}
	dir := t.TempDir()
	tracked, err := os.ReadFile("../testdata/wide.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var prior []byte
	for i := range 2 {
		out := filepath.Join(dir, fmt.Sprintf("wide%d", i))
		data := generateWide(t, out)
		sameFiles(t, directoryFiles(t, out), directoryFiles(t, "../generated/wide"))
		if !bytes.Equal(data, tracked) {
			t.Fatal("tracked wide IR is stale")
		}
		if i > 0 && !bytes.Equal(data, prior) {
			t.Fatal("fresh wide IR differs")
		}
		prior = data
	}
}
