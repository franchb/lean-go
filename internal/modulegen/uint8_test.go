package modulegen

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"testing"
)

func byteRecordProgram() *moduleir.Program {
	p := testProgram()
	p.Schema, p.Profile = "lean-go-ir/v12", "pure-module/v10"
	fields := []string{"add", "sub", "mul", "left", "and", "or", "xor", "not", "right", "eq", "lt", "le", "wide", "narrow", "literal"}
	p.API = &moduleir.APIMetadata{Records: map[string][]string{"Example.Bytes": fields}}
	shapes := []string{"UInt8", "UInt8", "UInt8", "UInt8", "UInt8", "UInt8", "UInt8", "UInt8", "UInt8", "Bool", "Bool", "Bool", "UInt32", "UInt8", "UInt8"}
	p.Types = append(typedRecordProgram().Types[:1], moduleir.TypeDecl{Name: "Example.Bytes", Constructors: []moduleir.Constructor{{Name: "Example.Bytes.mk", Fields: shapes}}})
	f := moduleir.Function{Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "UInt8"}, {ID: 2, Type: "UInt8"}, {ID: 3, Type: "UInt32"}}, Result: "Example.Bytes"}
	args := make([]int, len(fields))
	for i := range args {
		args[i] = i + 4
	}
	code := &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 19, Type: f.Result}, Value: &moduleir.Value{Kind: "constructor", Name: "Example.Bytes.mk", Args: args}, Next: &moduleir.Code{Op: "return", Var: 19}}
	ops := []string{"add", "sub", "mul", "shiftLeft", "land", "lor", "xor", "complement", "shiftRight", "decEq", "decLt", "decLe", "toUInt32", "toUInt8", "literal"}
	for i := len(ops) - 1; i >= 0; i-- {
		name, a, kind := "UInt8."+ops[i], []int{1, 2}, "primitive"
		if i == 7 || i == 12 {
			a = []int{1}
		}
		if i == 13 {
			name = "UInt32.toUInt8"
			a = []int{3}
		}
		if i == 14 {
			name = "255"
			kind = "uint8"
			a = []int{}
		}
		code = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: i + 4, Type: shapes[i]}, Value: &moduleir.Value{Kind: kind, Name: name, Args: a}, Next: code}
	}
	f.Body = code
	p.Functions = []moduleir.Function{f}
	p.Provenance = p.Provenance[:1]
	return p
}

func TestUInt8GeneratedRawTypedSemanticsAndLimits(t *testing.T) {
	files, err := Generate(byteRecordProgram())
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema, Generator string
		Primitives        []moduleir.Primitive
		GoAPI             *goAPI `json:"go_api"`
	}
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != "lean-go-manifest/v12" || manifest.Generator != "lean-go-modulegen/v10" || len(manifest.Primitives) != 14 || manifest.GoAPI.Types["UInt8"] != "uint8" {
		t.Fatal(manifest)
	}
	for _, s := range []string{"value.scalar = uint32(f.slots[0].scalar8)", "value.scalar8 = uint8(f.slots[2].scalar)", " & 7)"} {
		if !bytes.Contains(files["control.go"], []byte(s)) {
			t.Fatal(s)
		}
	}
	runGenerated(t, files, byteConsumerTests)
}

