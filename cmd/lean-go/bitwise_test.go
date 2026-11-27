package main

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"slices"
	"testing"
)

func TestCompileCapabilityFeatures(t *testing.T) {
	for _, capability := range []struct{ result, primitive, feature string }{
		{"UInt64", "UInt64.complement", moduleir.FeatureBitwise},
		{"UInt8", "UInt32.toUInt8", moduleir.FeatureUInt8},
		{"UInt16", "UInt8.toUInt16", moduleir.FeatureUInt16},
		{"UInt32", "UInt16.toUInt32", moduleir.FeatureWidthConversion},
		{"UInt16", "UInt16.xor", moduleir.FeatureBitwise},
		{"UInt32", "UInt64.toUInt32", moduleir.FeatureWidthConversion},
	} {
		for _, multiple := range []bool{false, true} {
			p := multiRootFixture()
			for i := range p.Functions {
				primitive, _ := moduleir.LookupPrimitive(capability.primitive)
				p.Functions[i].Params[0].Type = primitive.Params[0]
				p.Functions[i].Result = capability.result
				p.Functions[i].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 2, Type: capability.result}, Value: &moduleir.Value{Kind: "primitive", Name: capability.primitive, Args: []int{1, 1}[:len(primitive.Params)]}, Next: &moduleir.Code{Op: "return", Var: 2}}
			}
			entry := "func RunFirst("
			if !multiple {
				p.Roots = p.Roots[:1]
				p.Functions = p.Functions[:1]
				p.Provenance = p.Provenance[:1]
				entry = "func RunTyped("
			}
			declare(p)
			if !slices.Contains(p.Features, capability.feature) {
				t.Fatal("feature", capability.primitive, p.Features)
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			files, err := compile(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(files["api.go"], []byte(entry)) || !bytes.Contains(files["manifest.json"], []byte(moduleir.ManifestSchema)) {
				t.Fatal("dispatch", capability.primitive, multiple)
			}
			p.Features = slices.DeleteFunc(slices.Clone(p.Features), func(f string) bool { return f == capability.feature })
			raw, _ = json.Marshal(p)
			if files, err := compile(bytes.NewReader(raw)); err == nil || files != nil {
				t.Fatal("undeclared feature produced output", err)
			}
		}
	}
}
