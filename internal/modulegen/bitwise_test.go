package modulegen

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func bitwiseProgram() *moduleir.Program {
	p := typedProgram()
	p.Schema, p.Profile, p.Root = moduleir.SchemaV9, moduleir.ProfileV7, ""
	p.Types = []moduleir.TypeDecl{}
	p.API.Records = map[string][]string{}
	p.Functions = nil
	p.Provenance = nil
	for _, width := range []string{"32", "64"} {
		for _, op := range []string{"land", "lor", "xor", "complement", "shiftRight"} {
			name, typ := "Example."+op+width, "UInt"+width
			args := []int{1, 2}
			if op == "complement" {
				args = args[:1]
			}
			p.Functions = append(p.Functions, moduleir.Function{Name: name, Params: []moduleir.Param{{ID: 1, Type: typ}, {ID: 2, Type: typ}}, Result: typ,
				Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 3, Type: typ}, Value: &moduleir.Value{Kind: "primitive", Name: typ + "." + op, Args: args}, Next: &moduleir.Code{Op: "return", Var: 3}}})
		}
	}
	sort.Slice(p.Functions, func(i, j int) bool { return p.Functions[i].Name < p.Functions[j].Name })
	for _, f := range p.Functions {
		p.Roots = append(p.Roots, f.Name)
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: f.Name, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	return p
}

func TestBitwiseRawAndTypedExecution(t *testing.T) {
	files, err := Generate(bitwiseProgram())
	if err != nil {
		t.Fatal(err)
	}
	var consumer strings.Builder
	consumer.WriteString("package extracted\nimport(\"errors\";\"testing\")\n")
	for _, width := range []int{32, 64} {
		consumer.WriteString(strings.ReplaceAll(`func TestBitsWIDTH(t *testing.T){
   l:=DefaultLimits()
   values:=[]uintWIDTH{0,1,1<<(WIDTH-1),^uintWIDTH(0),0xaaaaaaaa,0x55555555}
   counts:=[]uintWIDTH{0,WIDTH-1,WIDTH,WIDTH+1,^uintWIDTH(0)}
   for _,a:=range values { for _,b:=range append(values,counts...) {
    x,e:=Import(&Input{Type:"UIntWIDTH",UIntWIDTH:a},l);if e!=nil{t.Fatal(e)}
    y,e:=Import(&Input{Type:"UIntWIDTH",UIntWIDTH:b},l);if e!=nil{t.Fatal(e)}
    cases:=[]struct{name string; call func(uintWIDTH,uintWIDTH,Limits)(uintWIDTH,error); want uintWIDTH}{
     {"landWIDTH",RunLandWIDTH,a&b},{"lorWIDTH",RunLorWIDTH,a|b},{"xorWIDTH",RunXorWIDTH,a^b},
     {"complementWIDTH",RunComplementWIDTH,^a},{"shiftRightWIDTH",RunShiftRightWIDTH,a>>(b%WIDTH)}}
    for _,tc:=range cases {
     raw,e:=RunRoot("Example."+tc.name,[]Value{x,y},l);got,ok:=raw.UIntWIDTH()
     typed,te:=tc.call(a,b,l)
     if e!=nil||te!=nil||!ok||got!=tc.want||typed!=tc.want{t.Fatal(tc.name,a,b,got,typed,tc.want,e,te)}
     for _,change:=range []func(*Limits){func(l *Limits){l.MaxSteps=1},func(l *Limits){l.MaxSlots=2}} {
      tiny:=l;change(&tiny);raw,e:=RunRoot("Example."+tc.name,[]Value{x,y},tiny);typed,te:=tc.call(a,b,tiny)
      if !errors.Is(e,ErrResource)||!errors.Is(te,ErrResource)||raw!=(Value{})||typed!=0{t.Fatal("partial resource result",tc.name,e,te)}
     }
    }
   }}
  }
`, "WIDTH", strconv.Itoa(width)))
	}
	runGenerated(t, files, consumer.String())
}

func TestBitwiseManifestAndSingleRoot(t *testing.T) {
	p := bitwiseProgram()
	for _, multi := range []bool{true, false} {
		if !multi {
			p.Root = p.Roots[0]
			p.Roots = nil
			p.Functions = p.Functions[:1]
			p.Provenance = p.Provenance[:1]
			p.Schema, p.Profile = moduleir.SchemaV8, moduleir.ProfileV6
		}
		files, err := Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Schema     string
			Primitives []moduleir.Primitive
			API        goAPI `json:"go_api"`
		}
		if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		expected, count := "lean-go-manifest/v8", 1
		if multi {
			expected, count = "lean-go-manifest/v9", 10
		}
		if manifest.Schema != expected || len(manifest.Primitives) != count {
			t.Fatal(manifest)
		}
		for _, primitive := range manifest.Primitives {
			arity := 2
			if strings.HasSuffix(primitive.Name, ".complement") {
				arity = 1
			}
			if len(primitive.Params) != arity || primitive.Semantics == "" {
				t.Fatal(primitive)
			}
		}
		again, err := Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if !bytes.Equal(data, again[name]) {
				t.Fatal("nondeterministic", name)
			}
		}
		if !multi {
			if manifest.API.Entrypoint != "RunTyped" || len(manifest.API.Entrypoints) != 0 {
				t.Fatal(manifest.API)
			}
			runGenerated(t, files, `package extracted
import "testing"
func TestUnary(t *testing.T){l:=DefaultLimits();l.MaxAllocations=1;l.MaxOutputNodes=1;l.MaxSteps=2;out,e:=RunTyped(0,99,l);if e!=nil||out!=^uint32(0){t.Fatal(out,e)}}`)
		}
	}
}
