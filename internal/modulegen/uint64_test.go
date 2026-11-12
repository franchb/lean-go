package modulegen

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func uint64Program(t *testing.T) *moduleir.Program {
	t.Helper()
	raw, err := json.Marshal(numericProgram())
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.NewReplacer("UInt32", "UInt64", "uint32", "uint64", "4294967295", "18446744073709551615").Replace(string(raw)))
	var p moduleir.Program
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	p.Schema, p.Profile = "lean-go-ir/v6", "pure-module/v4"
	p.API = &moduleir.APIMetadata{Records: map[string][]string{"Example.Numbers": {"add", "sub", "mul", "shift", "eq", "lt", "le", "zero", "max"}}}
	return &p
}

func TestUInt64GeneratedOperations(t *testing.T) {
	files, err := Generate(uint64Program(t))
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, uint64ConsumerTests)
}

func TestUInt64Manifest(t *testing.T) {
	p := uint64Program(t)
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if !bytes.Equal(content, again[name]) {
			t.Fatal("nondeterministic artifact", name)
		}
	}
	var m struct {
		Schema, Generator string
		Primitives        []moduleir.Primitive
		GoAPI             *goAPI `json:"go_api"`
	}
	if err = json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.Schema != "lean-go-manifest/v6" || m.Generator != Version || len(m.Primitives) != 7 || m.GoAPI == nil || m.GoAPI.Types["UInt64"] != "uint64" || m.GoAPI.Types["UInt32"] != "uint32" {
		t.Fatal("wide manifest", m)
	}
	for _, primitive := range m.Primitives {
		if !strings.HasPrefix(primitive.Name, "UInt64.") || strings.Join(primitive.Params, ",") != "UInt64,UInt64" {
			t.Fatal("wrong primitive evidence", primitive)
		}
	}
	if newGenerator(p).types["UInt64"] != len(p.Types)+1 || newGenerator(p).types["UInt32"] != 0 {
		t.Fatal("scalar type IDs")
	}
}

