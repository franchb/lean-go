package pipeline

import (
	"fmt"
	"strings"
	"testing"
)

// Lean-gated (LEAN_GO_MODULES=1) ports of the producer halves of
// scripts/test_uint8.py and scripts/test_uint16.py, and of
// scripts/test_byte_folds.py. The native UInt16 check is in
// integration/unsigned_native_test.go.

var byteOps = []string{"add", "sub", "mul", "decEq", "decLt", "decLe", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight"}

// scripts/test_uint8.py ByteAdmissionTests

func TestByteExactOperationsNotationAndConversionSignatures(t *testing.T) {
	requireModules(t)
	var b strings.Builder
	b.WriteString("structure Result where\n")
	for i, op := range byteOps {
		typ := "UInt8"
		if strings.HasPrefix(op, "dec") {
			typ = "Bool"
		}
		fmt.Fprintf(&b, "  x%d : %s\n", i, typ)
	}
	b.WriteString("  wide : UInt32\n  narrow : UInt8\n  maximum : UInt8\n  wrapped : UInt8\n")
	b.WriteString("def run (a b : UInt8) (word : UInt32) : Result :=\n  ⟨")
	var values []string
	for _, op := range byteOps {
		value := "UInt8." + op + " a"
		if op != "complement" {
			value += " b"
		}
		values = append(values, value)
	}
	b.WriteString(strings.Join(values, ", ") + ", a.toUInt32, word.toUInt8, 255, 257⟩\n")
	body := b.String()
	// dec* kernel results are dependent Decidable; ordinary decide supplies Bool.
	for _, c := range [][2]string{{"decEq", "a = b"}, {"decLt", "a < b"}, {"decLe", "a ≤ b"}} {
		body = strings.ReplaceAll(body, "UInt8."+c[0]+" a b", "decide ("+c[1]+")")
	}
	want := []string{"UInt8.toUInt32", "UInt32.toUInt8"}
	for _, op := range byteOps {
		want = append(want, "UInt8."+op)
	}
	for _, modern := range []bool{false, true} {
		program := numericExport(t, body, numericOptions{modern: modern})
		assertSubset(t, "features", []string{"bitwise", "uint32", "uint8", "width_conversion"}, featuresOf(program))
		assertSet(t, "primitives", emittedPrimitives(program), want...)
		if hasString(names(program["types"]), "UInt8") {
			t.Fatal("UInt8 exported as a type")
		}
		assertList(t, "params", typesOfParams(obj(arr(program["functions"])[0])["params"]), "UInt8", "UInt8", "UInt32")
	}
	program := numericExport(t, "def run (a b : UInt8) := ((a + b) - a) * b ^^^ (~~~a &&& b ||| a) <<< b >>> a\n", numericOptions{})
	var notation []string
	for _, op := range []string{"add", "sub", "mul", "land", "lor", "xor", "complement", "shiftLeft", "shiftRight"} {
		notation = append(notation, "UInt8."+op)
	}
	assertSubset(t, "notation primitives", notation, emittedPrimitives(program))
}

func TestByteIdentityFieldsHelpersJoinsAndDeadSourceUses(t *testing.T) {
	requireModules(t)
	for _, source := range []string{"def run (a : UInt8) := a\n",
		"structure Packet where\n  value : UInt8\n  word : UInt32\ndef run (p : Packet) := p\n",
		"structure Box where\n  value : UInt8\ndef run (a : UInt8) : Box := ⟨a⟩\n",
		"@[noinline] def helper (a b : UInt8) := a + b\ndef run (flag : Bool) (a b : UInt8) := if flag then helper a b else helper b a\n",
		"def run (a : UInt8) :=\n  let _unused := a.toUInt32\n  a\n"} {
		assertContains(t, source, featuresOf(numericExport(t, source, numericOptions{})), "uint8")
	}
	program := numericExport(t, "def run (a b : UInt32) :=\n  let _unused : UInt8 := a.toUInt8 + b.toUInt8\n  a\n", numericOptions{})
	assertList(t, "features", featuresOf(program), "uint32")
	assertList(t, "primitives", emittedPrimitives(program))
	assertContains(t, "provenance", provenanceNames(program), "instAddUInt8")
	multi := numericExportRoots(t, "namespace Numeric\ndef run (a : UInt8) := a\ndef second (a : UInt32) := a.toUInt8\nend Numeric\n", "Numeric.run", "Numeric.second")
	assertList(t, "multi-root features", featuresOf(multi), "uint32", "uint8", "width_conversion")
}

func TestByteUnsupportedWidthsAliasesAndOperatorsRemainRejected(t *testing.T) {
	requireModules(t)
	for _, source := range []string{"def run (a : USize) := a\n",
		"def run (a : UInt8) := a.toUSize\n",
		"def run (a : Int8) := a / 3\n",
		"def run (a : UInt8) := a.toNat\n",
		"abbrev Byte := UInt8\ndef run (a : Byte) := a\n",
		"unsafe def run (a : UInt8) := a\n",
		"axiom forged : UInt8\ndef run (a : UInt8) := a + forged\n"} {
		t.Run(source, func(t *testing.T) {
			numericReject(t, source, "Unsupported|unsupported|extern|unsafe|Unsafe|axiom|not an inductive type|not supported by code generator", numericOptions{})
		})
	}
}

// byteCatalogProbe tests every new leaf in one pristine build, restoring the
// environment around each deliberate corruption so later probes remain
// independent. "§" stands for a backquote.
var byteCatalogProbe = strings.ReplaceAll(`open Lean LeanGo.Module Lean.Compiler.LCNF
run_cmd Lean.Elab.Command.liftCoreM do
  let pristine ← getEnv
  let leaves := numericPrimitives.filter fun p =>
    #[§§UInt8, §§UInt16].contains p.operand || #[§§UInt8, §§UInt16].contains p.result
  -- 17 leaves of each operand width and four narrowing casts from UInt32/UInt64.
  unless leaves.size == 38 do throwError "UInt8/UInt16 catalog size"
  for primitive in leaves do
    validateNumericPrimitive primitive
    let some decl ← getMonoDecl? primitive.name | throwError "missing numeric leaf"
    let mutations := #[{ decl with safe := false }, { decl with params := #[] },
      { decl with levelParams := [§u] }, { decl with type := .const §§USize [] },
      { decl with params := decl.params.map fun p => { p with type := .const §§USize [] } },
      { decl with value := .extern { entries := [.standard §all "untrusted"] } }]
    for changed in mutations do
      withEnv pristine do
        let some index := pristine.getModuleIdxFor? primitive.name | throwError "not imported"
        modifyEnv fun env => monoExt.toEnvExtension.modifyState env fun state =>
          let entries := (state.importedEntries[index]!).map fun entry =>
            if entry.name == primitive.name then changed else entry
          { state with importedEntries := state.importedEntries.set! index entries }
        let rejected ← try
          validateNumericPrimitive primitive
          pure false
        catch _ => pure true
        unless rejected do throwError "accepted mono corruption: {primitive.name}"
    for safety in #[DefinitionSafety.safe, DefinitionSafety.unsafe] do
      withEnv (← mkEmptyEnvironment) do
        addDecl <| .axiomDecl { name := §Forged, levelParams := [], type := .sort .zero, isUnsafe := false }
        addDecl <| .defnDecl { name := primitive.name, levelParams := [], type := .sort .zero, value := .const §Forged [], hints := .opaque, safety := safety }
        let message ← try
          validateNumericDefinition primitive.name
          pure ""
        catch error => error.toMessageData.toString
        let expected := if safety == .safe then "Unexpected numeric boundary axioms" else "Unsafe numeric boundary"
        unless message.startsWith expected do
          throwError "unexpected definition validation result: {primitive.name}: {message}"
  let names := leaves.map (·.name) ++ #[§§UInt8.ofNat, §§instAddUInt8,
    §§instSubUInt8, §§instMulUInt8, §§instAndOpUInt8, §§instOrOpUInt8,
    §§instXorOpUInt8, §§instComplementUInt8, §§instShiftLeftUInt8,
    §§instShiftRightUInt8, §§instLinearOrderPackageUInt8, §§instDecidableEqUInt8,
    §§instLTUInt8, §§instLEUInt8, §§UInt16.ofNat, §§UInt16.instOfNat,
    §§instOrOpUInt16, §§instXorOpUInt16, §§instShiftLeftUInt16, §§instShiftRightUInt16]
  for name in names do
    let some index := pristine.getModuleIdxFor? name | throwError "not imported"
    for dimension in [:3] do
      withEnv pristine do
        if dimension == 0 then
          modifyEnv fun env => externAttr.ext.toEnvExtension.modifyState env fun state =>
            let entries := ((state.importedEntries[index]!).filter (·.1 != name)).push
              (name, { entries := [.standard §all "untrusted"] })
            let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
            { state with importedEntries := state.importedEntries.set! index entries }
        else if dimension == 1 then
          modifyEnv fun env => Compiler.implementedByAttr.ext.toEnvExtension.modifyState env fun state =>
            let entries := ((state.importedEntries[index]!).filter (·.1 != name)).push (name, §Untrusted)
            let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
            { state with importedEntries := state.importedEntries.set! index entries }
        else
          modifyEnv fun env => regularInitAttr.ext.toEnvExtension.modifyState env fun state =>
            let entries := ((state.importedEntries[index]!).filter (·.1 != name)).push (name, §Untrusted)
            let entries := entries.qsort (fun a b => Name.quickLt a.1 b.1)
            { state with importedEntries := state.importedEntries.set! index entries }
        if (numericBoundaryIssues (← getEnv) name).isEmpty then
          throwError "accepted source corruption: {name}"
`, "§", "`")

func TestByteExactCatalogSourceAttributesAndMonoSignatures(t *testing.T) {
	requireModules(t)
	program := numericExport(t, "def run (a : UInt8) := a.toUInt32\n", numericOptions{probe: byteCatalogProbe})
	assertContains(t, "features", featuresOf(program), "width_conversion")
}

func TestBytePristineLogicalClosureHelpersAreNotRuntimeLeaves(t *testing.T) {
	requireModules(t)
	for _, c := range [][3]string{{"UInt8.toNat", "UInt8.toUInt32", "UInt8"}, {"Nat.toUInt8", "UInt32.toUInt8", "UInt32"}} {
		t.Run(c[0], func(t *testing.T) {
			numericReject(t, fmt.Sprintf("def run (a : %s) := %s a\n", c[2], c[1]), "Numeric source extern changed",
				numericOptions{probe: importedAttributeProbe("externAttr", c[0], untrustedExtern)})
		})
	}
}

func TestByteLocalScopedLateErasedAndClosedScopeHistory(t *testing.T) {
	requireModules(t)
	checkPrimitiveHistory(t, []historyPrimitive{{"UInt8.toUInt32", "UInt8", true}, {"UInt32.toUInt8", "UInt32", true},
		{"UInt8.shiftLeft", "UInt8", false}}, "uint8")
}

// scripts/test_uint16.py UInt16AdmissionTests

func TestUInt16NestedRecordsLiteralsAndExactOperators(t *testing.T) {
	requireModules(t)
	source := `structure Words where
  first : UInt16
  second : UInt16
structure Packet where
  words : Words
  low : UInt8
  limit : UInt16
@[noinline] def combine (a b : UInt8) := (a.toUInt16 <<< 8) ||| b.toUInt16
def run (flag : Bool) (a b : UInt8) (p : Packet) : Packet :=
  let word := if flag then combine a b else p.words.first
  ⟨⟨word, 65537⟩, (word >>> p.words.second).toUInt8, 65535⟩
`
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprintf("modern=%v", modern), func(t *testing.T) {
			program := numericExport(t, source, numericOptions{modern: modern})
			assertSubset(t, "features", []string{"bitwise", "join", "uint16", "uint8", "width_conversion"}, featuresOf(program))
			assertSet(t, "primitives", emittedPrimitives(program), "UInt8.toUInt16", "UInt16.toUInt8", "UInt16.lor", "UInt16.shiftLeft", "UInt16.shiftRight")
			types := typesOf(program)
			if types["UInt16"] != nil {
				t.Fatal("UInt16 exported as a type")
			}
			assertList(t, "Words fields", typesAny(obj(arr(types["Numeric.Words"]["constructors"])[0])["fields"]), "UInt16", "UInt16")
			assertList(t, "Packet fields", typesAny(obj(arr(types["Numeric.Packet"]["constructors"])[0])["fields"]), "Numeric.Words", "UInt8", "UInt16")
			join := false
			var literals []string
			for _, function := range arr(program["functions"]) {
				for _, node := range nodes(obj(function)["body"]) {
					if node["op"] == "join" && strings.Join(typesOfParams(obj(node["join"])["params"]), ",") == "UInt16" {
						join = true
					}
					if node["op"] == "let" && obj(node["value"])["kind"] == "uint16" {
						literals = append(literals, str2(obj(node["value"])["name"]))
					}
				}
			}
			if !join {
				t.Fatal("no join with a single UInt16 parameter")
			}
			assertSubset(t, "UInt16 literals", []string{"1", "8", "65535"}, literals)
		})
	}
}

