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
	case "UInt8.toUInt16":
		return Primitive{name, []string{"UInt8"}, "UInt16", "zero extension: a"}, true
	case "UInt16.toUInt32":
		return Primitive{name, []string{"UInt16"}, "UInt32", "zero extension: a"}, true
	case "UInt16.toUInt8":
		return Primitive{name, []string{"UInt16"}, "UInt8", "a modulo 2^8"}, true
	case "UInt16.lor":
		return Primitive{name, []string{"UInt16", "UInt16"}, "UInt16", "bitwise OR of 16-bit operands"}, true
	case "UInt16.xor":
		return Primitive{name, []string{"UInt16", "UInt16"}, "UInt16", "bitwise XOR of 16-bit operands"}, true
	case "UInt16.shiftLeft":
		return Primitive{name, []string{"UInt16", "UInt16"}, "UInt16", "(a * 2^(b modulo 16)) modulo 2^16"}, true
	case "UInt16.shiftRight":
		return Primitive{name, []string{"UInt16", "UInt16"}, "UInt16", "floor(a / 2^(b modulo 16))"}, true
	case "UInt8.toUInt32":
		return Primitive{name, []string{"UInt8"}, "UInt32", "zero extension: a"}, true
	case "UInt32.toUInt8":
		return Primitive{name, []string{"UInt32"}, "UInt8", "a modulo 2^8"}, true
	case "UInt32.toUInt64":
		return Primitive{name, []string{"UInt32"}, "UInt64", "zero extension: a"}, true
	case "UInt64.toUInt32":
		return Primitive{name, []string{"UInt64"}, "UInt32", "a modulo 2^32"}, true
	}
	width, operation, ok := strings.Cut(name, ".")
	if !ok || (width != "UInt8" && width != "UInt32" && width != "UInt64") {
		return Primitive{}, false
	}
	bits := "32"
	if width == "UInt8" {
		bits = "8"
	}
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

func unsupportedSchema(schema string) error {
	return fmt.Errorf("unsupported module IR schema %q; regenerate it with the current lean-go tool (%s)", schema, Schema)
}

// checkIdentity accepts only the current schema and profile. Earlier module IR
// versions are not migrated: they must be regenerated with the current tool.
func checkIdentity(schema, profile string) error {
	if schema != Schema {
		return unsupportedSchema(schema)
	}
	if profile != Profile {
		return fmt.Errorf("unsupported module IR profile %q; regenerate it with the current lean-go tool (%s)", profile, Profile)
	}
	return nil
}

func (v *validator) validateNumericTypes() error {
	constructors, exists := v.types["Bool"]
	if !exists {
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
	switch value.Kind {
	case "uint8":
		return nil, "UInt8", validateLiteral(value.Name, 8, 3)
	case "uint16":
		return nil, "UInt16", validateLiteral(value.Name, 16, 5)
	case "uint32":
		return nil, "UInt32", validateLiteral(value.Name, 32, 10)
	case "uint64":
		return nil, "UInt64", validateLiteral(value.Name, 64, 20)
	}
	primitive, ok := LookupPrimitive(value.Name)
	if !ok {
		return nil, "", fmt.Errorf("unknown primitive %s", value.Name)
	}
	if !v.validType(primitive.Result) {
		return nil, "", fmt.Errorf("missing primitive result type %s", primitive.Result)
	}
	v.usePrimitive(primitive)
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