const uint64ConsumerTests = `package extracted
import("errors";"math/big";"math/rand";"testing")
func scalar(t *testing.T,n uint64)Value{t.Helper();in:=&Input{Type:"UInt64",UInt64:n};v,e:=Import(in,DefaultLimits());if e!=nil{t.Fatal(e)};in.UInt64=42;return v}
func oracle(a,b uint64)[]uint64{x,y:=new(big.Int).SetUint64(a),new(big.Int).SetUint64(b);mod:=new(big.Int).Lsh(big.NewInt(1),64);values:=[]*big.Int{new(big.Int).Add(x,y),new(big.Int).Sub(x,y),new(big.Int).Mul(x,y),new(big.Int).Lsh(x,uint(b%64))};out:=make([]uint64,len(values));for i,v:=range values{out[i]=v.Mod(v,mod).Uint64()};return out}
func TestWideSemantics(t *testing.T){boundary:=[]uint64{0,1,2,63,64,65,1<<32,1<<53+1,1<<63,^uint64(0)-1,^uint64(0)};for _,a:=range boundary{for _,b:=range boundary{checkPair(t,a,b)}};random:=rand.New(rand.NewSource(812734));for i:=0;i<200;i++{checkPair(t,random.Uint64(),random.Uint64())}}
func checkPair(t *testing.T,a,b uint64){t.Helper();left,right:=scalar(t,a),scalar(t,b);out,e:=Run([]Value{left,right},DefaultLimits());if e!=nil{t.Fatal(e)};fs:=out.Fields();if len(fs)!=9{t.Fatal("shape",len(fs))};want:=oracle(a,b);typed,e:=RunTyped(a,b,DefaultLimits());if e!=nil{t.Fatal(e)};typedNumbers:=[]func()(uint64,bool){typed.Add,typed.Sub,typed.Mul,typed.Shift};for i,n:=range want{raw,ok:=fs[i].UInt64();native,nativeOK:=typedNumbers[i]();if !ok||!nativeOK||raw!=n||native!=n{t.Fatalf("UInt64 arithmetic a=%d b=%d op=%d raw=%d native=%d want=%d",a,b,i,raw,native,n)}};typedBools:=[]func()(bool,bool){typed.Eq,typed.Lt,typed.Le};for i,want:=range []bool{a==b,a<b,a<=b}{name:="Bool.false";if want{name="Bool.true"};got,ok:=typedBools[i]();if fs[i+4].Constructor()!=name||!ok||got!=want{t.Fatal("unsigned comparison",a,b,i)}};zero,ok:=typed.Zero();max,maxOK:=typed.Max();if !ok||!maxOK||zero!=0||max!=^uint64(0){t.Fatal("literal precision",zero,max)};if _,ok:=left.UInt32();ok{t.Fatal("wide scalar read as UInt32")};if n,_:=left.UInt64();n!=a{t.Fatal("operand mutated")}}
func TestWideRawValidation(t *testing.T){for _,in:=range []*Input{{Type:"UInt32",UInt64:1},{Type:"UInt64",UInt32:1},{Type:"UInt64",Constructor:"x"},{Type:"UInt64",Fields:[]*Input{{Type:"UInt64"}}},{Type:"Bool",Constructor:"Bool.false",UInt64:1},{Type:"Bool",Constructor:"Bool.false",UInt32:1},{Type:"Example.Numbers",Constructor:"Example.Numbers.mk",UInt64:1}}{out,e:=Import(in,DefaultLimits());if !errors.Is(e,ErrMalformed)||out.Type()!=""{t.Fatal("inactive payload",in,out,e)}};small,e:=Import(&Input{Type:"UInt32",UInt32:7},DefaultLimits());if e!=nil{t.Fatal(e)};if _,ok:=small.UInt64();ok{t.Fatal("narrow scalar read as wide")};if n,ok:=small.UInt32();!ok||n!=7{t.Fatal("UInt32 changed")};if out,e:=Run([]Value{small,scalar(t,1)},DefaultLimits());!errors.Is(e,ErrMalformed)||out.Type()!=""{t.Fatal("mixed root arguments",e)};if _,ok:=(Value{}).UInt64();ok{t.Fatal("zero wide Value")};if out,e:=Run([]Value{{},scalar(t,1)},DefaultLimits());!errors.Is(e,ErrMalformed)||out.Type()!=""{t.Fatal("zero argument",e)}}
func TestWideBudgets(t *testing.T){args:=[]Value{scalar(t,1),scalar(t,2)};l:=DefaultLimits();l.MaxAllocations=10;l.MaxSteps=11;l.MaxOutputNodes=10;l.MaxNodes=2;l.MaxSlots=12;if _,e:=Run(args,l);e!=nil{t.Fatal("exact budget",e)};for _,set:=range []func(*Limits){func(l *Limits){l.MaxAllocations=9},func(l *Limits){l.MaxSteps=10},func(l *Limits){l.MaxOutputNodes=9},func(l *Limits){l.MaxSlots=11},func(l *Limits){l.MaxNodes=1},func(l *Limits){l.MaxDepth=1}}{tiny:=l;set(&tiny);out,e:=Run(args,tiny);if !errors.Is(e,ErrResource)||out.Type()!=""{t.Fatal("wide budget returned partial output",tiny,e)}};if out,e:=RunTyped(1,2,Limits{});!errors.Is(e,ErrLimits)||out.v.n!=nil{t.Fatal("typed error zero",out,e)}}
`

