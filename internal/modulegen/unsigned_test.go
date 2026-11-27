package modulegen

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

var unsignedWidths = []string{"UInt8", "UInt16", "UInt32", "UInt64"}

// unsignedMatrix lists the primitives whose operand is one of sources.
func unsignedMatrix(sources []string) []string {
	var names []string
	for _, width := range sources {
		for _, op := range []string{"add", "sub", "mul", "div", "mod", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight", "decEq", "decLt", "decLe"} {
			names = append(names, width+"."+op)
		}
		for _, target := range unsignedWidths {
			if target != width {
				names = append(names, width+".to"+target)
			}
		}
	}
	return names
}

func matrixRoot(name string) string { return "Example.p_" + strings.ReplaceAll(name, ".", "_") }

// unsignedMatrixProgram has one root per admitted primitive, plus division and
// remainder by a literal zero divisor, the form the pinned compiler keeps for
// `x / 0`.
func unsignedMatrixProgram(sources []string) *moduleir.Program {
	p := typedProgram()
	p.Types = []moduleir.TypeDecl{{Name: "Bool", Constructors: []moduleir.Constructor{
		{Name: "Bool.false", Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}}}
	p.Roots, p.Functions, p.Provenance = nil, nil, nil
	add := func(root string, params []moduleir.Param, result string, body *moduleir.Code) {
		p.Roots = append(p.Roots, root)
		p.Functions = append(p.Functions, moduleir.Function{Name: root, Params: params, Result: result, Body: body})
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: root, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	for _, name := range unsignedMatrix(sources) {
		primitive, _ := moduleir.LookupPrimitive(name)
		params := []moduleir.Param{{ID: 1, Type: primitive.Params[0]}, {ID: 2, Type: primitive.Params[0]}}[:len(primitive.Params)]
		add(matrixRoot(name), params, primitive.Result, &moduleir.Code{
			Op: "let", Bind: &moduleir.Param{ID: 3, Type: primitive.Result},
			Value: &moduleir.Value{Kind: "primitive", Name: name, Args: []int{1, 2}[:len(params)]},
			Next:  &moduleir.Code{Op: "return", Var: 3},
		})
	}
	for _, width := range sources {
		for _, op := range []string{"div", "mod"} {
			add(matrixRoot(width+"."+op+"Zero"), []moduleir.Param{{ID: 1, Type: width}}, width, &moduleir.Code{
				Op: "let", Bind: &moduleir.Param{ID: 2, Type: width},
				Value: &moduleir.Value{Kind: strings.ToLower(width), Name: "0", Args: []int{}},
				Next: &moduleir.Code{
					Op: "let", Bind: &moduleir.Param{ID: 3, Type: width},
					Value: &moduleir.Value{Kind: "primitive", Name: width + "." + op, Args: []int{1, 2}},
					Next:  &moduleir.Code{Op: "return", Var: 3},
				},
			})
		}
	}
	sort.Strings(p.Roots)
	sort.Slice(p.Provenance, func(i, j int) bool { return p.Provenance[i].Name < p.Provenance[j].Name })
	sort.Slice(p.Functions, func(i, j int) bool { return p.Functions[i].Name < p.Functions[j].Name })
	return p
}

// A package admits at most 64 roots, so the matrix runs in two packages.
func TestGeneratedUnsignedMatrix(t *testing.T) {
	for _, sources := range [][]string{{"UInt8", "UInt16"}, {"UInt32", "UInt64"}} {
		t.Run(strings.Join(sources, "_"), func(t *testing.T) { checkUnsignedMatrix(t, sources) })
	}
}

func checkUnsignedMatrix(t *testing.T, sources []string) {
	files, err := generate(unsignedMatrixProgram(sources))
	if err != nil {
		t.Fatal(err)
	}
	api := string(files["api.go"])
	for _, width := range sources {
		// Every typed root uses the scalar-native path with the zero-divisor guard.
		root := exportedAPIName(matrixRoot(width + ".div"))
		if !strings.Contains(api, "func Run"+root+"(") || !strings.Contains(api, "func scalarFunc") {
			t.Fatal("missing typed root", root)
		}
	}
	// Two width-specific division and remainder roots plus their zero-divisor forms.
	if n := len(regexp.MustCompile(`if v[23] != 0 \{`).FindAllString(api, -1)); n != 4*len(sources) {
		t.Fatal("scalar division without a zero-divisor guard", n)
	}
	if n := len(regexp.MustCompile(`if f\.slots\[[0-9]+\]\.scalar[0-9]* != 0 \{`).FindAllString(string(files["control.go"]), -1)); n != 4*len(sources) {
		t.Fatal("boxed division without a zero-divisor guard", n)
	}
	var cases strings.Builder
	for _, name := range unsignedMatrix(sources) {
		primitive, _ := moduleir.LookupPrimitive(name)
		width, operation, _ := strings.Cut(name, ".")
		fmt.Fprintf(&cases, "{%q, %q, %d, %d, %t, func(a, b uint64, l Limits) (uint64, error) { r, err := Run%s(%s(a), ",
			matrixRoot(name), operation, widthBits(width), widthBits(primitive.Result), primitive.Result == "Bool",
			exportedAPIName(matrixRoot(name)), strings.ToLower(width))
		if len(primitive.Params) == 2 {
			fmt.Fprintf(&cases, "%s(b), ", strings.ToLower(width))
		}
		if primitive.Result == "Bool" {
			cases.WriteString("l); return truth(r), err }},\n")
		} else {
			cases.WriteString("l); return uint64(r), err }},\n")
		}
	}
	for _, width := range sources {
		for _, op := range []string{"div", "mod"} {
			root := matrixRoot(width + "." + op + "Zero")
			fmt.Fprintf(&cases, "{%q, %q, %d, %d, false, func(a, _ uint64, l Limits) (uint64, error) { r, err := Run%s(%s(a), l); return uint64(r), err }},\n",
				root, op+"Zero", widthBits(width), widthBits(width), exportedAPIName(root), strings.ToLower(width))
		}
	}
	source := strings.Replace(unsignedConsumer, "// CASES\n", cases.String(), 1)
	if sources[0] == "UInt32" {
		source += divisionResources
	}
	runGenerated(t, files, source)
}

func widthBits(typ string) int {
	switch typ {
	case "UInt8":
		return 8
	case "UInt16":
		return 16
	case "UInt32":
		return 32
	case "UInt64":
		return 64
	}
	return 1
}

// unsignedConsumer compares both generated APIs with an arbitrary-precision
// model of Lean's definitions on boundary operands of every width.
const unsignedConsumer = `package extracted

import (
	"errors"
	"fmt"
	"math/big"
	"testing"
)

type matrixCase struct {
	root, op       string
	bits, result   int
	boolean        bool
	typed          func(a, b uint64, l Limits) (uint64, error)
}

var _ = errors.Is

func truth(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

var cases = []matrixCase{
// CASES
}

func modulus(bits int) *big.Int { return new(big.Int).Lsh(big.NewInt(1), uint(bits)) }

func reference(op string, bits, result int, a, b uint64) uint64 {
	x, y, m := new(big.Int).SetUint64(a), new(big.Int).SetUint64(b), modulus(bits)
	r := new(big.Int)
	switch op {
	case "add":
		r.Add(x, y)
	case "sub":
		r.Sub(x, y)
	case "mul":
		r.Mul(x, y)
	case "div":
		if y.Sign() != 0 {
			r.Quo(x, y)
		}
	case "mod":
		r.Set(x)
		if y.Sign() != 0 {
			r.Rem(x, y)
		}
	case "divZero":
	case "modZero":
		r.Set(x)
	case "land":
		r.And(x, y)
	case "lor":
		r.Or(x, y)
	case "xor":
		r.Xor(x, y)
	case "complement":
		r.Sub(new(big.Int).Sub(m, big.NewInt(1)), x)
	case "shiftLeft":
		r.Mul(x, new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(b%uint64(bits))), nil))
	case "shiftRight":
		r.Quo(x, new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(b%uint64(bits))), nil))
	case "decEq":
		return truth(x.Cmp(y) == 0)
	case "decLt":
		return truth(x.Cmp(y) < 0)
	case "decLe":
		return truth(x.Cmp(y) <= 0)
	default: // conversions keep the value modulo the target width
		r.Set(x)
		m = modulus(result)
	}
	return r.Mod(r, m).Uint64()
}

func operands(bits int) []uint64 {
	max := uint64(1)<<(bits-1)<<1 - 1
	values := []uint64{0, 1, 2, 3, 7, uint64(bits - 1), uint64(bits), uint64(bits + 1), uint64(2 * bits), max, max - 1, max >> 1, (max >> 1) + 1}
	for k := 0; k < bits; k += 5 {
		values = append(values, uint64(1)<<k, (uint64(1)<<k)-1)
	}
	return values
}

func importScalar(t *testing.T, bits int, v uint64) Value {
	t.Helper()
	in := &Input{}
	switch bits {
	case 8:
		in.Type, in.UInt8 = "UInt8", uint8(v)
	case 16:
		in.Type, in.UInt16 = "UInt16", uint16(v)
	case 32:
		in.Type, in.UInt32 = "UInt32", uint32(v)
	default:
		in.Type, in.UInt64 = "UInt64", v
	}
	value, err := Import(in, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func readRaw(c matrixCase, v Value) (uint64, error) {
	if c.boolean {
		switch v.Constructor() {
		case "Bool.true":
			return 1, nil
		case "Bool.false":
			return 0, nil
		}
		return 0, fmt.Errorf("not a Bool: %q", v.Constructor())
	}
	var value uint64
	ok := false
	switch c.result {
	case 8:
		var r uint8
		r, ok = v.UInt8()
		value = uint64(r)
	case 16:
		var r uint16
		r, ok = v.UInt16()
		value = uint64(r)
	case 32:
		var r uint32
		r, ok = v.UInt32()
		value = uint64(r)
	default:
		value, ok = v.UInt64()
	}
	if !ok || len(v.Fields()) != 0 {
		return 0, fmt.Errorf("invalid scalar result %s", v.Type())
	}
	return value, nil
}

func TestUnsignedMatrix(t *testing.T) {
	limits := DefaultLimits()
	for _, c := range cases {
		unary := c.op == "complement" || c.op == "divZero" || c.op == "modZero" || len(c.op) > 2 && c.op[:2] == "to"
		for _, a := range operands(c.bits) {
			for _, b := range operands(c.bits) {
				if unary && b != 0 {
					continue
				}
				want := reference(c.op, c.bits, c.result, a, b)
				got, err := c.typed(a, b, limits)
				if err != nil || got != want {
					t.Fatalf("typed %s(%d, %d) = %d, %v; want %d", c.root, a, b, got, err, want)
				}
				args := []Value{importScalar(t, c.bits, a)}
				if !unary {
					args = append(args, importScalar(t, c.bits, b))
				}
				raw, err := RunRoot(c.root, args, limits)
				if err != nil {
					t.Fatal(c.root, err)
				}
				if got, err := readRaw(c, raw); err != nil || got != want {
					t.Fatalf("raw %s(%d, %d) = %d, %v; want %d", c.root, a, b, got, err, want)
				}
			}
		}
	}
}
`

const divisionResources = `
// Division keeps the accounting of every other scalar primitive: one IR step
// for the let, one for the return and one logical allocation.
func TestDivisionResources(t *testing.T) {
	l := DefaultLimits()
	l.MaxAllocations, l.MaxSteps, l.MaxFrames, l.MaxSlots = 1, 2, 1, 3
	for _, divisor := range []uint32{0, 3} {
		if got, err := RunP_UInt32_div(7, divisor, l); err != nil || uint64(got) != reference("div", 32, 32, 7, uint64(divisor)) {
			t.Fatal(got, err)
		}
		args := []Value{importScalar(t, 32, 7), importScalar(t, 32, uint64(divisor))}
		if _, err := RunRoot("Example.p_UInt32_mod", args, l); err != nil {
			t.Fatal(err)
		}
		short := l
		short.MaxSteps--
		if _, err := RunP_UInt32_mod(7, divisor, short); !errors.Is(err, ErrResource) {
			t.Fatal(err)
		}
		if _, err := RunRoot("Example.p_UInt32_div", args, short); !errors.Is(err, ErrResource) {
			t.Fatal(err)
		}
	}
	// A literal zero divisor costs one more step and allocation.
	l.MaxAllocations, l.MaxSteps = 2, 3
	arg := []Value{importScalar(t, 32, 7)}
	if got, err := RunP_UInt32_divZero(7, l); err != nil || got != 0 {
		t.Fatal(got, err)
	}
	if raw, err := RunRoot("Example.p_UInt32_modZero", arg, l); err != nil {
		t.Fatal(err)
	} else if got, _ := raw.UInt32(); got != 7 {
		t.Fatal(got)
	}
	allocations, steps := l, l
	allocations.MaxAllocations--
	steps.MaxSteps--
	for _, short := range []Limits{allocations, steps} {
		if got, err := RunP_UInt32_modZero(7, short); !errors.Is(err, ErrResource) || got != 0 {
			t.Fatal(got, err)
		}
		if _, err := RunRoot("Example.p_UInt32_divZero", arg, short); !errors.Is(err, ErrResource) {
			t.Fatal(err)
		}
	}
}
`