func TestUInt16ScalarIdentityAndDeadProvenance(t *testing.T) {
	requireModules(t)
	identity := numericExport(t, "def run (w : UInt16) := w\n", numericOptions{})
	assertList(t, "identity features", featuresOf(identity), "uint16")
	if obj(obj(arr(identity["functions"])[0])["body"])["op"] != "return" {
		t.Fatal("identity body is not a return")
	}
	for _, c := range [][2]string{{"UInt8", "a.toUInt16"}, {"UInt32", "a.toUInt8.toUInt16"}} {
		program := numericExport(t, fmt.Sprintf("def run (a : %s) :=\n  let _unused := %s\n  a\n", c[0], c[1]), numericOptions{})
		assertList(t, "primitives", emittedPrimitives(program))
		assertContains(t, "provenance", provenanceNames(program), "UInt8.toUInt16")
		assertList(t, "features", featuresOf(program), strings.ToLower(c[0]))
	}
	multi := numericExportRoots(t, "namespace Numeric\ndef run (a b : UInt8) := (a + b).toUInt16\ndef second (w : UInt16) := w\nend Numeric\n", "Numeric.run", "Numeric.second")
	assertList(t, "multi-root features", featuresOf(multi), "uint16", "uint8", "width_conversion")
	assertSet(t, "multi-root primitives", emittedPrimitives(multi), "UInt8.add", "UInt8.toUInt16")
}