func TestUInt64MixedRecordFacade(t *testing.T) {
	p := typedRecordProgram()
	p.Schema, p.Profile = moduleir.SchemaV6, moduleir.ProfileV4
	p.Types[1].Constructors[0].Fields = []string{"Example.Count", "Bool", "UInt32", "UInt64"}
	p.Types[2].Constructors[0].Fields = []string{"UInt64"}
	p.API.Records["Example.Config"] = []string{"count", "enabled", "small", "wide"}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestMixedRecord(t *testing.T){l:=DefaultLimits();count,e:=NewCount(^uint64(0),l);if e!=nil{t.Fatal(e)};config,e:=NewConfig(count,true,^uint32(0),1<<53+1,l);if e!=nil{t.Fatal(e)};out,e:=RunTyped(config,l);if e!=nil{t.Fatal(e)};c,cok:=out.Count();wide,wok:=out.Wide();small,sok:=out.Small();enabled,bok:=out.Enabled();n,nok:=c.Value();if !cok||!wok||!sok||!bok||!nok||n!=^uint64(0)||wide!=1<<53+1||small!=^uint32(0)||!enabled{t.Fatal("mixed fields",n,wide,small,enabled)};if _,ok:=(Config{}).Wide();ok{t.Fatal("zero getter")};if _,ok:=Config(count).Wide();ok{t.Fatal("converted wrapper getter")};if out,e:=RunTyped(Config(count),l);!errors.Is(e,ErrMalformed)||out!=(Config{}){t.Fatal("converted root",e)};if out,e:=NewConfig(Count(config),false,0,0,l);!errors.Is(e,ErrMalformed)||out!=(Config{}){t.Fatal("converted child",e)};tiny:=l;tiny.MaxNodes=5;if out,e:=NewConfig(count,true,1,2,tiny);!errors.Is(e,ErrResource)||out!=(Config{}){t.Fatal("scalar occurrence budget",out,e)};tiny=l;tiny.MaxDepth=2;if out,e:=NewConfig(count,true,1,2,tiny);!errors.Is(e,ErrResource)||out!=(Config{}){t.Fatal("record depth",out,e)};if config.v.n.size!=6{t.Fatal("scalar one-node accounting",config.v.n.size)}}
func TestMalformedWideReaders(t *testing.T){wide:=apiUInt64Node(1);if wide.typ<1||wide.typ==0||fieldType(wide.typ,0,0)!=-1{t.Fatal("wide type ID",wide.typ)};for _,mutate:=range []func(*node){func(n *node){n.typ=0},func(n *node){n.scalar=1},func(n *node){n.tag=1},func(n *node){n.size=0},func(n *node){n.size=2},func(n *node){n.depth=0},func(n *node){n.depth=2},func(n *node){n.fields=[]*node{apiUInt32Node(1)}}}{n:=*wide;mutate(&n);if _,ok:=apiReadUInt64(&n);ok{t.Fatal("malformed UInt64 reader",n)}};if _,ok:=apiReadUInt64(nil);ok{t.Fatal("nil wide reader")};small:=apiUInt32Node(1);small.scalar64=1;if _,ok:=apiReadUInt32(small);ok{t.Fatal("inactive wide payload")};flag:=apiBoolNode(true);flag.scalar64=1;if _,ok:=apiReadBool(flag);ok{t.Fatal("Bool inactive wide payload")};count,_:=NewCount(1,DefaultLimits());copy:=*count.v.n;copy.scalar64=1;if _,ok:=(Count{v:Value{&copy}}).Value();ok{t.Fatal("ADT inactive wide payload")}}
`)
}

func TestUInt64ScalarRoots(t *testing.T) {
	p := joinTestProgram()
	p.Schema, p.Profile = moduleir.SchemaV6, moduleir.ProfileV4
	p.API = &moduleir.APIMetadata{Records: map[string][]string{}}
	p.Functions[0].Params[0].Type, p.Functions[0].Result = "UInt64", "UInt64"
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestWideJoin(t *testing.T){l:=DefaultLimits();l.MaxNodes=1;l.MaxOutputNodes=1;l.MaxFrames=1;l.MaxSlots=1;l.MaxSteps=3;for _,n:=range []uint64{0,1,1<<53+1,1<<63,^uint64(0)}{out,e:=RunTyped(n,l);if e!=nil||out!=n{t.Fatal(n,out,e)}};l.MaxSteps=2;if out,e:=RunTyped(1,l);!errors.Is(e,ErrResource)||out!=0{t.Fatal("wide join budget",out,e)}}
`)
	p.Functions[0].Params = []moduleir.Param{}
	p.Functions[0].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 1, Type: "UInt64"}, Value: &moduleir.Value{Kind: "uint64", Name: "18446744073709551615", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 1}}
	files, err = Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestWideLiteralRoot(t *testing.T){l:=DefaultLimits();l.MaxNodes=1;l.MaxOutputNodes=1;l.MaxAllocations=1;l.MaxDepth=1;out,e:=RunTyped(l);if e!=nil||out!=^uint64(0){t.Fatal(out,e)};l.MaxSteps=1;if out,e:=RunTyped(l);!errors.Is(e,ErrResource)||out!=0{t.Fatal("nullary error zero",out,e)}}
