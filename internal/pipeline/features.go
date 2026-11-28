package pipeline

import (
	"errors"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

var errShape = errors.New("unexpected producer field shape")

var scalarFeatures = map[string]string{"UInt8": moduleir.FeatureUInt8, "UInt16": moduleir.FeatureUInt16,
	"UInt32": moduleir.FeatureUInt32, "UInt64": moduleir.FeatureUInt64}
var bitwiseOperations = map[string]bool{"land": true, "lor": true, "xor": true, "complement": true, "shiftLeft": true, "shiftRight": true}

func field(v any, key string) (any, error) {
	o, ok := v.(*Object)
	if !ok {
		return nil, errShape
	}
	value, ok := o.Values[key]
	if !ok {
		return nil, errShape
	}
	return value, nil
}

func list(v any, key string) ([]any, error) {
	value, err := field(v, key)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errShape
	}
	return items, nil
}

// executableNodes walks every function body, including join and case bodies.
func executableNodes(program *Object) ([]any, error) {
	functions, err := list(program, "functions")
	if err != nil {
		return nil, err
	}
	var pending, out []any
	for _, function := range functions {
		body, err := field(function, "body")
		if err != nil {
			return nil, err
		}
		pending = append(pending, body)
	}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		out = append(out, node)
		op, err := field(node, "op")
		if err != nil {
			return nil, err
		}
		if op == "let" || op == "join" {
			next, err := field(node, "next")
			if err != nil {
				return nil, err
			}
			pending = append(pending, next)
		}
		if op == "join" {
			join, err := field(node, "join")
			if err != nil {
				return nil, err
			}
			body, err := field(join, "body")
			if err != nil {
				return nil, err
			}
			pending = append(pending, body)
		}
		if op == "cases" {
			alts, err := list(node, "alts")
			if err != nil {
				return nil, err
			}
			for _, alt := range alts {
				body, err := field(alt, "body")
				if err != nil {
					return nil, err
				}
				pending = append(pending, body)
			}
		}
	}
	return out, nil
}

func paramTypes(v any, key string, types map[any]bool) error {
	o, ok := v.(*Object)
	if !ok {
		return errShape
	}
	raw, ok := o.Values[key]
	if !ok {
		return nil // an absent key is an empty list
	}
	params, ok := raw.([]any)
	if !ok {
		return errShape
	}
	for _, p := range params {
		t, err := field(p, "type")
		if err != nil {
			return err
		}
		types[hashable(t)] = true
	}
	return nil
}

// hashable maps a JSON value to a comparable value, usable as a set key.
func hashable(v any) any {
	switch v.(type) {
	case *Object, []any:
		return errShape
	}
	return v
}

// programFeatures names the capability families the program uses, by the
// same rules the Go validator applies to the declared list.
func programFeatures(program *Object) ([]any, error) {
	nodes, err := executableNodes(program)
	if err != nil {
		return nil, err
	}
	features := map[string]bool{}
	types := map[any]bool{}
	declared, err := list(program, "types")
	if err != nil {
		return nil, err
	}
	for _, typ := range declared {
		constructors, err := list(typ, "constructors")
		if err != nil {
			return nil, err
		}
		for _, ctor := range constructors {
			fields, err := list(ctor, "fields")
			if err != nil {
				return nil, err
			}
			for _, f := range fields {
				types[hashable(f)] = true
			}
		}
	}
	functions, _ := list(program, "functions")
	for _, function := range functions {
		o, ok := function.(*Object)
		if !ok {
			return nil, errShape
		}
		types[hashable(o.Values["result"])] = true
		if err := paramTypes(function, "params", types); err != nil {
			return nil, err
		}
	}
	hasJoin := false
	for _, node := range nodes {
		o := node.(*Object)
		switch o.Values["op"] {
		case "let":
			bind := o.Values["bind"]
			if b, ok := bind.(*Object); ok {
				types[hashable(b.Values["type"])] = true
			} else if _, present := o.Values["bind"]; present {
				return nil, errShape
			} else {
				types[nil] = true
			}
			value, err := field(node, "value")
			if err != nil {
				return nil, err
			}
			kind, err := field(value, "kind")
			if err != nil {
				return nil, err
			}
			if kind == "uint8" || kind == "uint16" {
				types["UInt"+kind.(string)[4:]] = true
			}
			if kind == "primitive" {
				name, err := field(value, "name")
				if err != nil {
					return nil, err
				}
				// Only an admitted primitive declares a family. Any other
				// name (another type, a suffixed or renamed leaf) is rejected
				// by IR validation and must not claim a capability here.
				if s, ok := name.(string); ok {
					if _, admitted := moduleir.LookupPrimitive(s); admitted {
						operation := s[strings.IndexByte(s, '.')+1:]
						switch {
						case strings.HasPrefix(operation, "toUInt"):
							features[moduleir.FeatureWidthConversion] = true
						case bitwiseOperations[operation]:
							features[moduleir.FeatureBitwise] = true
						case operation == "div" || operation == "mod":
							features[moduleir.FeatureDivision] = true
						}
					}
				}
			}
		case "join":
			hasJoin = true
			join, err := field(node, "join")
			if err != nil {
				return nil, err
			}
			if err := paramTypes(join, "params", types); err != nil {
				return nil, err
			}
		case "cases":
			types[hashable(o.Values["type"])] = true
			types[hashable(o.Values["result"])] = true
			alts, _ := list(node, "alts")
			for _, alt := range alts {
				if err := paramTypes(alt, "params", types); err != nil {
					return nil, err
				}
			}
		}
	}
	if types[errShape] {
		return nil, errShape
	}
	for typ := range types {
		if s, ok := typ.(string); ok {
			if feature, ok := scalarFeatures[s]; ok {
				features[feature] = true
			}
		}
	}
	if hasJoin {
		features[moduleir.FeatureJoin] = true
	}
	byteFold := false
	for _, typ := range declared {
		name, err := field(typ, "name")
		if err != nil {
			return nil, err
		}
		if name == moduleir.ByteListType {
			byteFold = true
		}
	}
	if byteFold {
		features[moduleir.FeatureByteFold] = true
		api, err := field(program, "api")
		if err != nil {
			return nil, err
		}
		records, err := field(api, "records")
		if err != nil {
			return nil, err
		}
		for _, function := range functions {
			result, err := field(function, "result")
			if err != nil {
				return nil, err
			}
			if !contains(records, result) {
				continue
			}
			params, err := list(function, "params")
			if err != nil {
				return nil, err
			}
			if len(params) != 2 {
				continue
			}
			first, err := field(params[0], "type")
			if err != nil {
				return nil, err
			}
			second, err := field(params[1], "type")
			if err != nil {
				return nil, err
			}
			if safeEqual(first, result) && second == moduleir.ByteListType {
				features[moduleir.FeatureRecordFold] = true
			}
		}
	}
	names := make([]string, 0, len(features))
	for name := range features {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, len(names))
	for i, name := range names {
		out[i] = name
	}
	return out, nil
}

// contains reports whether value is a key of an object or an element of an array.
func contains(container, value any) bool {
	switch c := container.(type) {
	case *Object:
		s, ok := value.(string)
		_, present := c.Values[s]
		return ok && present
	case []any:
		for _, item := range c {
			if safeEqual(item, value) {
				return true
			}
		}
	}
	return false
}

// safeEqual compares JSON scalars; containers are never equal here.
func safeEqual(a, b any) bool {
	if hashable(a) == errShape || hashable(b) == errShape {
		return false
	}
	return a == b
}
