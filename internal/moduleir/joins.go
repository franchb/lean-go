package moduleir

import (
	"fmt"
	"maps"
)

func (v *validator) join(c *Code, f Function, scope map[int]string, joins map[int]*Join, used map[int]bool, seen map[*Code]bool, depth int) error {
	if !v.joins {
		return fmt.Errorf("join requires %s", ProfileV3)
	}
	if c.Join == nil || c.Next == nil || c.Bind != nil || c.Value != nil || c.Var != 0 || c.Type != "" || c.Result != "" || c.Alts != nil || c.Target != 0 || c.Args != nil {
		return fmt.Errorf("invalid join payload")
	}
	j := c.Join
	if err := v.addNodes(1); err != nil {
		return err
	}
	if j.ID <= 0 || used[j.ID] || j.Params == nil || len(j.Params) > MaxArity {
		return fmt.Errorf("invalid or duplicate join binder %d", j.ID)
	}
	used[j.ID] = true
	local := copyScope(scope)
	for _, param := range j.Params {
		if err := v.bind(param, local, used); err != nil {
			return err
		}
	}
	// The body cannot see its own join. This lexical order excludes recursive
	// local control flow, including cycles through nested declarations.
	if err := v.code(j.Body, f, local, joins, used, seen, depth+1); err != nil {
		return err
	}
	visible := maps.Clone(joins)
	visible[j.ID] = j
	return v.code(c.Next, f, scope, visible, used, seen, depth+1)
}

func (v *validator) jump(c *Code, scope map[int]string, joins map[int]*Join) error {
	if !v.joins {
		return fmt.Errorf("jump requires %s", ProfileV3)
	}
	if c.Target <= 0 || c.Args == nil || c.Join != nil || c.Next != nil || c.Bind != nil || c.Value != nil || c.Var != 0 || c.Type != "" || c.Result != "" || c.Alts != nil {
		return fmt.Errorf("invalid jump payload")
	}
	j, ok := joins[c.Target]
	if !ok {
		return fmt.Errorf("unknown or out-of-scope jump target %d", c.Target)
	}
	if len(c.Args) != len(j.Params) {
		return fmt.Errorf("unsaturated jump to %d", c.Target)
	}
	for i, id := range c.Args {
		if id <= 0 || scope[id] != j.Params[i].Type {
			return fmt.Errorf("unknown or mistyped jump argument %d", id)
		}
	}
	return nil
}