func TestUInt16WideningAndErasedFeatures(t *testing.T) {
	requireModules(t)
	source := "def run (word : UInt16) : UInt32 := word.toUInt32"
	program := numericExport(t, source, numericOptions{})
	assertList(t, "features", featuresOf(program), "uint16", "uint32", "width_conversion")
	if len(arr(program["functions"])) != 1 {
		t.Fatal("one function expected")
	}
	function := obj(arr(program["functions"])[0])
	assertList(t, "params", typesOfParams(function["params"]), "UInt16")
	if function["result"] != "UInt32" {
		t.Fatal("result")
	}
	jsonEqual(t, obj(function["body"])["value"], fmt.Sprintf(`{"kind":"primitive","name":"UInt16.toUInt32","args":[%v]}`, paramIDs(function["params"])[0]))
	erased := numericExport(t, "def run (word : UInt16) :=\n  let _unused := word.toUInt32\n  word", numericOptions{})
	assertList(t, "erased features", featuresOf(erased), "uint16")
	assertList(t, "erased primitives", emittedPrimitives(erased))
	assertContains(t, "erased provenance", provenanceNames(erased), "UInt16.toUInt32")
	multi := numericExportRoots(t, "module\npublic section\nnamespace Numeric\n"+source+"\ndef second (word : UInt16) := word.toUInt8\nend Numeric\n", "Numeric.run", "Numeric.second")
	assertList(t, "multi-root features", featuresOf(multi), "uint16", "uint32", "uint8", "width_conversion")
	assertSet(t, "multi-root primitives", emittedPrimitives(multi), "UInt16.toUInt32", "UInt16.toUInt8")
}

