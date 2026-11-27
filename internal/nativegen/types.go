package nativegen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

func (m *model) emitTypes(w *bytes.Buffer) {
	w.WriteString(header)
	for _, name := range m.order {
		info := m.types[name]
		switch info.kind {
		case kindRecord:
			m.emitRecord(w, info)
		case kindSum:
			m.emitSum(w, info)
		case kindBoxed:
			m.emitBoxed(w, info)
		}
	}
}

func (m *model) emitRecord(w *bytes.Buffer, info *typeInfo) {
	c := info.decl.Constructors[0]
	fmt.Fprintf(w, "\n// %s is the Lean structure %s, built by %s.\n// Every value, including the zero value, is a valid Lean value.\n", info.goName, info.lean, c.Name)
	fmt.Fprintf(w, "type %s struct {\n", info.goName)
	for i, field := range c.Fields {
		fmt.Fprintf(w, "%s %s\n", info.fields[0][i], m.goType(field))
	}
	w.WriteString("}\n")
}

func (m *model) parameters(fields []string) string {
	params := make([]string, len(fields))
	for i, field := range fields {
		params[i] = fmt.Sprintf("a%d %s", i, m.goType(field))
	}
	return strings.Join(params, ", ")
}

func (m *model) results(fields []string) string {
	results := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		results = append(results, m.goType(field))
	}
	return strings.Join(append(results, "bool"), ", ")
}

func (m *model) zeros(fields []string) string {
	zeros := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		zeros = append(zeros, m.zero(field))
	}
	return strings.Join(append(zeros, "false"), ", ")
}

func (m *model) zeroDoc(info *typeInfo) string {
	c := info.decl.Constructors[info.zeroCtor]
	if len(c.Fields) == 0 {
		return c.Name
	}
	parts := []string{c.Name}
	for _, field := range c.Fields {
		parts = append(parts, "(zero "+field+")")
	}
	return strings.Join(parts, " ")
}

// constructorExpr builds a value of constructor index i from Go expressions.
func (m *model) constructorExpr(info *typeInfo, i int, args []string) string {
	c := info.decl.Constructors[i]
	switch info.kind {
	case kindBool:
		return fmt.Sprint(i == 1)
	case kindRecord:
		parts := make([]string, len(args))
		for j, arg := range args {
			parts[j] = info.fields[0][j] + ": " + arg
		}
		return info.goName + "{" + strings.Join(parts, ", ") + "}"
	}
	if len(c.Fields) == 0 && i == info.zeroCtor {
		return info.goName + "{}"
	}
	if info.kind == kindBoxed && len(c.Fields) == 0 {
		return fmt.Sprintf("%s{p: &%s}", info.goName, info.nullaries[i])
	}
	parts := []string{}
	if info.tags[i] != 0 {
		parts = append(parts, fmt.Sprintf("tag: %d", info.tags[i]))
	}
	for j, arg := range args {
		parts = append(parts, info.fields[i][j]+": "+arg)
	}
	if info.kind == kindBoxed {
		return fmt.Sprintf("%s{p: &%s{%s}}", info.goName, info.nodeName, strings.Join(parts, ", "))
	}
	return info.goName + "{" + strings.Join(parts, ", ") + "}"
}

func (m *model) emitSum(w *bytes.Buffer, info *typeInfo) {
	what := "inductive type"
	if info.enum {
		what = "enumeration"
	}
	fmt.Fprintf(w, "\n// %s is the Lean %s %s. Its fields are unexported, so every value\n// is a valid constructor application. The zero value is %s.\n", info.goName, what, info.lean, m.zeroDoc(info))
	fmt.Fprintf(w, "type %s struct {\ntag uint8\n", info.goName)
	m.fieldDecls(w, info)
	w.WriteString("}\n")
	m.emitAPI(w, info, "v")
}

func (m *model) fieldDecls(w *bytes.Buffer, info *typeInfo) {
	for i, c := range info.decl.Constructors {
		for j, field := range c.Fields {
			fmt.Fprintf(w, "%s %s\n", info.fields[i][j], m.goType(field))
		}
	}
}

