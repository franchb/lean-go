package nativegen

import (
	"fmt"
	"slices"
	"sort"

	"github.com/franchb/lean-go/internal/moduleir"
)

type kind int

const (
	kindScalar kind = iota // uint8, uint16, uint32, uint64
	kindBool               // canonical Lean Bool as Go bool
	kindRecord             // one constructor with fields, not recursive: exported struct fields
	kindSum                // not recursive: value struct with an unexported tag
	kindBoxed              // recursive: handle to an immutable heap node
)

var kindNames = map[kind]string{kindScalar: "scalar", kindBool: "bool", kindRecord: "record", kindSum: "sum", kindBoxed: "boxed"}

type typeInfo struct {
	lean     string
	decl     moduleir.TypeDecl
	kind     kind
	goName   string
	zeroCtor int   // constructor index represented by the Go zero value
	tags     []int // internal tag per constructor index; the zero constructor gets 0
	fields   [][]string
	enum     bool // every constructor is nullary
	// Exported constructor functions and methods of sums and boxed types.
	ctorFuncs []string
	isMethods []string
	asMethods []string
	stringer  string
	// Unexported support names of boxed types.
	nodeName  string
	zeroVar   string
	nullaries []string // shared immutable nodes for nullary constructors
}

type foldInfo struct {
	step string
}

type funcInfo struct {
	decl     moduleir.Function
	index    int
	root     bool
	selfRec  bool
	depth    bool // takes the recursion depth and may return ErrDepth
	callees  []string
	fold     *foldInfo
	elided   bool   // only the exported byte loop exists
	callName string // name used by generated callers of the IR function
	exported string // exported entry point for roots
}

type model struct {
	program  *moduleir.Program
	types    map[string]*typeInfo
	order    []string // non-scalar emitted types in IR order
	ctors    map[string]ctorRef
	funcs    map[string]*funcInfo
	roots    []string
	listType string // elided List UInt8, if any
	maxDepth int
	// prefix is set for embedded code (see GenerateEmbedded): every
	// package-level name starts with it and nothing is exported.
	prefix string
}

type ctorRef struct {
	typ   string
	index int
}

const byteList = "List UInt8"

func isCanonicalBool(t moduleir.TypeDecl) bool {
	if t.Name != "Bool" || len(t.Constructors) != 2 {
		return false
	}
	for tag, name := range []string{"Bool.false", "Bool.true"} {
		c := t.Constructors[tag]
		if c.Name != name || c.Tag != tag || len(c.Fields) != 0 {
			return false
		}
	}
	return true
}

func newModel(p *moduleir.Program, maxDepth int) (*model, error) {
	return newModelWithPrefix(p, maxDepth, "")
}

func newModelWithPrefix(p *moduleir.Program, maxDepth int, prefix string) (*model, error) {
	m := &model{program: p, types: map[string]*typeInfo{}, ctors: map[string]ctorRef{}, funcs: map[string]*funcInfo{}, roots: moduleir.RootNames(p), maxDepth: maxDepth, prefix: prefix}
	for name := range scalarWidths {
		m.types[name] = &typeInfo{lean: name, kind: kindScalar, goName: scalarGo(name)}
	}
	for _, t := range p.Types {
		info := &typeInfo{lean: t.Name, decl: t}
		if isCanonicalBool(t) {
			info.kind, info.goName = kindBool, "bool"
		}
		m.types[t.Name] = info
		for i, c := range t.Constructors {
			m.ctors[c.Name] = ctorRef{t.Name, i}
		}
	}
	for i, f := range p.Functions {
		m.funcs[f.Name] = &funcInfo{decl: f, index: i, root: slices.Contains(m.roots, f.Name)}
	}
	for _, f := range m.funcs {
		calls := map[string]bool{}
		walkCode(f.decl.Body, func(c *moduleir.Code) {
			if c.Op == "let" && c.Value.Kind == "call" {
				calls[c.Value.Name] = true
			}
		})
		f.selfRec = calls[f.decl.Name]
		for name := range calls {
			f.callees = append(f.callees, name)
		}
		sort.Strings(f.callees)
	}
	m.recognizeFolds()
	m.classifyTypes()
	if err := m.chooseZeros(); err != nil {
		return nil, err
	}
	m.depthAnalysis()
	m.allocateNames()
	return m, nil
}

func walkCode(c *moduleir.Code, visit func(*moduleir.Code)) {
	visit(c)
	switch c.Op {
	case "let":
		walkCode(c.Next, visit)
	case "cases":
		for _, a := range c.Alts {
			walkCode(a.Body, visit)
		}
	case "join":
		walkCode(c.Join.Body, visit)
		walkCode(c.Next, visit)
	}
}

