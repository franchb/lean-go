package moduleir

import "fmt"

// RootNames returns the selected roots of an already validated program.
// The returned slice must not be modified by callers.
func RootNames(p *Program) []string {
	if HasMultipleRoots(p.Schema) {
		return p.Roots
	}
	return []string{p.Root}
}

// HasMultipleRoots identifies schemas with an explicit roots array.
func HasMultipleRoots(schema string) bool {
	return schema == SchemaV7 || schema == SchemaV9 || schema == SchemaV11 || schema == SchemaV13
}

// HasUInt64 identifies schemas with scalar UInt64 semantics.
func HasUInt64(schema string) bool {
	return schema == SchemaV6 || schema == SchemaV7 || schema == SchemaV8 || schema == SchemaV9 || schema == SchemaV10 || schema == SchemaV11 || HasUInt8(schema)
}

// HasUInt8 identifies schemas with scalar byte semantics.
func HasUInt8(schema string) bool {
	return schema == SchemaV12 || schema == SchemaV13
}

// HasTypedAPI identifies schemas carrying source record metadata.
func HasTypedAPI(schema string) bool {
	return schema == SchemaV5 || HasUInt64(schema)
}

// programFields keeps each schema's root representation exact at the JSON
// boundary, including rejection of an irrelevant root field with an empty value.
func programFields(schema string) []string {
	fields := append([]string{}, objectFields["program"]...)
	if HasMultipleRoots(schema) {
		for i, field := range fields {
			if field == "root" {
				fields[i] = "roots"
			}
		}
	}
	if HasTypedAPI(schema) {
		fields = append(fields, "api")
	}
	return fields
}

func validateRoots(p *Program) error {
	if !HasMultipleRoots(p.Schema) {
		if !validName(p.Root) || p.Roots != nil {
			return fmt.Errorf("legacy profile requires one root and no roots array")
		}
		return nil
	}
	if p.Root != "" || len(p.Roots) < 2 || len(p.Roots) > MaxRoots {
		return fmt.Errorf("multi-root profile requires 2..%d roots and no root field", MaxRoots)
	}
	previous := ""
	for _, root := range p.Roots {
		if !validName(root) || root <= previous {
			return fmt.Errorf("invalid, duplicate or unordered root %q", root)
		}
		previous = root
	}
	return nil
}
