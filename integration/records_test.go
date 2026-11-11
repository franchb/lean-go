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

	records "github.com/franchb/lean-go/generated/records"
)

// Wire rows preserve the source field order: count, nested.count, enabled,
// raw, delta. Result rows replace delta with the separate wrapper-root total.
type recordCase [5]uint32
type recordResults [5]uint64

func recordCases() []recordCase {
	boundary := []uint32{0, 1, 2, 65535, 65536, 1<<31 - 1, 1 << 31, 1<<32 - 2, 1<<32 - 1}
	inputs := make([]recordCase, 0, 2*len(boundary)*len(boundary)*len(boundary)+512)
	for i, count := range boundary {
		for j, nested := range boundary {
			for k, delta := range boundary {
				for bit := range uint32(2) {
					inputs = append(inputs, recordCase{count, nested, bit, boundary[(i+j+k)%len(boundary)], delta})
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(741533))
	for range 512 {
		inputs = append(inputs, recordCase{rng.Uint32(), rng.Uint32(), uint32(rng.Intn(2)), rng.Uint32(), rng.Uint32()})
	}
	return inputs
}

// Widen before adding and reduce explicitly, independently of UInt32 overflow.
func expectedRecords(input recordCase) recordResults {
	const modulus uint64 = 1 << 32
	count := (uint64(input[0]) + uint64(input[4])) % modulus
	nested := (uint64(input[1]) + uint64(input[0])) % modulus
	raw := (uint64(input[3]) + uint64(input[4])) % modulus
	return recordResults{count, nested, 1 - uint64(input[2]), raw, (count + nested + raw) % modulus}
}

func recordScalar(value uint32) *records.Input {
	return &records.Input{Type: "UInt32", UInt32: value}
}

func recordNode(name string, fields ...*records.Input) *records.Input {
	return &records.Input{Type: "Records." + name, Constructor: "Records." + name + ".mk", Fields: fields}
}

func recordInputs(input recordCase) []*records.Input {
	boolName := "Bool.false"
	if input[2] == 1 {
		boolName = "Bool.true"
	}
	return []*records.Input{
		recordNode("State", recordNode("Count", recordScalar(input[0])),
			recordNode("Nested", recordNode("Count", recordScalar(input[1]))),
			recordNode("Enabled", &records.Input{Type: "Bool", Constructor: boolName}), recordScalar(input[3])),
		recordNode("Count", recordScalar(input[4])),
	}
}

func importRecords(t *testing.T, inputs []*records.Input) []records.Value {
	t.Helper()
	args := make([]records.Value, len(inputs))
	for i, input := range inputs {
		var err error
		args[i], err = records.Import(input, records.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	return args
}

func recordFields(t *testing.T, value records.Value, name string, count int) []records.Value {
	t.Helper()
	fields := value.Fields()
	if value.Type() != "Records."+name || value.Constructor() != "Records."+name+".mk" || len(fields) != count {
		t.Fatalf("expected Records.%s with %d fields, got %s %s (%d fields)", name, count, value.Type(), value.Constructor(), len(fields))
	}
	if _, scalar := value.UInt32(); scalar {
		t.Fatalf("source wrapper %s exposed as scalar", name)
	}
	return fields
}

func readRecords(t *testing.T, out records.Value) recordResults {
	t.Helper()
	fields := recordFields(t, out, "State", 4)
	count := recordFields(t, fields[0], "Count", 1)[0]
	nested := recordFields(t, recordFields(t, fields[1], "Nested", 1)[0], "Count", 1)[0]
	enabled := recordFields(t, fields[2], "Enabled", 1)[0]
	var result recordResults
	for i, value := range map[int]records.Value{0: count, 1: nested, 3: fields[3]} {
		scalar, ok := value.UInt32()
		if !ok || value.Type() != "UInt32" || value.Constructor() != "" || len(value.Fields()) != 0 {
			t.Fatalf("record field %d is not UInt32", i)
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
	result[4] = (result[0] + result[1] + result[3]) % (uint64(1) << 32)
	return result
}

func runRecords(t *testing.T, input recordCase) recordResults {
	t.Helper()
	out, err := records.Run(importRecords(t, recordInputs(input)), records.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return readRecords(t, out)
}

func TestRecordsSemantics(t *testing.T) {
	for i, input := range recordCases() {
		if got, want := runRecords(t, input), expectedRecords(input); got != want {
			t.Fatalf("record %d %v: generated %v, oracle %v", i, input, got, want)
		}
	}
}

func TestRecordsOwnership(t *testing.T) {
	input := recordCase{4294967295, 3, 1, 7, 2}
	source := recordInputs(input)
	args := importRecords(t, source)
	source[0].Fields[0].Fields[0].UInt32 = 100
	source[0].Fields[1].Fields[0].Fields[0].UInt32 = 200
	source[0].Fields[2].Fields[0].Constructor = "Bool.false"
	source[0].Fields[3] = recordScalar(300)
	source[1].Fields[0].UInt32 = 400
	for range 3 {
		out, err := records.Run(args, records.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		fields := out.Fields()
		fields[0].Fields()[0] = records.Value{}
		fields[1].Fields()[0].Fields()[0] = records.Value{}
		fields[2] = records.Value{}
		if got, want := readRecords(t, out), expectedRecords(input); got != want {
			t.Fatalf("mutable source/accessor changed owned result: %v, want %v", got, want)
		}
	}
	// A returned source-shaped record can be reused as a new root argument.
	out, err := records.Run(args, records.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	prior := expectedRecords(input)
	next := recordCase{uint32(prior[0]), uint32(prior[1]), uint32(prior[2]), uint32(prior[3]), input[4]}
	out, err = records.Run([]records.Value{out, args[1]}, records.DefaultLimits())
	if err != nil || readRecords(t, out) != expectedRecords(next) {
		t.Fatalf("reused record: %v", err)
	}
}

func invalidRecordValue(t *testing.T, out records.Value, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || out.Type() != "" || out.Constructor() != "" || len(out.Fields()) != 0 {
		t.Fatalf("expected invalid result and %v, got %s %s and %v", want, out.Type(), out.Constructor(), err)
	}
	if _, ok := out.UInt32(); ok {
		t.Fatal("invalid result exposed scalar")
	}
}

func TestRecordsMalformed(t *testing.T) {
	for _, input := range []*records.Input{
		recordNode("Count"), recordNode("Count", recordScalar(1), recordScalar(2)),
		recordNode("Count", &records.Input{Type: "Bool", Constructor: "Bool.true"}),
		recordNode("Nested", recordScalar(1)), recordNode("Enabled", recordScalar(1)),
		{Type: "Records.Count", Constructor: "Records.Nested.mk", Fields: []*records.Input{recordScalar(1)}},
	} {
		out, err := records.Import(input, records.DefaultLimits())
		invalidRecordValue(t, out, err, records.ErrMalformed)
	}
	args := importRecords(t, recordInputs(recordCase{}))
	scalar := importRecords(t, []*records.Input{recordScalar(1)})[0]
	for _, wrong := range [][]records.Value{nil, {args[0]}, {args[1], args[0]}, {scalar, args[1]}, {args[0], scalar}, {args[0], {}}} {
		out, err := records.Run(wrong, records.DefaultLimits())
		invalidRecordValue(t, out, err, records.ErrMalformed)
	}
}

func TestRecordsBudgets(t *testing.T) {
	args := importRecords(t, recordInputs(recordCase{4294967295, 2, 1, 3, 4}))
	for _, tc := range []struct {
		name string
		get  func(records.Limits) uint64
		set  func(*records.Limits, uint64)
	}{
		{"allocations", func(l records.Limits) uint64 { return l.MaxAllocations }, func(l *records.Limits, n uint64) { l.MaxAllocations = n }},
		{"steps", func(l records.Limits) uint64 { return l.MaxSteps }, func(l *records.Limits, n uint64) { l.MaxSteps = n }},
		{"slots", func(l records.Limits) uint64 { return l.MaxSlots }, func(l *records.Limits, n uint64) { l.MaxSlots = n }},
		{"depth", func(l records.Limits) uint64 { return l.MaxDepth }, func(l *records.Limits, n uint64) { l.MaxDepth = n }},
		{"output nodes", func(l records.Limits) uint64 { return l.MaxOutputNodes }, func(l *records.Limits, n uint64) { l.MaxOutputNodes = n }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Locate the exact accepted boundary without coupling to compiler binder IDs.
			low, high := uint64(1), tc.get(records.DefaultLimits())
			for low < high {
				mid := low + (high-low)/2
				limits := records.DefaultLimits()
				tc.set(&limits, mid)
				out, err := records.Run(args, limits)
				if err == nil {
					high = mid
				} else {
					invalidRecordValue(t, out, err, records.ErrResource)
					low = mid + 1
				}
			}
			if low <= 1 {
				t.Fatal("fixture did not exercise budget")
			}
			limits := records.DefaultLimits()
			tc.set(&limits, low)
			out, err := records.Run(args, limits)
			if err != nil || readRecords(t, out) != expectedRecords(recordCase{4294967295, 2, 1, 3, 4}) {
				t.Fatalf("exact budget %d failed: %v", low, err)
			}
			tc.set(&limits, low-1)
			out, err = records.Run(args, limits)
			invalidRecordValue(t, out, err, records.ErrResource)
		})
	}
}

func TestRecordsGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module recordsconsumer\n\ngo 1.27.4\n",
		"main.go": `package main
import("fmt";"os/exec";r "recordsconsumer/extracted")
func scalar(n uint32)*r.Input{return &r.Input{Type:"UInt32",UInt32:n}}
func node(name string,fields ...*r.Input)*r.Input{return &r.Input{Type:"Records."+name,Constructor:"Records."+name+".mk",Fields:fields}}
func main(){
for _,name:=range []string{"lean","lake","python","python3","uv","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("producer available")}}
source:=node("State",node("Count",scalar(4294967295)),node("Nested",node("Count",scalar(3))),node("Enabled",&r.Input{Type:"Bool",Constructor:"Bool.true"}),scalar(7))
state,err:=r.Import(source,r.DefaultLimits());if err!=nil{panic(err)}
delta,err:=r.Import(node("Count",scalar(2)),r.DefaultLimits());if err!=nil{panic(err)}
source.Fields[1].Fields[0].Fields[0].UInt32=99
out,err:=r.Run([]r.Value{state,delta},r.DefaultLimits());if err!=nil{panic(err)}
if out.Type()!="Records.State"||out.Constructor()!="Records.State.mk"{panic("state shape")}
fs:=out.Fields();if len(fs)!=4||fs[0].Type()!="Records.Count"||fs[1].Type()!="Records.Nested"||fs[1].Fields()[0].Type()!="Records.Count"||fs[2].Type()!="Records.Enabled"{panic("wrapper shape")}
count,_:=fs[0].Fields()[0].UInt32();nested,_:=fs[1].Fields()[0].Fields()[0].UInt32();raw,_:=fs[3].UInt32()
if count!=1||nested!=2||raw!=9||fs[2].Fields()[0].Constructor()!="Bool.false"{panic("result")}
fs[0]=r.Value{};if out.Fields()[0].Type()!="Records.Count"{panic("field ownership")}
fmt.Println("records Go-only consumer: OK")
}`,
	}
	for name, data := range directoryFiles(t, "../generated/records") {
		if strings.HasSuffix(name, ".go") {
			files[filepath.Join("extracted", name)] = data
		}
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	if out, err := cmd.CombinedOutput(); err != nil || string(out) != "records Go-only consumer: OK\n" {
		t.Fatalf("record consumer: %v\n%s", err, out)
	}
}

func buildRecordsReference(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"lakefile.toml": "name = \"records_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"Records\"\n[[lean_exe]]\nname = \"records-reference\"\nroot = \"Reference\"\n"}
	for target, source := range map[string]string{"Records.lean": "../examples/modules/Records.lean", "Reference.lean": "../examples/reference/RecordsReference.lean", "lean-toolchain": "../producer/lean-toolchain"} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		files[target] = string(data)
	}
	writeNumbersFiles(t, dir, files)
	cmd := exec.Command("lake", "build", "records-reference")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native record build: %v\n%s", err, out)
	}
	return dir
}

func TestRecordsNativeReference(t *testing.T) {
	if os.Getenv("LEAN_GO_NATIVE") != "1" {
		t.Skip("set LEAN_GO_NATIVE=1 for native record comparison")
	}
	dir := buildRecordsReference(t)
	inputs := recordCases()
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/records-reference"), "input.json", "output.json")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native records run: %v\n%s", err, out)
	}
	data, err = os.ReadFile(filepath.Join(dir, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	var results [][]uint64
	if err = json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(inputs) {
		t.Fatalf("native record count %d, want %d", len(results), len(inputs))
	}
	for i, input := range inputs {
		if len(results[i]) != len(recordResults{}) {
			t.Fatalf("native record %d has %d fields", i, len(results[i]))
		}
		native, generated, want := recordResults(results[i]), runRecords(t, input), expectedRecords(input)
		if native != want || generated != want {
			t.Fatalf("record %d %v: native %v, generated %v, oracle %v", i, input, native, generated, want)
		}
	}
	for _, wire := range []string{"[[[0]]]", "[[4294967296,0,0,0,0]]", "[[0,0,2,0,0]]", "[[0,0,0,0]]", "[[00000000000]]", "[" + strings.Repeat("[],", 8192) + "[]]"} {
		if err := os.WriteFile(filepath.Join(dir, "invalid.json"), []byte(wire), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(filepath.Join(dir, ".lake/build/bin/records-reference"), "invalid.json", "invalid-output.json")
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

func generateRecords(t *testing.T, out, rootFunction string) []byte {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("uv", "run", "--locked", "python", "scripts/generate.py", "--source-dir", "examples/modules", "--module", "Records", "--root", rootFunction, "--out", out, "--ir-output", out+".json")
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	if text, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("record generation: %v\n%s", err, text)
	}
	data, err := os.ReadFile(out + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRecordsRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh record producer builds")
	}
	dir := t.TempDir()
	tracked, err := os.ReadFile("../testdata/records.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	var prior []byte
	for i := range 2 {
		out := filepath.Join(dir, fmt.Sprintf("records%d", i))
		data := generateRecords(t, out, "Records.update")
		sameFiles(t, directoryFiles(t, out), directoryFiles(t, "../generated/records"))
		if !bytes.Equal(data, tracked) {
			t.Fatal("tracked record IR is stale")
		}
		if i > 0 && !bytes.Equal(data, prior) {
			t.Fatal("fresh record IR differs")
		}
		prior = data
	}
}

func TestRecordsWrapperRoots(t *testing.T) {
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for wrapper-root extraction")
	}
	for _, tc := range []struct{ name, source string }{
		{"total", `package extracted
import("errors";"testing")
func scalar(n uint32)*Input{return &Input{Type:"UInt32",UInt32:n}}
func recordInput(name string,fields ...*Input)*Input{return &Input{Type:"Records."+name,Constructor:"Records."+name+".mk",Fields:fields}}
func TestTotal(t *testing.T){
for _,row:=range [][5]uint32{{0,0,0,0,0},{4294967295,3,1,7,2},{2147483647,2147483648,0,4294967295,4294967295}}{
bit:="Bool.false";if row[2]==1{bit="Bool.true"}
state,err:=Import(recordInput("State",recordInput("Count",scalar(row[0])),recordInput("Nested",recordInput("Count",scalar(row[1]))),recordInput("Enabled",&Input{Type:"Bool",Constructor:bit}),scalar(row[3])),DefaultLimits());if err!=nil{t.Fatal(err)}
delta,err:=Import(recordInput("Count",scalar(row[4])),DefaultLimits());if err!=nil{t.Fatal(err)}
out,err:=Run([]Value{state,delta},DefaultLimits());if err!=nil{t.Fatal(err)}
if out.Type()!="Records.Total"||out.Constructor()!="Records.Total.mk"||len(out.Fields())!=1{t.Fatal("nominal wrapper result",out)}
if _,ok:=out.UInt32();ok{t.Fatal("wrapper erased at API")}
want:=(uint64(row[0])+uint64(row[4])+uint64(row[1])+uint64(row[0])+uint64(row[3])+uint64(row[4]))%(1<<32)
got,ok:=out.Fields()[0].UInt32();if !ok||uint64(got)!=want{t.Fatal(row,got,want)}
wrong,err:=Import(recordInput("Total",scalar(row[4])),DefaultLimits());if err!=nil{t.Fatal(err)}
bad,err:=Run([]Value{state,wrong},DefaultLimits());if !errors.Is(err,ErrMalformed)||bad.Type()!=""{t.Fatal("same representation bypassed nominal type",err)}
}}
`},
		{"box", `package extracted
import("errors";"testing")
func TestBox(t *testing.T){for _,n:=range []uint32{0,1,2147483648,4294967295}{
input,err:=Import(&Input{Type:"UInt32",UInt32:n},DefaultLimits());if err!=nil{t.Fatal(err)}
limits:=DefaultLimits();limits.MaxAllocations=2;limits.MaxDepth=3;limits.MaxOutputNodes=3
out,err:=Run([]Value{input},limits);if err!=nil{t.Fatal("exact reconstruction budget",err)}
if out.Type()!="Records.Nested"||out.Constructor()!="Records.Nested.mk"||len(out.Fields())!=1{t.Fatal("outer source wrapper")}
count:=out.Fields()[0];if count.Type()!="Records.Count"||count.Constructor()!="Records.Count.mk"||len(count.Fields())!=1{t.Fatal("inner source wrapper")}
got,ok:=count.Fields()[0].UInt32();if !ok||got!=n{t.Fatal("wrapped scalar",got)}
for _,set:=range []func(*Limits){func(l *Limits){l.MaxAllocations=1},func(l *Limits){l.MaxDepth=2},func(l *Limits){l.MaxOutputNodes=2},func(l *Limits){l.MaxSteps=2},func(l *Limits){l.MaxSlots=1}}{
tiny:=limits;set(&tiny);bad,err:=Run([]Value{input},tiny);if !errors.Is(err,ErrResource)||bad.Type()!=""||bad.Constructor()!=""||len(bad.Fields())!=0{t.Fatal("partial reconstruction escaped",tiny,err)}
}
}}
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), tc.name)
			generateRecords(t, out, "Records."+tc.name)
			writeNumbersFiles(t, out, map[string]string{"go.mod": "module recordswrapper\n\ngo 1.27.4\n", "wrapper_test.go": tc.source})
			cmd := exec.Command(goBinary(), "test", "-count=1", ".")
			cmd.Dir = out
			cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
			if text, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("wrapper-root consumer: %v\n%s", err, text)
			}
		})
	}
}