// recognizeFolds finds roots of the form `def f (s : T) (xs : List UInt8) :=
// xs.foldl step s` in validated IR. They are exported as loops over a borrowed
// []byte. When nothing else mentions List UInt8, the list type and the
// recursive IR functions are not emitted at all.
func (m *model) recognizeFolds() {
	list, ok := m.types[byteList]
	if !ok || len(list.decl.Constructors) != 2 || m.prefix != "" {
		return
	}
	nilCtor, cons := list.decl.Constructors[0], list.decl.Constructors[1]
	if nilCtor.Name != "List.nil" || len(nilCtor.Fields) != 0 || cons.Name != "List.cons" || !slices.Equal(cons.Fields, []string{"UInt8", byteList}) {
		return
	}
	folds := map[string]bool{}
	for _, name := range m.roots {
		f := m.funcs[name]
		if step := foldStep(f.decl); step != "" {
			f.fold = &foldInfo{step: step}
			folds[name] = true
		}
	}
	if len(folds) == 0 {
		return
	}
	for _, t := range m.program.Types {
		if t.Name == byteList {
			continue
		}
		for _, c := range t.Constructors {
			if slices.Contains(c.Fields, byteList) {
				return
			}
		}
	}
	for _, f := range m.program.Functions {
		if folds[f.Name] {
			continue
		}
		if mentionsType(f, byteList) {
			return
		}
		for _, callee := range m.funcs[f.Name].callees {
			if folds[callee] {
				return
			}
		}
	}
	m.listType = byteList
	for name := range folds {
		m.funcs[name].elided = true
	}
}

func mentionsType(f moduleir.Function, typ string) bool {
	found := f.Result == typ
	for _, p := range f.Params {
		found = found || p.Type == typ
	}
	walkCode(f.Body, func(c *moduleir.Code) {
		switch c.Op {
		case "let":
			found = found || c.Bind.Type == typ
		case "cases":
			found = found || c.Type == typ
			for _, a := range c.Alts {
				for _, p := range a.Params {
					found = found || p.Type == typ
				}
			}
		case "join":
			for _, p := range c.Join.Params {
				found = found || p.Type == typ
			}
		}
	})
	return found
}

func foldStep(f moduleir.Function) string {
	if len(f.Params) != 2 || f.Params[0].Type != f.Result || f.Params[1].Type != byteList {
		return ""
	}
	body := f.Body
	if body.Op != "cases" || body.Type != byteList || body.Var != f.Params[1].ID || len(body.Alts) != 2 {
		return ""
	}
	empty, cons := body.Alts[0], body.Alts[1]
	if empty.Constructor != "List.nil" || empty.Body.Op != "return" || empty.Body.Var != f.Params[0].ID || cons.Constructor != "List.cons" || len(cons.Params) != 2 {
		return ""
	}
	call := cons.Body
	if call.Op != "let" || call.Value.Kind != "call" || call.Value.Name == f.Name || call.Bind.Type != f.Result || !slices.Equal(call.Value.Args, []int{f.Params[0].ID, cons.Params[0].ID}) {
		return ""
	}
	recur := call.Next
	if recur.Op != "let" || recur.Value.Kind != "call" || recur.Value.Name != f.Name || !slices.Equal(recur.Value.Args, []int{call.Bind.ID, cons.Params[1].ID}) || recur.Next.Op != "return" || recur.Next.Var != recur.Bind.ID {
		return ""
	}
	return call.Value.Name
}

// classifyTypes marks types on a cycle of the field graph as boxed; other
// types become Go value types.
func (m *model) classifyTypes() {
	edges := map[string][]string{}
	for _, t := range m.program.Types {
		for _, c := range t.Constructors {
			for _, field := range c.Fields {
				if _, scalar := scalarWidths[field]; !scalar {
					edges[t.Name] = append(edges[t.Name], field)
				}
			}
		}
	}
	reaches := func(from, to string) bool {
		seen := map[string]bool{}
		stack := append([]string(nil), edges[from]...)
		for len(stack) > 0 {
			t := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if t == to {
				return true
			}
			if !seen[t] {
				seen[t] = true
				stack = append(stack, edges[t]...)
			}
		}
		return false
	}
	for _, t := range m.program.Types {
		info := m.types[t.Name]
		if info.kind == kindBool || t.Name == m.listType {
			continue
		}
		m.order = append(m.order, t.Name)
		info.enum = true
		for _, c := range t.Constructors {
			info.enum = info.enum && len(c.Fields) == 0
		}
		switch {
		case reaches(t.Name, t.Name):
			info.kind = kindBoxed
		case len(t.Constructors) == 1 && len(t.Constructors[0].Fields) > 0:
			info.kind = kindRecord
		default:
			info.kind = kindSum
		}
	}
}

