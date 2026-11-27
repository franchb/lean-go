package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// A fixed campaign compares one tracked corpus source on fixed samples:
// native Lean, the metered Go package's raw and typed APIs and an
// independent arithmetic model, plus the smallest resource budgets that
// succeed. The width and byte campaigns replaced the retired Python scripts
// check_width_conversions.py and check_bytes.py.

// Row is one sample: two narrow operands and one word.
type Row [3]uint64

// Fixed describes one fixed campaign.
type Fixed struct {
	Name      string
	Module    string
	Source    string // checkout-relative source file
	Roots     []string
	Samples   func() []Row
	Model     func(Row) []uint64
	Reference func([]Row) string
	Consumer  string
	Trace     string              // optional pure mono trace module
	Leaves    map[string][]string // expected trace leaves per root
	Values    int                 // results per row
	Width     int                 // results are below 2^Width
}

// Assurance is the scope statement of every fixed-campaign report.
const Assurance = "Bounded differential testing; not a proof of compiler or universal Go correctness."

// FixedReport is report.json of a fixed campaign.
type FixedReport struct {
	Status  string          `json:"status"`
	Cases   int             `json:"cases"`
	Roots   []string        `json:"roots"`
	Budgets json.RawMessage `json:"budgets"`
	// StateMachineBudgets are the same thresholds measured on the package
	// without the fast path; RunFixed requires them to equal Budgets.
	StateMachineBudgets json.RawMessage `json:"state_machine_budgets"`
	Assurance           string          `json:"assurance"`
}

func bigOf(v uint64) *big.Int { return new(big.Int).SetUint64(v) }

// ---- widths ----

const radix = 4294967296

// WidthSamples are the mixed-width samples: boundary products, every wide
// boundary split into halves, then 128 seeded random rows.
func WidthSamples() []Row {
	small := []uint64{0, 1, 2147483647, 2147483648, 4294967294, 4294967295, 2863311530, 1431655765}
	wide := append(append([]uint64(nil), small...), 4294967296, 4294967297, 9223372036854775808, 18446744073709551615,
		12297829382473034410, 6148914691236517205)
	var rows []Row
	for i, a := range small {
		for j, b := range small {
			rows = append(rows, Row{a, b, wide[(i*len(small)+j)%len(wide)]})
		}
	}
	for _, n := range wide {
		rows = append(rows, Row{n % radix, n / radix, n})
	}
	r := rand.New(rand.NewPCG(320647, 320647))
	for i := 0; i < 128; i++ {
		rows = append(rows, Row{r.Uint64N(radix), r.Uint64N(radix), r.Uint64()})
	}
	return rows
}

// WidthModel is arithmetic alone: no target casts, masks or bitwise shifts.
// It returns widen, narrow, pack, high and low.
func WidthModel(row Row) []uint64 {
	a, b, word := bigOf(row[0]), bigOf(row[1]), bigOf(row[2])
	r := bigOf(radix)
	mod := func(x *big.Int) uint64 { return new(big.Int).Mod(x, r).Uint64() }
	pack := new(big.Int).Add(new(big.Int).Mul(a, r), b)
	return []uint64{a.Uint64(), mod(word), pack.Uint64(), new(big.Int).Quo(word, r).Uint64(), mod(word)}
}

func widthReference(rows []Row) string {
	triples := make([]string, len(rows))
	for i, row := range rows {
		triples[i] = fmt.Sprintf("(%d, %d, %d)", row[0], row[1], row[2])
	}
	return `import Widths

def samples : List (UInt32 × UInt32 × UInt64) := [
  ` + strings.Join(triples, ",\n  ") + `]

def main : IO Unit := do
  let rows := samples.map fun (a, b, word) =>
    let values := [(Widths.widen a).toNat, (Widths.narrow word).toNat,
                   (Widths.pack a b).toNat, (Widths.high word).toNat, (Widths.low word).toNat]
    "[" ++ String.intercalate "," (values.map toString) ++ "]"
  IO.println ("[" ++ String.intercalate "," rows ++ "]")
`
}

