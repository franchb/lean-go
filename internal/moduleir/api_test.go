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
	p.Types[0].Constructors = p.Types[0].Constructors[:1]
	p.API.Records["Example.Tree"] = []string{"value"}
	raw, err := json.Marshal(declare(p))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(raw)); err != nil {
		t.Fatal("valid record metadata rejected:", err)
	}
}

func apiProgram() *Program {
	p := validProgram()
	p.Types[0].Constructors = []Constructor{{Name: "Example.Tree.mk", Fields: []string{"UInt32", "UInt32"}}}
	p.API = &APIMetadata{Records: map[string][]string{"Example.Tree": {"left", "right"}}}
	return declare(p)
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
			if err := check(p); err != nil {
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
			if err := check(p); err == nil {
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
		t.Fatal("accepted program without api metadata")
	}
}
