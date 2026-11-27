package compatibility

import (
	"strings"

	"github.com/franchb/lean-go/internal/pipeline"
)

// Outcome is the JSON result of one case; keys are written sorted.
type Outcome = map[string]any

// PassStatuses are the case outcomes that qualify a compiler.
var PassStatuses = map[string]bool{"accepted": true, "expected_rejection": true}

// AdmissionCodes are the admission diagnostic codes a negative fixture may
// require. TestAdmissionCodesMatchThePipeline keeps them equal to the
// pipeline's private table.
var AdmissionCodes = []string{"source_audit", "unsupported_type", "unsupported_signature",
	"unsupported_lcnf_value", "unsupported_lcnf_control"}

func isAdmissionCode(code string) bool {
	for _, c := range AdmissionCodes {
		if c == code {
			return true
		}
	}
	return false
}

// plain converts parsed JSON (*pipeline.Object) to maps for encoding.
func plain(v any) any {
	switch t := v.(type) {
	case *pipeline.Object:
		out := map[string]any{}
		for _, k := range t.Keys {
			out[k] = plain(t.Values[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = plain(item)
		}
		return out
	}
	return v
}

// ExtractionOutcome classifies one `lean-go generate --diagnostics json`
// run. Only the exact mapped admission protocol can satisfy a negative case:
// exit status 1, no stdout and one well-formed diagnostics document.
func ExtractionOutcome(result CommandResult, expectedCode, expectedDeclaration *string) Outcome {
	if result.Code == 0 {
		if expectedCode != nil {
			return Outcome{"status": "unexpected_acceptance"}
		}
		return Outcome{"status": "extracted"}
	}
	if result.Code != 1 || result.Stdout != "" || !pipeline.ValidReport([]byte(result.Stderr)) {
		return Outcome{"status": "extraction_protocol_failed"}
	}
	report, err := pipeline.ParseJSON([]byte(result.Stderr))
	if err != nil {
		return Outcome{"status": "extraction_protocol_failed"}
	}
	item := report.(*pipeline.Object).Values["diagnostics"].([]any)[0].(*pipeline.Object)
	diagnostic := plain(item)
	if item.Values["stage"] != "admission" {
		return Outcome{"status": "extraction_failed", "diagnostic": diagnostic}
	}
	matched := expectedCode != nil && item.Values["code"] == *expectedCode &&
		(expectedDeclaration == nil || item.Values["declaration"] == *expectedDeclaration)
	if matched {
		return Outcome{"status": "expected_rejection", "diagnostic": diagnostic}
	}
	return Outcome{"status": "admission_mismatch", "diagnostic": diagnostic}
}

// identityFields are the top-level IR fields canonical IR excludes.
var identityFields = []string{"inputs", "lean_version", "lean_commit"}

// CanonicalIR excludes only the top-level source and toolchain identity and
// keeps all semantic evidence, with sorted keys.
func CanonicalIR(program any) (string, error) {
	o, ok := program.(*pipeline.Object)
	if !ok {
		return "", harnessError("invalid successful IR envelope")
	}
	for _, key := range identityFields {
		if _, ok := o.Values[key]; !ok {
			return "", harnessError("invalid successful IR envelope")
		}
	}
	if _, ok := o.Values["inputs"].(*pipeline.Object); !ok {
		return "", harnessError("invalid successful IR envelope")
	}
	if _, ok := o.Values["functions"].([]any); !ok {
		return "", harnessError("invalid successful IR envelope")
	}
	copy := pipeline.NewObject()
	for _, key := range o.Keys {
		if key != "inputs" && key != "lean_version" && key != "lean_commit" {
			copy.Set(key, o.Values[key])
		}
	}
	data, err := pipeline.EncodeIndentedSorted(copy)
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// IRShapes returns the compiler forms a fixture claims that did not survive
// optimization: called functions with a name suffix and control operations.
func IRShapes(program any, expected map[string][]string) ([]string, error) {
	o, ok := program.(*pipeline.Object)
	if !ok {
		return nil, harnessError("invalid successful IR envelope")
	}
	functions, ok := o.Values["functions"].([]any)
	if !ok {
		return nil, harnessError("invalid successful IR envelope")
	}
	names := map[string]bool{}
	for _, f := range functions {
		fo, ok := f.(*pipeline.Object)
		if !ok {
			return nil, harnessError("invalid IR function")
		}
		name, _ := fo.Values["name"].(string)
		names[name] = true
	}
	ops, calls := map[string]bool{}, map[string]bool{}
	var visit func(any)
	visit = func(v any) {
		switch t := v.(type) {
		case *pipeline.Object:
			if op, ok := t.Values["op"].(string); ok {
				ops[op] = true
			}
			if t.Values["kind"] == "call" {
				if name, ok := t.Values["name"].(string); ok {
					calls[name] = true
				}
			}
			for _, k := range t.Keys {
				visit(t.Values[k])
			}
		case []any:
			for _, item := range t {
				visit(item)
			}
		}
	}
	visit(functions)
	missing := []string{}
	for _, suffix := range expected["called_function_suffixes"] {
		found := false
		for name := range names {
			if strings.HasSuffix(name, suffix) && calls[name] {
				found = true
			}
		}
		if !found {
			missing = append(missing, "called function ending "+suffix)
		}
	}
	for _, op := range expected["ops"] {
		if !ops[op] {
			missing = append(missing, "control "+op)
		}
	}
	return missing, nil
}

// Difference is one reported incompatibility.
type Difference struct {
	Case string `json:"case,omitempty"`
	Kind string `json:"kind"`
}

// CompareCases lists, in corpus order, every candidate failure, changed
// outcome and canonical IR drift.
func CompareCases(names []string, baseline, candidate map[string]Outcome) []Difference {
	differences := []Difference{}
	for _, name := range names {
		before, after := baseline[name], candidate[name]
		afterStatus, _ := after["status"].(string)
		switch {
		case !PassStatuses[afterStatus]:
			differences = append(differences, Difference{name, afterStatus})
		case before["status"] != after["status"]:
			differences = append(differences, Difference{name, "outcome_changed"})
		case before["canonical_ir_sha256"] != after["canonical_ir_sha256"]:
			differences = append(differences, Difference{name, "canonical_ir_drift"})
		}
	}
	return differences
}

// IRDiff is the unified diff of two canonical IR documents.
func IRDiff(before, after string) string {
	return UnifiedDiff(before, after, "baseline/program.canonical.json", "candidate/program.canonical.json")
}

// Case is one corpus fixture.
type Case struct {
	Name              string              `json:"name"`
	Module            string              `json:"module"`
	Roots             []string            `json:"roots"`
	Reject            *string             `json:"reject,omitempty"`
	RejectDeclaration *string             `json:"reject_declaration,omitempty"`
	Shapes            map[string][]string `json:"shapes,omitempty"`
	SourceVersions    []string            `json:"source_versions,omitempty"`
}

func strings_(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// ValidateCases checks a parsed corpus manifest. Any malformed case is a
// harness precondition failure.
func ValidateCases(value any) ([]Case, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, harnessError("corpus manifest must contain cases")
	}
	allowed := map[string]bool{"name": true, "module": true, "roots": true, "reject": true, "reject_declaration": true, "shapes": true, "source_versions": true}
	seen := map[string]bool{}
	var cases []Case
	for _, item := range items {
		o, ok := item.(*pipeline.Object)
		if !ok {
			return nil, harnessError("invalid corpus case fields")
		}
		for _, key := range o.Keys {
			if !allowed[key] {
				return nil, harnessError("invalid corpus case fields")
			}
		}
		for _, key := range []string{"name", "module", "roots"} {
			if _, ok := o.Values[key]; !ok {
				return nil, harnessError("invalid corpus case fields")
			}
		}
		name, ok := o.Values["name"].(string)
		if !ok || name == "" || seen[name] || strings.IndexFunc(name, func(r rune) bool {
			return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789_-", r)
		}) >= 0 {
			return nil, harnessError("invalid or duplicate corpus name")
		}
		seen[name] = true
		c := Case{Name: name}
		if c.Module, ok = o.Values["module"].(string); !ok {
			return nil, harnessError("corpus module must be a string")
		}
		if err := pipeline.ValidateName(c.Module); err != nil {
			return nil, err
		}
		if c.Roots, ok = strings_(o.Values["roots"]); !ok {
			return nil, harnessError("corpus roots must be a list of strings")
		}
		if _, err := pipeline.SelectRoots(c.Roots); err != nil {
			return nil, err
		}
		if v, present := o.Values["reject"]; present {
			code, ok := v.(string)
			if !ok || !isAdmissionCode(code) {
				return nil, harnessError("negative fixture requires an explicit admission code")
			}
			c.Reject = &code
		}
		if v, present := o.Values["reject_declaration"]; present {
			declaration, ok := v.(string)
			if c.Reject == nil || !ok {
				return nil, harnessError("a rejection declaration requires an explicit rejection code and a Lean name")
			}
			if err := pipeline.ValidateName(declaration); err != nil {
				return nil, err
			}
			c.RejectDeclaration = &declaration
		}
		if v, present := o.Values["source_versions"]; present {
			versions, ok := strings_(v)
			distinct := map[string]bool{}
			for _, version := range versions {
				if !versionOnly.MatchString(version) {
					ok = false
				}
				distinct[version] = true
			}
			if !ok || len(versions) == 0 || len(distinct) != len(versions) {
				return nil, harnessError("source_versions must list distinct Lean versions")
			}
			c.SourceVersions = versions
		}
		if v, present := o.Values["shapes"]; present {
			shapes, ok := v.(*pipeline.Object)
			if !ok {
				return nil, harnessError("invalid fixture shape requirements")
			}
			c.Shapes = map[string][]string{}
			for _, key := range shapes.Keys {
				values, ok := strings_(shapes.Values[key])
				if (key != "ops" && key != "called_function_suffixes") || !ok {
					return nil, harnessError("invalid fixture shape requirements")
				}
				c.Shapes[key] = values
			}
		}
		cases = append(cases, c)
	}
	return cases, nil
}