func (m *model) emitBoxed(w *bytes.Buffer, info *typeInfo) {
	fmt.Fprintf(w, "\n// %s is the recursive Lean type %s. It is an immutable handle to a\n// shared node; values cannot form cycles. The zero value is %s.\n", info.goName, info.lean, m.zeroDoc(info))
	fmt.Fprintf(w, "type %s struct {\np *%s\n}\n\n", info.goName, info.nodeName)
	fmt.Fprintf(w, "type %s struct {\ntag uint8\n", info.nodeName)
	m.fieldDecls(w, info)
	w.WriteString("}\n\n")
	fmt.Fprintf(w, "// %s is the node a zero %s denotes. It is never modified.\nvar %s %s\n", info.zeroVar, info.goName, info.zeroVar, info.nodeName)
	for i, name := range info.nullaries {
		if name != "" {
			fmt.Fprintf(w, "var %s = %s{tag: %d}\n", name, info.nodeName, info.tags[i])
		}
	}
	fmt.Fprintf(w, "\nfunc (v %s) node() *%s {\nif v.p == nil {\nreturn &%s\n}\nreturn v.p\n}\n", info.goName, info.nodeName, info.zeroVar)
	m.emitAPI(w, info, "v.node()")
}

// emitAPI writes constructor functions and Is/As methods; value names the
// expression holding the tag and fields.
func (m *model) emitAPI(w *bytes.Buffer, info *typeInfo, value string) {
	for i, c := range info.decl.Constructors {
		args := make([]string, len(c.Fields))
		for j := range c.Fields {
			args[j] = fmt.Sprintf("a%d", j)
		}
		fmt.Fprintf(w, "\n// %s returns the Lean value %s.\nfunc %s(%s) %s {\nreturn %s\n}\n", info.ctorFuncs[i], applicationDoc(c), info.ctorFuncs[i], m.parameters(c.Fields), info.goName, m.constructorExpr(info, i, args))
		fmt.Fprintf(w, "\n// %s reports whether v was built by %s.\nfunc (v %s) %s() bool {\nreturn %s.tag == %d\n}\n", info.isMethods[i], c.Name, info.goName, info.isMethods[i], value, info.tags[i])
		if len(c.Fields) == 0 {
			continue
		}
		// A boxed value binds its node once. A sum reads the receiver in
		// place: copying it first would move every field of every
		// constructor, which for a sum carrying a record of more than four
		// fields is a memory-to-memory copy Go does not remove.
		n, bindNode := "v", ""
		if info.kind == kindBoxed {
			n, bindNode = "n", "n := "+value+"\n"
		}
		fields := make([]string, len(c.Fields))
		for j := range c.Fields {
			fields[j] = n + "." + info.fields[i][j]
		}
		fmt.Fprintf(w, "\n// %s returns the fields of a %s value and true, or zero values and false.\nfunc (v %s) %s() (%s) {\n%sif %s.tag != %d {\nreturn %s\n}\nreturn %s, true\n}\n",
			info.asMethods[i], c.Name, info.goName, info.asMethods[i], m.results(c.Fields), bindNode, n, info.tags[i], m.zeros(c.Fields), strings.Join(fields, ", "))
	}
	if info.stringer != "" {
		fmt.Fprintf(w, "\n// %s returns the full Lean constructor name.\nfunc (v %s) %s() string {\nswitch v.tag {\n", info.stringer, info.goName, info.stringer)
		for i, c := range info.decl.Constructors {
			if i == len(info.decl.Constructors)-1 {
				w.WriteString("default:\n")
			} else {
				fmt.Fprintf(w, "case %d:\n", info.tags[i])
			}
			fmt.Fprintf(w, "return %q\n", c.Name)
		}
		w.WriteString("}\n}\n")
	}
}

func applicationDoc(c moduleir.Constructor) string {
	if len(c.Fields) == 0 {
		return c.Name
	}
	parts := []string{c.Name}
	for i := range c.Fields {
		parts = append(parts, fmt.Sprintf("a%d", i))
	}
	return strings.Join(parts, " ")
}
