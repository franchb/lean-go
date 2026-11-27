package pipeline

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1) port of the producer half of
// scripts/test_unsigned.py: every operation of UInt8/16/32/64 and all twelve
// conversions, compiler rewrites of the new operations, and rejections that
// must stay. The native comparison is integration/unsigned_native_test.go.

var unsignedWidths = []int{8, 16, 32, 64}

var unsignedOperations = []string{"add", "sub", "mul", "div", "mod", "land", "lor", "xor", "complement",
	"shiftLeft", "shiftRight", "decEq", "decLt", "decLe"}

var unsignedFields = []string{"add", "sub", "mul", "div", "mod", "and", "or", "xor", "not", "shl", "shr", "eq", "lt", "le"}

var unsignedNotation = []string{"a + b", "a - b", "a * b", "a / b", "a % b", "a &&& b", "a ||| b", "a ^^^ b", "~~~a",
	"a <<< b", "a >>> b", "a == b", "decide (a < b)", "decide (a ≤ b)"}

// unsignedMatrix is the 68 primitive names: 14 operations per width and the
// conversions to the three other widths.
func unsignedMatrix() []string {
	var out []string
	for _, w := range unsignedWidths {
		for _, op := range unsignedOperations {
			out = append(out, fmt.Sprintf("UInt%d.%s", w, op))
		}
		for _, v := range unsignedTargets(w) {
			out = append(out, fmt.Sprintf("UInt%d.toUInt%d", w, v))
		}
	}
	return out
}

func unsignedTargets(width int) []int {
	var out []int
	for _, v := range unsignedWidths {
		if v != width {
			out = append(out, v)
		}
	}
	return out
}

// unsignedMatrixSource is one root per width returning every operation and
// conversion in a record, by notation or by direct calls.
func unsignedMatrixSource(direct bool) string {
	var lines []string
	for _, w := range unsignedWidths {
		t := fmt.Sprintf("UInt%d", w)
		lines = append(lines, fmt.Sprintf("structure Ops%d where", w))
		for _, field := range unsignedFields {
			typ := t
			if field == "eq" || field == "lt" || field == "le" {
				typ = "Bool"
			}
			lines = append(lines, fmt.Sprintf("  %s : %s", field, typ))
		}
		for _, v := range unsignedTargets(w) {
			lines = append(lines, fmt.Sprintf("  to%d : UInt%d", v, v))
		}
		var values []string
		if direct {
			for _, op := range unsignedOperations[:11] {
				value := fmt.Sprintf("%s.%s a", t, op)
				if op != "complement" {
					value += " b"
				}
				values = append(values, value)
			}
			values = append(values, fmt.Sprintf("@decide (a = b) (%s.decEq a b)", t), fmt.Sprintf("@decide (a < b) (%s.decLt a b)", t),
				fmt.Sprintf("@decide (a ≤ b) (%s.decLe a b)", t))
			for _, v := range unsignedTargets(w) {
				values = append(values, fmt.Sprintf("%s.toUInt%d a", t, v))
			}
		} else {
			values = append(values, unsignedNotation...)
			for _, v := range unsignedTargets(w) {
				values = append(values, fmt.Sprintf("a.toUInt%d", v))
			}
		}
		lines = append(lines, fmt.Sprintf("def ops%d (a b : %s) : Ops%d :=\n  ⟨%s⟩", w, t, w, strings.Join(values, ", ")))
	}
	return strings.Join(lines, "\n") + "\n"
}

func unsignedExportRoots(t *testing.T, body string, roots ...string) map[string]any {
	t.Helper()
	qualified := make([]string, len(roots))
	for i, root := range roots {
		qualified[i] = "Numeric." + root
	}
	return numericExportRoots(t, "namespace Numeric\n"+body+"end Numeric\n", qualified...)
}

func TestUnsignedOrdinaryNotationAdmitsEveryPrimitiveOfEveryWidth(t *testing.T) {
	requireModules(t)
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprintf("direct=%v", direct), func(t *testing.T) {
			program := unsignedExportRoots(t, unsignedMatrixSource(direct), "ops8", "ops16", "ops32", "ops64")
			if len(unsignedMatrix()) != 68 {
				t.Fatal("matrix size")
			}
			assertSet(t, "primitives", emittedPrimitives(program), unsignedMatrix()...)
			assertList(t, "features", featuresOf(program), "bitwise", "division", "uint16", "uint32", "uint64", "uint8", "width_conversion")
			if len(arr(program["rewrites"])) != 0 {
				t.Fatalf("rewrites %v", program["rewrites"])
			}
			if !direct {
				for _, w := range unsignedWidths {
					assertSubset(t, "provenance", []string{fmt.Sprintf("instDivUInt%d", w), fmt.Sprintf("instModUInt%d", w),
						fmt.Sprintf("instAndOpUInt%d", w), fmt.Sprintf("instComplementUInt%d", w),
						fmt.Sprintf("instLinearOrderPackageUInt%d", w)}, provenanceNames(program))
				}
			}
		})
	}
}