const byteConsumerTests = `package extracted
import("errors";"math/rand";"testing")
func byteInput(t *testing.T,n uint8)Value{t.Helper();in:=&Input{Type:"UInt8",UInt8:n};v,e:=Import(in,DefaultLimits());if e!=nil{t.Fatal(e)};in.UInt8=42;return v}
func model(a,b uint8)[]uint8{x,y:=int(a),int(b);return []uint8{uint8((x+y)%256),uint8((x-y+256)%256),uint8((x*y)%256),uint8((x*(1<<uint(y%8)))%256),a&b,a|b,a^b,255-a,uint8(x/(1<<uint(y%8)))}}
func TestBytes(t *testing.T){boundary:=[]uint8{0,1,2,7,8,9,31,32,63,64,127,128,170,254,255};for _,a:=range boundary{for _,b:=range boundary{checkPair(t,a,b,uint32(a)*16777216+uint32(b)*257)}};r:=rand.New(rand.NewSource(82087));for i:=0;i<256;i++{checkPair(t,uint8(r.Uint32()),uint8(r.Uint32()),r.Uint32())}}
func checkPair(t *testing.T,a,b uint8,n uint32){t.Helper();l:=DefaultLimits();left,right:=byteInput(t,a),byteInput(t,b);word,e:=Import(&Input{Type:"UInt32",UInt32:n},l);if e!=nil{t.Fatal(e)};raw,e:=Run([]Value{left,right,word},l);if e!=nil{t.Fatal(e)};typed,e:=RunTyped(a,b,n,l);if e!=nil{t.Fatal(e)};fs:=raw.Fields();accessors:=[]func()(uint8,bool){typed.Add,typed.Sub,typed.Mul,typed.Left,typed.And,typed.Or,typed.Xor,typed.Not,typed.Right};for i,want:=range model(a,b){got,ok:=fs[i].UInt8();native,nativeOK:=accessors[i]();if !ok||!nativeOK||got!=want||native!=want{t.Fatal(a,b,i,got,native,want)};if _,ok:=fs[i].UInt32();ok{t.Fatal("inactive small payload")};if _,ok:=fs[i].UInt64();ok{t.Fatal("inactive wide payload")}};for i,want:=range []bool{a==b,a<b,a<=b}{name:="Bool.false";if want{name="Bool.true"};accessor:=[]func()(bool,bool){typed.Eq,typed.Lt,typed.Le}[i];got,ok:=accessor();if !ok||got!=want||fs[i+9].Constructor()!=name{t.Fatal("comparison",a,b,i)}};wide,ok:=typed.Wide();narrow,narrowOK:=typed.Narrow();literal,litOK:=typed.Literal();rw,rwOK:=fs[12].UInt32();rn,rnOK:=fs[13].UInt8();if !ok||!narrowOK||!litOK||!rwOK||!rnOK||wide!=uint32(a)||rw!=wide||uint32(narrow)!=n%256||rn!=narrow||literal!=255{t.Fatal("conversions",a,n,wide,narrow)};if v,_:=left.UInt8();v!=a{t.Fatal("mutated input")};fs[0]=Value{};if v,_:=raw.Fields()[0].UInt8();v!=model(a,b)[0]{t.Fatal("mutable output fields")}}
func TestByteMalformed(t *testing.T){for _,in:=range []*Input{{Type:"UInt8",UInt32:1},{Type:"UInt8",UInt64:1},{Type:"UInt8",Constructor:"x"},{Type:"UInt8",Fields:[]*Input{{Type:"UInt8"}}},{Type:"UInt32",UInt8:1},{Type:"UInt64",UInt8:1},{Type:"Bool",Constructor:"Bool.false",UInt8:1},{Type:"Example.Bytes",Constructor:"Example.Bytes.mk",UInt8:1}}{v,e:=Import(in,DefaultLimits());if !errors.Is(e,ErrMalformed)||v.Type()!=""{t.Fatal(in,v,e)}};for _,n:=range []*node{{typ:apiUInt8Node(0).typ,scalar:1,size:1,depth:1},{typ:apiUInt8Node(0).typ,scalar64:1,size:1,depth:1},{typ:apiUInt8Node(0).typ,tag:1,size:1,depth:1}}{if _,ok:=apiReadUInt8(n);ok{t.Fatal("malformed scalar read")}};if _,ok:=(Value{}).UInt8();ok{t.Fatal("zero value")}}
func TestByteBudgets(t *testing.T){l:=DefaultLimits();l.MaxNodes=2;if out,e:=RunTyped(1,2,3,l);!errors.Is(e,ErrResource)||out.v.n!=nil{t.Fatal("input budget",out,e)};l=DefaultLimits();l.MaxOutputNodes=15;if out,e:=RunTyped(1,2,3,l);!errors.Is(e,ErrResource)||out.v.n!=nil{t.Fatal("output budget",out,e)};l=DefaultLimits();l.MaxAllocations=15;if out,e:=RunTyped(1,2,3,l);!errors.Is(e,ErrResource)||out.v.n!=nil{t.Fatal("allocation budget",out,e)};l=DefaultLimits();l.MaxSteps=16;if out,e:=RunTyped(1,2,3,l);!errors.Is(e,ErrResource)||out.v.n!=nil{t.Fatal("steps",out,e)};l.MaxSteps=17;if _,e:=RunTyped(1,2,3,l);e!=nil{t.Fatal(e)}}
`
