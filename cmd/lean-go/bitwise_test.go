package main

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"testing"
)

func TestCompileCapabilitySchemas(t *testing.T) {
	for _, capability := range []struct{ single, multi, singleProfile, multiProfile, result, primitive, singleManifest, multiManifest string }{
		{moduleir.SchemaV8, moduleir.SchemaV9, moduleir.ProfileV6, moduleir.ProfileV7, "UInt64", "UInt64.complement", "lean-go-manifest/v8", "lean-go-manifest/v9"},
		{moduleir.SchemaV12, moduleir.SchemaV13, moduleir.ProfileV10, moduleir.ProfileV11, "UInt8", "UInt32.toUInt8", "lean-go-manifest/v12", "lean-go-manifest/v13"},
		{moduleir.SchemaV14, moduleir.SchemaV15, moduleir.ProfileV12, moduleir.ProfileV13, "UInt16", "UInt8.toUInt16", "lean-go-manifest/v14", "lean-go-manifest/v15"},
		{moduleir.SchemaV16, moduleir.SchemaV17, moduleir.ProfileV14, moduleir.ProfileV15, "UInt32", "UInt16.toUInt32", "lean-go-manifest/v16", "lean-go-manifest/v17"},
		{moduleir.SchemaV18, moduleir.SchemaV19, moduleir.ProfileV16, moduleir.ProfileV17, "UInt16", "UInt16.xor", "lean-go-manifest/v18", "lean-go-manifest/v19"},
		{moduleir.SchemaV10, moduleir.SchemaV11, moduleir.ProfileV8, moduleir.ProfileV9, "UInt32", "UInt64.toUInt32", "lean-go-manifest/v10", "lean-go-manifest/v11"},
	} {
		for _, multiple := range []bool{false, true} {
			p := multiRootFixture()
			p.Schema, p.Profile = capability.multi, capability.multiProfile
			for i := range p.Functions {
				primitive, _ := moduleir.LookupPrimitive(capability.primitive)
				p.Functions[i].Params[0].Type = primitive.Params[0]
				p.Functions[i].Result = capability.result
				p.Functions[i].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 2, Type: capability.result}, Value: &moduleir.Value{Kind: "primitive", Name: capability.primitive, Args: []int{1, 1}[:len(primitive.Params)]}, Next: &moduleir.Code{Op: "return", Var: 2}}
			}
			entry, manifest := "func RunFirst(", capability.multiManifest
			if !multiple {
				p.Schema, p.Profile = capability.single, capability.singleProfile
				p.Root, p.Roots = p.Roots[0], nil
				p.Functions = p.Functions[:1]
				p.Provenance = p.Provenance[:1]
				entry, manifest = "func RunTyped(", capability.singleManifest
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			files, err := compile(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(files["api.go"], []byte(entry)) || !bytes.Contains(files["manifest.json"], []byte(manifest)) {
				t.Fatal("schema dispatch", p.Schema)
			}
			p.Profile = moduleir.ProfileV5
			raw, _ = json.Marshal(p)
			if files, err := compile(bytes.NewReader(raw)); err == nil || files != nil {
				t.Fatal("cross-pair produced output", err)
			}
		}
	}
}
