package modulegen

import (
	"bytes"
	"fmt"
	"sort"

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
		expression = primitiveExpression(c.Value.Name, left, right)
	}
	if result == "Bool" {
		fmt.Fprintf(w, "tag:=0;if %s{tag=1};value,err:=b.make(%d,tag,nil);if err!=nil{return nil,err}\n", expression, g.types["Bool"])
	} else {
		fmt.Fprintf(w, "value,err:=b.make(%d,0,nil);if err!=nil{return nil,err};value.%s=%s\n", g.types[result], scalarField(result), expression)
	}
	fmt.Fprintf(w, "f.slots[%d]=value;f.pc=%d\n", f.slots[c.Bind.ID], f.states[c.Next])
}

func primitiveExpression(name, left, right string) string {
	operator := ""
	switch name {
	case "UInt8.toUInt32":
		return "uint32(" + left + ")"
	case "UInt32.toUInt8":
		return "uint8(" + left + ")"
	case "UInt32.toUInt64":
		return "uint64(" + left + ")"
	case "UInt64.toUInt32":
		return "uint32(" + left + ")"
	case "UInt8.add", "UInt32.add", "UInt64.add":
		operator = "+"
	case "UInt8.sub", "UInt32.sub", "UInt64.sub":
		operator = "-"
	case "UInt8.mul", "UInt32.mul", "UInt64.mul":
		operator = "*"
	case "UInt8.shiftLeft":
		return left + " << (" + right + " & 7)"
	case "UInt32.shiftLeft":
		return left + " << (" + right + " & 31)"
	case "UInt64.shiftLeft":
		return left + " << (" + right + " & 63)"
	case "UInt8.land", "UInt32.land", "UInt64.land":
		operator = "&"
	case "UInt8.lor", "UInt32.lor", "UInt64.lor":
		operator = "|"
	case "UInt8.xor", "UInt32.xor", "UInt64.xor":
		operator = "^"
	case "UInt8.complement", "UInt32.complement", "UInt64.complement":
		return "^" + left
	case "UInt8.shiftRight":
		return left + " >> (" + right + " & 7)"
	case "UInt32.shiftRight":
		return left + " >> (" + right + " & 31)"
	case "UInt64.shiftRight":
		return left + " >> (" + right + " & 63)"
	case "UInt8.decEq", "UInt32.decEq", "UInt64.decEq":
		operator = "=="
	case "UInt8.decLt", "UInt32.decLt", "UInt64.decLt":
		operator = "<"
	case "UInt8.decLe", "UInt32.decLe", "UInt64.decLe":
		operator = "<="
	}
	return left + " " + operator + " " + right
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
	if typ == "UInt64" {
		return "scalar64"
	}
	return "scalar"
}
