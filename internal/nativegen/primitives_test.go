package nativegen

import (
	"fmt"
	"strings"
	"testing"
)

func TestPrimitiveMatrixComplete(t *testing.T) {
	names := AllPrimitives()
	// Four widths times fourteen operations, plus twelve conversions.
	if len(names) != 4*14+12 {
		t.Fatalf("%d primitives", len(names))
	}
	for _, name := range names {
		p, ok := LookupPrimitive(name)
		if !ok || p.Name != name || p.Semantics == "" {
			t.Fatalf("%s: %+v", name, p)
		}
		if _, err := primitiveStatements("v3", name, []string{"v1", "v2"}[:len(p.Params)]); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"UInt8.toUInt8", "UInt128.add", "UInt32.pow", "Int32.add", "UInt32", "UInt32.toNat"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatalf("%s should be unknown", name)
		}
	}
}

// TestPrimitiveSemantics compiles every primitive through the emitter's own
// statement templates and compares each with an arbitrary-precision model of
// the Lean definition on boundary operands, including division and remainder
// by zero and shift counts at and above the width, plus seeded random pairs.
func TestPrimitiveSemantics(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated package")
	}
	var impl, dispatch strings.Builder
	impl.WriteString("package prims\n\n")
	dispatch.WriteString("func call(name string, a, b uint64) uint64 {\nswitch name {\n")
	for _, name := range AllPrimitives() {
		p, _ := LookupPrimitive(name)
		fn := strings.ReplaceAll(name, ".", "_")
		goParams := []string{"v1 " + scalarGo(p.Params[0])}
		callArgs := []string{scalarGo(p.Params[0]) + "(a)"}
		if len(p.Params) == 2 {
			goParams = append(goParams, "v2 "+scalarGo(p.Params[1]))
			callArgs = append(callArgs, scalarGo(p.Params[1])+"(b)")
		}
		result := "bool"
		convert := "boolWord"
		if p.Result != "Bool" {
			result, convert = scalarGo(p.Result), "uint64"
		}
		body, err := primitiveStatements("v3", name, []string{"v1", "v2"}[:len(p.Params)])
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&impl, "func %s(%s) %s {\n%sreturn v3\n}\n\n", fn, strings.Join(goParams, ", "), result, body)
		fmt.Fprintf(&dispatch, "case %q:\nreturn %s(%s(%s))\n", name, convert, fn, strings.Join(callArgs, ", "))
	}
	dispatch.WriteString("}\npanic(name)\n}\n")
	var names strings.Builder
	for _, name := range AllPrimitives() {
		fmt.Fprintf(&names, "%q,\n", name)
	}
	test := strings.NewReplacer("DISPATCH", dispatch.String(), "NAMES", names.String()).Replace(primitiveTestSource)
	dir := t.TempDir()
	writePackage(t, dir, map[string][]byte{"go.mod": []byte("module prims\n\ngo 1.27\n"), "prims.go": []byte(impl.String()), "prims_test.go": []byte(test)})
	goCommand(t, dir, "vet", ".")
	t.Log(goCommand(t, dir, "test", "-count=1", "-v", "."))
}