func TestUInt16XorExactNotationAndErasedFeatures(t *testing.T) {
	requireModules(t)
	for _, c := range []struct {
		modern             bool
		expression, source string
	}{{false, "UInt16.xor a b", "UInt16.xor"}, {true, "a ^^^ b", "instXorOpUInt16"}} {
		t.Run(c.expression, func(t *testing.T) {
			o := numericOptions{modern: c.modern}
			program := numericExport(t, "def run (a b : UInt16) := "+c.expression, o)
			assertList(t, "features", featuresOf(program), "bitwise", "uint16")
			if len(arr(program["functions"])) != 1 {
				t.Fatal("one function expected")
			}
			function := obj(arr(program["functions"])[0])
			assertList(t, "params", typesOfParams(function["params"]), "UInt16", "UInt16")
			if function["result"] != "UInt16" {
				t.Fatal("result")
			}
			params := paramIDs(function["params"])
			jsonEqual(t, obj(function["body"])["value"], fmt.Sprintf(`{"kind":"primitive","name":"UInt16.xor","args":[%v,%v]}`, params[0], params[1]))
			if len(arr(program["rewrites"])) != 0 {
				t.Fatal("rewrites")
			}
			assertContains(t, "provenance", provenanceNames(program), c.source)
			erased := numericExport(t, "def run (a b : UInt16) :=\n  let _unused := "+c.expression+"\n  a", o)
			assertList(t, "erased features", featuresOf(erased), "uint16")
			assertList(t, "erased primitives", emittedPrimitives(erased))
			assertContains(t, "erased provenance", provenanceNames(erased), c.source)
		})
	}
}