`)
	p.Types = typedRecordProgram().Types[:1]
	p.Functions[0].Params = []moduleir.Param{{ID: 1, Type: "UInt64"}, {ID: 2, Type: "UInt64"}}
	p.Functions[0].Result = "Bool"
	p.Functions[0].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 3, Type: "Bool"}, Value: &moduleir.Value{Kind: "primitive", Name: "UInt64.decLt", Args: []int{1, 2}}, Next: &moduleir.Code{Op: "return", Var: 3}}
	files, err = Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import("errors";"testing")
func TestWideBoolRoot(t *testing.T){for _,pair:=range [][2]uint64{{0,^uint64(0)},{^uint64(0),0},{1<<63,0},{1,1}}{out,e:=RunTyped(pair[0],pair[1],DefaultLimits());if e!=nil||out!=(pair[0]<pair[1]){t.Fatal(pair,out,e)}};l:=DefaultLimits();l.MaxNodes=1;if out,e:=RunTyped(1,2,l);!errors.Is(e,ErrResource)||out{t.Fatal("Bool error zero",out,e)}}
`)
	p.Functions[0].Params = []moduleir.Param{}
	p.Functions[0].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 1, Type: "Bool"}, Value: &moduleir.Value{Kind: "constructor", Name: "Bool.true", Args: []int{}}, Next: &moduleir.Code{Op: "return", Var: 1}}
	files, err = Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestNullaryBool(t *testing.T){out,e:=RunTyped(DefaultLimits());if e!=nil||!out{t.Fatal(out,e)}}
`)
}

func TestUInt64LegacyADTAndUInt32Compatibility(t *testing.T) {
	for _, pair := range [][2]string{{moduleir.Schema, moduleir.Profile}, {moduleir.SchemaV3, moduleir.ProfileV2}, {moduleir.SchemaV4, moduleir.ProfileV3}, {moduleir.SchemaV5, moduleir.ProfileV3}} {
		p := testProgram()
		p.Schema, p.Profile = pair[0], pair[1]
		if p.Schema == moduleir.SchemaV5 {
			p.API = &moduleir.APIMetadata{Records: map[string][]string{}}
		}
		p.Types = []moduleir.TypeDecl{{Name: "UInt64", Constructors: []moduleir.Constructor{{Name: "UInt64.mk", Fields: []string{"UInt32"}}}}}
		p.Functions = []moduleir.Function{{Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "UInt64"}}, Result: "UInt64", Body: &moduleir.Code{Op: "return", Var: 1}}}
		p.Provenance = p.Provenance[:1]
		files, err := Generate(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(files["runtime.go"], []byte("scalar64")) || bytes.Contains(files["runtime.go"], []byte("UInt64()")) {
			t.Fatal("legacy runtime gained wide scalar", pair)
		}
		runGenerated(t, files, `package extracted
import "testing"
func TestOrdinaryUInt64(t *testing.T){v,e:=Import(&Input{Type:"UInt64",Constructor:"UInt64.mk",Fields:[]*Input{{Type:"UInt32",UInt32:9}}},DefaultLimits());if e!=nil{t.Fatal(e)};v,e=Run([]Value{v},DefaultLimits());n,ok:=v.Fields()[0].UInt32();if e!=nil||!ok||n!=9||v.Constructor()!="UInt64.mk"{t.Fatal(v,e)}}
`)
	}
	p := numericProgram()
	p.Schema, p.Profile = moduleir.SchemaV6, moduleir.ProfileV4
	p.API = &moduleir.APIMetadata{Records: map[string][]string{}}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, numericConsumerTests)
}
