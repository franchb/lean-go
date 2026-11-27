package moduleir

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Feature names declare the capability families a program uses. The validator
// recomputes them from the program, so a declared list is exact: sorted,
// duplicate-free and neither larger nor smaller than the used set.
const (
	// FeatureBitwise: AND, OR, XOR, complement and left/right shifts of any width.
	FeatureBitwise = "bitwise"
	// FeatureByteFold: the producer's byte-list type, used by byte-fold roots.
	FeatureByteFold = "byte_fold"
	// FeatureDivision: unsigned division or remainder of any width, with Lean's
	// x / 0 = 0 and x % 0 = x, which every emitter must guard.
	FeatureDivision = "division"
	// FeatureJoin: local join points and jumps.
	FeatureJoin = "join"
	// FeatureRecordFold: a byte-fold-shaped function whose accumulator is a source record.
	FeatureRecordFold = "record_fold"
	// FeatureUInt8 .. FeatureUInt64: a scalar width occurring in any executable type.
	FeatureUInt8  = "uint8"
	FeatureUInt16 = "uint16"
	FeatureUInt32 = "uint32"
	FeatureUInt64 = "uint64"
	// FeatureWidthConversion: an exact conversion between scalar widths.
	FeatureWidthConversion = "width_conversion"
)

// ByteListType is the producer's monomorphic byte-list type name.
const ByteListType = "List UInt8"

var scalarFeatures = map[string]string{"UInt8": FeatureUInt8, "UInt16": FeatureUInt16, "UInt32": FeatureUInt32, "UInt64": FeatureUInt64}

// UsedFeatures validates everything except the declared feature list and
// returns the sorted feature set the program actually uses.
func UsedFeatures(p *Program) ([]string, error) {
	v, err := validate(p)
	if err != nil {
		return nil, err
	}
	return v.featureList(), nil
}

func (v *validator) use(feature string) { v.features[feature] = true }

func (v *validator) useType(typ string) {
	if feature, ok := scalarFeatures[typ]; ok {
		v.use(feature)
	}
}

func (v *validator) usePrimitive(p Primitive) {
	operation := p.Name[strings.IndexByte(p.Name, '.')+1:]
	switch {
	case strings.HasPrefix(operation, "toUInt"):
		v.use(FeatureWidthConversion)
	case slices.Contains([]string{"land", "lor", "xor", "complement", "shiftLeft", "shiftRight"}, operation):
		v.use(FeatureBitwise)
	case operation == "div" || operation == "mod":
		v.use(FeatureDivision)
	}
}

// useFolds records the byte-list families after types, API and signatures are known.
func (v *validator) useFolds(p *Program) {
	if _, ok := v.types[ByteListType]; !ok {
		return
	}
	v.use(FeatureByteFold)
	for _, f := range p.Functions {
		if _, record := p.API.Records[f.Result]; record && len(f.Params) == 2 && f.Params[0].Type == f.Result && f.Params[1].Type == ByteListType {
			v.use(FeatureRecordFold)
		}
	}
}

func (v *validator) featureList() []string {
	out := make([]string, 0, len(v.features))
	for feature := range v.features {
		out = append(out, feature)
	}
	sort.Strings(out)
	return out
}

func (v *validator) checkFeatures(declared []string) error {
	used := v.featureList()
	if declared == nil || !slices.Equal(declared, used) {
		return fmt.Errorf("declared features %q differ from used features %q", declared, used)
	}
	return nil
}
