package modulegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/franchb/lean-go/internal/moduleir"
)

func typedProgram() *moduleir.Program {
	p := testProgram()
	p.Schema, p.Profile = moduleir.SchemaV5, moduleir.ProfileV3
	p.API = &moduleir.APIMetadata{Records: map[string][]string{}}
	return p
}

func TestTypedTreeFacade(t *testing.T) {
	files, err := Generate(typedProgram())
	if err != nil {
		t.Fatal(err)
	}
	if len(files["api.go"]) == 0 {
		t.Fatal("v5 has no typed facade")
	}
	runGenerated(t, files, typedTreeTests)
}

const typedTreeTests = `package extracted
import("errors";"reflect";"testing")
func TestTypedTree(t *testing.T){
 l:=DefaultLimits();a,e:=NewTreeLeaf(1,l);if e!=nil{t.Fatal(e)};b,e:=NewTreeLeaf(2,l);if e!=nil{t.Fatal(e)};tree,e:=NewTreeBranch(a,b,l);if e!=nil{t.Fatal(e)}
 out,e:=RunTyped(tree,l);raw,re:=Run([]Value{tree.v},l);if e!=nil||re!=nil||!reflect.DeepEqual(out.v,raw){t.Fatal("typed/raw",out,raw,e,re)}
 left,right,ok:=out.AsBranch();if !ok||!out.IsBranch()||out.IsLeaf(){t.Fatal("branch discriminator")};x,xok:=left.AsLeaf();y,yok:=right.AsLeaf();if !xok||!yok||x!=2||y!=1{t.Fatal("typed fields",x,y)}
 if _,ok:=tree.AsLeaf();ok{t.Fatal("wrong variant accepted")};if _,_,ok:=a.AsBranch();ok{t.Fatal("wrong variant accepted")}
 invalid:=[]Tree{{},{v:Value{&node{typ:0,size:1,depth:1}}},{v:Value{&node{typ:1,tag:99,size:1,depth:1}}}}
 for _,v:=range invalid{if v.IsLeaf()||v.IsBranch(){t.Fatal("invalid discriminator")};if _,ok:=v.AsLeaf();ok{t.Fatal("invalid destructor")}}
 for _,v:=range invalid[:2]{if out,e:=NewTreeBranch(a,v,l);!errors.Is(e,ErrMalformed)||out!=(Tree{}){t.Fatal("malformed child",out,e)};if out,e:=RunTyped(v,l);!errors.Is(e,ErrMalformed)||out!=(Tree{}){t.Fatal("malformed root",out,e)}}
}
func TestTypedConstructorBudgets(t *testing.T){
 l:=DefaultLimits();leaf,e:=NewTreeLeaf(7,l);if e!=nil{t.Fatal(e)};shared,e:=NewTreeBranch(leaf,leaf,l);if e!=nil||shared.v.n.size!=5||shared.v.n.fields[0]!=shared.v.n.fields[1]{t.Fatal("immutable sharing",e)}
 fields:=[]*node{leaf.v.n,leaf.v.n};owned,e:=apiMake(1,1,fields,l);if e!=nil{t.Fatal(e)};fields[0]=nil;if owned.n.fields[0]!=leaf.v.n{t.Fatal("constructor retained mutable fields")}
 tiny:=l;tiny.MaxNodes=4;if out,e:=NewTreeBranch(leaf,leaf,tiny);!errors.Is(e,ErrResource)||out!=(Tree{}){t.Fatal("shared occurrences",e)};tiny=l;tiny.MaxDepth=2;if _,e:=NewTreeBranch(leaf,leaf,tiny);!errors.Is(e,ErrResource){t.Fatal("constructor depth",e)}
 for i:=0;i<7;i++{for _,bad:=range []uint64{0,^uint64(0)}{tiny=l;fs:=[]*uint64{&tiny.MaxNodes,&tiny.MaxDepth,&tiny.MaxOutputNodes,&tiny.MaxAllocations,&tiny.MaxSteps,&tiny.MaxFrames,&tiny.MaxSlots};*fs[i]=bad;if out,e:=NewTreeLeaf(1,tiny);!errors.Is(e,ErrLimits)||out!=(Tree{}){t.Fatal("constructor limits",i,e)}}}
 for _,change:=range []func(*Limits){func(l *Limits){l.MaxNodes=4},func(l *Limits){l.MaxDepth=2},func(l *Limits){l.MaxOutputNodes=4},func(l *Limits){l.MaxAllocations=1},func(l *Limits){l.MaxSteps=1},func(l *Limits){l.MaxFrames=1},func(l *Limits){l.MaxSlots=1}}{tiny=l;change(&tiny);out,e:=RunTyped(shared,tiny);raw,re:=Run([]Value{shared.v},tiny);if !errors.Is(e,ErrResource)||!errors.Is(re,e)||out!=(Tree{})||raw!=(Value{}){t.Fatal("Run budget parity",tiny,e,re)}}
 current:=leaf;for i:=0;i<20;i++{next,e:=NewTreeBranch(current,current,l);if e!=nil{if !errors.Is(e,ErrResource){t.Fatal(e)};return};current=next};t.Fatal("sharing bypassed occurrence cap")
}
`

