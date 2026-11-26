package nativegen

import (
	"fmt"
	"strings"
)

// Unsigned scalar widths in Lean and their Go representations.
var scalarWidths = map[string]int{"UInt8": 8, "UInt16": 16, "UInt32": 32, "UInt64": 64}

func scalarGo(lean string) string { return fmt.Sprintf("uint%d", scalarWidths[lean]) }

// Primitive is one entry of the native primitive matrix, using the Lean
// declaration names of producer/LeanGo/Module/Primitives.lean.
type Primitive struct {
	Name      string   `json:"name"`
	Params    []string `json:"params"`
	Result    string   `json:"result"`
	Semantics string   `json:"semantics"`
}

var binaryOperators = map[string]string{"add": "+", "sub": "-", "mul": "*", "land": "&", "lor": "|", "xor": "^"}
var comparisons = map[string]string{"decEq": "==", "decLt": "<", "decLe": "<="}

// LookupPrimitive describes every unsigned primitive the native emitter
// implements: arithmetic, bitwise operations, shifts, division, remainder and
// comparisons for UInt8, UInt16, UInt32 and UInt64, and all twelve width
// conversions between them. The validator may admit only part of this matrix.
func LookupPrimitive(name string) (Primitive, bool) {
	width, operation, ok := strings.Cut(name, ".")
	bits, scalar := scalarWidths[width]
	if !ok || !scalar {
		return Primitive{}, false
	}
	n := fmt.Sprint(bits)
	two := []string{width, width}
	switch operation {
	case "add", "sub", "mul":
		symbol := binaryOperators[operation]
		return Primitive{name, two, width, "(a " + symbol + " b) modulo 2^" + n}, true
	case "div":
		return Primitive{name, two, width, "floor(a / b), and 0 when b = 0"}, true
	case "mod":
		return Primitive{name, two, width, "a modulo b, and a when b = 0"}, true
	case "land":
		return Primitive{name, two, width, "bitwise AND of " + n + "-bit operands"}, true
	case "lor":
		return Primitive{name, two, width, "bitwise OR of " + n + "-bit operands"}, true
	case "xor":
		return Primitive{name, two, width, "bitwise XOR of " + n + "-bit operands"}, true
	case "complement":
		return Primitive{name, []string{width}, width, "(2^" + n + " - 1) - a"}, true
	case "shiftLeft":
		return Primitive{name, two, width, "(a * 2^(b modulo " + n + ")) modulo 2^" + n}, true
	case "shiftRight":
		return Primitive{name, two, width, "floor(a / 2^(b modulo " + n + "))"}, true
	case "decEq":
		return Primitive{name, two, "Bool", "unsigned a == b"}, true
	case "decLt":
		return Primitive{name, two, "Bool", "unsigned a < b"}, true
	case "decLe":
		return Primitive{name, two, "Bool", "unsigned a <= b"}, true
	}
	target, conversion := strings.CutPrefix(operation, "to")
	if targetBits, known := scalarWidths[target]; conversion && known && target != width {
		semantics := "zero extension: a"
		if targetBits < bits {
			semantics = fmt.Sprintf("a modulo 2^%d", targetBits)
		}
		return Primitive{name, []string{width}, target, semantics}, true
	}
	return Primitive{}, false
}

// AllPrimitives lists the complete matrix in a stable order.
func AllPrimitives() []string {
	var names []string
	for _, width := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
		for _, op := range []string{"add", "sub", "mul", "div", "mod", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight", "decEq", "decLt", "decLe"} {
			names = append(names, width+"."+op)
		}
		for _, target := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
			if target != width {
				names = append(names, width+".to"+target)
			}
		}
	}
	return names
}

// primitiveStatements binds dst to the primitive applied to args. Every
// statement is total: Go would panic on division by zero, so both division
// and remainder test the divisor, matching Lean's x / 0 = 0 and x % 0 = x.
// Go shifts by counts at or above the width yield zero; Lean reduces the
// count modulo the width, so the count is masked first.
func primitiveStatements(dst, name string, args []string) (string, error) {
	primitive, ok := LookupPrimitive(name)
	if !ok || len(args) != len(primitive.Params) {
		return "", fmt.Errorf("native mode does not implement primitive %s", name)
	}
	width, operation, _ := strings.Cut(name, ".")
	goType := scalarGo(width)
	switch operation {
	case "add", "sub", "mul", "land", "lor", "xor":
		return fmt.Sprintf("%s := %s %s %s\n", dst, args[0], binaryOperators[operation], args[1]), nil
	case "decEq", "decLt", "decLe":
		return fmt.Sprintf("%s := %s %s %s\n", dst, args[0], comparisons[operation], args[1]), nil
	case "complement":
		return fmt.Sprintf("%s := ^%s\n", dst, args[0]), nil
	case "shiftLeft":
		return fmt.Sprintf("%s := %s << (%s & %d)\n", dst, args[0], args[1], scalarWidths[width]-1), nil
	case "shiftRight":
		return fmt.Sprintf("%s := %s >> (%s & %d)\n", dst, args[0], args[1], scalarWidths[width]-1), nil
	case "div":
		return fmt.Sprintf("var %s %s\nif %s != 0 {\n%s = %s / %s\n}\n", dst, goType, args[1], dst, args[0], args[1]), nil
	case "mod":
		return fmt.Sprintf("%s := %s\nif %s != 0 {\n%s = %s %% %s\n}\n", dst, args[0], args[1], dst, args[0], args[1]), nil
	}
	return fmt.Sprintf("%s := %s(%s)\n", dst, scalarGo(primitive.Result), args[0]), nil
}