func TestUInt16XorMultiRootDeclaresBothFamilies(t *testing.T) {
	requireModules(t)
	program := numericExportRoots(t, "namespace Numeric\ndef run (a b : UInt16) := a ^^^ b\ndef second (a : UInt16) := a.toUInt32\nend Numeric\n", "Numeric.run", "Numeric.second")
	assertList(t, "features", featuresOf(program), "bitwise", "uint16", "uint32", "width_conversion")
	assertList(t, "roots", typesAny(program["roots"]), "Numeric.run", "Numeric.second")
	assertSet(t, "primitives", emittedPrimitives(program), "UInt16.xor", "UInt16.toUInt32")
}

func TestUInt16XorPrimitiveAndNotationRewriteHistory(t *testing.T) {
	requireModules(t)
	checkPrimitiveHistory(t, []historyPrimitive{{"UInt16.xor", "UInt16", false}}, "bitwise")
	prelude := "def replacement : XorOp UInt16 := instXorOpUInt16\ntheorem replacement_eq : instXorOpUInt16 = replacement := rfl\n"
	for i, source := range rewriteSources(prelude, "def run (a b : UInt16) := a ^^^ b\n", "def run (a b : UInt16) :=\n  let _unused := a ^^^ b\n  a\n") {
		t.Run(fmt.Sprintf("wrapper/%d", i), func(t *testing.T) {
			numericReject(t, source, "rewrite|replacement|repeated compilation", numericOptions{})
		})
	}
}

func TestUInt16XorErasedNotationAuditsLogicalHelper(t *testing.T) {
	requireModules(t)
	numericReject(t, "def run (a b : UInt16) :=\n  let _unused := a ^^^ b\n  a", "Numeric source extern changed",
		numericOptions{probe: importedAttributeProbe("externAttr", "BitVec.instXorOp", untrustedExtern)})
}

func TestUInt16WideningRewriteHistory(t *testing.T) {
	requireModules(t)
	checkPrimitiveHistory(t, []historyPrimitive{{"UInt16.toUInt32", "UInt16", true}}, "width_conversion")
}

func TestUInt16RejectUnadmittedWidthsOperatorsAndAlias(t *testing.T) {
	requireModules(t)
	for _, source := range []string{
		"def run (a : UInt16) :=\n  let _unused := a.toNat * a.toNat\n  a",
		"def run (a : Int16) := a &&& a", "def run (a : UInt16) := a.toInt16",
		"def run (a : UInt16) := a.toUSize", "def run (a : UInt16) := a.toFloat",
		"def run (a : UInt16) := a.toNat",
		"abbrev Half := UInt16\ndef run (a : Half) := a",
	} {
		t.Run(source, func(t *testing.T) {
			numericReject(t, source, "extern|opaque|Unsupported|unsupported|not an inductive type|provenance", numericOptions{})
		})
	}
}

