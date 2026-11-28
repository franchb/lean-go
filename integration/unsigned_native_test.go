package integration_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

// The unsigned operations against native Lean: fresh Lean builds compared
// with the generated Go of both APIs and with an arithmetic model. Gated by
// LEAN_GO_MODULES. What the producer admits and rejects is tested in
// internal/pipeline/lean_unsigned_test.go and lean_bytes_test.go.

func requireModulesGate(t *testing.T) {
	t.Helper()
	if os.Getenv("LEAN_GO_MODULES") != "1" {
		t.Skip("set LEAN_GO_MODULES=1 for fresh Lean builds")
	}
}

func executable(dir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, ".lake", "build", "bin", name)
}

// runIn runs a command in dir with the pinned toolchain environment and
// returns its standard output; stdin, when non-nil, is its standard input.
func runIn(t *testing.T, dir string, env []string, stdin []byte, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s\n%s", name, args, err, out, stderr.String())
	}
	return out
}

func selectedCompiler(t *testing.T) pipeline.Compiler {
	t.Helper()
	compiler, err := pipeline.SelectedCompiler()
	if err != nil {
		t.Fatal(err)
	}
	return compiler
}

// generateMetered is lean-go generate --mode metered, in process: a fresh
// export emitted in the mode of the tracked artifacts.
func generateMetered(t *testing.T, cache *pipeline.Cache, source, module, out, ir string, roots ...string) {
	t.Helper()
	if _, err := pipeline.Generate(pipeline.GenerateOptions{SourceDir: source, Module: module, Roots: roots, Out: out, IROutput: ir,
		Emit: pipeline.EmitOptions{Mode: "metered"}, Cache: cache}); err != nil {
		t.Fatal(err)
	}
}

var matrixWidths = []int{8, 16, 32, 64}

var matrixFields = []string{"add", "sub", "mul", "div", "mod", "and", "or", "xor", "not", "shl", "shr", "eq", "lt", "le"}

var matrixNotation = []string{"a + b", "a - b", "a * b", "a / b", "a % b", "a &&& b", "a ||| b", "a ^^^ b", "~~~a",
	"a <<< b", "a >>> b", "a == b", "decide (a < b)", "decide (a ≤ b)"}

func matrixTargets(width int) []int {
	var out []int
	for _, v := range matrixWidths {
		if v != width {
			out = append(out, v)
		}
	}
	return out
}

// matrixSource is test_unsigned.matrix_source(): one root per width returning
// every operation and conversion in a record, by ordinary notation.
func matrixSource() string {
	var lines []string
	for _, w := range matrixWidths {
		lines = append(lines, fmt.Sprintf("structure Ops%d where", w))
		for _, field := range matrixFields {
			typ := fmt.Sprintf("UInt%d", w)
			if field == "eq" || field == "lt" || field == "le" {
				typ = "Bool"
			}
			lines = append(lines, fmt.Sprintf("  %s : %s", field, typ))
		}
		values := slices.Clone(matrixNotation)
		for _, v := range matrixTargets(w) {
			lines = append(lines, fmt.Sprintf("  to%d : UInt%d", v, v))
			values = append(values, fmt.Sprintf("a.toUInt%d", v))
		}
		lines = append(lines, fmt.Sprintf("def ops%d (a b : UInt%d) : Ops%d :=\n  ⟨%s⟩", w, w, w, strings.Join(values, ", ")))
	}
	return strings.Join(lines, "\n") + "\n"
}

