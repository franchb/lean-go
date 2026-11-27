package integration_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

// Ports of BitwiseModelTests and BitwiseNativeTests in scripts/test_bitwise.py
// and of ShiftLeftNativeTests in scripts/test_shift_left.py. The native tests
// follow scripts/differential_backend.py: one ordinary Lean module with a
// scalar root Generated.run is built natively with Lake and extracted with
// the pipeline; the generated package runs through both its raw and typed
// APIs with no producer tool on PATH, and all three channels must equal an
// independent arithmetic model. Gated by LEAN_GO_DIFFERENTIAL, as before.

// bitwiseModel evaluates one operation of a and b with arbitrary precision,
// bit by bit for the logical operations (differential_model.evaluate).
func bitwiseModel(op string, width int, a, b uint64) uint64 {
	x, y := new(big.Int).SetUint64(a), new(big.Int).SetUint64(b)
	m := new(big.Int).Lsh(big.NewInt(1), uint(width))
	power := new(big.Int).Lsh(big.NewInt(1), uint(new(big.Int).Mod(y, big.NewInt(int64(width))).Uint64()))
	perBit := func(f func(p, q uint) uint) uint64 {
		sum := new(big.Int)
		for i := 0; i < width; i++ {
			if f(x.Bit(i), y.Bit(i)) == 1 {
				sum.SetBit(sum, i, 1)
			}
		}
		return sum.Uint64()
	}
	switch op {
	case "land":
		return perBit(func(p, q uint) uint { return p * q })
	case "lor":
		return perBit(func(p, q uint) uint { return min(1, p+q) })
	case "xor":
		return perBit(func(p, q uint) uint { return (p + q) % 2 })
	case "complement":
		return new(big.Int).Sub(new(big.Int).Sub(m, big.NewInt(1)), x).Uint64()
	case "shr":
		return new(big.Int).Quo(x, power).Uint64()
	case "shl":
		return new(big.Int).Mod(new(big.Int).Mul(x, power), m).Uint64()
	}
	panic("unknown operation " + op)
}

var bitwiseNotation = map[string]string{"land": "(a &&& b)", "lor": "(a ||| b)", "xor": "(a ^^^ b)", "complement": "(~~~a)", "shr": "(a >>> b)", "shl": "(a <<< b)"}

// renderBitwise is differential_model.render_lean for Program(0, width,
// Expr(op, args), Expr('a')).
func renderBitwise(op string, width int) string {
	scalar := fmt.Sprintf("UInt%d", width)
	return "namespace Generated\n\n" +
		fmt.Sprintf("structure Box where\n  value : %s\n\n", scalar) +
		fmt.Sprintf("structure State where\n  inner : Box\n  other : %s\n\n", scalar) +
		fmt.Sprintf("@[noinline] def unbox (box : Box) : %s := box.value\n\n", scalar) +
		fmt.Sprintf("@[noinline] def readState (state : State) : %s :=\n", scalar) +
		"  state.inner.value + state.other\n\n" +
		fmt.Sprintf("@[noinline] def helper (a b : %s) : %s :=\n", scalar, scalar) +
		"  a\n\n" +
		fmt.Sprintf("def run (a b : %s) : %s :=\n", scalar, scalar) +
		"  " + bitwiseNotation[op] + "\n\n" +
		"end Generated\n"
}

func TestBitwiseArithmeticOracle(t *testing.T) {
	for _, width := range []int{32, 64} {
		maximum := uint64(1)<<(width-1)<<1 - 1
		for op, want := range map[string]uint64{"land": 0, "lor": maximum, "xor": maximum, "complement": maximum - 0xaaaaaaaa, "shr": 0x55555555} {
			b := maximum - 0xaaaaaaaa
			if op == "shr" {
				b = 1
			}
			if got := bitwiseModel(op, width, 0xaaaaaaaa, b); got != want {
				t.Fatalf("UInt%d %s = %d, want %d", width, op, got, want)
			}
			if !strings.Contains(renderBitwise(op, width), "def run") {
				t.Fatal("rendered module has no root")
			}
		}
	}
}

func differentialReference(width int, pairs [][2]uint64) string {
	var listed []string
	for _, p := range pairs {
		listed = append(listed, fmt.Sprintf("(%d, %d)", p[0], p[1]))
	}
	return fmt.Sprintf(`import Generated

def samples : List (UInt%d × UInt%d) := [
  %s]

def main : IO Unit := do
  let values := samples.map (fun pair => toString (Generated.run pair.1 pair.2).toNat)
  IO.println ("[" ++ String.intercalate "," values ++ "]")
`, width, width, strings.Join(listed, ",\n  "))
}

