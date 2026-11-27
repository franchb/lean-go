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

// scalarBits are the admitted unsigned widths. Each width has the same
// fourteen operations and one conversion to every other width.
var scalarBits = map[string]int{"UInt8": 8, "UInt16": 16, "UInt32": 32, "UInt64": 64}

// LookupPrimitive returns a fresh signature, so callers cannot mutate the catalog.
// The names are exactly those of the producer's numericPrimitives table.
func LookupPrimitive(name string) (Primitive, bool) {
	width, operation, ok := strings.Cut(name, ".")
	bits, scalar := scalarBits[width]
	if !ok || !scalar {
		return Primitive{}, false
	}
	n := strconv.Itoa(bits)
	result, semantics := width, ""
	params := []string{width, width}
	switch operation {
	case "add":
		semantics = "(a + b) modulo 2^" + n
	case "sub":
		semantics = "(a - b) modulo 2^" + n
	case "mul":
		semantics = "(a * b) modulo 2^" + n
	case "div":
		semantics = "floor(a / b), and 0 when b = 0"
	case "mod":
		semantics = "a modulo b, and a when b = 0"
	case "shiftLeft":
		semantics = "(a * 2^(b modulo " + n + ")) modulo 2^" + n
	case "land":
		semantics = "bitwise AND of " + n + "-bit operands"
	case "lor":
		semantics = "bitwise OR of " + n + "-bit operands"
	case "xor":
		semantics = "bitwise XOR of " + n + "-bit operands"
	case "complement":
		params = []string{width}
		semantics = "(2^" + n + " - 1) - a"
	case "shiftRight":
		semantics = "floor(a / 2^(b modulo " + n + "))"
	case "decEq":
		result, semantics = "Bool", "unsigned a == b"
	case "decLt":
		result, semantics = "Bool", "unsigned a < b"
	case "decLe":
		result, semantics = "Bool", "unsigned a <= b"
	default:
		target, conversion := strings.CutPrefix(operation, "to")
		targetBits, known := scalarBits[target]
		if !conversion || !known || target == width {
			return Primitive{}, false
		}
		params, result, semantics = []string{width}, target, "zero extension: a"
		if targetBits < bits {
			semantics = "a modulo 2^" + strconv.Itoa(targetBits)
		}
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
