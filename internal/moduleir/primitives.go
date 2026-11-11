package moduleir

import (
	"fmt"
	"strconv"
	"strings"
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
	width, operation, ok := strings.Cut(name, ".")
	if !ok || (width != "UInt32" && width != "UInt64") {
		return Primitive{}, false
	}
	bits := "32"
	if width == "UInt64" {
		bits = "64"
	}
	result, semantics := width, ""
	switch operation {
	case "add":
		semantics = "(a + b) modulo 2^" + bits
	case "sub":
		semantics = "(a - b) modulo 2^" + bits
	case "mul":
		semantics = "(a * b) modulo 2^" + bits
	case "shiftLeft":
		semantics = "(a * 2^(b modulo " + bits + ")) modulo 2^" + bits
	case "decEq":
		result, semantics = "Bool", "unsigned a == b"
	case "decLt":
		result, semantics = "Bool", "unsigned a < b"
	case "decLe":
		result, semantics = "Bool", "unsigned a <= b"
	default:
		return Primitive{}, false
	}
	return Primitive{name, []string{width, width}, result, semantics}, true
}

func validProfile(schema, profile string) bool {
	switch schema {
	case Schema:
		return profile == Profile
	case SchemaV3:
		return profile == ProfileV2
	case SchemaV4, SchemaV5:
		return profile == ProfileV3
	case SchemaV6:
		return profile == ProfileV4
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
		return nil, "UInt32", validateLiteral(value.Name, 32, 10)
	}
	if value.Kind == "uint64" {
		if !v.wide {
			return nil, "", fmt.Errorf("UInt64 literals require %s", ProfileV4)
		}
		return nil, "UInt64", validateLiteral(value.Name, 64, 20)
	}
	primitive, ok := LookupPrimitive(value.Name)
	if !ok {
		return nil, "", fmt.Errorf("unknown primitive %s", value.Name)
	}
	if primitive.Params[0] == "UInt64" && !v.wide {
		return nil, "", fmt.Errorf("UInt64 primitives require %s", ProfileV4)
	}
	if !v.validType(primitive.Result) {
		return nil, "", fmt.Errorf("missing primitive result type %s", primitive.Result)
	}
	return primitive.Params, primitive.Result, nil
}

func validateLiteral(name string, bits, maxDigits int) error {
	if len(name) > maxDigits {
		return fmt.Errorf("invalid UInt%d literal", bits)
	}
	n, err := strconv.ParseUint(name, 10, bits)
	if err != nil || strconv.FormatUint(n, 10) != name {
		return fmt.Errorf("invalid UInt%d literal %q", bits, name)
	}
	return nil
}
