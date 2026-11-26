package modulegen

import (
	"bytes"
	"os"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

func TestNativeScalarBudgetParity(t *testing.T) {
	fixture, err := os.ReadFile("../../examples/proofs/crc16/crc16.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := moduleir.Decode(bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	// Unused pure temporaries still consume steps, slots and allocations.
	for i := range p.Functions {
		f := &p.Functions[i]
		if f.Name != p.Root {
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
				got,err:=RunTyped(state,byt,l)
				v,wantErr:=Run(args,l)
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
