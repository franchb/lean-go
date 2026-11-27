package pipeline

import (
	"strings"
	"testing"
)

// Expected bytes were recorded from CPython's json module with the same options, which wrote the tracked artifacts of the retired Python pipeline.
func TestPythonCompatibleEncodings(t *testing.T) {
	input := "{\"b\":[1,{}],\"a\":\"x\\\"\\\\\\n\\t\\u0001\x7f é ✓  \",\"c\":[],\"big\":9007199254740993,\"n\":null,\"t\":true,\"z\":-0}"
	value, err := ParseJSON([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	compact, err := EncodeCompactSorted(value)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":\"x\\\"\\\\\\n\\t\\u0001\x7f é ✓  \",\"b\":[1,{}],\"big\":9007199254740993,\"c\":[],\"n\":null,\"t\":true,\"z\":0}"
	if string(compact) != want {
		t.Fatalf("compact:\n%s\nwant\n%s", compact, want)
	}
	indented, err := EncodeIndentedSorted(value)
	if err != nil {
		t.Fatal(err)
	}
	wantIndented := strings.Join([]string{"{",
		"  \"a\": \"x\\\"\\\\\\n\\t\\u0001\x7f é ✓  \",",
		"  \"b\": [", "    1,", "    {}", "  ],",
		"  \"big\": 9007199254740993,", "  \"c\": [],", "  \"n\": null,", "  \"t\": true,", "  \"z\": 0", "}"}, "\n")
	if string(indented) != wantIndented {
		t.Fatalf("indented:\n%s\nwant\n%s", indented, wantIndented)
	}
	ordered, err := EncodeCompactOrdered(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(ordered), `{"b":[1,{}],"a":`) {
		t.Fatalf("ordered encoding lost document order: %s", ordered)
	}
}

func TestStrictDecoding(t *testing.T) {
	for _, bad := range []string{`{"a":1,"a":2}`, `{`, `NaN`, `[Infinity]`, `{} {}`, "\"\xff\"", `[1.5]`} {
		value, err := ParseJSON([]byte(bad))
		if err == nil {
			if _, err = EncodeCompactSorted(value); err == nil {
				t.Errorf("%q accepted", bad)
			}
		}
	}
}

func TestPythonRepr(t *testing.T) {
	for input, want := range map[string]string{
		"a'b":                               `"a'b"`,
		`a"b'c`:                             `'a"b\'c'`,
		"x\x01\u0085é​\U0001F600\U000E0001": `'x\x01\x85é` + `\` + `u200b😀\U000e0001'`,
		"plain":                             `'plain'`,
	} {
		if got := pythonRepr(input); got != want {
			t.Errorf("repr(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestInstrumentationAndPositions(t *testing.T) {
	source := []byte("module\r\n/- nested /- comment -/ -/ public import Helper -- trailing\npublic def run (x : UInt32) : UInt32 := x -- é\n")
	mapping, imports, err := instrumentSource(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 1 || imports[0] != "Helper" {
		t.Fatalf("imports %v", imports)
	}
	if !strings.Contains(string(mapping.Instrumented), "public import Helper\npublic meta import LeanGo.Module.Audit\n") {
		t.Fatalf("instrumented:\n%s", mapping.Instrumented)
	}
	// Expected values were recorded from the retired scripts/source_locations.py.
	for _, c := range []struct {
		line, column int64
		want         string
	}{
		{5, 1, `{"line":2,"column":48,"utf16_column":48,"byte_offset":56}`},
		{6, 11, `{"line":3,"column":11,"utf16_column":11,"byte_offset":79}`},
		{6, 46, `{"line":3,"column":46,"utf16_column":46,"byte_offset":115}`},
		{2, 5, `{"line":2,"column":5,"utf16_column":5,"byte_offset":13}`},
	} {
		original := mapping.Position(c.line, c.column)
		if original == nil {
			t.Fatalf("%d:%d was not mapped", c.line, c.column)
		}
		data, _ := EncodeCompactOrdered(original)
		if string(data) != c.want {
			t.Fatalf("%d:%d mapped to %s, want %s", c.line, c.column, data, c.want)
		}
	}
	if mapping.Position(3, 0) != nil || mapping.Position(99, 0) != nil {
		t.Fatal("a position inside the insertion or past the end must not map")
	}
	for _, bad := range []string{"/- open", "import", "import 3x"} {
		if _, _, err := instrumentSource([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, _, err := instrumentSource([]byte("\xff")); err == nil {
		t.Error("invalid UTF-8 accepted")
	}
}

func TestRootsAndNames(t *testing.T) {
	roots, err := SelectRoots([]string{"B.z", "A.a"})
	if err != nil || strings.Join(roots, ",") != "A.a,B.z" {
		t.Fatalf("roots %v %v", roots, err)
	}
	for _, bad := range [][]string{nil, {"A", "A"}, {"A b"}, {"é"}} {
		if _, err := SelectRoots(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateName("x'y"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateName("a b"); err == nil || err.Error() != "invalid ASCII Lean name: 'a b'" {
		t.Fatalf("message %v", err)
	}
}

func TestDiagnosticText(t *testing.T) {
	var b strings.Builder
	EmitDiagnostics(&b, exportError("--source-dir, --module and --root must be supplied together"), "text")
	if b.String() != "input [invalid_input]: --source-dir, --module and --root must be supplied together\n" {
		t.Fatalf("text %q", b.String())
	}
	b.Reset()
	EmitDiagnostics(&b, staged("toolchain", "toolchain_mismatch", exportError("bad")), "json")
	want := `{"schema":"lean-go-diagnostics/v1","success":false,"diagnostics":[{"code":"toolchain_mismatch","stage":"toolchain","message":"bad","declaration":null,"compiler_form":null,"location":null,"hint":null}]}` + "\n"
	if b.String() != want {
		t.Fatalf("json %s", b.String())
	}
	if !ValidReport([]byte(want)) {
		t.Fatal("own report is invalid")
	}
	if got := RequestedMode([]string{"--diagnostics=json", "--diagnostics", "text"}); got != "text" {
		t.Fatal(got)
	}
}
