package wireformat

import (
	"fmt"
	"strings"
	"testing"
)

func build(t *testing.T, doc string) (*Format, error) {
	t.Helper()
	spec, err := DecodeSpec([]byte(doc))
	if err != nil {
		return nil, err
	}
	return Build(spec)
}

func TestSpecErrors(t *testing.T) {
	field := func(body string) string {
		return `{"name": "demo", "fields": [` + body + `]}`
	}
	for _, tc := range []struct {
		doc, want string
	}{
		{`{"name": "demo", "fields": [{"name": "a", "type": "uint8"}], "extra": 1}`, `unknown field "extra"`},
		{`{"name": "demo", "name": "y", "fields": []}`, `duplicate key "name"`},
		{field(`{"name": "a", "type": "uint8", "type": "uint16"}`), `duplicate key "type"`},
		{field(`{"name": "a", "type": "uint8"}`) + ` {}`, "unexpected data"},
		{`{"name": "X", "fields": [{"name": "a", "type": "uint8"}]}`, "lower-case Go package name"},
		{`{"name": "demo", "module": "lower", "fields": [{"name": "a", "type": "uint8"}]}`, "Lean module name"},
		{`{"name": "demo", "fields": []}`, "at least one field"},
		{field(`{"name": "a", "type": "uint12"}`), "must be uint8, uint16, uint32 or uint64"},
		{field(`{"name": "a", "type": "uint8", "endian": "little"}`), "meaningless for uint8"},
		{field(`{"name": "a", "type": "uint16", "endian": "middle"}`), `"big" or "little"`},
		{field(`{"name": "a", "type": "uint8"}, {"name": "a", "type": "uint8"}`), `duplicate name "a"`},
		{field(`{"name": "aB", "type": "uint8"}, {"name": "ab", "type": "uint8"}`), "differs only in case"},
		{field(`{"name": "end", "type": "uint8"}`), "reserved"},
		{field(`{"name": "b7", "type": "uint8"}`), "reserved"},
		{field(`{"name": "Upper", "type": "uint8"}`), "lower-case letter"},
		{field(`{"name": "a_b", "type": "uint8"}`), "only letters and digits"},
		{field(`{"name": "a", "type": "uint8", "const": 256}`), "does not fit in 8 bits"},
		{field(`{"name": "a", "type": "uint8", "const": 1, "min": 1}`), "cannot be combined"},
		{field(`{"name": "a", "type": "uint8", "reserved": true, "enum": [0]}`), "cannot be combined"},
		{field(`{"name": "a", "type": "uint8", "enum": []}`), "at least one value"},
		{field(`{"name": "a", "type": "uint8", "enum": [1, 1]}`), "listed twice"},
		{field(`{"name": "a", "type": "uint8", "enum": [300]}`), "does not fit"},
		{field(`{"name": "a", "type": "uint8", "min": 5, "max": 4}`), "no value is accepted"},
		{field(`{"name": "a", "type": "uint8", "enum": [1, 2], "min": 3}`), "no enum value lies within"},
		{field(`{"name": "a", "type": "uint8", "min": 0}`), "always satisfied"},
		{field(`{"name": "a", "type": "uint8", "max": 255}`), "always satisfied"},
		{field(`{"name": "a", "type": "uint8", "min": -1}`), "non-negative integer"},
		{field(`{"name": "a", "type": "uint8", "min": 1.5}`), "non-negative integer"},
		{field(`{"name": "a", "type": "uint64", "max": 18446744073709551616}`), "out of range"},
		{field(`{"name": "a", "type": "uint8", "const": "0xg"}`), "invalid number"},
		{field(`{"bitfield": "uint8", "bits": [{"name": "a", "width": 4}, {"name": "b", "width": 3}]}`), "bits 0..0 of the 8-bit word are not covered"},
		{field(`{"bitfield": "uint8", "bits": [{"name": "a", "width": 4}, {"name": "b", "width": 5}]}`), "need 9 bits"},
		{field(`{"bitfield": "uint8", "bits": [{"name": "a", "width": 0}, {"name": "b", "width": 8}]}`), "width 0 must be between 1 and 8"},
		{field(`{"bitfield": "uint16", "bits": [{"name": "a", "width": 8, "lsb": 4}, {"name": "b", "width": 8, "lsb": 0}]}`), "overlaps"},
		{field(`{"bitfield": "uint16", "bits": [{"name": "a", "width": 4, "lsb": 12}, {"name": "b", "width": 8, "lsb": 0}]}`), "bits 8..11 of the 16-bit word are not covered"},
		{field(`{"bitfield": "uint16", "bits": [{"name": "a", "width": 8, "lsb": 12}, {"name": "b", "width": 8, "lsb": 0}]}`), "outside the 16-bit word"},
		{field(`{"bitfield": "uint16", "bits": [{"name": "a", "width": 8, "lsb": 8}, {"name": "b", "width": 8}]}`), `"lsb" for every bit-field`},
		{field(`{"bitfield": "uint64", "bits": [{"name": "a", "width": 64}]}`), "must be uint8, uint16 or uint32"},
		{field(`{"bitfield": "uint8", "bits": [{"name": "a", "width": 8}]}`), "single member"},
		{field(`{"bitfield": "uint8", "name": "w", "bits": [{"name": "a", "width": 8}]}`), `takes only "bitfield" and "bits"`},
		{field(`{"name": "a", "type": "uint8", "bitfield": "uint8"}`), "not both"},
		{field(`{"name": "a"}`), `needs "type"`},
		{field(`{"name": "a", "type": "uint8", "reserved": true}`), "neither constant nor reserved"},
		{field(`{"name": "a", "type": "uint8", "const": 1, "payload_length": {}}`), "cannot be constant"},
		{field(`{"name": "a", "type": "uint64", "payload_length": {}}`), "at most 32 bits"},
		{field(`{"name": "a", "type": "uint8", "payload_length": {}}, {"name": "b", "type": "uint8", "payload_length": {}}`), "only one payload_length"},
		{field(`{"name": "a", "type": "uint8", "max": 1, "payload_length": {"includes_header": true}}, {"name": "b", "type": "uint8"}`), "below the header size 2"},
		{field(`{"name": "a", "type": "uint8", "payload_length": {"includes": true}}`), `unknown field "includes"`},
	} {
		_, err := build(t, tc.doc)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s:\n got %v\nwant error containing %q", tc.doc, err, tc.want)
		}
	}
}