var widthRoots = []string{"widen", "narrow", "pack", "high", "low"}

func widthTrace() string {
	names := make([]string, len(widthRoots))
	for i, name := range widthRoots {
		names[i] = "``Widths." + name
	}
	return `import Widths
import Lean
import Lean.Compiler.LCNF
open Lean Lean.Compiler Lean.Elab.Command
run_elab do
  for name in [` + strings.Join(names, ", ") + `] do
    let some decl ← LCNF.getMonoDecl? name | throwError "missing mono declaration"
    decl.value.forCodeM (m := CoreM) fun code =>
      code.forM (m := CoreM) fun node => do
        match node with
        | .let binding _ =>
          match binding.value with
          | .const callee _ _ _ => logInfo m!"LEAF {name} -> {callee}"
          | .lit _ => pure ()
          | _ => throwError "unexpected value"
        | .return _ => pure ()
        | _ => throwError "unexpected control"
`
}

const widthConsumer = `package main
import (
 "encoding/json"
 "errors"
 "os"
 x "fixedconsumer/extracted"
)
var roots=[]string{"widen","narrow","pack","high","low"}
func arguments(root string,row [3]uint64)[]x.Value{
 inputs:=[]*x.Input{{Type:"UInt64",UInt64:row[2]}}
 if root=="widen"||root=="pack"{inputs=[]*x.Input{{Type:"UInt32",UInt32:uint32(row[0])}}}
 if root=="pack"{inputs=append(inputs,&x.Input{Type:"UInt32",UInt32:uint32(row[1])})}
 args:=make([]x.Value,len(inputs));for i,in:=range inputs{v,e:=x.Import(in,x.DefaultLimits());if e!=nil{panic(e)};args[i]=v;in.UInt32=0;in.UInt64=0};return args
}
func typed(root string,row [3]uint64,l x.Limits)(uint64,error){
 switch root{
 case "widen":return x.RunWiden(uint32(row[0]),l)
 case "pack":return x.RunPack(uint32(row[0]),uint32(row[1]),l)
 case "narrow":n,e:=x.RunNarrow(row[2],l);return uint64(n),e
 case "high":n,e:=x.RunHigh(row[2],l);return uint64(n),e
 case "low":n,e:=x.RunLow(row[2],l);return uint64(n),e
 };panic("root")
}
func read(root string,v x.Value)uint64{
 if root=="widen"||root=="pack"{n,ok:=v.UInt64();if !ok{panic("wide payload")};if _,ok:=v.UInt32();ok{panic("inactive payload")};return n}
 n,ok:=v.UInt32();if !ok{panic("narrow payload")};if _,ok:=v.UInt64();ok{panic("inactive payload")};return uint64(n)
}
func budgets(root string,row [3]uint64)[2]int{
 args:=arguments(root,row);found:=[2]int{}
 for dimension:=0;dimension<2;dimension++{
  for bound:=1;bound<=16;bound++{
   l:=x.DefaultLimits();if dimension==0{l.MaxAllocations=uint64(bound)}else{l.MaxSteps=uint64(bound)}
   raw,e:=x.RunRoot("Widths."+root,args,l);got,te:=typed(root,row,l)
   if e==nil{if te!=nil||got!=read(root,raw){panic("typed budget divergence")};found[dimension]=bound;break}
   if !errors.Is(e,x.ErrResource)||raw.Type()!=""||!errors.Is(te,x.ErrResource)||got!=0{panic("partial budget output")}
  }
  if found[dimension]==0{panic("budget search exceeded bound")}
 }
 l:=x.DefaultLimits();l.MaxAllocations=uint64(found[0]);l.MaxSteps=uint64(found[1])
 if _,e:=x.RunRoot("Widths."+root,args,l);e!=nil{panic(e)}
 // Allocation budget zero is invalid, rather than a partially executed result.
 l.MaxAllocations=0
 raw,e:=x.RunRoot("Widths."+root,args,l);got,te:=typed(root,row,l)
 if !errors.Is(e,x.ErrLimits)||raw.Type()!=""||!errors.Is(te,x.ErrLimits)||got!=0{panic("invalid budget output")}
 return found
}
func main(){
 var inputs [][3]uint64;if e:=json.NewDecoder(os.Stdin).Decode(&inputs);e!=nil{panic(e)}
 result:=struct{Raw,Typed [][]uint64;Budgets map[string][2]int}{Budgets:map[string][2]int{}}
 for _,row:=range inputs{
  rawRow,typedRow:=[]uint64{},[]uint64{}
  for _,root:=range roots{
   args:=arguments(root,row);raw,e:=x.RunRoot("Widths."+root,args,x.DefaultLimits());if e!=nil{panic(e)}
   native,e:=typed(root,row,x.DefaultLimits());if e!=nil{panic(e)}
   rawRow=append(rawRow,read(root,raw));typedRow=append(typedRow,native)
  }
  result.Raw=append(result.Raw,rawRow);result.Typed=append(result.Typed,typedRow)
 }
 for _,root:=range roots{result.Budgets[root]=budgets(root,inputs[len(inputs)-1])}
 if e:=json.NewEncoder(os.Stdout).Encode(result);e!=nil{panic(e)}
}
`

