package nativegen

import (
	"fmt"
	"strings"
)

// Unsigned scalar widths in Lean and their Go representations.
var scalarWidths = map[string]int{"UInt8": 8, "UInt16": 16, "UInt32": 32, "UInt64": 64}

func scalarGo(lean string) string { return fmt.Sprintf("uint%d", scalarWidths[lean]) }

func mask(bits int) uint64 {
	if bits == 64 {
		return ^uint64(0)
	}
	return 1<<bits - 1
}

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

// expr is a Go expression for an IR value. Constants are always written as a
// typed literal such as uint32(7), never as an operator expression: Go
// rejects constant expressions that overflow or divide by zero, while Lean
// wraps and defines x / 0.
type expr struct {
	text     string
	atomic   bool // needs no parentheses as an operand
	constant bool
	value    uint64 // for constants; Bool constants use 0 and 1
}

func (e expr) operand() string {
	if e.atomic {
		return e.text
	}
	return "(" + e.text + ")"
}

func literal(lean string, value uint64) expr {
	if lean == "Bool" {
		return expr{text: fmt.Sprint(value == 1), atomic: true, constant: true, value: value}
	}
	return expr{text: fmt.Sprintf("%s(%d)", scalarGo(lean), value), atomic: true, constant: true, value: value}
}

// foldPrimitive evaluates a primitive on constants with Lean's semantics.
func foldPrimitive(name string, args []uint64) uint64 {
	width, operation, _ := strings.Cut(name, ".")
	bits := scalarWidths[width]
	m := mask(bits)
	a, b := args[0], uint64(0)
	if len(args) > 1 {
		b = args[1]
	}
	truth := func(ok bool) uint64 {
		if ok {
			return 1
		}
		return 0
	}
	switch operation {
	case "add":
		return (a + b) & m
	case "sub":
		return (a - b) & m
	case "mul":
		return (a * b) & m
	case "div":
		if b == 0 {
			return 0
		}
		return a / b
	case "mod":
		if b == 0 {
			return a
		}
		return a % b
	case "land":
		return a & b
	case "lor":
		return a | b
	case "xor":
		return a ^ b
	case "complement":
		return ^a & m
	case "shiftLeft":
		return (a << (b % uint64(bits))) & m
	case "shiftRight":
		return a >> (b % uint64(bits))
	case "decEq":
		return truth(a == b)
	case "decLt":
		return truth(a < b)
	case "decLe":
		return truth(a <= b)
	}
	return a & mask(scalarWidths[strings.TrimPrefix(operation, "to")])
}

// primitiveExpr applies a primitive to operand expressions. It returns
// ok=false only for division or remainder by a non-constant divisor, which
// needs the statements of primitiveStatements. Every expression is total:
// shift counts are reduced modulo the width before Go shifts, because Go
// yields zero for counts at or above the width.
func primitiveExpr(name string, args []expr) (expr, bool, error) {
	primitive, known := LookupPrimitive(name)
	if !known || len(args) != len(primitive.Params) {
		return expr{}, false, fmt.Errorf("native mode does not implement primitive %s", name)
	}
	constant := true
	values := make([]uint64, len(args))
	for i, arg := range args {
		constant = constant && arg.constant
		values[i] = arg.value
	}
	if constant {
		return literal(primitive.Result, foldPrimitive(name, values)), true, nil
	}
	width, operation, _ := strings.Cut(name, ".")
	bits := scalarWidths[width]
	binary := func(symbol string) expr {
		return expr{text: args[0].operand() + " " + symbol + " " + args[1].operand()}
	}
	switch operation {
	case "add", "sub", "mul", "land", "lor", "xor":
		return binary(binaryOperators[operation]), true, nil
	case "decEq", "decLt", "decLe":
		return binary(comparisons[operation]), true, nil
	case "complement":
		return expr{text: "^" + args[0].operand()}, true, nil
	case "shiftLeft", "shiftRight":
		symbol := map[string]string{"shiftLeft": "<<", "shiftRight": ">>"}[operation]
		count := fmt.Sprintf("(%s & %d)", args[1].operand(), bits-1)
		if args[1].constant {
			count = fmt.Sprint(args[1].value % uint64(bits))
		}
		return expr{text: args[0].operand() + " " + symbol + " " + count}, true, nil
	case "div", "mod":
		if !args[1].constant {
			return expr{}, false, nil
		}
		if args[1].value == 0 {
			if operation == "div" {
				return literal(width, 0), true, nil
			}
			return args[0], true, nil
		}
		return binary(map[string]string{"div": "/", "mod": "%"}[operation]), true, nil
	}
	return expr{text: scalarGo(primitive.Result) + "(" + args[0].text + ")", atomic: true}, true, nil
}

// primitiveStatements binds dst to the primitive applied to args. Division
// and remainder by a value that may be zero test the divisor first, matching
// Lean's x / 0 = 0 and x % 0 = x where Go would panic.
func primitiveStatements(dst, name string, args []expr) (string, error) {
	e, ok, err := primitiveExpr(name, args)
	if err != nil {
		return "", err
	}
	if ok {
		return fmt.Sprintf("%s := %s\n", dst, e.text), nil
	}
	width, operation, _ := strings.Cut(name, ".")
	a, b := args[0].operand(), args[1].operand()
	if operation == "div" {
		return fmt.Sprintf("var %s %s\nif %s != 0 {\n%s = %s / %s\n}\n", dst, scalarGo(width), b, dst, a, b), nil
	}
	return fmt.Sprintf("%s := %s\nif %s != 0 {\n%s = %s %% %s\n}\n", dst, args[0].text, b, dst, a, b), nil
}
