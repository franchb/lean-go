package modulegen

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

func (g *generator) emitNumeric(w *bytes.Buffer, f *function, c *moduleir.Code) {
	expression := c.Value.Name
	result := c.Bind.Type
	if c.Value.Kind == "primitive" {
		primitive, _ := moduleir.LookupPrimitive(c.Value.Name)
		result = primitive.Result
		field := scalarField(primitive.Params[0])
		left := fmt.Sprintf("f.slots[%d].%s", f.slots[c.Value.Args[0]], field)
		right := ""
		if len(primitive.Params) == 2 {
			right = fmt.Sprintf("f.slots[%d].%s", f.slots[c.Value.Args[1]], field)
		}
		if division(c.Value.Name) {
			// Go panics on a zero divisor; Lean defines x / 0 = 0 and x % 0 = x.
			fmt.Fprintf(w, "value,err:=b.make(%d,0,nil);if err!=nil{return nil,err};%s\n", g.types[result],
				divisionStatement(c.Value.Name, "value."+scalarField(result), left, right, false, ""))
			fmt.Fprintf(w, "f.slots[%d]=value;f.pc=%d\n", f.slots[c.Bind.ID], f.states[c.Next])
			return
		}
		expression = primitiveExpression(c.Value.Name, left, right)
	}
	if result == "Bool" {
		fmt.Fprintf(w, "tag:=0;if %s{tag=1};value,err:=b.make(%d,tag,nil);if err!=nil{return nil,err}\n", expression, g.types["Bool"])
	} else {
		fmt.Fprintf(w, "value,err:=b.make(%d,0,nil);if err!=nil{return nil,err};value.%s=%s\n", g.types[result], scalarField(result), expression)
	}
	fmt.Fprintf(w, "f.slots[%d]=value;f.pc=%d\n", f.slots[c.Bind.ID], f.states[c.Next])
}

var shiftMasks = map[string]string{"UInt8": "7", "UInt16": "15", "UInt32": "31", "UInt64": "63"}
var primitiveOperators = map[string]string{"add": "+", "sub": "-", "mul": "*", "land": "&", "lor": "|", "xor": "^",
	"decEq": "==", "decLt": "<", "decLe": "<="}

// division reports whether a primitive needs the zero-divisor guard of divisionStatement.
func division(name string) bool {
	_, operation, _ := strings.Cut(name, ".")
	return operation == "div" || operation == "mod"
}

// primitiveExpression renders every primitive except division and remainder
// as a Go expression on operands of the primitive's width. Shift counts are
// masked to the width, as in Lean; conversions truncate or zero-extend.
func primitiveExpression(name, left, right string) string {
	primitive, _ := moduleir.LookupPrimitive(name)
	width, operation, _ := strings.Cut(name, ".")
	switch operation {
	case "shiftLeft":
		return left + " << (" + right + " & " + shiftMasks[width] + ")"
	case "shiftRight":
		return left + " >> (" + right + " & " + shiftMasks[width] + ")"
	case "complement":
		return "^" + left
	}
	if operator, ok := primitiveOperators[operation]; ok {
		return left + " " + operator + " " + right
	}
	return strings.ToLower(primitive.Result) + "(" + left + ")"
}

// divisionStatement assigns Lean's division or remainder to dst. With declare,
// it declares dst with the given Go type; otherwise dst is a zero-valued field.
func divisionStatement(name, dst, left, right string, declare bool, goType string) string {
	_, operation, _ := strings.Cut(name, ".")
	if operation == "div" {
		prefix := ""
		if declare {
			prefix = "var " + dst + " " + goType + ";"
		}
		return prefix + "if " + right + "!=0{" + dst + "=" + left + "/" + right + "}"
	}
	assign := "="
	if declare {
		assign = ":="
	}
	return dst + assign + left + ";if " + right + "!=0{" + dst + "=" + left + "%" + right + "}"
}

func (g *generator) primitiveCatalog() []moduleir.Primitive {
	used := map[string]bool{}
	for _, f := range g.functions {
		for _, c := range f.nodes {
			if c.Value != nil && c.Value.Kind == "primitive" {
				used[c.Value.Name] = true
			}
		}
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	primitives := make([]moduleir.Primitive, 0, len(names))
	for _, name := range names {
		primitive, _ := moduleir.LookupPrimitive(name)
		primitives = append(primitives, primitive)
	}
	return primitives
}

func scalarField(typ string) string {
	if typ == "UInt8" {
		return "scalar8"
	}
	if typ == "UInt16" {
		return "scalar16"
	}
	if typ == "UInt64" {
		return "scalar64"
	}
	return "scalar"
}
