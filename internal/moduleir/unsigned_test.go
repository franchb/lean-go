package moduleir

import (
	"slices"
	"strings"
	"testing"
)

var unsignedWidths = []string{"UInt8", "UInt16", "UInt32", "UInt64"}

// unsignedMatrix is the complete admitted catalog: fourteen operations per
// width and the twelve conversions between distinct widths.
func unsignedMatrix() []string {
	var names []string
	for _, width := range unsignedWidths {
		for _, op := range []string{"add", "sub", "mul", "div", "mod", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight", "decEq", "decLt", "decLe"} {
			names = append(names, width+"."+op)
		}
		for _, target := range unsignedWidths {
			if target != width {
				names = append(names, width+".to"+target)
			}
		}
	}
	return names
}

func TestUnsignedMatrixSignaturesAndFeatures(t *testing.T) {
	names := unsignedMatrix()
	if len(names) != 68 {
		t.Fatal(len(names))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			primitive, ok := LookupPrimitive(name)
			if !ok || primitive.Name != name || primitive.Semantics == "" {
				t.Fatal(primitive, ok)
			}
			width, operation, _ := strings.Cut(name, ".")
			p := scalarProgram(&Value{Kind: "primitive", Name: name, Args: []int{1, 2}[:len(primitive.Params)]}, primitive.Result, width)
			p.Functions[0].Params = p.Functions[0].Params[:len(primitive.Params)]
			features, err := UsedFeatures(p)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{scalarFeatures[width]: true}
			if feature, ok := scalarFeatures[primitive.Result]; ok {
				want[feature] = true
			}
			switch {
			case operation == "div" || operation == "mod":
				want[FeatureDivision] = true
			case strings.HasPrefix(operation, "to"):
				want[FeatureWidthConversion] = true
			case slices.Contains([]string{"land", "lor", "xor", "complement", "shiftLeft", "shiftRight"}, operation):
				want[FeatureBitwise] = true
			}
			for feature := range want {
				if !slices.Contains(features, feature) {
					t.Fatal("missing feature", feature, features)
				}
			}
			if len(features) != len(want) {
				t.Fatal("unexpected features", features)
			}
			p.Features = features
			if err := Validate(p); err != nil {
				t.Fatal(err)
			}
			p.Features = slices.DeleteFunc(slices.Clone(features), func(f string) bool { return f == FeatureDivision })
			if want[FeatureDivision] && Validate(p) == nil {
				t.Fatal("accepted an undeclared division feature")
			}
		})
	}
	for _, name := range []string{"UInt8.toUInt8", "UInt16.toUInt16", "UInt32.toUInt32", "UInt64.toUInt64",
		"Int8.div", "Int16.add", "Int32.mod", "Int64.toUInt64", "USize.div", "USize.toUInt64", "UInt64.toUSize",
		"Nat.div", "Float.div", "UInt16.div._boxed", "UInt32.modCore", "UInt64.log2", "UInt8.toFloat", "UInt32.toNat"} {
		if _, ok := LookupPrimitive(name); ok {
			t.Fatal("admitted", name)
		}
	}
}