// WidthCampaign is the mixed-width conversion campaign.
func WidthCampaign() Fixed {
	roots := make([]string, len(widthRoots))
	for i, name := range widthRoots {
		roots[i] = "Widths." + name
	}
	return Fixed{Name: "widths", Module: "Widths", Source: "compatibility/corpus/width_conversions/source/Widths.lean",
		Roots: roots, Samples: WidthSamples, Model: WidthModel, Reference: widthReference, Consumer: widthConsumer,
		Trace: widthTrace(), Leaves: map[string][]string{
			"widen": {"UInt32.toUInt64"}, "narrow": {"UInt64.toUInt32"}, "low": {"UInt64.toUInt32"},
			"high": {"UInt64.shiftRight", "UInt64.toUInt32"},
			"pack": {"UInt32.toUInt64", "UInt64.shiftLeft", "UInt32.toUInt64", "UInt64.lor"},
		}, Values: 5, Width: 64}
}

// ---- bytes ----

// ByteSamples are every pair of boundary bytes with a cycling word, then 128
// seeded random rows.
func ByteSamples() []Row {
	boundary := []uint64{0, 1, 2, 7, 8, 9, 31, 32, 63, 64, 127, 128, 170, 254, 255}
	words := []uint64{0, 1, 255, 256, 257, 65535, 65536, 2147483648, 4294967295}
	var rows []Row
	for i, a := range boundary {
		for j, b := range boundary {
			rows = append(rows, Row{a, b, words[(i*len(boundary)+j)%len(words)]})
		}
	}
	r := rand.New(rand.NewPCG(825632, 825632))
	for i := 0; i < 128; i++ {
		rows = append(rows, Row{r.Uint64N(256), r.Uint64N(256), r.Uint64N(1 << 32)})
	}
	return rows
}

// ByteModel uses Boolean truth tables and positional arithmetic,
// independently of target bit operators, casts and fixed-width arithmetic.
func ByteModel(row Row) []uint64 {
	a, b, word := int64(row[0]), int64(row[1]), int64(row[2])
	bits := func(x int64) []int64 {
		out := make([]int64, 8)
		for i := range out {
			out[i] = (x / (int64(1) << i)) % 2
		}
		return out
	}
	left, right := bits(a), bits(b)
	var both, either, different int64
	for i := 0; i < 8; i++ {
		x, y, place := left[i], right[i], int64(1)<<i
		both += x * y * place
		either += min(1, x+y) * place
		different += ((x + y) % 2) * place
	}
	mod := func(x int64) int64 { return ((x % 256) + 256) % 256 }
	boolean := func(c bool) int64 {
		if c {
			return 1
		}
		return 0
	}
	shift := int64(1) << (b % 8)
	total := mod(a + b)
	values := []int64{total, mod(a - b), mod(a * b), mod(a * shift),
		both, either, different, 255 - a, a / shift,
		boolean(a == b), boolean(a < b), boolean(a <= b), a, word % 256, 255, 1,
		total, word % 256, a}
	out := make([]uint64, len(values))
	for i, v := range values {
		out[i] = uint64(v)
	}
	return out
}

