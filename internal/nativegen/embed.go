package nativegen

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/franchb/lean-go/internal/moduleir"
)

// Embedded is native code for the call trees of some roots, written to be
// pasted into another generated package (the metered fast path). It holds
// declarations only: no package clause, imports, exported names, constructor
// functions or methods. Every package-level name it declares starts with the
// prefix given to GenerateEmbedded, followed by an upper-case letter.
type Embedded struct {
	// Source declares the types and functions, in IR order.
	Source []byte
	// Types describes every type the emitted functions use, transitively
	// through fields, scalars and Bool included.
	Types map[string]EmbeddedType
	// Functions maps each emitted Lean function to its Go name. Every
	// emitted function is total: it returns its result and never an error.
	Functions map[string]string

	m *model
}

// EmbeddedType is the Go representation of one Lean type in embedded code.
type EmbeddedType struct {
	// Go is the Go type: uint8 … uint64, bool, or a declared struct.
	Go string
	// Kind is "scalar", "bool", "record" or "sum".
	Kind string
	// Tags holds, per constructor index, the value of the struct's tag
	// field (sums only; Bool is false for index 0 and true for index 1).
	Tags []int
	// Fields holds, per constructor index, the Go field names.
	Fields [][]string
}

var embeddedPrefix = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// GenerateEmbedded emits native code for the functions reachable from roots.
// It fails unless every reachable function is non-recursive, so no emitted
// function takes a depth or returns ErrDepth, and every type it uses is
// non-recursive, so no value is boxed. prefix must be a short lower-case
// identifier that no other declaration of the target package starts with.
func GenerateEmbedded(p *moduleir.Program, roots []string, prefix string) (*Embedded, error) {
	if err := moduleir.Validate(p); err != nil {
		return nil, fmt.Errorf("validate module IR: %w", err)
	}
	if !embeddedPrefix.MatchString(prefix) {
		return nil, fmt.Errorf("embedded prefix %q is not a lower-case identifier", prefix)
	}
	m, err := newModelWithPrefix(p, DefaultMaxDepth, prefix)
	if err != nil {
		return nil, err
	}
	reached := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		f, ok := m.funcs[name]
		if !ok {
			return fmt.Errorf("unknown function %s", name)
		}
		if reached[name] {
			return nil
		}
		reached[name] = true
		if f.depth {
			return fmt.Errorf("%s is recursive", name)
		}
		for _, callee := range f.callees {
			if err := visit(callee); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	used := map[string]bool{}
	var use func(string) error
	use = func(typ string) error {
		if used[typ] {
			return nil
		}
		used[typ] = true
		info := m.types[typ]
		if info.kind == kindBoxed {
			return fmt.Errorf("type %s is recursive", typ)
		}
		for _, c := range info.decl.Constructors {
			for _, field := range c.Fields {
				if err := use(field); err != nil {
					return err
				}
			}
		}
		return nil
	}
	e := &Embedded{Types: map[string]EmbeddedType{}, Functions: map[string]string{}, m: m}
	for _, decl := range p.Functions {
		if !reached[decl.Name] {
			continue
		}
		var types []string
		types = append(types, decl.Result)
		for _, param := range decl.Params {
			types = append(types, param.Type)
		}
		walkCode(decl.Body, func(c *moduleir.Code) {
			switch c.Op {
			case "let":
				types = append(types, c.Bind.Type)
			case "cases":
				types = append(types, c.Type)
			case "join":
				for _, param := range c.Join.Params {
					types = append(types, param.Type)
				}
			}
		})
		for _, typ := range types {
			if err := use(typ); err != nil {
				return nil, fmt.Errorf("%s: %w", decl.Name, err)
			}
		}
		e.Functions[decl.Name] = m.funcs[decl.Name].callName
	}
	var w bytes.Buffer
	for _, name := range m.order {
		if !used[name] {
			continue
		}
		info := m.types[name]
		if info.kind == kindRecord {
			m.emitRecord(&w, info)
		} else {
			fmt.Fprintf(&w, "\n// %s is the Lean inductive type %s. The zero value is %s.\n", info.goName, info.lean, m.zeroDoc(info))
			fmt.Fprintf(&w, "type %s struct {\ntag uint8\n", info.goName)
			m.fieldDecls(&w, info)
			w.WriteString("}\n")
		}
	}
	primitives := map[string]bool{}
	for _, decl := range p.Functions {
		if reached[decl.Name] {
			if err := m.emitFunction(&w, m.funcs[decl.Name], primitives); err != nil {
				return nil, fmt.Errorf("%s: %w", decl.Name, err)
			}
		}
	}
	e.Source = w.Bytes()
	for typ := range used {
		info := m.types[typ]
		t := EmbeddedType{Go: info.goName, Kind: kindNames[info.kind], Fields: info.fields}
		if info.kind == kindSum {
			t.Tags = info.tags
		}
		e.Types[typ] = t
	}
	return e, nil
}

// Constructor returns a Go expression for the constructor of typ with index
// i (its Lean tag) applied to the Go expressions args.
func (e *Embedded) Constructor(typ string, i int, args []string) string {
	return e.m.constructorExpr(e.m.types[typ], i, args)
}
