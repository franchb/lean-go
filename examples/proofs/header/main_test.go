package main

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"testing"

	codec "example.com/lean-go-header-demo/header"
)

func rawRecord(t *testing.T, name string, fields ...*codec.Input) codec.Value {
	t.Helper()
	value, err := codec.Import(&codec.Input{Type: name, Constructor: name + ".mk", Fields: fields}, codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func rawWire(t *testing.T, wire [4]byte) codec.Value {
	t.Helper()
	fields := make([]*codec.Input, len(wire))
	for i, b := range wire {
		fields[i] = &codec.Input{Type: "UInt8", UInt8: b}
	}
	return rawRecord(t, "ByteQuad", fields...)
}

func rawHeader(t *testing.T, flags byte, length uint32) codec.Value {
	t.Helper()
	return rawRecord(t, "Header", &codec.Input{Type: "UInt8", UInt8: flags},
		&codec.Input{Type: "UInt32", UInt32: length})
}

func runRaw(t *testing.T, root string, arg codec.Value) codec.Value {
	t.Helper()
	got, err := codec.RunRoot("HeaderCodec."+root, []codec.Value{arg}, codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func fieldsOf(t *testing.T, v codec.Value, ctor string, count int) []codec.Value {
	t.Helper()
	fields := v.Fields()
	if v.Constructor() != ctor || len(fields) != count {
		t.Fatalf("got %q with %d fields, want %q with %d", v.Constructor(), len(fields), ctor, count)
	}
	return fields
}

func checkParse(t *testing.T, wire [4]byte) {
	t.Helper()
	q, err := codec.NewByteQuadFromBytes(wire, codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := codec.RunParse(q, codec.DefaultLimits())
	if err != nil {
		t.Fatalf("parse %x: %v", wire, err)
	}
	raw := runRaw(t, "parse", rawWire(t, wire))
	switch {
	case wire[0] != 1:
		if !parsed.IsUnsupportedVersion() {
			t.Fatalf("parse %x: expected unsupported version", wire)
		}
		fieldsOf(t, raw, "ParseResult.unsupportedVersion", 0)
	case wire[1]&0xfc != 0:
		if !parsed.IsReservedFlags() {
			t.Fatalf("parse %x: expected reserved flags", wire)
		}
		fieldsOf(t, raw, "ParseResult.reservedFlags", 0)
	default:
		h, ok := parsed.AsParsed()
		flags, okFlags := h.Flags()
		length, okLength := h.Length()
		expected := uint32(binary.BigEndian.Uint16(wire[2:]))
		if !ok || !okFlags || !okLength || flags != wire[1] || length != expected {
			t.Fatalf("parse %x: got flags=%d length=%d", wire, flags, length)
		}
		header := fieldsOf(t, raw, "ParseResult.parsed", 1)[0]
		fields := fieldsOf(t, header, "Header.mk", 2)
		rawFlags, f := fields[0].UInt8()
		rawLength, l := fields[1].UInt32()
		if !f || !l || rawFlags != flags || rawLength != expected {
			t.Fatalf("raw parse %x: got flags=%d length=%d", wire, rawFlags, rawLength)
		}
	}
}

func checkEncode(t *testing.T, flags byte, length uint32) {
	t.Helper()
	h, err := codec.NewHeader(flags, length, codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.RunEncode(h, codec.DefaultLimits())
	if err != nil {
		t.Fatalf("encode flags=%d length=%d: %v", flags, length, err)
	}
	raw := runRaw(t, "encode", rawHeader(t, flags, length))
	switch {
	case flags&0xfc != 0:
		if !encoded.IsReservedFlags() {
			t.Fatalf("encode flags=%d length=%d: expected reserved flags", flags, length)
		}
		fieldsOf(t, raw, "EncodeResult.reservedFlags", 0)
	case length > 0xffff:
		if !encoded.IsLengthOverflow() {
			t.Fatalf("encode length=%d: expected length overflow", length)
		}
		fieldsOf(t, raw, "EncodeResult.lengthOverflow", 0)
	default:
		expected := [4]byte{1, flags}
		binary.BigEndian.PutUint16(expected[2:], uint16(length))
		q, ok := encoded.AsEncoded()
		wire, err := q.Bytes()
		if !ok || err != nil || wire != expected {
			t.Fatalf("encode flags=%d length=%d: got %x, %v", flags, length, wire, err)
		}
		quad := fieldsOf(t, raw, "EncodeResult.encoded", 1)[0]
		fields := fieldsOf(t, quad, "ByteQuad.mk", 4)
		for i, field := range fields {
			b, ok := field.UInt8()
			if !ok || b != expected[i] {
				t.Fatalf("raw encode flags=%d length=%d: byte %d is %d", flags, length, i, b)
			}
		}
	}
}

func TestHeaderCodec(t *testing.T) {
	for i := range 256 {
		checkParse(t, [4]byte{byte(i), 0, 0xab, 0xcd})
		checkParse(t, [4]byte{byte(i), 0xff, 0xab, 0xcd})
		checkParse(t, [4]byte{1, byte(i), 0x34, 0x12})
		checkEncode(t, byte(i), 0)
		checkEncode(t, byte(i), 0x10000)
	}
	// Every valid header has one of four flag values and a sixteen-bit length.
	for flags := range byte(4) {
		for n := range 1 << 16 {
			wire := [4]byte{1, flags}
			binary.BigEndian.PutUint16(wire[2:], uint16(n))
			checkParse(t, wire)
			checkEncode(t, flags, uint32(n))
		}
		for _, n := range []uint32{0x10000, 0x10001, 0xffffffff} {
			checkEncode(t, flags, n)
		}
	}
}

func TestRecordAPI(t *testing.T) {
	wire := [4]byte{1, 3, 0x12, 0x34}
	q, err := codec.NewByteQuad(wire[0], wire[1], wire[2], wire[3], codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for i, field := range []func() (uint8, bool){q.B0, q.B1, q.B2, q.B3} {
		val, ok := field()
		if !ok || val != wire[i] {
			t.Fatalf("record field %d: got %d, %v", i, val, ok)
		}
	}
	got, err := q.Bytes()
	if err != nil || got != wire {
		t.Fatalf("array view of record: %x, %v", got, err)
	}
}

func TestMalformedAndLimitedCalls(t *testing.T) {
	limits := codec.DefaultLimits()
	if got, err := (codec.ByteQuad{}).Bytes(); !errors.Is(err, codec.ErrMalformed) || got != [4]byte{} {
		t.Fatalf("invalid array view: %x, %v", got, err)
	}
	if got, err := codec.RunParse(codec.ByteQuad{}, limits); !errors.Is(err, codec.ErrMalformed) || got != (codec.ParseResult{}) {
		t.Fatalf("invalid typed parse: %v", err)
	}
	if got, err := codec.RunEncode(codec.Header{}, limits); !errors.Is(err, codec.ErrMalformed) || got != (codec.EncodeResult{}) {
		t.Fatalf("invalid typed encode: %v", err)
	}
	wire := [4]byte{1, 3, 0xff, 0xff}
	q, err := codec.NewByteQuadFromBytes(wire, limits)
	if err != nil {
		t.Fatal(err)
	}
	h, err := codec.NewHeader(3, 65535, limits)
	if err != nil {
		t.Fatal(err)
	}
	rawQ := rawWire(t, wire)
	rawH := rawHeader(t, 3, 65535)
	for _, call := range []struct {
		root  string
		wrong codec.Value
	}{{"HeaderCodec.parse", rawH}, {"HeaderCodec.encode", rawQ}} {
		for _, args := range [][]codec.Value{nil, {call.wrong}, {{}}} {
			got, err := codec.RunRoot(call.root, args, limits)
			if !errors.Is(err, codec.ErrMalformed) || got.Type() != "" {
				t.Fatalf("malformed raw %s: %q, %v", call.root, got.Type(), err)
			}
		}
	}
	limited := limits
	limited.MaxSteps = 1
	for _, failure := range []struct {
		limits codec.Limits
		want   error
	}{{limited, codec.ErrResource}, {codec.Limits{}, codec.ErrLimits}} {
		if got, err := codec.RunParse(q, failure.limits); !errors.Is(err, failure.want) || got != (codec.ParseResult{}) {
			t.Fatalf("limited parse: %v, want %v", err, failure.want)
		}
		if got, err := codec.RunEncode(h, failure.limits); !errors.Is(err, failure.want) || got != (codec.EncodeResult{}) {
			t.Fatalf("limited encode: %v, want %v", err, failure.want)
		}
		for root, arg := range map[string]codec.Value{"HeaderCodec.parse": rawQ, "HeaderCodec.encode": rawH} {
			got, err := codec.RunRoot(root, []codec.Value{arg}, failure.limits)
			if !errors.Is(err, failure.want) || got.Type() != "" {
				t.Fatalf("limited raw %s: %q, %v, want %v", root, got.Type(), err, failure.want)
			}
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
	// [01 03 12 34] -> flags=3 length=4660 -> [01 03 12 34]
	// [02 80 00 00] -> unsupported version
	// [01 04 00 00] -> reserved flags
}