func TestUInt16TrustedLiteralAndLogicalHelperAttributeChangesReject(t *testing.T) {
	requireModules(t)
	for _, c := range [][2]string{{"UInt16.ofNat", "def run : UInt16 := 65535"},
		{"UInt16.toNat", "def run (w : UInt16) := w.toUInt8"},
		{"UInt16.toNat", "def run (w : UInt16) := w.toUInt32"}} {
		t.Run(c[1], func(t *testing.T) {
			numericReject(t, c[1], "Numeric source extern changed", numericOptions{probe: importedAttributeProbe("externAttr", c[0], untrustedExtern)})
		})
	}
}

func TestUInt16ScopedRewritesOfErasedOperationsReject(t *testing.T) {
	requireModules(t)
	numericReject(t, `@[noinline] def replacement (w c : UInt16) := UInt16.shiftLeft w c
theorem replacement_eq : UInt16.shiftLeft = replacement := rfl
namespace Scope
attribute [scoped csimp] replacement_eq
end Scope
open scoped Scope
def run (a b : UInt8) :=
  let _unused := a.toUInt16 <<< b.toUInt16
  a
`, "rewrite|replacement|repeated compilation", numericOptions{})
}

// scripts/test_byte_folds.py ByteFoldTests: any export failure rejects, as
// assertRaises(ExportError) did.

func byteFoldRejects(t *testing.T, body string) {
	t.Helper()
	f := numericFixture(t, body, numericOptions{})
	if _, err := f.tryExport("Numeric", "Numeric.run"); err == nil {
		t.Fatal("accepted")
	}
}

func TestByteFoldDirect(t *testing.T) {
	requireModules(t)
	source := "def tick (s : UInt32) (b : UInt8) := s + b.toUInt32\ndef run (s : UInt32) (bytes : List UInt8) := bytes.foldl tick s\n"
	for _, modern := range []bool{false, true} {
		ir := numericExport(t, source, numericOptions{modern: modern})
		assertList(t, "features", featuresOf(ir), "byte_fold", "uint32", "uint8", "width_conversion")
		assertContains(t, "functions", names(ir["functions"]), "Numeric.tick")
		assertList(t, "List UInt8 cons fields", typesAny(obj(arr(typesOf(ir)["List UInt8"]["constructors"])[1])["fields"]), "UInt8", "List UInt8")
	}
}

func TestByteFoldRecord(t *testing.T) {
	requireModules(t)
	ir := numericExport(t, `structure Pair where
  count : UInt32
  total : UInt32
def see (s : Pair) (b : UInt8) : Pair := ⟨s.count + 1, s.total + b.toUInt32⟩
def run (s : Pair) (bs : List UInt8) := bs.foldl see s
`, numericOptions{})
	assertList(t, "features", featuresOf(ir), "byte_fold", "record_fold", "uint32", "uint8", "width_conversion")
	assertList(t, "Pair record", typesAny(obj(obj(ir["api"])["records"])["Numeric.Pair"]), "count", "total")
}

func TestByteFoldRejectOtherAccumulators(t *testing.T) {
	requireModules(t)
	for _, c := range [][2]string{
		{"structure S where\n  a : UInt32\n  b : UInt16", "⟨s.a + b.toUInt32, s.b⟩"},
		{"structure S where\n  a : UInt32\n  b : UInt32\n  c : UInt32", "⟨s.a + b.toUInt32, s.b, s.c⟩"},
		{"inductive S where | mk (a b : UInt32)", "match s with | .mk a c => .mk (a + b.toUInt32) c"},
	} {
		t.Run(c[0], func(t *testing.T) {
			byteFoldRejects(t, c[0]+"\ndef step (s : S) (b : UInt8) : S := "+c[1]+"\ndef run (s : S) (bs : List UInt8) := bs.foldl step s\n")
		})
	}
}

func TestByteFoldRejectDifferentRecurrences(t *testing.T) {
	requireModules(t)
	for _, source := range []string{
		"def run (s : UInt32) (bs : List UInt32) := bs.foldl UInt32.add s",
		"def run (s : UInt32) (bs : List UInt8) := bs.foldl (fun n b => n + b.toUInt32 + s) s",
		"def tick (s : UInt32) (b : UInt8) := s + b.toUInt32\ndef run (s : UInt32) (bs : List UInt8) := bs.foldl tick (s + 1)",
	} {
		t.Run(source, func(t *testing.T) { byteFoldRejects(t, source+"\n") })
	}
}
