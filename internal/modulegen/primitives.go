package modulegen

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/franchb/lean-go/internal/moduleir"
)

func (g *generator) emitNumeric(w *bytes.Buffer, f *function, c *moduleir.Code) {
	expression := c.Value.Name
	result := "UInt32"
	if c.Value.Kind == "primitive" {
		primitive, _ := moduleir.LookupPrimitive(c.Value.Name)
		result = primitive.Result
		left := fmt.Sprintf("f.slots[%d].scalar", f.slots[c.Value.Args[0]])
		right := fmt.Sprintf("f.slots[%d].scalar", f.slots[c.Value.Args[1]])
		expression = primitiveExpression(c.Value.Name, left, right)
	}
	if result == "Bool" {
		fmt.Fprintf(w, "tag:=0;if %s{tag=1};value,err:=b.make(%d,tag,nil);if err!=nil{return nil,err}\n", expression, g.types["Bool"])
	} else {
		fmt.Fprintf(w, "value,err:=b.make(0,0,nil);if err!=nil{return nil,err};value.scalar=%s\n", expression)
	}
	fmt.Fprintf(w, "f.slots[%d]=value;f.pc=%d\n", f.slots[c.Bind.ID], f.states[c.Next])
}

func primitiveExpression(name, left, right string) string {
	operator := ""
	switch name {
	case "UInt32.add":
		operator = "+"
	case "UInt32.sub":
		operator = "-"
	case "UInt32.mul":
		operator = "*"
	case "UInt32.shiftLeft":
		return left + " << (" + right + " & 31)"
	case "UInt32.decEq":
		operator = "=="
	case "UInt32.decLt":
		operator = "<"
	case "UInt32.decLe":
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
