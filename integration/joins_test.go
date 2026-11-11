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
	"strings"
	"testing"

	joins "github.com/franchb/lean-go/generated/joins"
)

// Inputs are count, nested.count, enabled, raw, delta, flag. Native outputs
// preserve the four state fields followed by the separate Total wrapper result.
type joinCase [6]uint32
type joinResults [5]uint64

func joinCases() []joinCase {
	boundary := []uint32{0, 1, 2, 65535, 65536, 1<<31 - 1, 1 << 31, 1<<32 - 2, 1<<32 - 1}
	inputs := make([]joinCase, 0, 4*len(boundary)*len(boundary)*len(boundary)+512)
	for i, count := range boundary {
		for j, nested := range boundary {
			for k, delta := range boundary {
				for enabled := range uint32(2) {
					for flag := range uint32(2) {
						inputs = append(inputs, joinCase{count, nested, enabled, boundary[(i+j+k)%len(boundary)], delta, flag})
					}
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(741544))
	for range 512 {
		inputs = append(inputs, joinCase{rng.Uint32(), rng.Uint32(), uint32(rng.Intn(2)), rng.Uint32(), rng.Uint32(), uint32(rng.Intn(2))})
	}
	return inputs
}

// Widen each operand and reduce explicitly, independently of UInt32 overflow.
func expectedJoins(input joinCase) joinResults {
	const modulus uint64 = 1 << 32
	count, nested, raw, delta := uint64(input[0]), uint64(input[1]), uint64(input[3]), uint64(input[4])
	selected := count
	if input[5] == 1 {
		selected = delta
	}
	updatedNested := nested + selected
	if input[2] == 1 {
		updatedNested = min(selected, raw) + nested + delta + count + raw
	}
	return joinResults{(selected + count) % modulus, updatedNested % modulus, 1 - uint64(input[2]),
		(raw + selected) % modulus, (selected + raw + nested + count + delta) % modulus}
}

func joinScalar(value uint32) *joins.Input {
	return &joins.Input{Type: "UInt32", UInt32: value}
}

func joinNode(name string, fields ...*joins.Input) *joins.Input {
	return &joins.Input{Type: "Joins." + name, Constructor: "Joins." + name + ".mk", Fields: fields}
}

func joinBool(bit uint32) *joins.Input {
	name := "Bool.false"
	if bit == 1 {
		name = "Bool.true"
	}
	return &joins.Input{Type: "Bool", Constructor: name}
}

func joinInputs(input joinCase) []*joins.Input {
	return []*joins.Input{
		joinNode("State", joinNode("Count", joinScalar(input[0])),
			joinNode("Nested", joinNode("Count", joinScalar(input[1]))),
			joinNode("Enabled", joinBool(input[2])), joinScalar(input[3])),
		joinNode("Count", joinScalar(input[4])), joinBool(input[5]),
	}
}

func importJoins(t *testing.T, inputs []*joins.Input) []joins.Value {
	t.Helper()
	args := make([]joins.Value, len(inputs))
	for i, input := range inputs {
		var err error
		args[i], err = joins.Import(input, joins.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	return args
}

func joinFields(t *testing.T, value joins.Value, name string, count int) []joins.Value {
	t.Helper()
	fields := value.Fields()
	if value.Type() != "Joins."+name || value.Constructor() != "Joins."+name+".mk" || len(fields) != count {
		t.Fatalf("expected Joins.%s with %d fields, got %s %s (%d fields)", name, count, value.Type(), value.Constructor(), len(fields))
	}
	if _, scalar := value.UInt32(); scalar {
		t.Fatalf("source wrapper %s exposed as scalar", name)
	}
	return fields
}

func readJoins(t *testing.T, out joins.Value) [4]uint64 {
	t.Helper()
	fields := joinFields(t, out, "State", 4)
	count := joinFields(t, fields[0], "Count", 1)[0]
	nested := joinFields(t, joinFields(t, fields[1], "Nested", 1)[0], "Count", 1)[0]
	enabled := joinFields(t, fields[2], "Enabled", 1)[0]
	var result [4]uint64
	for i, value := range map[int]joins.Value{0: count, 1: nested, 3: fields[3]} {
		scalar, ok := value.UInt32()
		if !ok || value.Type() != "UInt32" || value.Constructor() != "" || len(value.Fields()) != 0 {
			t.Fatalf("state field %d is not UInt32", i)
		}
		result[i] = uint64(scalar)
	}
	if enabled.Type() != "Bool" || len(enabled.Fields()) != 0 {
		t.Fatal("enabled wrapper did not retain its Bool field")
	}
	switch enabled.Constructor() {
	case "Bool.false":
	case "Bool.true":
		result[2] = 1
	default:
		t.Fatalf("unknown Bool constructor %q", enabled.Constructor())
	}
	return result
}

func expectedJoinState(input joinCase) [4]uint64 {
	result := expectedJoins(input)
	return [4]uint64(result[:4])
}

func runJoins(t *testing.T, input joinCase) [4]uint64 {
	t.Helper()
	out, err := joins.Run(importJoins(t, joinInputs(input)), joins.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return readJoins(t, out)
}

func TestJoinsSemantics(t *testing.T) {
	for i, input := range joinCases() {
		if got, want := runJoins(t, input), expectedJoinState(input); got != want {
			t.Fatalf("join %d %v: generated %v, oracle %v", i, input, got, want)
		}
	}
}

func TestJoinsOwnership(t *testing.T) {
	input := joinCase{4294967295, 3, 1, 7, 2, 1}
	source := joinInputs(input)
	args := importJoins(t, source)
	source[0].Fields[0].Fields[0].UInt32 = 100
	source[0].Fields[1].Fields[0].Fields[0].UInt32 = 200
	source[0].Fields[2].Fields[0].Constructor = "Bool.false"
	source[0].Fields[3] = joinScalar(300)
	source[1].Fields[0].UInt32 = 400
	source[2].Constructor = "Bool.false"
	for range 3 {
		out, err := joins.Run(args, joins.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		fields := out.Fields()
		fields[0].Fields()[0] = joins.Value{}
		fields[1].Fields()[0].Fields()[0] = joins.Value{}
		fields[2] = joins.Value{}
		if got, want := readJoins(t, out), expectedJoinState(input); got != want {
			t.Fatalf("mutable source/accessor changed owned result: %v, want %v", got, want)
		}
	}
	out, err := joins.Run(args, joins.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	prior := expectedJoinState(input)
	next := joinCase{uint32(prior[0]), uint32(prior[1]), uint32(prior[2]), uint32(prior[3]), input[4], input[5]}
	out, err = joins.Run([]joins.Value{out, args[1], args[2]}, joins.DefaultLimits())
	if err != nil || readJoins(t, out) != expectedJoinState(next) {
		t.Fatalf("reused joined record: %v", err)
	}
}

func invalidJoinValue(t *testing.T, out joins.Value, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || out.Type() != "" || out.Constructor() != "" || len(out.Fields()) != 0 {
		t.Fatalf("expected invalid result and %v, got %s %s and %v", want, out.Type(), out.Constructor(), err)
	}
	if _, ok := out.UInt32(); ok {
		t.Fatal("invalid result exposed scalar")
	}
}

func TestJoinsMalformed(t *testing.T) {
	for _, input := range []*joins.Input{
		nil, joinNode("Count"), joinNode("Count", joinScalar(1), joinScalar(2)),
		joinNode("Count", joinBool(0)), joinNode("Nested", joinScalar(1)), joinNode("Enabled", joinScalar(1)),
		{Type: "Joins.Count", Constructor: "Joins.Nested.mk", Fields: []*joins.Input{joinScalar(1)}},
		{Type: "Bool", Constructor: "Bool.unknown"},
		{Type: "Bool", Constructor: "Bool.true", Fields: []*joins.Input{joinScalar(1)}},
	} {
		out, err := joins.Import(input, joins.DefaultLimits())
		invalidJoinValue(t, out, err, joins.ErrMalformed)
	}
	args := importJoins(t, joinInputs(joinCase{}))
	scalar := importJoins(t, []*joins.Input{joinScalar(1)})[0]
	for _, wrong := range [][]joins.Value{nil, {args[0], args[1]}, {args[1], args[0], args[2]},
		{scalar, args[1], args[2]}, {args[0], scalar, args[2]}, {args[0], args[1], scalar}, {args[0], args[1], {}}} {
		out, err := joins.Run(wrong, joins.DefaultLimits())
		invalidJoinValue(t, out, err, joins.ErrMalformed)
	}
}

func TestJoinsBudgets(t *testing.T) {
	for _, input := range []joinCase{{4294967295, 2, 1, 3, 4, 1}, {4, 2, 1, 9, 3, 0}, {4, 2, 0, 9, 3, 1}} {
		args := importJoins(t, joinInputs(input))
		for _, tc := range []struct {
			name string
			get  func(joins.Limits) uint64
			set  func(*joins.Limits, uint64)
		}{
			{"allocations", func(l joins.Limits) uint64 { return l.MaxAllocations }, func(l *joins.Limits, n uint64) { l.MaxAllocations = n }},
			{"steps", func(l joins.Limits) uint64 { return l.MaxSteps }, func(l *joins.Limits, n uint64) { l.MaxSteps = n }},
			{"slots", func(l joins.Limits) uint64 { return l.MaxSlots }, func(l *joins.Limits, n uint64) { l.MaxSlots = n }},
			{"depth", func(l joins.Limits) uint64 { return l.MaxDepth }, func(l *joins.Limits, n uint64) { l.MaxDepth = n }},
			{"output nodes", func(l joins.Limits) uint64 { return l.MaxOutputNodes }, func(l *joins.Limits, n uint64) { l.MaxOutputNodes = n }},
		} {
			t.Run(fmt.Sprintf("%s/%v", tc.name, input), func(t *testing.T) {
				low, high := uint64(1), tc.get(joins.DefaultLimits())
				for low < high {
					mid := low + (high-low)/2
					limits := joins.DefaultLimits()
					tc.set(&limits, mid)
					out, err := joins.Run(args, limits)
					if err == nil {
						high = mid
					} else {
						invalidJoinValue(t, out, err, joins.ErrResource)
						low = mid + 1
					}
				}
				if low <= 1 {
					t.Fatal("fixture did not exercise budget")
				}
				limits := joins.DefaultLimits()
				tc.set(&limits, low)
				out, err := joins.Run(args, limits)
				if err != nil || readJoins(t, out) != expectedJoinState(input) {
					t.Fatalf("exact budget %d failed: %v", low, err)
				}
				tc.set(&limits, low-1)
				out, err = joins.Run(args, limits)
				invalidJoinValue(t, out, err, joins.ErrResource)
			})
		}
	}
}

func TestJoinsGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module joinsconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";j "joinsconsumer/extracted")
func scalar(n uint32)*j.Input{return &j.Input{Type:"UInt32",UInt32:n}}
func node(name string,fields ...*j.Input)*j.Input{return &j.Input{Type:"Joins."+name,Constructor:"Joins."+name+".mk",Fields:fields}}
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
source:=node("State",node("Count",scalar(4294967295)),node("Nested",node("Count",scalar(3))),node("Enabled",&j.Input{Type:"Bool",Constructor:"Bool.true"}),scalar(7))
state,err:=j.Import(source,j.DefaultLimits());if err!=nil{panic(err)}
delta,err:=j.Import(node("Count",scalar(2)),j.DefaultLimits());if err!=nil{panic(err)}
flag,err:=j.Import(&j.Input{Type:"Bool",Constructor:"Bool.true"},j.DefaultLimits());if err!=nil{panic(err)}
source.Fields[1].Fields[0].Fields[0].UInt32=99
out,err:=j.Run([]j.Value{state,delta,flag},j.DefaultLimits());if err!=nil{panic(err)}
if out.Type()!="Joins.State"||out.Constructor()!="Joins.State.mk"{panic("state shape")}
fs:=out.Fields();if len(fs)!=4||fs[0].Type()!="Joins.Count"||fs[1].Type()!="Joins.Nested"||fs[1].Fields()[0].Type()!="Joins.Count"||fs[2].Type()!="Joins.Enabled"{panic("wrapper shape")}
count,_:=fs[0].Fields()[0].UInt32();nested,_:=fs[1].Fields()[0].Fields()[0].UInt32();raw,_:=fs[3].UInt32()
if count!=1||nested!=13||raw!=9||fs[2].Fields()[0].Constructor()!="Bool.false"{panic("result")}
fs[0]=j.Value{};if out.Fields()[0].Type()!="Joins.Count"{panic("field ownership")}
fmt.Println("joins Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/joins") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "joins Go-only consumer: OK\n" {
		t.Fatalf("join consumer: %v\n%s", err, out)
	}
}

func buildJoinsReference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"lakefile.toml": "name = \"joins_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Joins\"\n[[lean_exe]]\nname = \"joins-reference\"\nroot = \"Reference\"\n"}
	for target, source := range map[string]string{"Joins.lean": "../examples/modules/Joins.lean", "Reference.lean": "../examples/reference/JoinsReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "joins-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native join build: %v\n%s", err, out)
	}
	return dir
}

func nativeJoins(t *testing.T, dir string, inputs []joinCase) []joinResults {
	t.Helper()
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/joins-reference"), "input.json", "output.json")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native joins run: %v\n%s", err, out)
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
		t.Fatalf("native join count %d, want %d", len(rows), len(inputs))
	}
	results := make([]joinResults, len(rows))
	for i, row := range rows {
		if len(row) != len(joinResults{}) {
			t.Fatalf("native join %d has %d fields", i, len(row))
		}
		results[i] = joinResults(row)
	}
	return results
}

func TestJoinsNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native join comparison")
	}
	dir := buildJoinsReference(t)
	inputs := joinCases()
	results := nativeJoins(t, dir, inputs)
	for i, input := range inputs {
		native, generated, want := results[i], runJoins(t, input), expectedJoins(input)
		if native != want || generated != expectedJoinState(input) {
			t.Fatalf("join %d %v: native %v, generated %v, oracle %v", i, input, native, generated, want)
		}
	}
	for _, wire := range []string{"[[[0]]]", "[[4294967296,0,0,0,0,0]]", "[[0,0,2,0,0,0]]", "[[0,0,0,0,0,2]]",
		"[[0,0,0,0,0]]", "[[0,0,0,0,0,0,0]]", "[[00000000000]]", "[", "]", "{}", "[[-1,0,0,0,0,0]]",
		"[" + strings.Repeat("[],", 8192) + "[]]", string([]byte{0xff}), strings.Repeat(" ", 4194305)} {
		if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(wire), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/joins-reference"), "invalid.json", "invalid-output.json")
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

func generateJoins(t *testing.T, out, rootFunction string) []byte {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Joins", "--root", rootFunction, "--out", out, "--ir-output", out+".json")
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("join generation: %v\n%s", err, text)
	}
	data, err := os.ReadFile(out + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Count lexical join nesting and jumps in fresh producer output, so an optimizer
// that duplicates the continuations cannot silently make these branch-only tests.
func requireJoinControl(t *testing.T, data []byte, minimum, nesting int) {
	t.Helper()
	var program map[string]any
	if err := json.Unmarshal(data, &program); err != nil {
		t.Fatal(err)
	}
	if program["schema"] != "lean-go-ir/v4" || program["profile"] != "pure-module/v3" {
		t.Fatalf("unexpected join IR schema/profile: %v/%v", program["schema"], program["profile"])
	}
	joinCount, jumpCount, maxDepth := 0, 0, 0
	var visit func(any, int)
	visit = func(value any, depth int) {
		switch value := value.(type) {
		case map[string]any:
			if value["op"] == "join" {
				joinCount++
				depth++
				maxDepth = max(maxDepth, depth)
			}
			if value["op"] == "jump" {
				jumpCount++
			}
			for _, child := range value {
				visit(child, depth)
			}
		case []any:
			for _, child := range value {
				visit(child, depth)
			}
		}
	}
	visit(program, 0)
	if joinCount < minimum || jumpCount <= joinCount || maxDepth < nesting {
		t.Fatalf("fixture lost shared continuations: %d joins, %d jumps, nesting %d", joinCount, jumpCount, maxDepth)
	}
}

func TestJoinsRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh join producer builds")
	}
	dir := t.TempDir()
	tracked, err := os.ReadFile("../testdata/joins.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var prior []byte
	for i := range 2 {
		out := filepath.Join(dir, fmt.Sprintf("joins%d", i))
		data := generateJoins(t, out, "Joins.update")
		requireJoinControl(t, data, 4, 4)
		sameFiles(t, directoryFiles(t, out), directoryFiles(t, "../generated/joins"))
		if !bytes.Equal(data, tracked) {
			t.Fatal("tracked join IR is stale")
		}
		if i > 0 && !bytes.Equal(data, prior) {
			t.Fatal("fresh join IR differs")
		}
		prior = data
	}
}

func TestJoinsWrapperRoot(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for join wrapper-root extraction")
	}
	out := filepath.Join(t.TempDir(), "total")
	data := generateJoins(t, out, "Joins.total")
	requireJoinControl(t, data, 1, 1)
	inputs := joinCases()
	inputData, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"go.mod": "module joinswrapper\n\ngo 1.27.4\n", "inputs.json": string(inputData),
		"wrapper_test.go": `package extracted
import("encoding/json";"errors";"os";"testing")
func scalar(n uint32)*Input{return &Input{Type:"UInt32",UInt32:n}}
func wrapperInput(name string,fields ...*Input)*Input{return &Input{Type:"Joins."+name,Constructor:"Joins."+name+".mk",Fields:fields}}
func boolean(bit uint32)*Input{name:="Bool.false";if bit==1{name="Bool.true"};return &Input{Type:"Bool",Constructor:name}}
func TestTotal(t *testing.T){
encoded,err:=os.ReadFile("inputs.json");if err!=nil{t.Fatal(err)}
var inputs [][6]uint32;if err=json.Unmarshal(encoded,&inputs);err!=nil{t.Fatal(err)}
var native [][5]uint64
if encoded,err=os.ReadFile("native.json");err==nil{if err=json.Unmarshal(encoded,&native);err!=nil{t.Fatal(err)};if len(native)!=len(inputs){t.Fatal("native count")}}else if !errors.Is(err,os.ErrNotExist){t.Fatal(err)}
for i,row:=range inputs{
source:=wrapperInput("State",wrapperInput("Count",scalar(row[0])),wrapperInput("Nested",wrapperInput("Count",scalar(row[1]))),wrapperInput("Enabled",boolean(row[2])),scalar(row[3]))
state,err:=Import(source,DefaultLimits());if err!=nil{t.Fatal(err)}
delta,err:=Import(wrapperInput("Count",scalar(row[4])),DefaultLimits());if err!=nil{t.Fatal(err)}
flag,err:=Import(boolean(row[5]),DefaultLimits());if err!=nil{t.Fatal(err)}
source.Fields[0].Fields[0].UInt32++
out,err:=Run([]Value{state,delta,flag},DefaultLimits());if err!=nil{t.Fatal(err)}
if out.Type()!="Joins.Total"||out.Constructor()!="Joins.Total.mk"||len(out.Fields())!=1{t.Fatal("nominal wrapper result",out)}
if _,ok:=out.UInt32();ok{t.Fatal("wrapper erased at API")}
selected:=uint64(row[0]);if row[5]==1{selected=uint64(row[4])}
want:=(selected+uint64(row[3])+uint64(row[1])+uint64(row[0])+uint64(row[4]))%(1<<32)
got,ok:=out.Fields()[0].UInt32();if !ok||uint64(got)!=want{t.Fatal(row,got,want)}
if native!=nil&&uint64(got)!=native[i][4]{t.Fatal("native wrapper result",row,got,native[i][4])}
out.Fields()[0]=Value{};got,ok=out.Fields()[0].UInt32();if !ok||uint64(got)!=want{t.Fatal("result field ownership")}
wrong,err:=Import(wrapperInput("Total",scalar(row[4])),DefaultLimits());if err!=nil{t.Fatal(err)}
bad,err:=Run([]Value{state,wrong,flag},DefaultLimits());if !errors.Is(err,ErrMalformed)||bad.Type()!=""{t.Fatal("same representation bypassed nominal type",err)}
}}
`}
	if os.Getenv("LEAN_GO_NATIVE") == "1" {
		data, err = json.Marshal(nativeJoins(t, buildJoinsReference(t), inputs))
		if err != nil {
			t.Fatal(err)
		}
		files["native.json"] = string(data)
	}
	writeNumbersFiles(t, out, files)
	cmd := exec.Command(goBinary(), "test", "-count=1", ".")
	cmd.Dir = out
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("join wrapper-root consumer: %v\n%s", err, text)
	}
}