func differentialConsumer(width int) string {
	return strings.ReplaceAll(`package main

import (
    "encoding/json"
    "os"
    x "differentialconsumer/extracted"
)

func main() {
    var inputs [][2]uintW
    if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil { panic(err) }
    // uint64 results: encoding/json would write a []uint8 as base64.
    result := struct {
        Raw []uint64 `+"`json:\"raw\"`"+`
        Typed []uint64 `+"`json:\"typed\"`"+`
    }{make([]uint64, len(inputs)), make([]uint64, len(inputs))}
    limits := x.DefaultLimits()
    for i, pair := range inputs {
        args := make([]x.Value, 2)
        for j, value := range pair {
            v, err := x.Import(&x.Input{Type: "UIntW", UIntW: value}, limits)
            if err != nil { panic(err) }
            args[j] = v
        }
        raw, err := x.Run(args, limits)
        if err != nil { panic(err) }
        value, ok := raw.UIntW()
        if !ok || raw.Type() != "UIntW" || len(raw.Fields()) != 0 { panic("invalid scalar result") }
        result.Raw[i] = uint64(value)
        typed, err := x.RunTyped(pair[0], pair[1], limits)
        if err != nil { panic(err) }
        result.Typed[i] = uint64(typed)
    }
    if err := json.NewEncoder(os.Stdout).Encode(result); err != nil { panic(err) }
}
`, "W", fmt.Sprint(width))
}

type differentialResult struct {
	native, raw, typed []uint64
	ir                 map[string]any
}

// differentialCheck is differential_backend.Backend.check for one rendered
// source: a fresh native build, a fresh extraction and a Go-only consumer.
func differentialCheck(t *testing.T, cache *pipeline.Cache, source string, width int, pairs [][2]uint64) differentialResult {
	t.Helper()
	compiler := selectedCompiler(t)
	env := commandEnv(map[string]string{"ELAN_TOOLCHAIN": compiler.Toolchain(), "LEAN_GO_TOOLCHAIN": compiler.Version})
	work := t.TempDir()
	native, consumer := filepath.Join(work, "native"), filepath.Join(work, "consumer")
	wire, err := json.Marshal(pairs)
	if err != nil {
		t.Fatal(err)
	}
	writeNumbersFiles(t, work, map[string]string{
		"source/Generated.lean": source,
		"native/Generated.lean": source,
		"native/Reference.lean": differentialReference(width, pairs),
		"native/lean-toolchain": string(compiler.PinFile()),
		"native/lakefile.toml": "name = \"differential_reference\"\nversion = \"0.0.0\"\n" +
			"[[lean_lib]]\nname = \"Generated\"\n[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n",
		"inputs.json":      string(wire) + "\n",
		"consumer/go.mod":  "module differentialconsumer\n\ngo 1.27.4\n",
		"consumer/main.go": differentialConsumer(width),
	})
	if err := compiler.CheckVersion(string(runIn(t, work, env, nil, "lean", "--version"))); err != nil {
		t.Fatal(err)
	}
	if version := string(runIn(t, work, env, nil, goBinary(), "version")); !strings.HasPrefix(version, "go version go1.27.4 ") {
		t.Fatalf("expected Go 1.27.4, got %s", version)
	}
	runIn(t, native, env, nil, "lake", "build", "reference")
	result := differentialResult{native: checkedValues(t, "native-output", runIn(t, native, env, nil, executable(native, "reference")), len(pairs), width)}
	ir := filepath.Join(work, "program.ir.json")
	generateMetered(t, cache, filepath.Join(work, "source"), "Generated", filepath.Join(consumer, "extracted"), ir, "Generated.run")
	binary := filepath.Join(consumer, "consumer")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runIn(t, consumer, env, nil, goBinary(), "build", "-buildvcs=false", "-o", binary, ".")
	goOnly := commandEnv(map[string]string{"ELAN_TOOLCHAIN": compiler.Toolchain(), "LEAN_GO_TOOLCHAIN": compiler.Version,
		"PATH": filepath.Join(consumer, "no-producer-tools")})
	var channels map[string]json.RawMessage
	if err := json.Unmarshal(runIn(t, consumer, goOnly, wire, binary), &channels); err != nil || len(channels) != 2 {
		t.Fatalf("Go result must contain exactly raw and typed arrays: %v %v", err, channels)
	}
	result.raw = checkedValues(t, "raw-output", channels["raw"], len(pairs), width)
	result.typed = checkedValues(t, "typed-output", channels["typed"], len(pairs), width)
	data, err := os.ReadFile(ir)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &result.ir); err != nil {
		t.Fatal(err)
	}
	return result
}

// checkedValues requires exactly count unsigned integers of the width.
func checkedValues(t *testing.T, stage string, data []byte, count, width int) []uint64 {
	t.Helper()
	var values []json.Number
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil || len(values) != count {
		t.Fatalf("%s: expected %d output values: %v %s", stage, count, err, data)
	}
	limit := new(big.Int).Lsh(big.NewInt(1), uint(width))
	out := make([]uint64, count)
	for i, v := range values {
		n, ok := new(big.Int).SetString(v.String(), 10)
		if !ok || n.Sign() < 0 || n.Cmp(limit) >= 0 {
			t.Fatalf("%s: expected exact unsigned %d-bit integers, got %s", stage, width, v)
		}
		out[i] = n.Uint64()
	}
	return out
}

