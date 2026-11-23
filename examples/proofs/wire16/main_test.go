package main

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"testing"

	codec "example.com/lean-go-wire16-demo/wire16"
)

func importRaw(t *testing.T, input *codec.Input) codec.Value {
	t.Helper()
	v, err := codec.Import(input, codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func rawByte(b byte) *codec.Input { return &codec.Input{Type: "UInt8", UInt8: b} }

func rawWord(n uint16) *codec.Input { return &codec.Input{Type: "UInt16", UInt16: n} }

func record(name string, fields ...*codec.Input) *codec.Input {
	return &codec.Input{Type: name, Constructor: name + ".mk", Fields: fields}
}

func runRaw(t *testing.T, name string, input *codec.Input) codec.Value {
	t.Helper()
	arg := importRaw(t, input)
	v, err := codec.RunRoot("Wire16."+name, []codec.Value{arg}, codec.DefaultLimits())
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

func fieldsOf(t *testing.T, value codec.Value, ctor string, count int) []codec.Value {
	t.Helper()
	fields := value.Fields()
	if value.Constructor() != ctor || len(fields) != count {
		t.Fatalf("got %q with %d fields, want %q with %d", value.Constructor(), len(fields), ctor, count)
	}
	return fields
}

func checkBytes(t *testing.T, fields []codec.Value, want []byte) {
	t.Helper()
	for i, field := range fields {
		b, ok := field.UInt8()
		if !ok || b != want[i] {
			t.Fatalf("raw byte %d: got %d, %v, want %d", i, b, ok, want[i])
		}
	}
}

func encodeWord(t *testing.T, n uint16) [2]byte {
	t.Helper()
	p, err := codec.RunEncodeBE(n, codec.DefaultLimits())
	if err != nil {
		t.Fatalf("encodeBE(%04x): %v", n, err)
	}
	hi, okHi := p.Hi()
	lo, okLo := p.Lo()
	if !okHi || !okLo {
		t.Fatalf("encodeBE(%04x) returned invalid fields", n)
	}
	return [2]byte{hi, lo}
}

func decodePair(t *testing.T, p [2]byte) uint16 {
	t.Helper()
	l := codec.DefaultLimits()
	input, err := codec.NewBytePair(p[0], p[1], l)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.RunDecodeBE(input, l)
	if err != nil {
		t.Fatalf("decodeBE(%x): %v", p, err)
	}
	return got
}

func TestBigEndianCodec(t *testing.T) {
	for i := range 1 << 16 {
		n := uint16(i)
		var wire [2]byte
		binary.BigEndian.PutUint16(wire[:], n)
		if got := encodeWord(t, n); got != wire {
			t.Fatalf("encodeBE(%04x): got %x, want %x", n, got, wire)
		}
		want := binary.BigEndian.Uint16(wire[:])
		if got := decodePair(t, wire); got != want {
			t.Fatalf("decodeBE(%x): got %04x, want %04x", wire, got, want)
		}
		pair := runRaw(t, "encodeBE", rawWord(n))
		checkBytes(t, fieldsOf(t, pair, "BytePair.mk", 2), wire[:])
		word := runRaw(t, "decodeBE", record("BytePair", rawByte(wire[0]), rawByte(wire[1])))
		if got, ok := word.UInt16(); !ok || got != want {
			t.Fatalf("raw decodeBE(%x): got %04x, %v, want %04x", wire, got, ok, want)
		}
	}
}

func TestNativeLeanCodec(t *testing.T) {
	path := os.Getenv("LEAN_GO_WIRE16_NATIVE")
	if path == "" {
		t.Skip("set LEAN_GO_WIRE16_NATIVE to a native Lean oracle file")
	}
	oracle, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const words = 1 << 16
	if len(oracle) != 4*words {
		t.Fatalf("native Lean oracle has %d bytes, want %d", len(oracle), 4*words)
	}
	for i := range words {
		sample := oracle[4*i : 4*i+4]
		n := uint16(i)
		if got, want := encodeWord(t, n), [2]byte(sample[:2]); got != want {
			t.Fatalf("encodeBE(%04x): Go %x, native Lean %x", n, got, want)
		}
		p := [2]byte{byte(n >> 8), byte(n)}
		if got, want := decodePair(t, p), binary.BigEndian.Uint16(sample[2:]); got != want {
			t.Fatalf("decodeBE(%x): Go %04x, native Lean %04x", p, got, want)
		}
	}
}

func checkParse(t *testing.T, wire [4]byte) {
	t.Helper()
	l := codec.DefaultLimits()
	q, err := codec.NewByteQuadFromBytes(wire, l)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := codec.RunParse(q, l)
	if err != nil {
		t.Fatalf("parse(%x): %v", wire, err)
	}
	raw := runRaw(t, "parse", record("ByteQuad", rawByte(wire[0]), rawByte(wire[1]), rawByte(wire[2]), rawByte(wire[3])))
	switch {
	case wire[0] != 1:
		if !parsed.IsUnsupportedVersion() {
			t.Fatalf("parse(%x): expected unsupported version", wire)
		}
		fieldsOf(t, raw, "ParseResult.unsupportedVersion", 0)
	case wire[1]&0xfc != 0:
		if !parsed.IsReservedFlags() {
			t.Fatalf("parse(%x): expected reserved flags", wire)
		}
		fieldsOf(t, raw, "ParseResult.reservedFlags", 0)
	default:
		h, ok := parsed.AsParsed()
		flags, okFlags := h.Flags()
		length, okLength := h.Length()
		want := binary.BigEndian.Uint16(wire[2:])
		if !ok || !okFlags || !okLength || flags != wire[1] || length != want {
			t.Fatalf("parse(%x): got flags=%d length=%d", wire, flags, length)
		}
		header := fieldsOf(t, raw, "ParseResult.parsed", 1)[0]
		fields := fieldsOf(t, header, "Header.mk", 2)
		checkBytes(t, fields[:1], wire[1:2])
		if n, ok := fields[1].UInt16(); !ok || n != want {
			t.Fatalf("raw parse(%x): got length=%d, %v", wire, n, ok)
		}
	}
}

func checkEncode(t *testing.T, flags byte, length uint16) {
	t.Helper()
	l := codec.DefaultLimits()
	h, err := codec.NewHeader(flags, length, l)
	if err != nil {
		t.Fatal(err)
	}
	result, err := codec.RunEncode(h, l)
	if err != nil {
		t.Fatalf("encode(%d,%d): %v", flags, length, err)
	}
	raw := runRaw(t, "encode", record("Header", rawByte(flags), rawWord(length)))
	if flags&0xfc != 0 {
		if !result.IsReservedFlags() {
			t.Fatalf("encode(%d,%d): expected reserved flags", flags, length)
		}
		fieldsOf(t, raw, "EncodeResult.reservedFlags", 0)
		return
	}
	want := [4]byte{1, flags}
	binary.BigEndian.PutUint16(want[2:], length)
	q, ok := result.AsEncoded()
	got, err := q.Bytes()
	if !ok || err != nil || got != want {
		t.Fatalf("encode(%d,%d): got %x, %v, want %x", flags, length, got, err, want)
	}
	quad := fieldsOf(t, raw, "EncodeResult.encoded", 1)[0]
	checkBytes(t, fieldsOf(t, quad, "ByteQuad.mk", 4), want[:])
}

func TestHeader(t *testing.T) {
	for b := range 256 {
		checkParse(t, [4]byte{byte(b), 0, 0xab, 0xcd})
		checkParse(t, [4]byte{byte(b), 0xff, 0xab, 0xcd})
		checkParse(t, [4]byte{1, byte(b), 0x12, 0x34})
		checkEncode(t, byte(b), 0xffff)
	}
	for flag := range byte(4) {
		for i := range 1 << 16 {
			n := uint16(i)
			wire := [4]byte{1, flag}
			binary.BigEndian.PutUint16(wire[2:], n)
			checkParse(t, wire)
			checkEncode(t, flag, n)
		}
	}
}

func TestMalformedAndLimitedCalls(t *testing.T) {
	l := codec.DefaultLimits()
	if got, err := codec.RunDecodeBE(codec.BytePair{}, l); !errors.Is(err, codec.ErrMalformed) || got != 0 {
		t.Fatalf("decodeBE with invalid pair: %d, %v", got, err)
	}
	if got, err := codec.RunParse(codec.ByteQuad{}, l); !errors.Is(err, codec.ErrMalformed) || got != (codec.ParseResult{}) {
		t.Fatalf("parse with invalid quad: %v, %v", got, err)
	}
	if got, err := codec.RunEncode(codec.Header{}, l); !errors.Is(err, codec.ErrMalformed) || got != (codec.EncodeResult{}) {
		t.Fatalf("encode with invalid header: %v, %v", got, err)
	}
	for _, input := range []*codec.Input{
		{Type: "UInt16", UInt8: 1}, {Type: "UInt16", UInt32: 1}, {Type: "UInt16", UInt64: 1},
		{Type: "UInt8", UInt16: 1}, {Type: "UInt32", UInt16: 1}, {Type: "UInt64", UInt16: 1},
		record("Header", rawByte(3), &codec.Input{Type: "UInt32", UInt32: 1}),
		record("BytePair", rawByte(1), rawWord(2)),
	} {
		if got, err := codec.Import(input, l); !errors.Is(err, codec.ErrMalformed) || got.Type() != "" {
			t.Fatalf("invalid %s input: %q, %v", input.Type, got.Type(), err)
		}
	}
	arg := importRaw(t, rawWord(0x1234))
	for _, root := range []string{"Wire16.decodeBE", "Wire16.encode", "Wire16.parse"} {
		got, err := codec.RunRoot(root, []codec.Value{arg}, l)
		if !errors.Is(err, codec.ErrMalformed) || got.Type() != "" {
			t.Fatalf("%s with wrong argument type: %q, %v", root, got.Type(), err)
		}
	}
	tiny := l
	tiny.MaxSteps = 1
	for _, tc := range []struct {
		limits codec.Limits
		want   error
	}{{tiny, codec.ErrResource}, {codec.Limits{}, codec.ErrLimits}} {
		if got, err := codec.RunEncodeBE(0x1234, tc.limits); !errors.Is(err, tc.want) || got != (codec.BytePair{}) {
			t.Fatalf("limited encodeBE: %v, %v, want %v", got, err, tc.want)
		}
		got, err := codec.RunRoot("Wire16.encodeBE", []codec.Value{arg}, tc.limits)
		if !errors.Is(err, tc.want) || got.Type() != "" {
			t.Fatalf("limited raw encodeBE: %q, %v, want %v", got.Type(), err, tc.want)
		}
	}
}

func TestGoOnlyEnvironment(t *testing.T) {
	if os.Getenv("LEAN_GO_DEMO_GO_ONLY") != "1" {
		t.Skip("producer tools are hidden by the repository verification gate")
	}
	for _, name := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
		if _, err := exec.LookPath(name); err == nil {
			t.Fatalf("producer tool available in Go-only environment: %s", name)
		}
	}
}

func Example() {
	main()
	// Output:
	// 0xabcd -> [ab cd] -> 0xabcd
	// [01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]
	// [02 80 00 00] -> unsupported version
	// [01 04 00 00] -> reserved flags
}
