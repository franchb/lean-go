package moduleir

import "fmt"

// RootNames returns the selected roots of an already validated program.
// The returned slice must not be modified by callers.
func RootNames(p *Program) []string {
	if p.Schema == SchemaV7 {
		return p.Roots
	}
	return []string{p.Root}
}

// HasUInt64 identifies schemas with scalar UInt64 semantics.
func HasUInt64(schema string) bool { return schema == SchemaV6 || schema == SchemaV7 }

// HasTypedAPI identifies schemas carrying source record metadata.
func HasTypedAPI(schema string) bool {
	return schema == SchemaV5 || schema == SchemaV6 || schema == SchemaV7
}

// programFields keeps each schema's root representation exact at the JSON
// boundary, including rejection of an irrelevant root field with an empty value.
func programFields(schema string) []string {
	fields := append([]string{}, objectFields["program"]...)
	if schema == SchemaV7 {
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
	if p.Schema != SchemaV7 {
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
