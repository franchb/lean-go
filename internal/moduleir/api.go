package moduleir

import "fmt"

func checkAPIShape(value any) error {
	api, ok := value.(map[string]any)
	if !ok || len(api) != 1 {
		return fmt.Errorf("api must contain only records")
	}
	records, ok := api["records"].(map[string]any)
	if !ok {
		return fmt.Errorf("api.records must be an object")
	}
	for name, value := range records {
		fields, ok := value.([]any)
		if !ok {
			return fmt.Errorf("api.records[%q] must be an array", name)
		}
		for _, field := range fields {
			if _, ok := field.(string); !ok {
				return fmt.Errorf("api.records[%q] fields must be strings", name)
			}
		}
	}
	return nil
}

func (v *validator) validateAPI(p *Program) error {
	if !HasTypedAPI(p.Schema) {
		if p.API != nil {
			return fmt.Errorf("api metadata requires %s", SchemaV5)
		}
		return nil
	}
	if p.API == nil || p.API.Records == nil || len(p.API.Records) > MaxTypes {
		return fmt.Errorf("invalid api.records map")
	}
	for name, fields := range p.API.Records {
		constructors, exists := v.types[name]
		if !exists || len(constructors) != 1 {
			return fmt.Errorf("api record %q requires a known single-constructor type", name)
		}
		if fields == nil || len(fields) != len(constructors[0].Fields) {
			return fmt.Errorf("api record %q field arity mismatch", name)
		}
		seen := map[string]bool{}
		for _, field := range fields {
			if !validName(field) || seen[field] {
				return fmt.Errorf("api record %q has invalid or duplicate field %q", name, field)
			}
			seen[field] = true
		}
	}
	return nil
}