func byteReference(rows []Row) string {
	triples := make([]string, len(rows))
	for i, row := range rows {
		triples[i] = fmt.Sprintf("(%d, %d, %d)", row[0], row[1], row[2])
	}
	return `import Bytes

def samples : List (UInt8 × UInt8 × UInt32) := [
  ` + strings.Join(triples, ",\n  ") + `]

def main : IO Unit := do
  let rows := samples.map fun (a, b, word) =>
    let r := Bytes.calculate a b word
    let values := [r.add.toNat, r.sub.toNat, r.mul.toNat, r.left.toNat,
      r.and.toNat, r.or.toNat, r.xor.toNat, r.not.toNat, r.right.toNat,
      r.eq.toNat, r.lt.toNat, r.le.toNat, r.wide.toNat, r.narrow.toNat,
      r.maximum.toNat, r.wrapped.toNat, (Bytes.choose (a < b) a b).toNat,
      (Bytes.narrow word).toNat, (Bytes.widen a).toNat]
    "[" ++ String.intercalate "," (values.map toString) ++ "]"
  IO.println ("[" ++ String.intercalate "," rows ++ "]")
`
}

const byteConsumer = `package main
import("encoding/json";"errors";"os";x "fixedconsumer/extracted")
func boolean(b bool)uint64{if b{return 1};return 0}
func typed(row [3]uint32,l x.Limits)([]uint64,error){
 a,b,n:=uint8(row[0]),uint8(row[1]),row[2]
 r,e:=x.RunCalculate(a,b,n,l);if e!=nil{if r!=(x.Result{}){panic("partial typed record")};return nil,e}
 out:=[]uint64{}
 for _,f:=range []func()(uint8,bool){r.Add,r.Sub,r.Mul,r.Left,r.And,r.Or,r.Xor,r.Not,r.Right}{v,ok:=f();if !ok{panic("byte accessor")};out=append(out,uint64(v))}
 for _,f:=range []func()(bool,bool){r.Eq,r.Lt,r.Le}{v,ok:=f();if !ok{panic("bool accessor")};out=append(out,boolean(v))}
 wide,ok:=r.Wide();if !ok{panic("wide accessor")};out=append(out,uint64(wide))
 for _,f:=range []func()(uint8,bool){r.Narrow,r.Maximum,r.Wrapped}{v,ok:=f();if !ok{panic("literal accessor")};out=append(out,uint64(v))}
 choice,e:=x.RunChoose(a<b,a,b,l);if e!=nil{return nil,e};narrow,e:=x.RunNarrow(n,l);if e!=nil{return nil,e};widen,e:=x.RunWiden(a,l);if e!=nil{return nil,e}
 return append(out,uint64(choice),uint64(narrow),uint64(widen)),nil
}
func arguments(row [3]uint32)[]x.Value{
 args:=[]x.Value{};for _,in:=range []*x.Input{{Type:"UInt8",UInt8:uint8(row[0])},{Type:"UInt8",UInt8:uint8(row[1])},{Type:"UInt32",UInt32:row[2]}}{v,e:=x.Import(in,x.DefaultLimits());if e!=nil{panic(e)};args=append(args,v);in.UInt8=42;in.UInt32=42};return args
}
func raw(row [3]uint32,l x.Limits)([]uint64,error){
 args:=arguments(row);v,e:=x.RunRoot("Bytes.calculate",args,l);if e!=nil{if v.Type()!=""{panic("partial raw record")};return nil,e}
 out:=[]uint64{};for i,f:=range v.Fields(){
  if i>=9&&i<=11{switch f.Constructor(){case "Bool.false":out=append(out,0);case "Bool.true":out=append(out,1);default:panic("bool shape")};continue}
  if i==12{n,ok:=f.UInt32();if !ok{panic("wide shape")};if _,ok=f.UInt8();ok{panic("inactive byte payload")};out=append(out,uint64(n));continue}
  n,ok:=f.UInt8();if !ok{panic("byte shape")};if _,ok=f.UInt32();ok{panic("inactive small payload")};if _,ok=f.UInt64();ok{panic("inactive wide payload")};out=append(out,uint64(n))
 }
 flag:="Bool.false";if row[0]<row[1]{flag="Bool.true"};b,e:=x.Import(&x.Input{Type:"Bool",Constructor:flag},x.DefaultLimits());if e!=nil{panic(e)}
 choice,e:=x.RunRoot("Bytes.choose",[]x.Value{b,args[0],args[1]},l);if e!=nil{return nil,e};c,ok:=choice.UInt8();if !ok{panic("choice type")}
 narrow,e:=x.RunRoot("Bytes.narrow",args[2:],l);if e!=nil{return nil,e};n,ok:=narrow.UInt8();if !ok{panic("narrow type")}
 wide,e:=x.RunRoot("Bytes.widen",args[:1],l);if e!=nil{return nil,e};w,ok:=wide.UInt32();if !ok{panic("wide type")}
 return append(out,uint64(c),uint64(n),uint64(w)),nil
}
func budget(row [3]uint32)[]int{
 out:=[]int{};for dimension:=0;dimension<2;dimension++{found:=0;for bound:=1;bound<=64;bound++{l:=x.DefaultLimits();if dimension==0{l.MaxAllocations=uint64(bound)}else{l.MaxSteps=uint64(bound)};r,e:=raw(row,l);t,te:=typed(row,l);if e==nil{if te!=nil||len(t)!=len(r){panic("budget divergence")};found=bound;break};if !errors.Is(e,x.ErrResource)||!errors.Is(te,x.ErrResource)||r!=nil||t!=nil{panic("partial budget result")}};if found==0{panic("budget exceeded search")};out=append(out,found)};return out
}
func main(){var inputs [][3]uint32;if e:=json.NewDecoder(os.Stdin).Decode(&inputs);e!=nil{panic(e)};result:=struct{Raw,Typed [][]uint64;Budgets []int}{};for _,row:=range inputs{r,e:=raw(row,x.DefaultLimits());if e!=nil{panic(e)};t,e:=typed(row,x.DefaultLimits());if e!=nil{panic(e)};result.Raw=append(result.Raw,r);result.Typed=append(result.Typed,t)};result.Budgets=budget(inputs[len(inputs)-1]);if e:=json.NewEncoder(os.Stdout).Encode(result);e!=nil{panic(e)}}
`

