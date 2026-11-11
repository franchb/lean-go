package moduleir

import "fmt"

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (v *validator) validType(typ string) bool { _, ok := v.types[typ]; return typ == "UInt32" || ok }
func (v *validator) bind(param Param, scope map[int]string, used map[int]bool) error {
	if err := v.addNodes(1); err != nil {
		return err
	}
	if param.ID <= 0 || used[param.ID] || !v.validType(param.Type) {
		return fmt.Errorf("invalid or duplicate binder %d", param.ID)
	}
	used[param.ID] = true
	scope[param.ID] = param.Type
	return nil
}
func copyScope(scope map[int]string) map[int]string {
	copy := make(map[int]string, len(scope))
	for id, typ := range scope {
		copy[id] = typ
	}
	return copy
}

func (v *validator) code(c *Code, f Function, scope map[int]string, joins map[int]*Join, used map[int]bool, seen map[*Code]bool, depth int) error {
	if c == nil {
		return fmt.Errorf("missing code")
	}
	if depth > MaxCodeDepth {
		return fmt.Errorf("code depth limit exceeded")
	}
	if seen[c] {
		return fmt.Errorf("code must be a finite tree")
	}
	seen[c] = true
	if err := v.addNodes(1); err != nil {
		return err
	}
	switch c.Op {
	case "let":
		if c.Bind == nil || c.Value == nil || c.Next == nil || c.Var != 0 || c.Type != "" || c.Result != "" || c.Alts != nil || c.Join != nil || c.Target != 0 || c.Args != nil {
			return fmt.Errorf("invalid let payload")
		}
		typ, err := v.value(c.Value, f.Name, scope)
		if err != nil {
			return err
		}
		if typ != c.Bind.Type {
			return fmt.Errorf("let result type mismatch")
		}
		if err = v.bind(*c.Bind, scope, used); err != nil {
			return err
		}
		return v.code(c.Next, f, scope, joins, used, seen, depth+1)
	case "return":
		if c.Bind != nil || c.Value != nil || c.Next != nil || c.Type != "" || c.Result != "" || c.Alts != nil || c.Join != nil || c.Target != 0 || c.Args != nil {
			return fmt.Errorf("invalid return payload")
		}
		if c.Var <= 0 || scope[c.Var] != f.Result {
			return fmt.Errorf("invalid return variable/type")
		}
	case "cases":
		if c.Bind != nil || c.Value != nil || c.Next != nil || c.Var <= 0 || scope[c.Var] != c.Type || c.Result != f.Result || c.Join != nil || c.Target != 0 || c.Args != nil {
			return fmt.Errorf("invalid cases payload or type")
		}
		constructors, ok := v.types[c.Type]
		if !ok || len(c.Alts) != len(constructors) {
			return fmt.Errorf("non-exhaustive cases")
		}
		found := map[string]bool{}
		for _, alt := range c.Alts {
			if err := v.addNodes(1); err != nil {
				return err
			}
			info, ok := v.constructors[alt.Constructor]
			if !ok || info.result != c.Type || found[alt.Constructor] || alt.Params == nil || len(alt.Params) != len(info.constructor.Fields) {
				return fmt.Errorf("invalid case alternative %s", alt.Constructor)
			}
			found[alt.Constructor] = true
			local := copyScope(scope)
			for i, param := range alt.Params {
				if param.Type != info.constructor.Fields[i] {
					return fmt.Errorf("case field type mismatch")
				}
				if err := v.bind(param, local, used); err != nil {
					return err
				}
			}
			if err := v.code(alt.Body, f, local, joins, used, seen, depth+1); err != nil {
				return err
			}
		}
	case "join":
		return v.join(c, f, scope, joins, used, seen, depth)
	case "jump":
		return v.jump(c, scope, joins)
	default:
		return fmt.Errorf("unsupported code operation %q", c.Op)
	}
	return nil
}
func (v *validator) value(value *Value, function string, scope map[int]string) (string, error) {
	if err := v.addNodes(1); err != nil {
		return "", err
	}
	if value.Args == nil {
		return "", fmt.Errorf("value args must not be null")
	}
	var params []string
	var result string
	switch value.Kind {
	case "constructor":
		info, ok := v.constructors[value.Name]
		if !ok {
			return "", fmt.Errorf("unknown constructor %s", value.Name)
		}
		params = info.constructor.Fields
		result = info.result
	case "call":
		callee, ok := v.functions[value.Name]
		if !ok {
			return "", fmt.Errorf("unresolved call %s", value.Name)
		}
		for _, param := range callee.Params {
			params = append(params, param.Type)
		}
		result = callee.Result
		v.edges[function][value.Name] = true
	case "uint32", "primitive":
		var err error
		params, result, err = v.numericValue(value)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported value kind %q", value.Kind)
	}
	if len(value.Args) != len(params) {
		return "", fmt.Errorf("unsaturated value %s", value.Name)
	}
	for i, id := range value.Args {
		if id <= 0 || scope[id] != params[i] {
			return "", fmt.Errorf("unknown or mistyped argument %d", id)
		}
	}
	return result, nil
}
