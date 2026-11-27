package integration_test

import (
	"math/big"
	"reflect"
	"testing"

	unsigned "github.com/franchb/lean-go/generated/unsigned"
)

// unsignedModel is Lean's definition of every operation and conversion of one
// width on natural numbers, independent of Go's fixed-width operators and its
// panicking zero divisor. The order is that of the Unsigned.OpsN records.
func unsignedModel(bits uint, a, b uint64) []uint64 {
	x, y := new(big.Int).SetUint64(a), new(big.Int).SetUint64(b)
	modulus := func(bits uint) *big.Int { return new(big.Int).Lsh(big.NewInt(1), bits) }
	m := modulus(bits)
	reduce := func(v *big.Int) uint64 { return new(big.Int).Mod(v, m).Uint64() }
	power := new(big.Int).Lsh(big.NewInt(1), uint(b%uint64(bits)))
	quotient, remainder := uint64(0), a
	if b != 0 {
		quotient, remainder = a/b, a%b
	}
	truth := func(ok bool) uint64 {
		if ok {
			return 1
		}
		return 0
	}
	values := []uint64{
		reduce(new(big.Int).Add(x, y)), reduce(new(big.Int).Sub(x, y)), reduce(new(big.Int).Mul(x, y)),
		quotient, remainder, a & b, a | b, a ^ b, reduce(new(big.Int).Sub(new(big.Int).Sub(m, big.NewInt(1)), x)),
		reduce(new(big.Int).Mul(x, power)), new(big.Int).Quo(x, power).Uint64(),
		truth(a == b), truth(a < b), truth(a <= b),
	}
	for _, target := range []uint{8, 16, 32, 64} {
		if target != bits {
			values = append(values, new(big.Int).Mod(x, modulus(target)).Uint64())
		}
	}
	return values
}

func unsignedOperands(bits uint) []uint64 {
	maximum := uint64(1)<<(bits-1)<<1 - 1
	values := []uint64{0, 1, 2, 3, 7, uint64(bits - 1), uint64(bits), uint64(bits + 1), 2 * uint64(bits),
		255 & maximum, maximum, maximum - 1, maximum >> 1, maximum>>1 + 1}
	for k := uint(0); k < bits; k += 7 {
		values = append(values, uint64(1)<<k)
	}
	return values
}

// fieldValues reads every accessor of a generated record in declaration order.
func fieldValues(t *testing.T, record reflect.Value, names []string) []uint64 {
	t.Helper()
	var values []uint64
	for _, name := range names {
		method := record.MethodByName(name)
		if !method.IsValid() {
			continue
		}
		out := method.Call(nil)
		if !out[1].Bool() {
			t.Fatalf("invalid field %s", name)
		}
		if v, ok := out[0].Interface().(bool); ok {
			values = append(values, map[bool]uint64{false: 0, true: 1}[v])
		} else {
			values = append(values, out[0].Convert(reflect.TypeFor[uint64]()).Uint())
		}
	}
	return values
}

func TestUnsignedMatrixFixture(t *testing.T) {
	l := unsigned.DefaultLimits()
	fields := []string{"Add", "Sub", "Mul", "Div", "Mod", "And", "Or", "Xor", "Not", "Shl", "Shr", "Eq", "Lt", "Le", "To8", "To16", "To32", "To64"}
	roots := map[uint]any{8: unsigned.RunOps8, 16: unsigned.RunOps16, 32: unsigned.RunOps32, 64: unsigned.RunOps64}
	for _, bits := range []uint{8, 16, 32, 64} {
		run := reflect.ValueOf(roots[bits])
		for _, a := range unsignedOperands(bits) {
			for _, b := range unsignedOperands(bits) {
				operand := func(n uint64) reflect.Value { return reflect.ValueOf(n).Convert(run.Type().In(0)) }
				out := run.Call([]reflect.Value{operand(a), operand(b), reflect.ValueOf(l)})
				if !out[1].IsNil() {
					t.Fatal(out[1].Interface())
				}
				got, want := fieldValues(t, out[0], fields), unsignedModel(bits, a, b)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("UInt%d (%d, %d): generated %v, model %v", bits, a, b, got, want)
				}
			}
		}
	}
	for _, a := range unsignedOperands(16) {
		r, err := unsigned.RunFolded(uint16(a), l)
		if err != nil {
			t.Fatal(err)
		}
		got := fieldValues(t, reflect.ValueOf(r), []string{"Quotient", "Remainder", "Scaled", "ByZero", "ModZero", "ByOne"})
		if want := []uint64{a / 16, a % 16, a * 8 % 65536, 0, a, a}; !reflect.DeepEqual(got, want) {
			t.Fatalf("folded(%d) = %v, want %v", a, got, want)
		}
	}
}
