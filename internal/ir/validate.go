package ir

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

var inputKeys = []string{
	"feasibility/LeanGo/Feasibility/Model.lean", "producer/lean-toolchain", "producer/lakefile.toml", "producer/Export.lean", "producer/LeanGo/Pilot/Export.lean", "producer/Reference.lean", "scripts/export.py",
}

const auxName = "LeanGo.Feasibility.normalizeAux"
const negName = "LeanGo.Feasibility.Tri.neg"

func expectedTypes() []TypeDecl {
	return []TypeDecl{
		{"bool", []Constructor{{"Bool.false", 0, []string{}}, {"Bool.true", 1, []string{}}}},
		{"tri", []Constructor{{"LeanGo.Feasibility.Tri.f", 0, []string{}}, {"LeanGo.Feasibility.Tri.u", 1, []string{}}, {"LeanGo.Feasibility.Tri.t", 2, []string{}}}},
		{"expr", []Constructor{{"LeanGo.Feasibility.Expr.lit", 0, []string{"tri"}}, {"LeanGo.Feasibility.Expr.var", 1, []string{"u32"}}, {"LeanGo.Feasibility.Expr.neg", 2, []string{"expr"}}, {"LeanGo.Feasibility.Expr.conj", 3, []string{"expr", "expr"}}, {"LeanGo.Feasibility.Expr.disj", 4, []string{"expr", "expr"}}}},
	}
}

