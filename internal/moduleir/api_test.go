package moduleir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestAPIRecordSchema(t *testing.T) {
	p := validProgram()
	p.Schema, p.Profile = "lean-go-ir/v5", ProfileV3
	p.Types[0].Constructors = p.Types[0].Constructors[:1]
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], []byte(`,"api":{"records":{"Example.Tree":["value"]}}}`)...)
	if _, err := Decode(bytes.NewReader(raw)); err != nil {
		t.Fatal("valid v5 record metadata rejected:", err)
	}
}

func apiProgram() *Program {
	p := validProgram()
	p.Schema, p.Profile = SchemaV5, ProfileV3
	p.Types[0].Constructors = []Constructor{{Name: "Example.Tree.mk", Fields: []string{"UInt32", "UInt32"}}}
	p.API = &APIMetadata{Records: map[string][]string{"Example.Tree": {"left", "right"}}}
	return p
}

func TestAPIValidation(t *testing.T) {
	for name, change := range map[string]func(*Program){
		"record":             func(*Program) {},
		"empty map":          func(p *Program) { p.API.Records = map[string][]string{} },
		"presentation names": func(p *Program) { p.API.Records["Example.Tree"] = []string{"данные", "value-with punctuation"} },
		"maximum name":       func(p *Program) { p.API.Records["Example.Tree"][0] = strings.Repeat("x", MaxNameBytes) },
		"empty record": func(p *Program) {
			p.Types[0].Constructors[0].Fields = []string{}
			p.API.Records["Example.Tree"] = []string{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := apiProgram()
			change(p)
			if err := Validate(p); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(bytes.NewReader(raw))
			if err != nil || !reflect.DeepEqual(p, decoded) {
				t.Fatalf("metadata round trip: %v", err)
			}
		})
	}
}

func TestRejectInvalidAPI(t *testing.T) {
	for name, change := range map[string]func(*Program){
		"missing api":     func(p *Program) { p.API = nil },
		"missing records": func(p *Program) { p.API.Records = nil },
		"unknown type":    func(p *Program) { p.API.Records["Missing"] = []string{} },
		"scalar":          func(p *Program) { p.API.Records["UInt32"] = []string{} },
		"sum":             func(p *Program) { p.Types[0].Constructors = validProgram().Types[0].Constructors },
		"nil fields":      func(p *Program) { p.API.Records["Example.Tree"] = nil },
		"short arity":     func(p *Program) { p.API.Records["Example.Tree"] = []string{"left"} },
		"long arity":      func(p *Program) { p.API.Records["Example.Tree"] = []string{"left", "right", "extra"} },
		"empty name":      func(p *Program) { p.API.Records["Example.Tree"][0] = "" },
		"duplicate name":  func(p *Program) { p.API.Records["Example.Tree"][0] = "right" },
		"control name":    func(p *Program) { p.API.Records["Example.Tree"][0] = "line\nfeed" },
		"invalid UTF-8":   func(p *Program) { p.API.Records["Example.Tree"][0] = string([]byte{0xff}) },
		"long name":       func(p *Program) { p.API.Records["Example.Tree"][0] = strings.Repeat("x", MaxNameBytes+1) },
		"map bound": func(p *Program) {
			for i := 0; i < MaxTypes; i++ {
				p.API.Records[fmt.Sprintf("T%d", i)] = []string{}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := apiProgram()
			change(p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted invalid API metadata")
			}
		})
	}
}

func TestStrictAPIDecode(t *testing.T) {
	raw, err := json.Marshal(apiProgram())
	if err != nil {
		t.Fatal(err)
	}
	const valid = `"api":{"records":{"Example.Tree":["left","right"]}}`
	for _, metadata := range []string{
		`"api":null`, `"api":[]`, `"api":{}`, `"api":{"records":null}`,
		`"api":{"records":[]}`, `"api":{"other":{}}`, `"api":{"records":{},"extra":0}`,
		`"api":{"records":{},"records":{}}`,
		`"api":{"records":{"Example.Tree":[],"Example.\u0054ree":[]}}`,
		`"api":{"records":{"Example.Tree":null}}`, `"api":{"records":{"Example.Tree":{}}}`,
		`"api":{"records":{"Example.Tree":[null,"right"]}}`,
		`"api":{"records":{"Example.Tree":[7,"right"]}}`,
		`"api":{"records":{"Example.Tree":["same","same"]}}`,
		`"api":{"records":{"Example.Tree":["left"]}}`,
		`"api":{"records":{"Missing":[]}}`,
	} {
		t.Run(metadata, func(t *testing.T) {
			bad := bytes.Replace(raw, []byte(valid), []byte(metadata), 1)
			if bytes.Equal(bad, raw) {
				t.Fatal("test did not mutate metadata")
			}
			if _, err := Decode(bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted malformed API JSON")
			}
		})
	}
	missing := bytes.Replace(raw, []byte(","+valid), nil, 1)
	if _, err := Decode(bytes.NewReader(missing)); err == nil {
		t.Fatal("accepted v5 without metadata")
	}
}

func TestLegacyAPIRejection(t *testing.T) {
	for _, pair := range [][2]string{{Schema, Profile}, {SchemaV3, ProfileV2}, {SchemaV4, ProfileV3}} {
		p := apiProgram()
		p.Schema, p.Profile = pair[0], pair[1]
		if err := Validate(p); err == nil {
			t.Fatal("legacy schema accepted constructed API metadata", pair)
		}
		p.API = nil
		if err := Validate(p); err != nil {
			t.Fatal("legacy semantics changed", err)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, api := range []string{"null", "{}", `{"records":{}}`} {
			bad := append(bytes.Clone(raw[:len(raw)-1]), []byte(`,"api":`+api+`}`)...)
			if _, err := Decode(bytes.NewReader(bad)); err == nil {
				t.Fatal("legacy schema accepted API JSON", pair, api)
			}
		}
	}
}

func TestAPISchemaExecutionCapabilities(t *testing.T) {
	for _, p := range []*Program{
		validProgram(), joinProgram(),
		numericProgram(&Value{Kind: "uint32", Name: "4294967295", Args: []int{}}, "UInt32"),
		numericProgram(&Value{Kind: "primitive", Name: "UInt32.decEq", Args: []int{1, 2}}, "Bool"),
	} {
		p.Schema, p.Profile = SchemaV5, ProfileV3
		p.API = &APIMetadata{Records: map[string][]string{}}
		if err := Validate(p); err != nil {
			t.Fatal("v5 changed execution admission:", err)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		for _, profile := range []string{Profile, ProfileV2} {
			p.Profile = profile
			if err := Validate(p); err == nil {
				t.Fatal("v5 accepted mismatched profile", profile)
			}
		}
	}
}
