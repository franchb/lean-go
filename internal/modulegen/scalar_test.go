package modulegen

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func crcProgram(t *testing.T) *moduleir.Program {
	t.Helper()
	fixture, err := os.ReadFile("../../examples/proofs/crc16/crc16.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := moduleir.Decode(bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRecordFoldRequiresSchemaAndSourceMetadata(t *testing.T) {
	fixture, err := os.ReadFile("../../examples/proofs/adler32/adler32.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		schema, profile  string
		keepRecord, want bool
	}{
		{moduleir.SchemaV23, moduleir.ProfileV21, true, true},
		{moduleir.SchemaV23, moduleir.ProfileV21, false, false},
		{moduleir.SchemaV21, moduleir.ProfileV19, true, false},
	} {
		p, err := moduleir.Decode(bytes.NewReader(fixture))
		if err != nil {
			t.Fatal(err)
		}
		p.Schema, p.Profile = tc.schema, tc.profile
		if !tc.keepRecord {
			delete(p.API.Records, "Adler32.State")
		}
		if err := moduleir.Validate(p); err != nil {
			t.Fatal(err)
		}
		_, got := byteFoldSteps(p)["Adler32.update"]
		if got != tc.want {
			t.Fatalf("schema %s with record metadata %t: got slice API %t", tc.schema, tc.keepRecord, got)
		}
	}
}

func TestExportedRecordStepAndFold(t *testing.T) {
	fixture, err := os.ReadFile("../../examples/proofs/adler32/adler32.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := moduleir.Decode(bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	p.Roots = append(p.Roots, "Adler32.step")
	slices.Sort(p.Roots)
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestBoth(t *testing.T) {
	l:=DefaultLimits()
	s,err:=RunInitial(l);if err!=nil{t.Fatal(err)}
	a,err:=RunStep(s,255,l);if err!=nil{t.Fatal(err)}
	b,err:=RunUpdate(s,[]byte{255},l);if err!=nil{t.Fatal(err)}
	v,_:=a.High();w,_:=b.High();if v!=256 || w!=v {t.Fatal(v,w)}
}`)
}

func TestByteFoldWithNonNativeStep(t *testing.T) {
	p := crcProgram(t)
	for i := range p.Functions {
		f := &p.Functions[i]
		if f.Name == "Crc16.updateByte" {
			f.Body = &moduleir.Code{Op: "join", Join: &moduleir.Join{ID: 10001, Params: []moduleir.Param{}, Body: f.Body}, Next: &moduleir.Code{Op: "jump", Target: 10001, Args: []int{}}}
		}
	}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestFallback(t *testing.T){
    l:=DefaultLimits()
    got,err:=RunUpdate(0,[]byte("123456789"),l)
    if err!=nil || got!=0x31c3 {t.Fatal(got,err)}
    l.MaxDepth=1
    if _,err:=RunUpdate(0,[]byte{42},l);err!=ErrResource {t.Fatal(err)}
    if got,err:=RunUpdate(0x1234,nil,l);err!=nil || got!=0x1234 {t.Fatal(got,err)}
}`)
}

func TestNativeScalarBudgetParity(t *testing.T) {
	p := crcProgram(t)
	// Unused pure temporaries still consume steps, slots and allocations.
	for i := range p.Functions {
		f := &p.Functions[i]
		if f.Name != "Crc16.updateByte" {
			continue
		}
		f.Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 10001, Type: "UInt16"},
			Value: &moduleir.Value{Kind: "uint16", Name: "7", Args: []int{}},
			Next: &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 10002, Type: "UInt16"},
				Value: &moduleir.Value{Kind: "primitive", Name: "UInt16.xor", Args: []int{10001, 10001}}, Next: f.Body}}
	}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["api.go"], []byte("scalarWork(")) {
		t.Fatal("expected native scalar execution")
	}
	runGenerated(t, files, scalarBudgetConsumer)
}

const scalarBudgetConsumer = `package extracted
import "testing"

func TestParity(t *testing.T) {
	for _,state:=range []uint16{0,0x8042,0xffff} {
		for _,byt:=range []uint8{0,0x39,0xff} {
			args:=[]Value{{apiUInt16Node(state)},{apiUInt8Node(byt)}}
			check:=func(l Limits){
				t.Helper()
				got,err:=RunUpdateByte(state,byt,l)
				v,wantErr:=RunRoot("Crc16.updateByte",args,l)
				want,ok:=v.UInt16()
				if err!=wantErr || got!=want || ok!=(wantErr==nil) {
					t.Fatalf("%04x %02x %+v: typed %04x/%v, raw %04x/%v",state,byt,l,got,err,want,wantErr)
				}
			}
			for n:=uint64(0);n<=128;n++ {
				l:=DefaultLimits();l.MaxSteps=n;check(l)
				l=DefaultLimits();l.MaxAllocations=n;check(l)
				l=DefaultLimits();l.MaxSlots=n;check(l)
				if n<=3 {
					l=DefaultLimits();l.MaxFrames=n;check(l)
					l=DefaultLimits();l.MaxNodes=n;check(l)
					l=DefaultLimits();l.MaxOutputNodes=n;check(l)
					l=DefaultLimits();l.MaxDepth=n;check(l)
				}
			}
		}
	}
}
`