func TestSpecLimits(t *testing.T) {
	var fields []string
	for i := range 65 {
		fields = append(fields, fmt.Sprintf(`{"name": "f%d", "type": "uint8"}`, i))
	}
	if _, err := build(t, `{"name": "demo", "fields": [`+strings.Join(fields, ",")+`]}`); err == nil ||
		!strings.Contains(err.Error(), "at most 64 are supported") {
		t.Fatalf("65-byte header: %v", err)
	}
	fields = fields[:0]
	for i := range 32 {
		fields = append(fields, fmt.Sprintf(`{"name": "f%d", "type": "uint8", "min": 1}`, i))
	}
	if _, err := build(t, `{"name": "demo", "fields": [`+strings.Join(fields, ",")+`]}`); err == nil ||
		!strings.Contains(err.Error(), "proof contract holds at most 64") {
		t.Fatalf("too many claims: %v", err)
	}
}

func TestLayout(t *testing.T) {
	f, err := Load("testdata/mix.json")
	if err != nil {
		t.Fatal(err)
	}
	if f.Size != 41 || len(f.Fields) != 15 || f.Module != "MixHeader" || f.HeaderBytes != 41 {
		t.Fatalf("size %d fields %d module %s header bytes %d", f.Size, len(f.Fields), f.Module, f.HeaderBytes)
	}
	total := f.Payload
	if total.Name != "total" || *total.Min != 41 || *total.Max != 9000 || total.Word.Order[0] != 7 {
		t.Fatalf("payload field %+v", total)
	}
	// Explicit LSB positions are sorted from the most significant member.
	var names []string
	for _, s := range f.Words[5].Subs {
		names = append(names, fmt.Sprintf("%s@%d", s.Name, s.Shift))
	}
	if got := strings.Join(names, " "); got != "ecn@30 dscp@24 label@4 unused@0" {
		t.Fatalf("word 5 layout %s", got)
	}
	var reasons []string
	for _, c := range f.Parse {
		reasons = append(reasons, c.Reason)
	}
	want := "magicMismatch versionMismatch kindNotAllowed padNonzero totalTooSmall totalTooLarge fragmentTooLarge " +
		"dscpTooSmall unusedNonzero seqTooSmall stampTooLarge cookieNotAllowed tagTooSmall tagTooLarge"
	if got := strings.Join(reasons, " "); got != want {
		t.Fatalf("parse checks:\n got %s\nwant %s", got, want)
	}
	reasons = reasons[:0]
	for _, c := range f.Valid {
		reasons = append(reasons, c.Reason)
	}
	want = "kindNotAllowed urgentTooWide totalTooSmall totalTooLarge flagsHiTooWide fragmentTooLarge ecnTooWide " +
		"dscpTooWide dscpTooSmall labelTooWide bigTooWide lastTooWide seqTooSmall stampTooLarge cookieNotAllowed tagTooSmall tagTooLarge"
	if got := strings.Join(reasons, " "); got != want {
		t.Fatalf("validation checks:\n got %s\nwant %s", got, want)
	}
}
