package main

import (
	"bytes"
	"encoding/json"
	"github.com/franchb/lean-go/internal/moduleir"
	"testing"
)

func TestCompileBitwiseSchemas(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		p := multiRootFixture()
		p.Schema, p.Profile = moduleir.SchemaV9, moduleir.ProfileV7
		for i := range p.Functions {
			p.Functions[i].Body = &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: 2, Type: "UInt64"}, Value: &moduleir.Value{Kind: "primitive", Name: "UInt64.complement", Args: []int{1}}, Next: &moduleir.Code{Op: "return", Var: 2}}
		}
		entry, manifest := "func RunFirst(", "lean-go-manifest/v9"
		if !multiple {
			p.Schema, p.Profile = moduleir.SchemaV8, moduleir.ProfileV6
			p.Root, p.Roots = p.Roots[0], nil
			p.Functions = p.Functions[:1]
			p.Provenance = p.Provenance[:1]
			entry, manifest = "func RunTyped(", "lean-go-manifest/v8"
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
	}
}
