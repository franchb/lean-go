package modulegen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

// Scalar mappings keep immediate conversion separate from structured handles.
// A new native scalar can supply its own node helpers without changing traversal.
type apiScalar struct {
	GoType, Zero, Node, Read string
}

func apiScalars(p *moduleir.Program) map[string]apiScalar {
	scalars := map[string]apiScalar{"UInt32": {"uint32", "0", "apiUInt32Node", "apiReadUInt32"}}
	if moduleir.HasUInt64(p.Schema) {
		scalars["UInt64"] = apiScalar{"uint64", "0", "apiUInt64Node", "apiReadUInt64"}
	}
	if moduleir.HasUInt8(p.Schema) {
		scalars["UInt8"] = apiScalar{"uint8", "0", "apiUInt8Node", "apiReadUInt8"}
	}
	for _, typ := range p.Types {
		if canonicalAPIBool(typ) {
			scalars[typ.Name] = apiScalar{"bool", "false", "apiBoolNode", "apiReadBool"}
		}
	}
	return scalars
}

func canonicalAPIBool(typ moduleir.TypeDecl) bool {
	if typ.Name != "Bool" || len(typ.Constructors) != 2 {
		return false
	}
	for tag, name := range []string{"Bool.false", "Bool.true"} {
		ctor := typ.Constructors[tag]
		if ctor.Name != name || ctor.Tag != tag || len(ctor.Fields) != 0 {
			return false
		}
	}
	return true
}

type apiEmitter struct {
	g       *generator
	names   *goAPI
	scalars map[string]apiScalar
}

func (g *generator) emitAPI() ([]byte, error) {
	template, err := g.renderTemplate("api.go.tmpl")
	if err != nil {
		return nil, err
	}
	w := bytes.NewBuffer(template)
	e := apiEmitter{g: g, names: g.api, scalars: apiScalars(g.program)}
	if _, ok := e.scalars["Bool"]; ok {
		typ := g.types["Bool"]
		fmt.Fprintf(w, "func apiBoolNode(value bool)*node{tag:=0;if value{tag=1};return &node{typ:%d,tag:tag,size:1,depth:1}}\n", typ)
		inactive := ""
		if moduleir.HasUInt64(g.program.Schema) {
			inactive = "||n.scalar64!=0"
		}
		if moduleir.HasUInt8(g.program.Schema) {
			inactive += "||n.scalar8!=0"
		}
		fmt.Fprintf(w, "func apiReadBool(n *node)(bool,bool){if !apiNode(n,%d)||len(n.fields)!=0||n.scalar!=0%s||n.size!=1||n.depth!=1||(n.tag!=0&&n.tag!=1){return false,false};return n.tag==1,true}\n", typ, inactive)
	}
	for _, typ := range g.program.Types {
		if _, scalar := e.scalars[typ.Name]; scalar {
			continue
		}
		fmt.Fprintf(w, "// %s is an immutable value; its zero value is invalid.\ntype %s struct{v Value}\n", e.names.Types[typ.Name], e.names.Types[typ.Name])
		for _, ctor := range typ.Constructors {
			e.constructor(w, typ, ctor)
			if fields, record := g.program.API.Records[typ.Name]; record {
				e.accessors(w, typ, ctor, fields)
			} else {
				e.destructor(w, typ, ctor)
			}
		}
	}
	for _, root := range moduleir.RootNames(g.program) {
		e.run(w, root)
	}
	return w.Bytes(), nil
}

func (e *apiEmitter) zero(typ string) string {
	if scalar, ok := e.scalars[typ]; ok {
		return scalar.Zero
	}
	return e.names.Types[typ] + "{}"
}

func (e *apiEmitter) node(typ, value string) string {
	if scalar, ok := e.scalars[typ]; ok {
		return scalar.Node + "(" + value + ")"
	}
	return value + ".v.n"
}

func (e *apiEmitter) parameters(fields []string) string {
	var out strings.Builder
	for i, field := range fields {
		fmt.Fprintf(&out, "a%d %s,", i, e.names.Types[field])
	}
	return out.String()
}