func TestUnsignedCompilerRewritesOfDivisionRemainderAndMultiplication(t *testing.T) {
	requireModules(t)
	source := `def scaled (a : UInt16) : UInt16 := a * 8
def quotient (a : UInt8) : UInt8 := a / 8
def remainder (a : UInt64) : UInt64 := a % 1024
def byZero (a : UInt32) : UInt32 := a / 0
def modZero (a : UInt32) : UInt32 := a % 0
def byOne (a : UInt16) : UInt16 := a / 1
def modOne (a : UInt16) : UInt16 := a % 1
def wrapped (a : UInt8) : UInt8 := a / 256
def odd (a : UInt32) : UInt32 := a / 3
def top (a : UInt64) : UInt64 := a / 9223372036854775808
def numerator (b : UInt16) : UInt16 := 0 / b
def folded : UInt16 := 100 / 7 + 100 % 0
`
	program := unsignedExportRoots(t, source, "byOne", "byZero", "folded", "modOne", "modZero", "numerator", "odd", "quotient",
		"remainder", "scaled", "top", "wrapped")
	shapes := map[string][]string{}
	for _, function := range arr(program["functions"]) {
		code := obj(obj(function)["body"])
		steps := []string{}
		for code["op"] == "let" {
			value := obj(code["value"])
			steps = append(steps, str2(value["kind"])+" "+str2(value["name"]))
			code = obj(code["next"])
		}
		shapes[strings.TrimPrefix(str2(obj(function)["name"]), "Numeric.")] = steps
	}
	want := map[string][]string{
		"scaled":    {"uint16 3", "primitive UInt16.shiftLeft"},
		"quotient":  {"uint8 3", "primitive UInt8.shiftRight"},
		"remainder": {"uint64 1024", "primitive UInt64.mod"},
		"byZero":    {"uint32 0", "primitive UInt32.div"},
		"modZero":   {}, "byOne": {}, "modOne": {"uint16 0"},
		// 256 wraps to the UInt8 literal 0, so the division stays and yields 0.
		"wrapped":   {"uint8 0", "primitive UInt8.div"},
		"odd":       {"uint32 3", "primitive UInt32.div"},
		"top":       {"uint64 63", "primitive UInt64.shiftRight"},
		"numerator": {"uint16 0"},
		// 100 / 7 + 100 % 0 = 14 + 100: Lean's own folding is exact.
		"folded": {"uint16 114"},
	}
	if !reflect.DeepEqual(shapes, want) {
		t.Fatalf("shapes\n got %v\nwant %v", shapes, want)
	}
}

func TestUnsignedErasedDivisionIsAuditedButNotEmitted(t *testing.T) {
	requireModules(t)
	for _, w := range unsignedWidths {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			program := numericExport(t, fmt.Sprintf("def run (a b : UInt%d) :=\n  let _unused := a / b + a %% b\n  a\n", w), numericOptions{})
			assertList(t, "primitives", emittedPrimitives(program))
			assertList(t, "features", featuresOf(program), fmt.Sprintf("uint%d", w))
			assertSubset(t, "provenance", []string{fmt.Sprintf("instDivUInt%d", w), fmt.Sprintf("instModUInt%d", w)}, provenanceNames(program))
		})
	}
}

func TestUnsignedDivisionRewriteHistoryAndWrapperReplacementReject(t *testing.T) {
	requireModules(t)
	checkPrimitiveHistory(t, []historyPrimitive{{"UInt16.div", "UInt16", false}, {"UInt8.mod", "UInt8", false}}, "division")
	prelude := "def replacement : Mod UInt64 := instModUInt64\ntheorem replacement_eq : instModUInt64 = replacement := rfl\n"
	call := "def run (a b : UInt64) := a % b\n"
	erased := "def run (a b : UInt64) :=\n  let _unused := a % b\n  a\n"
	for i, source := range rewriteSources(prelude, call, erased) {
		t.Run(fmt.Sprintf("wrapper/%d", i), func(t *testing.T) {
			numericReject(t, source, "rewrite|replacement|repeated compilation", numericOptions{})
		})
	}
}

func TestUnsignedChangedDivisionInstanceOrPrimitiveAttributesReject(t *testing.T) {
	requireModules(t)
	for _, c := range [][2]string{
		{"instDivUInt32", "def run (a b : UInt32) := a / b\n"},
		{"UInt8.div", "def run (a b : UInt8) := UInt8.div a b\n"},
		{"UInt16.toUInt64", "def run (a : UInt16) := a.toUInt64\n"},
	} {
		t.Run(c[0], func(t *testing.T) {
			numericReject(t, c[1], "Numeric source extern changed|numeric primitive",
				numericOptions{probe: importedAttributeProbe("externAttr", c[0], untrustedExtern)})
		})
	}
}

func TestUnsignedSignedNaturalPlatformAndFloatNumbersStayRejected(t *testing.T) {
	requireModules(t)
	for name, body := range map[string]string{
		"Int8":             "def run (a b : Int8) := a / b\n",
		"Int16":            "def run (a b : Int16) := a + b\n",
		"Int32":            "def run (a b : Int32) := a % b\n",
		"Int64":            "def run (a b : Int64) := a &&& b\n",
		"Nat":              "def run (a b : Nat) := a / b\n",
		"Nat helper":       "def run (a b : UInt32) : UInt32 := (a.toNat / b.toNat).toUInt32\n",
		"USize":            "def run (a b : USize) := a / b\n",
		"USize conversion": "def run (a : UInt64) := a.toUSize\n",
		"Float":            "def run (a b : Float) := a / b\n",
		"Float conversion": "def run (a : UInt16) := a.toFloat\n",
	} {
		t.Run(name, func(t *testing.T) {
			numericReject(t, body, "source audit|Source audit|unsupported|Unsupported|extern source dependency", numericOptions{})
		})
	}
}
