package modulegen

import (
	_ "embed"
	"encoding/json"
	"slices"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

//go:embed testdata/uint16/consumer_test.go
var uint16Consumer string

func TestSingleRootUInt16Allocations(t *testing.T) {
	p := typedProgram()
	p.Schema, p.Profile = moduleir.SchemaV14, moduleir.ProfileV12
	p.Types = []moduleir.TypeDecl{}
	p.Provenance = p.Provenance[:1]
	p.Functions = []moduleir.Function{{
		Name: p.Root, Params: []moduleir.Param{{ID: 1, Type: "UInt16"}}, Result: "UInt16",
		Body: &moduleir.Code{
			Op: "let", Bind: &moduleir.Param{ID: 2, Type: "UInt16"},
			Value: &moduleir.Value{Kind: "uint16", Name: "8", Args: []int{}},
			Next: &moduleir.Code{
				Op: "let", Bind: &moduleir.Param{ID: 3, Type: "UInt16"},
				Value: &moduleir.Value{Kind: "primitive", Name: "UInt16.shiftRight", Args: []int{1, 2}},
				Next:  &moduleir.Code{Op: "return", Var: 3},
			},
		},
	}}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, files, `package extracted
import ("errors"; "testing")
func TestHighByte(t *testing.T) {
	l := DefaultLimits()
	l.MaxAllocations, l.MaxSteps, l.MaxFrames, l.MaxSlots = 2, 3, 1, 3
	if out, err := RunTyped(0xabcd, l); err != nil || out != 0xab { t.Fatal(out, err) }
	arg, err := Import(&Input{Type: "UInt16", UInt16: 0xabcd}, l)
	if err != nil { t.Fatal(err) }
	raw, err := Run([]Value{arg}, l)
	if out, ok := raw.UInt16(); err != nil || !ok || out != 0xab { t.Fatal(out, err) }
	l.MaxAllocations--
	if out, err := RunTyped(0xabcd, l); !errors.Is(err, ErrResource) || out != 0 { t.Fatal(out, err) }
	if out, err := Run([]Value{arg}, l); !errors.Is(err, ErrResource) || out != (Value{}) { t.Fatal(out, err) }
}`)
}

func TestGeneratedUInt16(t *testing.T) {
	p := typedRecordProgram()
	p.Schema, p.Profile = moduleir.SchemaV15, moduleir.ProfileV13
	p.Root, p.Roots, p.Functions, p.Provenance = "", nil, nil, nil
	p.Types = append(p.Types[:1], moduleir.TypeDecl{Name: "Example.Sample", Constructors: []moduleir.Constructor{
		{Name: "Example.Sample.mk", Fields: []string{"UInt16", "Bool"}},
	}})
	p.API.Records = map[string][]string{"Example.Sample": {"word", "enabled"}}
	for _, tc := range []struct {
		root, op, operand, result string
		arity                     int
	}{
		{"id", "", "UInt16", "UInt16", 1},
		{"left", "UInt16.shiftLeft", "UInt16", "UInt16", 2},
		{"literal", "65535", "", "UInt16", 0},
		{"low", "UInt16.toUInt8", "UInt16", "UInt8", 1},
		{"or", "UInt16.lor", "UInt16", "UInt16", 2},
		{"record", "", "Example.Sample", "Example.Sample", 1},
		{"right", "UInt16.shiftRight", "UInt16", "UInt16", 2},
		{"wide", "UInt8.toUInt16", "UInt8", "UInt16", 1},
	} {
		name := "Example." + tc.root
		params := []moduleir.Param{{ID: 1, Type: tc.operand}, {ID: 2, Type: tc.operand}}[:tc.arity]
		code := &moduleir.Code{Op: "return", Var: 1}
		if tc.op != "" {
			kind := "primitive"
			if tc.arity == 0 {
				kind = "uint16"
			}
			code = &moduleir.Code{
				Op: "let", Bind: &moduleir.Param{ID: 3, Type: tc.result},
				Value: &moduleir.Value{Kind: kind, Name: tc.op, Args: []int{1, 2}[:tc.arity]},
				Next:  &moduleir.Code{Op: "return", Var: 3},
			}
		}
		p.Roots = append(p.Roots, name)
		p.Functions = append(p.Functions, moduleir.Function{Name: name, Params: params, Result: tc.result, Body: code})
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: name, Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}})
	}
	files, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema, Generator string
		Primitives        []moduleir.Primitive
		GoAPI             goAPI `json:"go_api"`
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, leaf := range manifest.Primitives {
		names = append(names, leaf.Name)
	}
	want := []string{"UInt16.lor", "UInt16.shiftLeft", "UInt16.shiftRight", "UInt16.toUInt8", "UInt8.toUInt16"}
	if manifest.Schema != "lean-go-manifest/v15" || manifest.Generator != "lean-go-modulegen/v12" || !slices.Equal(names, want) || manifest.GoAPI.Types["UInt16"] != "uint16" {
		t.Fatalf("manifest: %s, %s, %v, %v", manifest.Schema, manifest.Generator, names, manifest.GoAPI.Types)
	}
	runGenerated(t, files, uint16Consumer)
}