func (e *apiEmitter) constructor(w *bytes.Buffer, typ moduleir.TypeDecl, ctor moduleir.Constructor) {
	name := e.names.Types[typ.Name]
	fmt.Fprintf(w, "func %s(%sl Limits)(%s,error){v,err:=apiMake(%d,%d,[]*node{", e.names.Constructors[ctor.Name], e.parameters(ctor.Fields), name, e.g.types[typ.Name], ctor.Tag)
	for i, field := range ctor.Fields {
		fmt.Fprintf(w, "%s,", e.node(field, fmt.Sprintf("a%d", i)))
	}
	fmt.Fprintf(w, "},l);if err!=nil{return %s{},err};return %s{v:v},nil}\n", name, name)
}

func (e *apiEmitter) read(w *bytes.Buffer, typ, expression, name, failure string) {
	if scalar, ok := e.scalars[typ]; ok {
		fmt.Fprintf(w, "%s,ok:= %s(%s);if !ok{%s}\n", name, scalar.Read, expression, failure)
	} else {
		fmt.Fprintf(w, "if !apiNode(%s,%d){%s};%s:=%s{v:Value{%s}}\n", expression, e.g.types[typ], failure, name, e.names.Types[typ], expression)
	}
}

func (e *apiEmitter) accessors(w *bytes.Buffer, typ moduleir.TypeDecl, ctor moduleir.Constructor, fields []string) {
	for i, field := range fields {
		fieldType := ctor.Fields[i]
		failure := "return " + e.zero(fieldType) + ",false"
		fmt.Fprintf(w, "func (v %s)%s()(%s,bool){if !apiMatches(v.v,%d,%d,%d){%s}\n", e.names.Types[typ.Name], e.names.Accessors[typ.Name][field], e.names.Types[fieldType], e.g.types[typ.Name], ctor.Tag, len(fields), failure)
		e.read(w, fieldType, fmt.Sprintf("v.v.n.fields[%d]", i), "value", failure)
		w.WriteString("return value,true}\n")
	}
}

func (e *apiEmitter) destructor(w *bytes.Buffer, typ moduleir.TypeDecl, ctor moduleir.Constructor) {
	names := e.names.Destructors[ctor.Name]
	match := fmt.Sprintf("apiMatches(v.v,%d,%d,%d)", e.g.types[typ.Name], ctor.Tag, len(ctor.Fields))
	fmt.Fprintf(w, "func (v %s)%s()bool{return %s}\n", e.names.Types[typ.Name], names.Is, match)
	if len(ctor.Fields) == 0 {
		fmt.Fprintf(w, "func (v %s)%s()bool{return %s}\n", e.names.Types[typ.Name], names.As, match)
		return
	}
	var returns, zeros, values []string
	for i, field := range ctor.Fields {
		returns = append(returns, e.names.Types[field])
		zeros = append(zeros, e.zero(field))
		values = append(values, fmt.Sprintf("a%d", i))
	}
	failure := "return " + strings.Join(zeros, ",") + ",false"
	fmt.Fprintf(w, "func (v %s)%s()(%s,bool){if !%s{%s}\n", e.names.Types[typ.Name], names.As, strings.Join(returns, ","), match, failure)
	for i, field := range ctor.Fields {
		e.read(w, field, fmt.Sprintf("v.v.n.fields[%d]", i), values[i], failure)
	}
	fmt.Fprintf(w, "return %s,true}\n", strings.Join(values, ","))
}

func (e *apiEmitter) run(w *bytes.Buffer, name string) {
	root := e.g.functions[name].decl
	fields := make([]string, len(root.Params))
	for i, param := range root.Params {
		fields[i] = param.Type
	}
	if moduleir.HasMultipleRoots(e.g.program.Schema) {
		entry := e.names.Entrypoints[name]
		fmt.Fprintf(w, "// %s invokes its selected Lean root through RunRoot and its existing limits.\nfunc %s(%sl Limits)(%s,error){out,err:=RunRoot(%q,[]Value{", entry, entry, e.parameters(fields), e.names.Types[root.Result], name)
	} else {
		fmt.Fprintf(w, "// RunTyped invokes the selected Lean root through Run and its existing limits.\nfunc RunTyped(%sl Limits)(%s,error){out,err:=Run([]Value{", e.parameters(fields), e.names.Types[root.Result])
	}
	for i, field := range fields {
		fmt.Fprintf(w, "{n:%s},", e.node(field, fmt.Sprintf("a%d", i)))
	}
	fmt.Fprintf(w, "},l);if err!=nil{return %s,err}\n", e.zero(root.Result))
	e.read(w, root.Result, "out.n", "value", "return "+e.zero(root.Result)+",ErrMalformed")
	w.WriteString("return value,nil}\n")
}
