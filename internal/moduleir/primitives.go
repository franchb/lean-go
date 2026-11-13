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
	switch name {
	case "UInt32.toUInt64":
		return Primitive{name, []string{"UInt32"}, "UInt64", "zero extension: a"}, true
	case "UInt64.toUInt32":
		return Primitive{name, []string{"UInt64"}, "UInt32", "a modulo 2^32"}, true
	}
	width, operation, ok := strings.Cut(name, ".")
	if !ok || (width != "UInt32" && width != "UInt64") {
		return Primitive{}, false
	}
	bits := "32"
	if width == "UInt64" {
		bits = "64"
	}
	result, semantics := width, ""
	params := []string{width, width}
	switch operation {
	case "add":
		semantics = "(a + b) modulo 2^" + bits
	case "sub":
		semantics = "(a - b) modulo 2^" + bits
	case "mul":
		semantics = "(a * b) modulo 2^" + bits
	case "shiftLeft":
		semantics = "(a * 2^(b modulo " + bits + ")) modulo 2^" + bits
	case "land":
		semantics = "bitwise AND of " + bits + "-bit operands"
	case "lor":
		semantics = "bitwise OR of " + bits + "-bit operands"
	case "xor":
		semantics = "bitwise XOR of " + bits + "-bit operands"
	case "complement":
		params = []string{width}
		semantics = "(2^" + bits + " - 1) - a"
	case "shiftRight":
		semantics = "floor(a / 2^(b modulo " + bits + "))"
	case "decEq":
		result, semantics = "Bool", "unsigned a == b"
	case "decLt":
		result, semantics = "Bool", "unsigned a < b"
	case "decLe":
		result, semantics = "Bool", "unsigned a <= b"
	default:
		return Primitive{}, false
	}
	return Primitive{name, params, result, semantics}, true
}

func isBitwisePrimitive(name string) bool {
	switch name {
	case "UInt32.land", "UInt32.lor", "UInt32.xor", "UInt32.complement", "UInt32.shiftRight",
		"UInt64.land", "UInt64.lor", "UInt64.xor", "UInt64.complement", "UInt64.shiftRight":
		return true
	}
	return false
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
	case SchemaV7:
		return profile == ProfileV5
	case SchemaV8:
		return profile == ProfileV6
	case SchemaV9:
		return profile == ProfileV7
	case SchemaV10:
		return profile == ProfileV8
	case SchemaV11:
		return profile == ProfileV9
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
	if (value.Name == "UInt32.toUInt64" || value.Name == "UInt64.toUInt32") && !v.conversions {
		return nil, "", fmt.Errorf("width conversions require %s or %s", ProfileV8, ProfileV9)
	}
	if primitive.Params[0] == "UInt64" && !v.wide {
		return nil, "", fmt.Errorf("UInt64 primitives require %s", ProfileV4)
	}
	if isBitwisePrimitive(value.Name) && !v.bitwise {
		return nil, "", fmt.Errorf("bitwise primitives require %s or %s", ProfileV6, ProfileV7)
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
