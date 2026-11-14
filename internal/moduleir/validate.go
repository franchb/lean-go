package moduleir

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type constructorInfo struct {
	constructor Constructor
	result      string
}
type validator struct {
	nodes        int
	numeric      bool
	wide         bool
	bytes        bool
	bitwise      bool
	conversions  bool
	joins        bool
	functions    map[string]Function
	constructors map[string]constructorInfo
	types        map[string][]Constructor
	edges        map[string]map[string]bool
}

func validName(s string) bool {
	if s == "" || len(s) > MaxNameBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (v *validator) addNodes(n int) error {
	if n > MaxNodes-v.nodes {
		return fmt.Errorf("IR node limit exceeded")
	}
	v.nodes += n
	return nil
}

// Validate checks the complete typed runtime closure. Metadata is traceability,
// not authentication: only the source producer checks Lean proof evidence.
func Validate(p *Program) error {
	if p == nil {
		return fmt.Errorf("IR: nil program")
	}
	v := validator{functions: map[string]Function{}, constructors: map[string]constructorInfo{}, types: map[string][]Constructor{}, edges: map[string]map[string]bool{}}
	if err := v.validateMetadata(p); err != nil {
		return err
	}
	if err := v.validateTypes(p.Types); err != nil {
		return err
	}
	if err := v.validateAPI(p); err != nil {
		return err
	}
	if err := v.validateNumericTypes(); err != nil {
		return err
	}
	if len(p.Functions) == 0 || len(p.Functions) > MaxFunctions {
		return fmt.Errorf("function count exceeds profile")
	}
	prev := ""
	for _, f := range p.Functions {
		if !validName(f.Name) || f.Name <= prev || f.Params == nil || len(f.Params) > MaxArity || !v.validType(f.Result) {
			return fmt.Errorf("invalid or unordered function signature %s", f.Name)
		}
		prev = f.Name
		v.functions[f.Name] = f
		v.edges[f.Name] = map[string]bool{}
	}
	for _, f := range p.Functions {
		scope, used := map[int]string{}, map[int]bool{}
		for _, param := range f.Params {
			if err := v.bind(param, scope, used); err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
		}
		if err := v.code(f.Body, f, scope, map[int]*Join{}, used, map[*Code]bool{}, 1); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	if err := v.validateClosure(RootNames(p)); err != nil {
		return err
	}
	records := map[string]bool{}
	for _, r := range p.Provenance {
		records[r.Name] = true
	}
	for _, f := range p.Functions {
		if !records[f.Name] {
			return fmt.Errorf("missing compilation provenance for %s", f.Name)
		}
	}
	return v.validateTypeClosure()
}
func (v *validator) validateMetadata(p *Program) error {
	if !validProfile(p.Schema, p.Profile) || p.Stage != Stage || p.LeanVersion != LeanVersion || p.LeanCommit != LeanCommit || !validName(p.Module) {
		return fmt.Errorf("unexpected IR profile metadata")
	}
	if err := validateRoots(p); err != nil {
		return err
	}
	v.numeric = p.Schema != Schema
	v.wide = HasUInt64(p.Schema)
	v.bytes = HasUInt8(p.Schema)
	v.joins = v.numeric && p.Schema != SchemaV3
	v.conversions = p.Schema == SchemaV10 || p.Schema == SchemaV11 || v.bytes
	v.bitwise = p.Schema == SchemaV8 || p.Schema == SchemaV9 || v.conversions
	if p.Types == nil || p.Claims == nil || p.Rewrites == nil || p.Provenance == nil || p.Functions == nil {
		return fmt.Errorf("required arrays must not be null")
	}
	if len(p.Claims) != 0 {
		return fmt.Errorf("source claims are not admitted by this profile")
	}
	if len(p.Inputs) == 0 || len(p.Inputs) > MaxNodes {
		return fmt.Errorf("invalid source identity set")
	}
	if err := v.addNodes(1 + len(p.Roots) + len(p.Inputs) + len(p.Types) + len(p.Functions) + len(p.Provenance) + 2*len(p.Rewrites)); err != nil {
		return err
	}
	for key, hash := range p.Inputs {
		if !validName(key) || strings.Contains(key, "\\") || path.IsAbs(key) || path.Clean(key) != key || key == "." || key == ".." || strings.HasPrefix(key, "../") {
			return fmt.Errorf("invalid input path %q", key)
		}
		if len(hash) != 64 || hash != strings.ToLower(hash) {
			return fmt.Errorf("invalid input digest for %s", key)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return fmt.Errorf("invalid input digest for %s", key)
		}
	}
	if len(p.Provenance) == 0 || len(p.Provenance) > MaxNodes || len(p.Rewrites) > MaxNodes {
		return fmt.Errorf("invalid provenance count")
	}
	union := map[string]bool{}
	prev := ""
	for _, r := range p.Provenance {
		if !validName(r.Name) || r.Name <= prev || r.Dependencies == nil || r.Rewrites == nil || len(r.Dependencies) > MaxNodes || len(r.Rewrites) > MaxNodes {
			return fmt.Errorf("invalid or unordered provenance %s", r.Name)
		}
		if err := v.addNodes(len(r.Dependencies) + 2*len(r.Rewrites)); err != nil {
			return err
		}
		prev = r.Name
		last := ""
		for _, dep := range r.Dependencies {
			if !validName(dep) || dep <= last {
				return fmt.Errorf("invalid or unordered dependency in %s", r.Name)
			}
			last = dep
		}
		keys, err := validateRewrites(r.Rewrites)
		if err != nil {
			return err
		}
		for _, key := range keys {
			union[key] = true
		}
	}
	keys, err := validateRewrites(p.Rewrites)
	if err != nil {
		return err
	}
	if len(keys) != len(union) {
		return fmt.Errorf("rewrite union differs from compilation records")
	}
	for _, key := range keys {
		if !union[key] {
			return fmt.Errorf("rewrite missing compilation provenance")
		}
	}
	return nil
}
func validateRewrites(rs []Rewrite) ([]string, error) {
	keys := make([]string, 0, len(rs))
	previous := ""
	for _, r := range rs {
		if r.Source != "Bool.not" || r.Target != "Bool.Internal.not" || r.Theorem != "Bool.not_eq_internalNot" || len(r.Axioms) != 1 || r.Axioms[0] != "Quot.sound" {
			return nil, fmt.Errorf("invalid compiler rewrite")
		}
		raw, _ := json.Marshal(r)
		key := string(raw)
		if key <= previous {
			return nil, fmt.Errorf("duplicate or unordered compiler rewrite")
		}
		previous = key
		keys = append(keys, key)
	}
	return keys, nil
}
func (v *validator) validateTypes(types []TypeDecl) error {
	if len(types) > MaxTypes {
		return fmt.Errorf("type count exceeds profile")
	}
	previous := ""
	for _, t := range types {
		if !validName(t.Name) || t.Name == "UInt8" || t.Name == "UInt32" || (v.wide && t.Name == "UInt64") || t.Name <= previous || len(t.Constructors) == 0 || len(t.Constructors) > MaxConstructors {
			return fmt.Errorf("invalid or unordered type %s", t.Name)
		}
		previous = t.Name
		v.types[t.Name] = t.Constructors
	}
	for _, t := range types {
		for i, c := range t.Constructors {
			_, duplicate := v.constructors[c.Name]
			if !validName(c.Name) || duplicate || c.Tag != i || c.Fields == nil || len(c.Fields) > MaxArity {
				return fmt.Errorf("invalid constructor %s", c.Name)
			}
			for _, field := range c.Fields {
				if !v.validType(field) {
					return fmt.Errorf("unknown field type %s", field)
				}
			}
			if err := v.addNodes(1 + len(c.Fields)); err != nil {
				return err
			}
			v.constructors[c.Name] = constructorInfo{c, t.Name}
		}
	}
	return nil
}
func (v *validator) validateClosure(roots []string) error {
	for _, root := range roots {
		if _, ok := v.functions[root]; !ok {
			return fmt.Errorf("root %s not in function closure", root)
		}
	}
	reached, active := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if active[name] {
			return fmt.Errorf("unsupported mutual recursion through %s", name)
		}
		if reached[name] {
			return nil
		}
		reached[name] = true
		active[name] = true
		callees := make([]string, 0, len(v.edges[name]))
		for callee := range v.edges[name] {
			callees = append(callees, callee)
		}
		sort.Strings(callees)
		for _, callee := range callees {
			if callee == name {
				continue
			}
			if err := visit(callee); err != nil {
				return err
			}
		}
		active[name] = false
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return err
		}
	}
	if len(reached) != len(v.functions) {
		return fmt.Errorf("unreachable runtime functions")
	}
	return nil
}
func (v *validator) validateTypeClosure() error {
	used := map[string]bool{"UInt32": true}
	if v.wide {
		used["UInt64"] = true
	}
	if v.bytes {
		used["UInt8"] = true
	}
	scalarCount := len(used)
	pending := []string{}
	for _, f := range v.functions {
		pending = append(pending, f.Result)
		for _, p := range f.Params {
			pending = append(pending, p.Type)
		}
		collectCodeTypes(f.Body, &pending)
	}
	for len(pending) > 0 {
		t := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if used[t] {
			continue
		}
		used[t] = true
		for _, c := range v.types[t] {
			pending = append(pending, c.Fields...)
		}
	}
	if len(used)-scalarCount != len(v.types) {
		return fmt.Errorf("unreachable runtime types")
	}
	return nil
}
func collectCodeTypes(c *Code, out *[]string) {
	if c.Op == "join" {
		for _, p := range c.Join.Params {
			*out = append(*out, p.Type)
		}
		collectCodeTypes(c.Join.Body, out)
		collectCodeTypes(c.Next, out)
	}
	if c.Op == "let" {
		*out = append(*out, c.Bind.Type)
		collectCodeTypes(c.Next, out)
	}
	if c.Op == "cases" {
		*out = append(*out, c.Type)
		for _, a := range c.Alts {
			for _, p := range a.Params {
				*out = append(*out, p.Type)
			}
			collectCodeTypes(a.Body, out)
		}
	}
}
