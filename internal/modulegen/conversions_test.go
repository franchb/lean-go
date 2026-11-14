package modulegen

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"testing"
)

func TestWidthConversionGeneratedPayloads(t *testing.T) {
	p := testProgram()
	p.Schema, p.Profile = moduleir.SchemaV11, moduleir.ProfileV9
	p.Root, p.Roots = "", []string{"Example.narrow", "Example.widen"}
	p.Types = []moduleir.TypeDecl{}
	p.API = &moduleir.APIMetadata{Records: map[string][]string{}}
	p.Functions = nil
	p.Provenance = nil
	for i, name := range p.Roots {
		operand, result, primitive := "UInt64", "UInt32", "UInt64.toUInt32"
		if i == 1 {
			operand, result, primitive = "UInt32", "UInt64", "UInt32.toUInt64"
		}
		p.Functions = append(p.Functions, moduleir.Function{Name: name, Params: []moduleir.Param{{ID: 1, Type: operand}}, Result: result, Body: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 2, Type: result}, Value: &moduleir.Value{Kind: "primitive", Name: primitive, Args: []int{1}}, Next: &moduleir.Code{Op: "return", Var: 2}}})
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: name, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, expression := range []string{"value.scalar64 = uint64(f.slots[0].scalar)", "value.scalar = uint32(f.slots[0].scalar64)"} {
		if !bytes.Contains(files["control.go"], []byte(expression)) {
			t.Fatal("wrong operand/result payload", expression)
		}
	}
	var manifest struct {
		Schema, Generator string
		Primitives        []moduleir.Primitive
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "lean-go-manifest/v11" || manifest.Generator != Version || len(manifest.Primitives) != 2 {
		t.Fatal(manifest)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestConversions(t *testing.T){
 l:=DefaultLimits()
 for _,n:=range []uint64{0,1,2147483648,4294967295,4294967296,4294967297,9223372036854775808,18446744073709551615,12297829382473034410}{
  input:=&Input{Type:"UInt64",UInt64:n};v,e:=Import(input,l);if e!=nil{t.Fatal(e)};input.UInt64=42
  raw,e:=RunRoot("Example.narrow",[]Value{v},l);if e!=nil{t.Fatal(e)};low,ok:=raw.UInt32();if !ok||uint64(low)!=n%4294967296{t.Fatal(n,low)}
  if _,ok:=raw.UInt64();ok{t.Fatal("inactive result payload")};if got,_:=v.UInt64();got!=n{t.Fatal("input mutated")}
  typed,e:=RunNarrow(n,l);if e!=nil||typed!=low{t.Fatal(typed,e)}
  wide,e:=RunWiden(low,l);if e!=nil||wide!=n%4294967296{t.Fatal(wide,e)}
  small,e:=Import(&Input{Type:"UInt32",UInt32:low},l);if e!=nil{t.Fatal(e)}
  raw,e=RunRoot("Example.widen",[]Value{small},l);if e!=nil{t.Fatal(e)};got,ok:=raw.UInt64();if !ok||got!=wide{t.Fatal(got)}
  if _,ok:=raw.UInt32();ok{t.Fatal("inactive wide payload")}
 }
 l.MaxAllocations=1;l.MaxSteps=2
 if _,e:=RunWiden(4294967295,l);e!=nil{t.Fatal(e)};if _,e:=RunNarrow(18446744073709551615,l);e!=nil{t.Fatal(e)}
 l.MaxSteps=1
 for _,root:=range []string{"Example.narrow","Example.widen"}{
  input:=&Input{Type:"UInt64",UInt64:1};if root=="Example.widen"{input=&Input{Type:"UInt32",UInt32:1}}
  v,e:=Import(input,DefaultLimits());if e!=nil{t.Fatal(e)}
  out,e:=RunRoot(root,[]Value{v},l);if !errors.Is(e,ErrResource)||out.Type()!=""{t.Fatal("partial result",out,e)}
 }
 if out,e:=RunWiden(1,l);out!=0||!errors.Is(e,ErrResource){t.Fatal(out,e)}
 if out,e:=RunNarrow(1,l);out!=0||!errors.Is(e,ErrResource){t.Fatal(out,e)}
}
`)
}