// matrixModel is Lean's definitions on natural numbers, bit by bit for the
// logical operations, independent of Go's fixed-width operators.
func matrixModel(width int, a, b uint64) []uint64 {
	x, y := new(big.Int).SetUint64(a), new(big.Int).SetUint64(b)
	m := new(big.Int).Lsh(big.NewInt(1), uint(width))
	reduce := func(v *big.Int) uint64 { return new(big.Int).Mod(v, m).Uint64() }
	bit := func(f func(p, q uint) uint) uint64 {
		sum := new(big.Int)
		for i := 0; i < width; i++ {
			if f(x.Bit(i), y.Bit(i)) == 1 {
				sum.SetBit(sum, i, 1)
			}
		}
		return sum.Uint64()
	}
	truth := func(ok bool) uint64 {
		if ok {
			return 1
		}
		return 0
	}
	power := new(big.Int).Lsh(big.NewInt(1), uint(b%uint64(width)))
	quotient, remainder := uint64(0), a
	if b != 0 {
		quotient, remainder = new(big.Int).Quo(x, y).Uint64(), new(big.Int).Rem(x, y).Uint64()
	}
	values := []uint64{reduce(new(big.Int).Add(x, y)), reduce(new(big.Int).Sub(x, y)), reduce(new(big.Int).Mul(x, y)),
		quotient, remainder,
		bit(func(p, q uint) uint { return p * q }), bit(func(p, q uint) uint { return max(p, q) }), bit(func(p, q uint) uint { return (p + q) % 2 }),
		reduce(new(big.Int).Sub(new(big.Int).Sub(m, big.NewInt(1)), x)),
		reduce(new(big.Int).Mul(x, power)), new(big.Int).Quo(x, power).Uint64(),
		truth(a == b), truth(a < b), truth(a <= b)}
	for _, v := range matrixTargets(width) {
		values = append(values, new(big.Int).Mod(x, new(big.Int).Lsh(big.NewInt(1), uint(v))).Uint64())
	}
	return values
}

