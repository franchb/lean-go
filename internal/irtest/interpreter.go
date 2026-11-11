// Package irtest provides a bounded reference interpreter for compiler tests.
// It is never imported by a generated consumer package.
package irtest

import (
	"fmt"
	"github.com/franchb/lean-go/internal/ir"
)

type Tree struct {
	Tag         int
	Tri         uint8
	ID          uint32
	Left, Right *Tree
}
type value struct {
	kind    string
	boolean bool
	tri     uint8
	id      uint32
	tree    *Tree
}
type constructor struct {
	owner string
	tag   int
}
type Evaluator struct {
	root         string
	functions    map[string]ir.Function
	constructors map[string]constructor
}
type execution struct {
	evaluator    *Evaluator
	steps, depth int
}

func New(program *ir.Program) (*Evaluator, error) {
	if err := ir.Validate(program); err != nil {
		return nil, err
	}
	e := &Evaluator{root: program.Root, functions: map[string]ir.Function{}, constructors: map[string]constructor{}}
	for _, f := range program.Functions {
		e.functions[f.Name] = f
	}
	for _, t := range program.Types {
		for _, c := range t.Constructors {
			e.constructors[c.Name] = constructor{t.Name, c.Tag}
		}
	}
	return e, nil
}
func (e *Evaluator) Evaluate(tree *Tree) (*Tree, error) {
	if tree == nil {
		return nil, fmt.Errorf("nil reference input")
	}
	run := &execution{evaluator: e}
	v, err := run.call(e.root, []value{{kind: "expr", tree: tree}})
	if err != nil {
		return nil, err
	}
	if v.kind != "expr" || v.tree == nil {
		return nil, fmt.Errorf("reference result is not Expr")
	}
	return v.tree, nil
}
func (x *execution) call(name string, args []value) (value, error) {
	if x.depth >= 128 {
		return value{}, fmt.Errorf("reference call depth limit")
	}
	x.depth++
	defer func() { x.depth-- }()
	f, ok := x.evaluator.functions[name]
	if !ok || len(args) != len(f.Params) {
		return value{}, fmt.Errorf("invalid reference call %s", name)
	}
	locals := map[int]value{}
	for i, p := range f.Params {
		locals[p.ID] = args[i]
	}
	code := f.Body
	for code != nil {
		x.steps++
		if x.steps > 1000000 {
			return value{}, fmt.Errorf("reference step limit")
		}
		switch code.Op {
		case "return":
			v, ok := locals[code.Var]
			if !ok {
				return value{}, fmt.Errorf("missing return variable")
			}
			return v, nil
		case "let":
			args := make([]value, len(code.Value.Args))
			for i, id := range code.Value.Args {
				var ok bool
				args[i], ok = locals[id]
				if !ok {
					return value{}, fmt.Errorf("missing argument")
				}
			}
			var v value
			var err error
			if code.Value.Kind == "call" {
				v, err = x.call(code.Value.Name, args)
			} else {
				v, err = x.construct(code.Value.Name, args)
			}
			if err != nil {
				return value{}, err
			}
			locals[code.Bind.ID] = v
			code = code.Next
		case "cases":
			v, ok := locals[code.Var]
			if !ok {
				return value{}, fmt.Errorf("missing discriminant")
			}
			tag, fields, err := decompose(v)
			if err != nil {
				return value{}, err
			}
			var branch *ir.Code
			for _, a := range code.Alts {
				c := x.evaluator.constructors[a.Constructor]
				if c.owner == v.kind && c.tag == tag {
					if len(fields) != len(a.Params) {
						return value{}, fmt.Errorf("case field mismatch")
					}
					for i, p := range a.Params {
						locals[p.ID] = fields[i]
					}
					branch = a.Body
					break
				}
			}
			if branch == nil {
				return value{}, fmt.Errorf("no reference case alternative")
			}
			code = branch
		default:
			return value{}, fmt.Errorf("unsupported reference opcode")
		}
	}
	return value{}, fmt.Errorf("missing reference return")
}
func (x *execution) construct(name string, args []value) (value, error) {
	c, ok := x.evaluator.constructors[name]
	if !ok {
		return value{}, fmt.Errorf("unknown reference constructor")
	}
	switch c.owner {
	case "bool":
		return value{kind: "bool", boolean: c.tag == 1}, nil
	case "tri":
		return value{kind: "tri", tri: uint8(c.tag)}, nil
	case "expr":
		t := &Tree{Tag: c.tag}
		switch c.tag {
		case 0:
			t.Tri = args[0].tri
		case 1:
			t.ID = args[0].id
		case 2:
			t.Left = args[0].tree
		case 3, 4:
			t.Left = args[0].tree
			t.Right = args[1].tree
		default:
			return value{}, fmt.Errorf("unknown Expr tag")
		}
		return value{kind: "expr", tree: t}, nil
	}
	return value{}, fmt.Errorf("unsupported reference type")
}
func decompose(v value) (int, []value, error) {
	switch v.kind {
	case "bool":
		if v.boolean {
			return 1, nil, nil
		}
		return 0, nil, nil
	case "tri":
		return int(v.tri), nil, nil
	case "expr":
		if v.tree == nil {
			return 0, nil, fmt.Errorf("nil Expr in reference case")
		}
		e := v.tree
		switch e.Tag {
		case 0:
			return 0, []value{{kind: "tri", tri: e.Tri}}, nil
		case 1:
			return 1, []value{{kind: "u32", id: e.ID}}, nil
		case 2:
			return 2, []value{{kind: "expr", tree: e.Left}}, nil
		case 3, 4:
			return e.Tag, []value{{kind: "expr", tree: e.Left}, {kind: "expr", tree: e.Right}}, nil
		}
	}
	return 0, nil, fmt.Errorf("invalid reference discriminant")
}