// The reference model follows the Lean definitions over natural numbers:
// arithmetic modulo 2^w, shift counts modulo w, x / 0 = 0, x % 0 = x, and
// conversions modulo 2^target.
const primitiveTestSource = `package prims

import (
	"math/big"
	"math/rand"
	"strings"
	"testing"
)

func boolWord(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

DISPATCH

var names = []string{
NAMES
}

func width(s string) uint {
	switch s {
	case "UInt8":
		return 8
	case "UInt16":
		return 16
	case "UInt32":
		return 32
	}
	return 64
}

func model(name string, a, b uint64) uint64 {
	typ, op, _ := strings.Cut(name, ".")
	w := width(typ)
	modulus := new(big.Int).Lsh(big.NewInt(1), w)
	x, y := new(big.Int).SetUint64(a), new(big.Int).SetUint64(b)
	wrap := func(v *big.Int) uint64 { return new(big.Int).Mod(v, modulus).Uint64() }
	count := new(big.Int).Mod(y, big.NewInt(int64(w))).Uint64()
	switch op {
	case "add":
		return wrap(new(big.Int).Add(x, y))
	case "sub":
		return wrap(new(big.Int).Sub(x, y))
	case "mul":
		return wrap(new(big.Int).Mul(x, y))
	case "div":
		if y.Sign() == 0 {
			return 0
		}
		return new(big.Int).Quo(x, y).Uint64()
	case "mod":
		if y.Sign() == 0 {
			return a
		}
		return new(big.Int).Rem(x, y).Uint64()
	case "land":
		return new(big.Int).And(x, y).Uint64()
	case "lor":
		return new(big.Int).Or(x, y).Uint64()
	case "xor":
		return new(big.Int).Xor(x, y).Uint64()
	case "complement":
		return new(big.Int).Sub(new(big.Int).Sub(modulus, big.NewInt(1)), x).Uint64()
	case "shiftLeft":
		return wrap(new(big.Int).Lsh(x, uint(count)))
	case "shiftRight":
		return new(big.Int).Rsh(x, uint(count)).Uint64()
	case "decEq":
		return boolWord(x.Cmp(y) == 0)
	case "decLt":
		return boolWord(x.Cmp(y) < 0)
	case "decLe":
		return boolWord(x.Cmp(y) <= 0)
	}
	target := width(strings.TrimPrefix(op, "to"))
	return new(big.Int).Mod(x, new(big.Int).Lsh(big.NewInt(1), target)).Uint64()
}

func operands(w uint, r *rand.Rand) []uint64 {
	max := uint64(1)<<w - 1
	if w == 64 {
		max = ^uint64(0)
	}
	values := []uint64{0, 1, 2, 3, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 127, 128, 255, 256, 1 << (w - 1), 1<<(w-1) - 1, max, max - 1, max / 3}
	for range 40 {
		values = append(values, r.Uint64())
	}
	out := values[:0]
	for _, v := range values {
		out = append(out, v&max)
	}
	return out
}

func TestMatrix(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	checked := 0
	for _, name := range names {
		typ, _, _ := strings.Cut(name, ".")
		ops := operands(width(typ), r)
		for _, a := range ops {
			for _, b := range ops {
				if got, want := call(name, a, b), model(name, a, b); got != want {
					t.Fatalf("%s(%d, %d) = %d, want %d", name, a, b, got, want)
				}
				checked++
			}
		}
	}
	t.Logf("checked %d primitive applications", checked)
}

func TestByZeroAndWideShifts(t *testing.T) {
	cases := []struct {
		name    string
		a, b, z uint64
	}{
		{"UInt8.div", 200, 0, 0}, {"UInt16.div", 65535, 0, 0}, {"UInt32.div", 7, 0, 0}, {"UInt64.div", 1 << 63, 0, 0},
		{"UInt8.mod", 200, 0, 200}, {"UInt16.mod", 65535, 0, 65535}, {"UInt32.mod", 7, 0, 7}, {"UInt64.mod", 1 << 63, 0, 1 << 63},
		{"UInt8.shiftLeft", 1, 8, 1}, {"UInt8.shiftLeft", 1, 9, 2}, {"UInt8.shiftRight", 128, 15, 1},
		{"UInt16.shiftLeft", 1, 16, 1}, {"UInt16.shiftRight", 0x8000, 31, 1},
		{"UInt32.shiftLeft", 1, 32, 1}, {"UInt32.shiftLeft", 1, 33, 2}, {"UInt32.shiftRight", 1 << 31, 63, 1},
		{"UInt64.shiftLeft", 1, 64, 1}, {"UInt64.shiftLeft", 1, 65, 2}, {"UInt64.shiftRight", 1 << 63, 127, 1},
		{"UInt8.sub", 0, 1, 255}, {"UInt64.mul", 1 << 63, 2, 0}, {"UInt32.complement", 0, 0, 1<<32 - 1},
		{"UInt64.toUInt8", 0x1ff, 0, 0xff}, {"UInt8.toUInt64", 0xff, 0, 0xff},
	}
	for _, c := range cases {
		if got := call(c.name, c.a, c.b); got != c.z {
			t.Errorf("%s(%d, %d) = %d, want %d", c.name, c.a, c.b, got, c.z)
		}
	}
}
`