// matrixBoundary is test_unsigned.boundary().
func matrixBoundary(width int) []uint64 {
	w := uint64(width)
	maximum := uint64(1)<<(w-1)<<1 - 1
	set := map[uint64]bool{}
	for _, v := range []uint64{0, 1, 2, 3, 7, w - 1, w, w + 1, 2 * w, 255 & maximum, maximum, maximum - 1, 1 << (w - 1), 1<<(w-1) - 1} {
		set[v] = true
	}
	for k := uint64(0); k < w; k += 3 {
		set[1<<k] = true
	}
	var out []uint64
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// The raw API is read generically: every record field is a scalar or a Bool.
// The typed API calls each root and reads the same fields through reflection.
const matrixConsumer = `package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"

	x "matrixconsumer/extracted"
)

func scalar(v x.Value) uint64 {
	if a, ok := v.UInt8(); ok {
		return uint64(a)
	}
	if a, ok := v.UInt16(); ok {
		return uint64(a)
	}
	if a, ok := v.UInt32(); ok {
		return uint64(a)
	}
	if a, ok := v.UInt64(); ok {
		return a
	}
	switch v.Constructor() {
	case "Bool.true":
		return 1
	case "Bool.false":
		return 0
	}
	panic("unexpected field " + v.Type())
}

func input(width int, n uint64) *x.Input {
	switch width {
	case 8:
		return &x.Input{Type: "UInt8", UInt8: uint8(n)}
	case 16:
		return &x.Input{Type: "UInt16", UInt16: uint16(n)}
	case 32:
		return &x.Input{Type: "UInt32", UInt32: uint32(n)}
	}
	return &x.Input{Type: "UInt64", UInt64: n}
}

func typed(width int, a, b uint64, l x.Limits) []uint64 {
	run := reflect.ValueOf(map[int]any{8: x.RunOps8, 16: x.RunOps16, 32: x.RunOps32, 64: x.RunOps64}[width])
	argument := func(n uint64) reflect.Value { return reflect.ValueOf(n).Convert(run.Type().In(0)) }
	out := run.Call([]reflect.Value{argument(a), argument(b), reflect.ValueOf(l)})
	if !out[1].IsNil() {
		panic(out[1].Interface())
	}
	record := out[0]
	var values []uint64
	for _, name := range []string{"Add", "Sub", "Mul", "Div", "Mod", "And", "Or", "Xor", "Not", "Shl", "Shr", "Eq", "Lt", "Le", "To8", "To16", "To32", "To64"} {
		method := record.MethodByName(name)
		if !method.IsValid() {
			continue
		}
		result := method.Call(nil)
		if !result[1].Bool() {
			panic("missing field " + name)
		}
		switch v := result[0].Interface().(type) {
		case bool:
			if v {
				values = append(values, 1)
			} else {
				values = append(values, 0)
			}
		default:
			values = append(values, reflect.ValueOf(v).Convert(reflect.TypeFor[uint64]()).Uint())
		}
	}
	return values
}

func main() {
	data, err := os.ReadFile("pairs.json")
	if err != nil {
		panic(err)
	}
	var pairs map[string][][2]uint64
	if err := json.Unmarshal(data, &pairs); err != nil {
		panic(err)
	}
	l := x.DefaultLimits()
	result := struct{ Raw, Typed [][]uint64 }{}
	for _, width := range []int{8, 16, 32, 64} {
		for _, pair := range pairs[strconv.Itoa(width)] {
			args := make([]x.Value, 2)
			for i, n := range pair {
				if args[i], err = x.Import(input(width, n), l); err != nil {
					panic(err)
				}
			}
			raw, err := x.RunRoot("Matrix.ops"+strconv.Itoa(width), args, l)
			if err != nil {
				panic(err)
			}
			var row []uint64
			for _, field := range raw.Fields() {
				row = append(row, scalar(field))
			}
			result.Raw = append(result.Raw, row)
			result.Typed = append(result.Typed, typed(width, pair[0], pair[1], l))
		}
	}
	out, err := json.Marshal(map[string][][]uint64{"raw": result.Raw, "typed": result.Typed})
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(out)
}
`

// test_native_lean_both_go_apis_and_model_agree_on_boundaries
func TestUnsignedNativeLeanBothGoAPIsAndModelAgreeOnBoundaries(t *testing.T) {
	requireModulesGate(t)
	compiler := selectedCompiler(t)
	env := commandEnv(map[string]string{"ELAN_TOOLCHAIN": compiler.Toolchain()})
	dir := t.TempDir()
	sources, native, consumer := filepath.Join(dir, "sources"), filepath.Join(dir, "native"), filepath.Join(dir, "consumer")
	source := "namespace Matrix\n" + matrixSource() + "end Matrix\n"
	pairs := map[string][][2]uint64{}
	reference := []string{"import Matrix", "open Matrix", "def main : IO Unit := do"}
	var expected [][]uint64
	for _, w := range matrixWidths {
		var listed []string
		for _, a := range matrixBoundary(w) {
			for _, b := range matrixBoundary(w) {
				pairs[strconv.Itoa(w)] = append(pairs[strconv.Itoa(w)], [2]uint64{a, b})
				listed = append(listed, fmt.Sprintf("(%d, %d)", a, b))
				expected = append(expected, matrixModel(w, a, b))
			}
		}
		var fields []string
		for _, f := range matrixFields {
			if f == "eq" || f == "lt" || f == "le" {
				fields = append(fields, fmt.Sprintf("(if r.%s then 1 else 0)", f))
			} else {
				fields = append(fields, fmt.Sprintf("r.%s.toNat", f))
			}
		}
		for _, v := range matrixTargets(w) {
			fields = append(fields, fmt.Sprintf("r.to%d.toNat", v))
		}
		reference = append(reference, fmt.Sprintf("  for (a, b) in ([%s] : List (UInt%d × UInt%d)) do", strings.Join(listed, ", "), w, w),
			fmt.Sprintf("    let r := ops%d a b", w), fmt.Sprintf("    IO.println (toString [%s])", strings.Join(fields, ", ")))
	}
	pairsJSON, err := json.Marshal(pairs)
	if err != nil {
		t.Fatal(err)
	}
	writeNumbersFiles(t, dir, map[string]string{
		"sources/Matrix.lean":   source,
		"native/Matrix.lean":    source,
		"native/Reference.lean": strings.Join(reference, "\n") + "\n",
		"native/lean-toolchain": string(compiler.PinFile()),
		"native/lakefile.toml": "name = \"matrix_reference\"\nversion = \"0.0.0\"\n[[lean_lib]]\n" +
			"name = \"Matrix\"\n[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n",
		"consumer/go.mod":     "module matrixconsumer\n\ngo 1.27.4\n",
		"consumer/main.go":    matrixConsumer,
		"consumer/pairs.json": string(pairsJSON),
	})
	runIn(t, native, env, nil, "lake", "build", "reference")
	var nativeRows [][]uint64
	for _, line := range strings.Split(strings.TrimSpace(string(runIn(t, native, env, nil, executable(native, "reference")))), "\n") {
		var row []uint64
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("native line %q: %v", line, err)
		}
		nativeRows = append(nativeRows, row)
	}
	roots := []string{"Matrix.ops8", "Matrix.ops16", "Matrix.ops32", "Matrix.ops64"}
	generateMetered(t, &pipeline.Cache{Dir: filepath.Join(dir, "cache")}, sources, "Matrix", filepath.Join(consumer, "extracted"), filepath.Join(dir, "matrix.ir.json"), roots...)
	var goRows struct{ Raw, Typed [][]uint64 }
	if err := json.Unmarshal(runIn(t, consumer, env, nil, goBinary(), "run", "."), &goRows); err != nil {
		t.Fatal(err)
	}
	if len(nativeRows) != len(expected) || len(goRows.Raw) != len(expected) || len(goRows.Typed) != len(expected) {
		t.Fatalf("row counts: native %d, raw %d, typed %d, model %d", len(nativeRows), len(goRows.Raw), len(goRows.Typed), len(expected))
	}
	if len(expected) <= 1000 {
		t.Fatalf("only %d boundary pairs", len(expected))
	}
	for i, want := range expected {
		if !reflect.DeepEqual(nativeRows[i], want) || !reflect.DeepEqual(goRows.Raw[i], want) || !reflect.DeepEqual(goRows.Typed[i], want) {
			t.Fatalf("row %d: native %v, raw %v, typed %v, model %v", i, nativeRows[i], goRows.Raw[i], goRows.Typed[i], want)
		}
	}
}