// chooseZeros selects, for every emitted type, the constructor its Go zero
// value denotes: the first constructor whose fields' zero values do not
// lead back to the type itself. Choices are memoized depth first, so the
// graph of chosen zero constructors is acyclic and every zero value is a
// finite Lean value.
func (m *model) chooseZeros() error {
	ready := map[string]bool{}
	visiting := map[string]bool{}
	for name, info := range m.types {
		if info.kind == kindScalar || info.kind == kindBool {
			ready[name] = true
		}
	}
	var resolve func(string) bool
	resolve = func(name string) bool {
		if ready[name] {
			return true
		}
		if visiting[name] {
			return false
		}
		visiting[name] = true
		defer delete(visiting, name)
		info := m.types[name]
		for i, c := range info.decl.Constructors {
			ok := true
			for _, field := range c.Fields {
				ok = ok && resolve(field)
			}
			if ok {
				info.zeroCtor = i
				ready[name] = true
				return true
			}
		}
		return false
	}
	for _, name := range m.order {
		if !resolve(name) {
			return fmt.Errorf("native mode: type %s has no finite value to serve as its Go zero value", name)
		}
		info := m.types[name]
		if info.kind == kindRecord && info.zeroCtor != 0 {
			return fmt.Errorf("native mode: record %s has no zero value", name)
		}
		n := len(info.decl.Constructors)
		info.tags = make([]int, n)
		for i := range info.tags {
			info.tags[i] = (i - info.zeroCtor + n) % n
		}
	}
	return nil
}

// depthAnalysis marks functions that recurse, directly or through callees.
// They take the current call depth and fail with ErrDepth beyond MaxDepth.
func (m *model) depthAnalysis() {
	for changed := true; changed; {
		changed = false
		for _, f := range m.funcs {
			if f.depth {
				continue
			}
			depth := f.selfRec && !f.elided
			for _, callee := range f.callees {
				if callee != f.decl.Name && m.funcs[callee].depth && !f.elided {
					depth = true
				}
			}
			if f.fold != nil && f.elided {
				depth = false
			}
			if depth {
				f.depth = true
				changed = true
			}
		}
	}
}

// foldFails reports whether a byte loop can fail: only through its step.
func (m *model) foldFails(f *funcInfo) bool { return m.funcs[f.fold.step].depth }

func (m *model) allocateNames() {
	exported := allocator{"MaxDepth": true, "ErrDepth": true}
	internal := newInternalAllocator()
	for _, name := range m.order {
		info := m.types[name]
		if m.prefix != "" {
			info.goName = internal.allocateInternal(m.prefix + exportedName(name))
		} else {
			info.goName = exported.allocate(exportedName(name))
		}
	}
	type candidate struct {
		lean string
		ref  ctorRef
	}
	var ctors []candidate
	for _, name := range m.order {
		info := m.types[name]
		n := len(info.decl.Constructors)
		info.fields = make([][]string, n)
		if info.kind == kindRecord {
			c := info.decl.Constructors[0]
			fieldNames := allocator{}
			var source []string
			if m.program.API != nil {
				source = m.program.API.Records[name]
			}
			for i := range c.Fields {
				preferred := fmt.Sprintf("Field%d", i)
				if len(source) == len(c.Fields) {
					preferred = exportedName(source[i])
				}
				info.fields[0] = append(info.fields[0], fieldNames.allocate(preferred))
			}
			continue
		}
		info.ctorFuncs = make([]string, n)
		info.isMethods = make([]string, n)
		info.asMethods = make([]string, n)
		for i, c := range info.decl.Constructors {
			for j := range c.Fields {
				info.fields[i] = append(info.fields[i], fmt.Sprintf("f%d_%d", i, j))
			}
			ctors = append(ctors, candidate{c.Name, ctorRef{name, i}})
		}
		methods := allocator{}
		sorted := slices.Clone(info.decl.Constructors)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
		for _, c := range sorted {
			i := m.ctors[c.Name].index
			short := exportedName(c.Name)
			info.isMethods[i] = methods.allocate("Is" + short)
			if len(c.Fields) > 0 {
				info.asMethods[i] = methods.allocate("As" + short)
			}
		}
		if info.enum {
			info.stringer = methods.allocate("String")
		}
		if info.kind == kindBoxed {
			base := lowerFirst(info.goName)
			info.nodeName = internal.allocateInternal(base + "Node")
			info.zeroVar = internal.allocateInternal(base + "Zero")
			info.nullaries = make([]string, n)
			for i, c := range info.decl.Constructors {
				if len(c.Fields) == 0 && i != info.zeroCtor {
					info.nullaries[i] = internal.allocateInternal(base + exportedName(c.Name))
				}
			}
		}
	}
	sort.Slice(ctors, func(i, j int) bool { return ctors[i].lean < ctors[j].lean })
	for _, c := range ctors {
		info := m.types[c.ref.typ]
		info.ctorFuncs[c.ref.index] = exported.allocate(info.goName + exportedName(c.lean))
	}
	for _, root := range m.roots {
		m.funcs[root].exported = exported.allocate(exportedName(root))
	}
	for _, decl := range m.program.Functions {
		f := m.funcs[decl.Name]
		switch {
		case m.prefix != "":
			f.callName = internal.allocateInternal(m.prefix + exportedName(decl.Name))
		case f.elided:
		case f.root && !f.depth && f.fold == nil:
			f.callName = f.exported
		default:
			f.callName = internal.allocateInternal(lowerFirst(exportedName(decl.Name)))
		}
	}
}

func (m *model) goType(lean string) string { return m.types[lean].goName }

func (m *model) zero(lean string) string {
	switch m.types[lean].kind {
	case kindScalar:
		return "0"
	case kindBool:
		return "false"
	}
	return m.goType(lean) + "{}"
}