func typedRecordProgram() *moduleir.Program {
	p := typedProgram()
	p.Types = []moduleir.TypeDecl{
		{Name: "Bool", Constructors: []moduleir.Constructor{{Name: "Bool.false", Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}},
		{Name: "Example.Config", Constructors: []moduleir.Constructor{{Name: "Example.Config.mk", Fields: []string{"Example.Count", "Bool"}}}},
		{Name: "Example.Count", Constructors: []moduleir.Constructor{{Name: "Example.Count.mk", Fields: []string{"UInt32"}}}},
	}
	p.API.Records = map[string][]string{"Example.Config": {"count", "enabled"}, "Example.Count": {"value"}}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "Example.Config"}}, Result: "Example.Config", Body: &moduleir.Code{Op: "return", Var: 1}}}
	p.Provenance = p.Provenance[:1]
	return p
}

func TestTypedRecordFacade(t *testing.T) {
	files, err := Generate(typedRecordProgram())
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestRecords(t *testing.T){l:=DefaultLimits();count,e:=NewCount(42,l);if e!=nil{t.Fatal(e)};config,e:=NewConfig(count,true,l);if e!=nil{t.Fatal(e)};out,e:=RunTyped(config,l);if e!=nil{t.Fatal(e)};c,ok:=out.Count();n,nok:=c.Value();enabled,eok:=out.Enabled();if !ok||!nok||!eok||n!=42||!enabled{t.Fatal("getters",n,enabled)};if _,ok:=(Count{}).Value();ok{t.Fatal("zero getter")};if _,ok:=(Count{v:config.v}).Value();ok{t.Fatal("wrong type getter")};if _,ok:=(Config{v:count.v}).Enabled();ok{t.Fatal("wrong type bool getter")};if _,e=NewConfig(Count{},false,l);!errors.Is(e,ErrMalformed){t.Fatal("zero nested record",e)};falseConfig,e:=NewConfig(count,false,l);if e!=nil{t.Fatal(e)};enabled,ok=falseConfig.Enabled();if !ok||enabled{t.Fatal("false bool")}}
`)
}

func TestTypedScalarRoots(t *testing.T) {
	for _, typ := range []string{"UInt32", "Bool"} {
		t.Run(typ, func(t *testing.T) {
			p := typedRecordProgram()
			p.API.Records = map[string][]string{}
			p.Types = p.Types[:1]
			if typ == "UInt32" {
				p.Types = []moduleir.TypeDecl{}
			}
			p.Functions[0].Params[0].Type, p.Functions[0].Result = typ, typ
			files, err := Generate(p)
			if err != nil {
				t.Fatal(err)
			}
			source := `package extracted
import("errors";"testing")
func TestScalar(t *testing.T){for _,input:=range []SCALAR{VALUES}{out,e:=RunTyped(input,DefaultLimits());if e!=nil||out!=input{t.Fatal(out,e)}};l:=DefaultLimits();l.MaxNodes=0;if out,e:=RunTyped(VALUE,l);!errors.Is(e,ErrLimits)||out!=ZERO{t.Fatal("zero result",out,e)}}`
			replacements := []string{"SCALAR", "uint32", "VALUES", "0,1,^uint32(0)", "VALUE", "1", "ZERO", "0"}
			if typ == "Bool" {
				replacements = []string{"SCALAR", "bool", "VALUES", "false,true", "VALUE", "true", "ZERO", "false"}
			}
			source = strings.NewReplacer(replacements...).Replace(source)
			runGenerated(t, files, source)
		})
	}
}

func TestTypedManifestAndLegacyCompatibility(t *testing.T) {
	p := typedRecordProgram()
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Schema    string `json:"schema"`
		Generator string `json:"generator"`
		API       struct {
			Entrypoint   string                       `json:"entrypoint"`
			Types        map[string]string            `json:"types"`
			Constructors map[string]string            `json:"constructors"`
			Accessors    map[string]map[string]string `json:"accessors"`
		} `json:"go_api"`
	}
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.Schema != "lean-go-manifest/v5" || m.Generator != Version || m.API.Entrypoint != "RunTyped" || m.API.Types["UInt32"] != "uint32" || m.API.Types["Bool"] != "bool" || m.API.Constructors["Example.Count.mk"] != "NewCount" || m.API.Accessors["Example.Count"]["value"] != "Value" {
		t.Fatalf("wrong typed manifest: %+v", m)
	}
	for _, legacy := range []*moduleir.Program{testProgram(), numericProgram(), joinTestProgram()} {
		old, err := Generate(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := old["api.go"]; ok || bytes.Contains(old["manifest.json"], []byte(`"go_api"`)) {
			t.Fatal("legacy facade changed")
		}
		legacy.Schema, legacy.Profile = moduleir.SchemaV5, moduleir.ProfileV3
		legacy.API = &moduleir.APIMetadata{Records: map[string][]string{}}
		current, err := Generate(legacy)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"runtime.go", "schema.go", "control.go"} {
			if !bytes.Equal(old[name], current[name]) {
				t.Fatal("legacy raw output changed", name)
			}
		}
	}
}

func namingProgram() *moduleir.Program {
	p := typedProgram()
	p.Types = nil
	for _, name := range []string{"A.Tree", "B.Tree", "C.tree", "N.NewTreeLeaf", "N.RunTyped", "N.Value", "N.Input", "N.type", "N.uint32", "N.éclair", "N.Éclair", "N.树", "N.x-y", "N.x_y", `N.x"); panic("injection"); //`, "Z.Other", "Z.Record"} {
		p.Types = append(p.Types, moduleir.TypeDecl{Name: name, Constructors: []moduleir.Constructor{{Name: name + ".mk", Fields: []string{"UInt32"}}}})
	}
	sort.Slice(p.Types, func(i, j int) bool { return p.Types[i].Name < p.Types[j].Name })
	p.Types[0].Constructors = []moduleir.Constructor{
		{Name: "A.Tree.Leaf", Tag: 0, Fields: []string{}},
		{Name: "A.Tree.leaf", Tag: 1, Fields: []string{"UInt32"}},
		{Name: "A.Tree.leaf-", Tag: 2, Fields: []string{}},
		{Name: "A.Tree.leaf_", Tag: 3, Fields: []string{}},
	}
	fields := []string{"value", "Value", "value_", "value-", "type", "树", "éclair", "Éclair", "RunTyped", "AsLeaf", "v", `x"); panic(0); //`}
	p.API.Records = map[string][]string{"Z.Other": {"value"}, "Z.Record": fields}
	last := &p.Types[len(p.Types)-1].Constructors[0]
	last.Fields = make([]string, len(fields))
	for i := range last.Fields {
		last.Fields[i] = "UInt32"
	}
	params := make([]moduleir.Param, len(p.Types))
	for i, typ := range p.Types {
		params[i] = moduleir.Param{ID: i + 1, Type: typ.Name}
	}
	p.Functions = []moduleir.Function{{Name: p.Root, Params: params, Result: p.Types[0].Name, Body: &moduleir.Code{Op: "return", Var: 1}}}
	p.Provenance = p.Provenance[:1]
	return p
}