// UInt16 bitwise operations and byte casts: native Lean against the
// arithmetic definition.
func TestUInt16NativeBitwiseOperationsAndByteCasts(t *testing.T) {
	requireModulesGate(t)
	compiler := selectedCompiler(t)
	env := commandEnv(map[string]string{"ELAN_TOOLCHAIN": compiler.Toolchain()})
	dir := t.TempDir()
	if err := compiler.CheckVersion(string(runIn(t, dir, env, nil, "lean", "--version"))); err != nil {
		t.Fatal(err)
	}
	writeNumbersFiles(t, dir, map[string]string{
		"lean-toolchain": string(compiler.PinFile()),
		"lakefile.toml":  "name = \"word16_reference\"\nversion = \"0.0.0\"\n[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n",
		"Reference.lean": `def main (args : List String) : IO Unit := do
  for arg in args do
    let n := arg.toNat!
    let w := (n / 65536).toUInt16
    let c := n.toUInt16
    IO.println s!"{w <<< c} {w >>> c} {w ||| c} {w ^^^ c} {w.toUInt8} {c.toUInt8.toUInt16}"
`,
	})
	runIn(t, dir, env, nil, "lake", "build", "reference")
	words := []uint64{0, 1, 255, 256, 0x5555, 0x8001, 0xaaaa, 65535}
	counts := []uint64{0, 1, 7, 8, 15, 16, 17, 255, 256, 0x5555, 0xaaaa, 65535}
	var args []string
	var want [][]uint64
	for _, w := range words {
		for _, c := range counts {
			args = append(args, strconv.FormatUint(w<<16+c, 10))
			want = append(want, []uint64{(w * (1 << (c % 16))) % 65536, w / (1 << (c % 16)), w | c, w ^ c, w % 256, c % 256})
		}
	}
	var got [][]uint64
	for _, line := range strings.Split(strings.TrimSpace(string(runIn(t, dir, env, nil, executable(dir, "reference"), args...))), "\n") {
		var row []uint64
		for _, field := range strings.Fields(line) {
			n, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			row = append(row, n)
		}
		got = append(got, row)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native\n%v\nwant\n%v", got, want)
	}
}