// compareChannels is differential_cases.compare: every channel must match
// the model on every pair (an empty mismatch signature).
func compareChannels(t *testing.T, op string, width int, pairs [][2]uint64, result differentialResult) {
	t.Helper()
	for i, p := range pairs {
		want := bitwiseModel(op, width, p[0], p[1])
		if result.native[i] != want || result.raw[i] != want || result.typed[i] != want {
			t.Fatalf("UInt%d %s (%d, %d): native %d, raw %d, typed %d, model %d", width, op, p[0], p[1], result.native[i], result.raw[i], result.typed[i], want)
		}
	}
}

func requireDifferential(t *testing.T) {
	t.Helper()
	if os.Getenv("LEAN_GO_DIFFERENTIAL") != "1" {
		t.Skip("set LEAN_GO_DIFFERENTIAL=1 for native Lean comparisons")
	}
}

func alternating(width int) uint64 {
	var v uint64
	for i := 0; i < width; i += 2 {
		v |= 1 << i
	}
	return v
}

func TestBitwiseNativeAllPrimitivesAtWidthBoundaries(t *testing.T) {
	requireDifferential(t)
	cache := &pipeline.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	for _, width := range []int{32, 64} {
		maximum := uint64(1)<<(width-1)<<1 - 1
		w := uint64(width)
		values := []uint64{0, maximum, 1 << (width - 1), alternating(width), maximum - alternating(width)}
		var pairs [][2]uint64
		for _, a := range values {
			for _, b := range []uint64{0, w - 1, w, w + 1, maximum} {
				pairs = append(pairs, [2]uint64{a, b})
			}
		}
		for _, a := range values {
			for _, b := range values {
				pairs = append(pairs, [2]uint64{a, b})
			}
		}
		for _, op := range []string{"land", "lor", "xor", "complement", "shr"} {
			t.Run(fmt.Sprintf("%d/%s", width, op), func(t *testing.T) {
				compareChannels(t, op, width, pairs, differentialCheck(t, cache, renderBitwise(op, width), width, pairs))
			})
		}
	}
}

func TestShiftLeftNativeNotationAndDirectCallsMatchNativeRawTypedAndArithmetic(t *testing.T) {
	requireDifferential(t)
	cache := &pipeline.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	for _, width := range []int{32, 64} {
		maximum := uint64(1)<<(width-1)<<1 - 1
		w := uint64(width)
		values := []uint64{0, 1, maximum, 1 << (width - 1), alternating(width), maximum - alternating(width)}
		var pairs [][2]uint64
		for _, a := range values {
			for _, b := range []uint64{0, w - 1, w, w + 1, maximum} {
				pairs = append(pairs, [2]uint64{a, b})
			}
		}
		// A fixed seed per width; Python's Mersenne Twister draws differ.
		rng := rand.New(rand.NewPCG(uint64(3072+width), 0))
		draw := func() uint64 {
			if width == 64 {
				return rng.Uint64()
			}
			return rng.Uint64N(maximum + 1)
		}
		for range 32 {
			pairs = append(pairs, [2]uint64{draw(), draw()})
		}
		source := renderBitwise("shl", width)
		var results []differentialResult
		for _, direct := range []bool{false, true} {
			rendered := source
			if direct {
				rendered = strings.Replace(source, "(a <<< b)", fmt.Sprintf("(UInt%d.shiftLeft a b)", width), -1)
				if rendered == source {
					t.Fatal("direct call not rendered")
				}
			}
			result := differentialCheck(t, cache, rendered, width, pairs)
			compareChannels(t, "shl", width, pairs, result)
			var primitives []string
			for _, function := range result.ir["functions"].([]any) {
				primitives = append(primitives, irPrimitives(function.(map[string]any)["body"])...)
			}
			for _, p := range primitives {
				if p != fmt.Sprintf("UInt%d.shiftLeft", width) {
					t.Fatalf("emitted primitive %s", p)
				}
			}
			if len(primitives) == 0 {
				t.Fatal("no shift-left leaf emitted")
			}
			if result.ir["schema"] != "lean-go-ir/v24" || result.ir["profile"] != "pure-module/v22" {
				t.Fatal("schema or profile")
			}
			bitwise := false
			for _, feature := range result.ir["features"].([]any) {
				bitwise = bitwise || feature == "bitwise"
			}
			if !bitwise {
				t.Fatal("bitwise feature missing")
			}
			results = append(results, result)
		}
		if !reflect.DeepEqual(results[0].native, results[1].native) || !reflect.DeepEqual(results[0].raw, results[1].raw) || !reflect.DeepEqual(results[0].typed, results[1].typed) {
			t.Fatal("notation and direct calls differ")
		}
	}
}

// irPrimitives lists the primitive leaves of a body: lets, joins and cases.
func irPrimitives(code any) []string {
	node := code.(map[string]any)
	var out []string
	switch node["op"] {
	case "let":
		if value := node["value"].(map[string]any); value["kind"] == "primitive" {
			out = append(out, value["name"].(string))
		}
		out = append(out, irPrimitives(node["next"])...)
	case "join":
		out = append(out, irPrimitives(node["join"].(map[string]any)["body"])...)
		out = append(out, irPrimitives(node["next"])...)
	case "cases":
		for _, alt := range node["alts"].([]any) {
			out = append(out, irPrimitives(alt.(map[string]any)["body"])...)
		}
	}
	return out
}