// Validate independently checks the admitted profile and lexical typed closure.
// It also validates constructed Programs, without relying on Decode's checks.
func Validate(p *Program) error {
	if p == nil {
		return fmt.Errorf("IR: nil program")
	}
	if err := validateMetadata(p); err != nil {
		return err
	}
	v := validator{functions: map[string]Function{}, constructors: map[string]constructorInfo{}, types: map[string][]Constructor{}, edges: map[string]map[string]bool{}}
	if err := v.addNodes(2 + len(p.Types) + len(p.Claims) + len(p.Rewrites) + len(p.Functions)); err != nil {
		return err
	}
	if err := v.validateTypes(p.Types); err != nil {
		return err
	}
	names := []string{negName, Root, auxName}
	signatures := [][]string{{"tri"}, {"expr"}, {"bool", "expr"}}
	results := []string{"tri", "expr", "expr"}
	if len(p.Functions) != len(names) {
		return fmt.Errorf("expected exactly three functions")
	}
	for i, f := range p.Functions {
		if f.Name != names[i] || f.Result != results[i] || len(f.Params) != len(signatures[i]) {
			return fmt.Errorf("unexpected function signature or order: %s", f.Name)
		}
		for j, param := range f.Params {
			if param.Type != signatures[i][j] {
				return fmt.Errorf("wrong parameter type in %s", f.Name)
			}
		}
		v.functions[f.Name] = f
		v.edges[f.Name] = map[string]bool{}
	}
	for _, f := range p.Functions {
		scope := map[int]string{}
		used := map[int]bool{}
		for _, param := range f.Params {
			if err := v.bind(param, scope, used); err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
		}
		if err := v.code(f.Body, f, scope, used, map[*Code]bool{}, 1); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return v.validateClosure()
}

func validateMetadata(p *Program) error {
	if p.Schema != Schema || p.Profile != Profile || p.Stage != Stage || p.LeanVersion != LeanVersion || p.LeanCommit != LeanCommit || p.ModelSHA256 != ModelSHA256 || p.Root != Root {
		return fmt.Errorf("unexpected IR profile metadata")
	}
	if len(p.Inputs) != len(inputKeys) {
		return fmt.Errorf("unexpected input key set")
	}
	for _, key := range inputKeys {
		hash, ok := p.Inputs[key]
		if !ok || len(hash) != 64 || strings.ToLower(hash) != hash {
			return fmt.Errorf("invalid input digest for %s", key)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return fmt.Errorf("invalid input digest for %s", key)
		}
	}
	if p.Inputs[inputKeys[0]] != ModelSHA256 {
		return fmt.Errorf("model input digest mismatch")
	}
	statements := map[string]string{
		"LeanGo.Feasibility.normalize_preserves_eval": "forall e : Expr, forall rho : UInt32 -> Tri, eval rho (normalize e) = eval rho e",
		"LeanGo.Feasibility.normalize_is_normal":      "forall e : Expr, IsNormalForm (normalize e)",
	}
	if len(p.Claims) != 2 {
		return fmt.Errorf("expected two claims")
	}
	for _, claim := range p.Claims {
		statement, ok := statements[claim.Name]
		if !ok || claim.Statement != statement || !reflect.DeepEqual(claim.Axioms, []string{"propext"}) {
			return fmt.Errorf("unexpected claim %s", claim.Name)
		}
		delete(statements, claim.Name)
	}
	if len(p.Rewrites) != 1 {
		return fmt.Errorf("expected one rewrite")
	}
	rewrite := p.Rewrites[0]
	if rewrite.Source != "Bool.not" || rewrite.Target != "Bool.Internal.not" || rewrite.Theorem != "Bool.not_eq_internalNot" || !reflect.DeepEqual(rewrite.Axioms, []string{"Quot.sound"}) {
		return fmt.Errorf("unexpected compiler rewrite")
	}
	return nil
}

type constructorInfo struct {
	constructor Constructor
	result      string
}
type validator struct {
	nodes        int
	functions    map[string]Function
	constructors map[string]constructorInfo
	types        map[string][]Constructor
	edges        map[string]map[string]bool
}

func (v *validator) addNodes(n int) error {
	if n > MaxNodes-v.nodes {
		return fmt.Errorf("IR node limit exceeded")
	}
	v.nodes += n
	return nil
}
func (v *validator) validateTypes(types []TypeDecl) error {
	if len(types) != 3 {
		return fmt.Errorf("expected three type declarations")
	}
	expected := map[string]TypeDecl{}
	for _, typ := range expectedTypes() {
		expected[typ.Name] = typ
	}
	for _, typ := range types {
		want, ok := expected[typ.Name]
		if !ok || len(typ.Constructors) != len(want.Constructors) {
			return fmt.Errorf("unexpected type %s", typ.Name)
		}
		for i, ctor := range typ.Constructors {
			wc := want.Constructors[i]
			if ctor.Name != wc.Name || ctor.Tag != wc.Tag || !sameStrings(ctor.Fields, wc.Fields) {
				return fmt.Errorf("unexpected constructor in %s", typ.Name)
			}
			v.constructors[ctor.Name] = constructorInfo{ctor, typ.Name}
		}
		if err := v.addNodes(len(typ.Constructors)); err != nil {
			return err
		}
		v.types[typ.Name] = typ.Constructors
		delete(expected, typ.Name)
	}
	return nil
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func validType(typ string) bool {
	return typ == "bool" || typ == "tri" || typ == "expr" || typ == "u32"
}
func (v *validator) bind(param Param, scope map[int]string, used map[int]bool) error {
	if err := v.addNodes(1); err != nil {
		return err
	}
	if param.ID <= 0 || used[param.ID] || !validType(param.Type) {
		return fmt.Errorf("invalid or duplicate binder %d", param.ID)
	}
	used[param.ID] = true
	scope[param.ID] = param.Type
	return nil
}
func copyScope(scope map[int]string) map[int]string {
	copy := make(map[int]string, len(scope))
	for id, typ := range scope {
		copy[id] = typ
	}
	return copy
}

func (v *validator) code(c *Code, f Function, scope map[int]string, used map[int]bool, seen map[*Code]bool, depth int) error {
	if c == nil {
		return fmt.Errorf("missing code")
	}
	if depth > MaxCodeDepth {
		return fmt.Errorf("code depth limit exceeded")
	}
	if seen[c] {
		return fmt.Errorf("code must be a finite tree")
	}
	seen[c] = true
	if err := v.addNodes(1); err != nil {
		return err
	}
	switch c.Op {
	case "let":
		if c.Bind == nil || c.Value == nil || c.Next == nil || c.Var != 0 || c.Type != "" || c.Result != "" || c.Alts != nil {
			return fmt.Errorf("invalid let payload")
		}
		typ, err := v.value(c.Value, f.Name, scope)
		if err != nil {
			return err
		}
		if typ != c.Bind.Type {
			return fmt.Errorf("let result type mismatch")
		}
		if err = v.bind(*c.Bind, scope, used); err != nil {
			return err
		}
		return v.code(c.Next, f, scope, used, seen, depth+1)
	case "return":
		if c.Bind != nil || c.Value != nil || c.Next != nil || c.Type != "" || c.Result != "" || c.Alts != nil {
			return fmt.Errorf("invalid return payload")
		}
		if c.Var <= 0 || scope[c.Var] != f.Result {
			return fmt.Errorf("invalid return variable/type")
		}
	case "cases":
		if c.Bind != nil || c.Value != nil || c.Next != nil || c.Var <= 0 || scope[c.Var] != c.Type || c.Result != f.Result {
			return fmt.Errorf("invalid cases payload or type")
		}
		constructors, ok := v.types[c.Type]
		if !ok || len(c.Alts) != len(constructors) {
			return fmt.Errorf("non-exhaustive cases")
		}
		found := map[string]bool{}
		for _, alt := range c.Alts {
			if err := v.addNodes(1); err != nil {
				return err
			}
			info, ok := v.constructors[alt.Constructor]
			if !ok || info.result != c.Type || found[alt.Constructor] || len(alt.Params) != len(info.constructor.Fields) {
				return fmt.Errorf("invalid case alternative %s", alt.Constructor)
			}
			found[alt.Constructor] = true
			local := copyScope(scope)
			for i, param := range alt.Params {
				if param.Type != info.constructor.Fields[i] {
					return fmt.Errorf("case field type mismatch")
				}
				if err := v.bind(param, local, used); err != nil {
					return err
				}
			}
			if err := v.code(alt.Body, f, local, used, seen, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported code operation %q", c.Op)
	}
	return nil
}
func (v *validator) value(value *Value, function string, scope map[int]string) (string, error) {
	if err := v.addNodes(1); err != nil {
		return "", err
	}
	var params []string
	var result string
	switch value.Kind {
	case "constructor":
		info, ok := v.constructors[value.Name]
		if !ok {
			return "", fmt.Errorf("unknown constructor %s", value.Name)
		}
		params = info.constructor.Fields
		result = info.result
	case "call":
		callee, ok := v.functions[value.Name]
		if !ok {
			return "", fmt.Errorf("unresolved call %s", value.Name)
		}
		for _, param := range callee.Params {
			params = append(params, param.Type)
		}
		result = callee.Result
		v.edges[function][value.Name] = true
	default:
		return "", fmt.Errorf("unsupported value kind %q", value.Kind)
	}
	if len(value.Args) != len(params) {
		return "", fmt.Errorf("unsaturated value %s", value.Name)
	}
	for i, id := range value.Args {
		if id <= 0 || scope[id] != params[i] {
			return "", fmt.Errorf("unknown or mistyped argument %d", id)
		}
	}
	return result, nil
}
func (v *validator) validateClosure() error {
	reached := map[string]bool{}
	active := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if active[name] {
			return fmt.Errorf("unsupported recursion through %s", name)
		}
		if reached[name] {
			return nil
		}
		reached[name] = true
		active[name] = true
		// Sorted traversal makes diagnostics stable for constructed Programs too.
		callees := make([]string, 0, len(v.edges[name]))
		for callee := range v.edges[name] {
			callees = append(callees, callee)
		}
		sort.Strings(callees)
		for _, callee := range callees {
			if name == auxName && callee == auxName {
				continue
			}
			if err := visit(callee); err != nil {
				return err
			}
		}
		active[name] = false
		return nil
	}
	if err := visit(Root); err != nil {
		return err
	}
	if len(reached) != 3 {
		return fmt.Errorf("code closure must reach all three functions")
	}
	return nil
}