// ByteCampaign is the UInt8 arithmetic, comparison and conversion campaign.
func ByteCampaign() Fixed {
	return Fixed{Name: "bytes", Module: "Bytes", Source: "compatibility/corpus/bytes/source/Bytes.lean",
		Roots:   []string{"Bytes.calculate", "Bytes.choose", "Bytes.narrow", "Bytes.widen"},
		Samples: ByteSamples, Model: ByteModel, Reference: byteReference, Consumer: byteConsumer, Values: 19, Width: 32}
}

// Fixed campaign IR identity, as the Python campaigns required.
const (
	fixedSchema  = "lean-go-ir/v24"
	fixedProfile = "pure-module/v22"
)

// RunFixed runs a fixed campaign in the new directory work and writes its
// inputs, results and report there.
func RunFixed(f Fixed, repo, work string, timeout time.Duration) (*FixedReport, error) {
	work, err := filepath.Abs(work)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(work), 0o755); err != nil {
		return nil, err
	}
	if err := os.Mkdir(work, 0o755); err != nil {
		return nil, err
	}
	b, err := NewBackend(repo, work, timeout)
	if err != nil {
		return nil, err
	}
	b.LastWork = work
	if err := b.preflight(); err != nil {
		return nil, err
	}
	rows := f.Samples()
	source, err := os.ReadFile(filepath.Join(b.Repo, filepath.FromSlash(f.Source)))
	if err != nil {
		return nil, err
	}
	sourceDir, native, consumer := filepath.Join(work, "source"), filepath.Join(work, "native"), filepath.Join(work, "consumer")
	for _, dir := range []string{sourceDir, native, consumer} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return nil, err
		}
	}
	wire, err := json.Marshal(rows)
	if err != nil {
		return nil, err
	}
	files := map[string]string{
		filepath.Join(sourceDir, f.Module+".lean"): string(source),
		filepath.Join(native, f.Module+".lean"):    string(source),
		filepath.Join(native, "Reference.lean"):    f.Reference(rows),
		filepath.Join(native, "lean-toolchain"):    string(b.compiler.PinFile()),
		filepath.Join(native, "lakefile.toml"): fmt.Sprintf("name = \"%s_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\nname = \"%s\"\n"+
			"[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n", f.Name, f.Module),
		filepath.Join(work, "inputs.json"): string(wire) + "\n",
	}
	if f.Trace != "" {
		files[filepath.Join(native, "Trace.lean")] = f.Trace
	}
	if err := writeFiles(files); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(work, "toolchain.json"), b.versions); err != nil {
		return nil, err
	}
	if _, err := b.run("native-build", []string{b.tools["lake"], "build", "reference"}, native, "", false); err != nil {
		return nil, err
	}
	if f.Trace != "" {
		trace, err := b.run("pure-mono-trace", []string{b.tools["lake"], "env", "lean", "Trace.lean"}, native, "", false)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(work, "pure-mono-trace.txt"), []byte(trace), 0o644); err != nil {
			return nil, err
		}
		for _, root := range sortedKeys(f.Leaves) {
			prefix := "LEAF " + f.Module + "." + root + " -> "
			actual := []string{}
			for _, line := range strings.Split(trace, "\n") {
				if _, after, ok := strings.Cut(line, prefix); ok {
					actual = append(actual, after)
				}
			}
			if !reflect.DeepEqual(actual, f.Leaves[root]) {
				return nil, backendError("pure-mono-trace", "%s: expected %v, got %v", root, f.Leaves[root], actual)
			}
		}
	}
	nativeOutput, err := b.run("native-run", []string{filepath.Join(native, ".lake", "build", "bin", "reference"+exeSuffix())}, native, "", false)
	if err != nil {
		return nil, err
	}
	command := []string{b.tools["lean-go"], "generate", "--source-dir", sourceDir, "--module", f.Module}
	for _, root := range f.Roots {
		command = append(command, "--root", root)
	}
	ir := filepath.Join(work, "program.ir.json")
	command = append(command, "--out", filepath.Join(consumer, "extracted"), "--ir-output", ir, "--mode", "metered", "--strict-go")
	if _, err := b.run("extract", command, b.Repo, "", false); err != nil {
		return nil, err
	}
	// The thresholds are the static bounds of the fast path: below them the
	// state machine fails, at them the fast path answers. The same consumer
	// against the state-machine package runs every row and every budget on
	// the explicit-stack state machine.
	if err := b.emitStateMachine(ir, filepath.Join(consumer, "statemachine")); err != nil {
		return nil, err
	}
	if err := writeFiles(map[string]string{filepath.Join(consumer, "go.mod"): "module fixedconsumer\n\ngo 1.27.4\n"}); err != nil {
		return nil, err
	}
	nativeValues, err := decodeJSON([]byte(nativeOutput))
	if err != nil {
		return nil, backendError("native-output", "%v", err)
	}
	modelRows := make([]any, len(rows))
	for i, row := range rows {
		values := f.Model(row)
		items := make([]any, len(values))
		for j, v := range values {
			items[j] = json.Number(fmt.Sprint(v))
		}
		modelRows[i] = items
	}
	labelled := map[string]any{"model": modelRows, "native": nativeValues}
	budgets := map[string]json.RawMessage{}
	for _, variant := range []struct{ suffix, pkg string }{{"", "extracted"}, {"_state_machine", "statemachine"}} {
		dir := filepath.Join(consumer, "cmd"+strings.ReplaceAll(variant.suffix, "_", ""))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		source := strings.Replace(f.Consumer, `x "fixedconsumer/extracted"`, `x "fixedconsumer/`+variant.pkg+`"`, 1)
		if err := writeFiles(map[string]string{filepath.Join(dir, "main.go"): source}); err != nil {
			return nil, err
		}
		binary := filepath.Join(consumer, "consumer"+variant.suffix+exeSuffix())
		if _, err := b.run("go-build"+variant.suffix, []string{b.tools["go"], "build", "-buildvcs=false", "-o", binary, "./" + filepath.Base(dir)}, consumer, "", false); err != nil {
			return nil, err
		}
		goOutput, err := b.run("go-run"+variant.suffix, []string{binary}, consumer, string(wire), true)
		if err != nil {
			return nil, err
		}
		goValue, err := decodeJSON([]byte(goOutput))
		if err != nil {
			return nil, backendError("output", "%v", err)
		}
		goValues, ok := goValue.(map[string]any)
		if !ok || !exactKeys(goValues, "Raw", "Typed", "Budgets") {
			return nil, backendError("output", "Go result must contain exactly Raw, Typed and Budgets")
		}
		labelled["raw"+variant.suffix], labelled["typed"+variant.suffix] = goValues["Raw"], goValues["Typed"]
		if budgets[variant.suffix], err = json.Marshal(goValues["Budgets"]); err != nil {
			return nil, err
		}
	}
	checked := map[string][][]uint64{}
	for _, label := range []string{"model", "native", "raw", "typed", "raw_state_machine", "typed_state_machine"} {
		items, ok := labelled[label].([]any)
		if !ok || len(items) != len(rows) {
			return nil, backendError(label, "wrong row count")
		}
		for i, item := range items {
			values, err := CheckedValues(item, f.Values, f.Width, label)
			if err != nil {
				return nil, err
			}
			checked[label] = append(checked[label], values)
			if label != "model" && !reflect.DeepEqual(values, checked["model"][i]) {
				return nil, backendError(label, "case %d disagrees with arithmetic model", i)
			}
		}
	}
	if string(budgets[""]) != string(budgets["_state_machine"]) {
		return nil, backendError("budgets", "metered thresholds %s differ from the state machine's %s", budgets[""], budgets["_state_machine"])
	}
	irData, err := os.ReadFile(ir)
	if err != nil {
		return nil, err
	}
	var identity struct{ Schema, Profile string }
	if err := json.Unmarshal(irData, &identity); err != nil || identity.Schema != fixedSchema || identity.Profile != fixedProfile {
		return nil, backendError("extract", "wrong %s schema: %s %s", f.Name, identity.Schema, identity.Profile)
	}
	report := &FixedReport{Status: "passed", Cases: len(rows), Roots: f.Roots, Budgets: budgets[""],
		StateMachineBudgets: budgets["_state_machine"], Assurance: Assurance}
	if err := writeJSON(filepath.Join(work, "results.json"), checked); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(work, "report.json"), report); err != nil {
		return nil, err
	}
	return report, nil
}

var errNoCampaign = errors.New("unknown fixed campaign")

// FixedCampaign returns a fixed campaign by name: widths or bytes.
func FixedCampaign(name string) (Fixed, error) {
	switch name {
	case "widths":
		return WidthCampaign(), nil
	case "bytes":
		return ByteCampaign(), nil
	}
	return Fixed{}, errNoCampaign
}