func TestTypedNameCollisionsAndDeterminism(t *testing.T) {
	p := namingProgram()
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		API goAPI `json:"go_api"`
	}
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	for lean, want := range map[string]string{"A.Tree": "Tree", "B.Tree": "Tree2", "C.tree": "Tree3", "N.RunTyped": "RunTyped2", "N.Value": "Value2", "N.Input": "Input2", "N.NewTreeLeaf": "NewTreeLeaf", "N.type": "Type", "N.树": "X树", "N.Éclair": "Éclair", "N.éclair": "Éclair2"} {
		if got := m.API.Types[lean]; got != want {
			t.Errorf("%s mapped to %s, want %s", lean, got, want)
		}
	}
	if m.API.Constructors["A.Tree.Leaf"] != "NewTreeLeaf2" || m.API.Constructors["A.Tree.leaf"] != "NewTreeLeaf3" || m.API.Accessors["Z.Record"]["Value"] != "Value" || m.API.Accessors["Z.Record"]["value"] != "Value2" || m.API.Destructors["A.Tree.leaf"].As != "AsLeaf2" {
		t.Fatalf("collision allocation: %+v", m.API)
	}
	for _, names := range []map[string]string{m.API.Types, m.API.Constructors, m.API.Accessors["Z.Record"]} {
		seen := map[string]bool{}
		for _, name := range names {
			if name == "uint32" {
				continue
			}
			first, _ := utf8.DecodeRuneInString(name)
			if !token.IsIdentifier(name) || !unicode.IsUpper(first) || seen[name] {
				t.Fatal("unsafe or repeated identifier", name)
			}
			seen[name] = true
		}
	}
	// Records are metadata maps; insertion order cannot choose public names.
	p.API.Records = map[string][]string{}
	for _, name := range []string{"Z.Record", "Z.Other"} {
		if name == "Z.Record" {
			p.API.Records[name] = []string{"value", "Value", "value_", "value-", "type", "树", "éclair", "Éclair", "RunTyped", "AsLeaf", "v", `x"); panic(0); //`}
		} else {
			p.API.Records[name] = []string{"value"}
		}
	}
	again, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if !bytes.Equal(data, again[name]) {
			t.Fatal("map order changed output", name)
		}
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestNames(t *testing.T){l:=DefaultLimits();zero,e:=NewTreeLeaf2(l);if e!=nil||!zero.IsLeaf()||!zero.AsLeaf(){t.Fatal("nullary",e)};value,e:=NewTreeLeaf3(7,l);n,ok:=value.AsLeaf2();if e!=nil||!ok||n!=7||value.IsLeaf(){t.Fatal("case collision",n,e)};if (Tree{}).AsLeaf(){t.Fatal("zero nullary")};a,e:=NewTreeLeaf_(l);if e!=nil||!a.IsLeaf_(){t.Fatal("punctuation constructor",e)};b,e:=NewTreeLeaf_2(l);if e!=nil||!b.IsLeaf_2(){t.Fatal("punctuation collision",e)}}`)
}

func TestAPIRuntimeNamesReserved(t *testing.T) {
	files, err := Generate(typedProgram())
	if err != nil {
		t.Fatal(err)
	}
	reserved := map[string]bool{}
	for _, name := range strings.Fields(apiReservedNames) {
		reserved[name] = true
	}
	for _, file := range []string{"runtime.go", "schema.go", "control.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, files[file], 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			var names []string
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					names = append(names, decl.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						names = append(names, spec.Name.Name)
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							names = append(names, name.Name)
						}
					}
				}
			}
			for _, name := range names {
				if !reserved[name] {
					t.Errorf("runtime name %s needs reservation", name)
				}
			}
		}
	}
}

func TestTypedCanonicalBoolAndMalformedScalars(t *testing.T) {
	canonical := typedRecordProgram().Types[0]
	if !canonicalAPIBool(canonical) {
		t.Fatal("canonical Bool not mapped")
	}
	for _, change := range []func(*moduleir.TypeDecl){func(x *moduleir.TypeDecl) { x.Name = "Other.Bool" }, func(x *moduleir.TypeDecl) { x.Constructors = x.Constructors[:1] }, func(x *moduleir.TypeDecl) { x.Constructors[0].Name = "Bool.no" }, func(x *moduleir.TypeDecl) { x.Constructors[0].Tag = 1 }, func(x *moduleir.TypeDecl) { x.Constructors[0].Fields = []string{"UInt32"} }} {
		typ := canonical
		typ.Constructors = append([]moduleir.Constructor(nil), canonical.Constructors...)
		change(&typ)
		if canonicalAPIBool(typ) {
			t.Fatal("noncanonical Bool mapped", typ)
		}
	}
	files, err := Generate(typedRecordProgram())
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestMalformedScalar(t *testing.T){for _,n:=range []*node{nil,{typ:1,tag:2,size:1,depth:1},{typ:1,scalar:1,size:1,depth:1},{typ:1,size:1,depth:1,fields:[]*node{{typ:0,size:1,depth:1}}},{typ:1,size:0,depth:1},{typ:1,size:1,depth:0},{typ:0,size:1,depth:1}} {if _,ok:=apiReadBool(n);ok{t.Fatal("invalid Bool accepted",n)}};for _,n:=range []*node{nil,{typ:1,size:1,depth:1},{typ:0,tag:1,size:1,depth:1},{typ:0,size:2,depth:1},{typ:0,size:1,depth:2},{typ:0,size:1,depth:1,fields:[]*node{{typ:0,size:1,depth:1}}}} {if _,ok:=apiReadUInt32(n);ok{t.Fatal("invalid UInt32 accepted",n)}}}
`)
}

func TestTypedNullaryScalarRoots(t *testing.T) {
	for _, typ := range []string{"UInt32", "Bool"} {
		t.Run(typ, func(t *testing.T) {
			p := typedProgram()
			p.Types = []moduleir.TypeDecl{}
			value := &moduleir.Value{Kind: "uint32", Name: "4294967295", Args: []int{}}
			want := "^uint32(0)"
			if typ == "Bool" {
				p.Types = typedRecordProgram().Types[:1]
				value = &moduleir.Value{Kind: "constructor", Name: "Bool.true", Args: []int{}}
				want = "true"
			}
			p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{}, Result: typ, Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 1, Type: typ}, Value: value, Next: &moduleir.Code{Op: "return", Var: 1}}}}
			p.Provenance = p.Provenance[:1]
			files, err := Generate(p)
			if err != nil {
				t.Fatal(err)
			}
			runGenerated(t, files, fmt.Sprintf("package extracted\nimport \"testing\"\nfunc TestNullary(t *testing.T){out,e:=RunTyped(DefaultLimits());if e!=nil||out!=%s{t.Fatal(out,e)}}", want))
		})
	}
}
