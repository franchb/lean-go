package moduleir

import (
	"fmt"
	"strconv"
)

// Primitive describes one pinned pure-mono runtime leaf. Kernel signatures of
// the dec* declarations return Decidable; the admitted monomorphic result is Bool.
type Primitive struct {
	Name      string   `json:"name"`
	Params    []string `json:"params"`
	Result    string   `json:"result"`
	Semantics string   `json:"semantics"`
}

// LookupPrimitive returns a fresh signature, so callers cannot mutate the catalog.
func LookupPrimitive(name string) (Primitive, bool) {
	result, semantics := "UInt32", ""
	switch name {
	case "UInt32.add":
		semantics = "(a + b) modulo 2^32"
	case "UInt32.sub":
		semantics = "(a - b) modulo 2^32"
	case "UInt32.mul":
		semantics = "(a * b) modulo 2^32"
	case "UInt32.shiftLeft":
		semantics = "(a * 2^(b modulo 32)) modulo 2^32"
	case "UInt32.decEq":
		result, semantics = "Bool", "unsigned a == b"
	case "UInt32.decLt":
		result, semantics = "Bool", "unsigned a < b"
	case "UInt32.decLe":
		result, semantics = "Bool", "unsigned a <= b"
	default:
		return Primitive{}, false
	}
	return Primitive{name, []string{"UInt32", "UInt32"}, result, semantics}, true
}

func validProfile(schema, profile string) bool {
	switch schema {
	case Schema:
		return profile == Profile
	case SchemaV3:
		return profile == ProfileV2
	case SchemaV4, SchemaV5:
		return profile == ProfileV3
	default:
		return false
	}
}

func (v *validator) validateNumericTypes() error {
	constructors, exists := v.types["Bool"]
	if !v.numeric || !exists {
		return nil
	}
	if len(constructors) != 2 {
		return fmt.Errorf("numeric profile requires canonical Bool")
	}
	for tag, name := range []string{"Bool.false", "Bool.true"} {
		c := constructors[tag]
		if c.Name != name || c.Tag != tag || len(c.Fields) != 0 {
			return fmt.Errorf("numeric profile requires canonical Bool")
		}
	}
	return nil
}

func (v *validator) numericValue(value *Value) ([]string, string, error) {
	if !v.numeric {
		return nil, "", fmt.Errorf("value kind %q requires %s", value.Kind, ProfileV2)
	}
	if value.Kind == "uint32" {
		if len(value.Name) > 10 {
			return nil, "", fmt.Errorf("invalid UInt32 literal")
		}
		n, err := strconv.ParseUint(value.Name, 10, 32)
		if err != nil || strconv.FormatUint(n, 10) != value.Name {
			return nil, "", fmt.Errorf("invalid UInt32 literal %q", value.Name)
		}
		return nil, "UInt32", nil
	}
	primitive, ok := LookupPrimitive(value.Name)
	if !ok {
		return nil, "", fmt.Errorf("unknown primitive %s", value.Name)
	}
	if !v.validType(primitive.Result) {
		return nil, "", fmt.Errorf("missing primitive result type %s", primitive.Result)
	}
	return primitive.Params, primitive.Result, nil
}
