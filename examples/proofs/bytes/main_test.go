package main

import (
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	codec "example.com/lean-go-bytes-demo/bytes"
)

func typedQuad(t *testing.T, q codec.ByteQuad) [4]byte {
	t.Helper()
	var out [4]byte
	for i, get := range []func() (uint8, bool){q.B0, q.B1, q.B2, q.B3} {
		value, ok := get()
		if !ok {
			t.Fatalf("invalid typed byte %d", i)
		}
		out[i] = value
	}
	return out
}

func importValue(t *testing.T, input *codec.Input) codec.Value {
	t.Helper()
	value, err := codec.Import(input, codec.DefaultLimits())
	if err == nil && value.Type() == input.Type && value.Constructor() == input.Constructor {
		return value
	}
	t.Fatalf("import %s/%s returned %s/%s, %v", input.Type, input.Constructor,
		value.Type(), value.Constructor(), err)
	return codec.Value{}
}

func quadInput(q [4]byte) *codec.Input {
	fields := make([]*codec.Input, 4)
	for i, value := range q {
		fields[i] = &codec.Input{Type: "UInt8", UInt8: value}
	}
	return &codec.Input{Type: "ByteQuad", Constructor: "ByteQuad.mk", Fields: fields}
}

func rawQuad(t *testing.T, q codec.Value) [4]byte {
	t.Helper()
	fields := q.Fields()
	_, scalar8 := q.UInt8()
	_, scalar32 := q.UInt32()
	_, scalar64 := q.UInt64()
	if q.Type() != "ByteQuad" || q.Constructor() != "ByteQuad.mk" || len(fields) != 4 || scalar8 || scalar32 || scalar64 {
		t.Fatalf("invalid raw ByteQuad: %q/%q with %d fields", q.Type(), q.Constructor(), len(fields))
	}
	var out [4]byte
	for i, field := range fields {
		value, ok := field.UInt8()
		_, wrong32 := field.UInt32()
		_, wrong64 := field.UInt64()
		if !ok || wrong32 || wrong64 || field.Type() != "UInt8" || field.Constructor() != "" || len(field.Fields()) != 0 {
			t.Fatalf("field %d is not an isolated UInt8", i)
		}
		out[i] = value
	}
	return out
}

func rawWord(t *testing.T, value codec.Value) uint32 {
	t.Helper()
	number, ok := value.UInt32()
	_, wrong8 := value.UInt8()
	_, wrong64 := value.UInt64()
	if !ok || wrong8 || wrong64 || value.Type() != "UInt32" {
		t.Fatalf("expected UInt32, got %q", value.Type())
	}
	return number
}

func runRaw(t *testing.T, root string, input codec.Value) codec.Value {
	t.Helper()
	value, err := codec.RunRoot("ByteCodec."+root, []codec.Value{input}, codec.DefaultLimits())
	wantType := "UInt32"
	if root == "encodeLE" || root == "encodeBE" {
		wantType = "ByteQuad"
	}
	if err != nil || value.Type() != wantType {
		t.Fatalf("%s returned %s, %v; want %s", root, value.Type(), err, wantType)
	}
	return value
}

func checkCodec(t *testing.T, word uint32, bytes [4]byte) {
	t.Helper()
	limits := codec.DefaultLimits()
	quad, err := codec.NewByteQuad(bytes[0], bytes[1], bytes[2], bytes[3], limits)
	if err != nil {
		t.Fatal(err)
	}
	rawInput := importValue(t, quadInput(bytes))
	rawScalar := importValue(t, &codec.Input{Type: "UInt32", UInt32: word})
	for _, order := range []struct {
		name     string
		model    binary.ByteOrder
		encode   func(uint32, codec.Limits) (codec.ByteQuad, error)
		decode   func(codec.ByteQuad, codec.Limits) (uint32, error)
		divisors [4]uint32
	}{
		{"LE", binary.LittleEndian, codec.RunEncodeLE, codec.RunDecodeLE, [4]uint32{1, 256, 65536, 16777216}},
		{"BE", binary.BigEndian, codec.RunEncodeBE, codec.RunDecodeBE, [4]uint32{16777216, 65536, 256, 1}},
	} {
		var expected [4]byte
		order.model.PutUint32(expected[:], word)
		encoded, ee := order.encode(word, limits)
		decoded, ed := order.decode(quad, limits)
		if err := errors.Join(ee, ed); err != nil {
			t.Fatal(err)
		}
		got := typedQuad(t, encoded)
		if got != expected || decoded != order.model.Uint32(bytes[:]) {
			t.Fatalf("%s typed roots: word=%#x bytes=%x encoded=%x decoded=%#x", order.name, word, bytes, got, decoded)
		}
		rawEncoded := runRaw(t, "encode"+order.name, rawScalar)
		rawDecoded := runRaw(t, "decode"+order.name, rawInput)
		rawBytes := rawQuad(t, rawEncoded)
		if rawBytes != expected || rawWord(t, rawDecoded) != order.model.Uint32(bytes[:]) {
			t.Fatalf("%s raw roots disagree with encoding/binary", order.name)
		}
		// All eight position laws use division/modulo, independently of runtime shifts and casts.
		for i, divisor := range order.divisors {
			position := uint32(word/divisor) % 256
			if uint32(got[i]) != position || uint32(rawBytes[i]) != position {
				t.Fatalf("%s byte %d of %#x differs from arithmetic position", order.name, i, word)
			}
		}
		// Both word and arbitrary-record inverses, in both byte orders and both APIs.
		roundWord, ew := order.decode(encoded, limits)
		roundQuad, eq := order.encode(decoded, limits)
		if err := errors.Join(ew, eq); err != nil || roundWord != word || typedQuad(t, roundQuad) != bytes {
			t.Fatalf("%s inverse laws: %v", order.name, err)
		}
		if rawWord(t, runRaw(t, "decode"+order.name, rawEncoded)) != word ||
			rawQuad(t, runRaw(t, "encode"+order.name, rawDecoded)) != bytes {
			t.Fatalf("%s raw inverse laws", order.name)
		}
	}
}

func TestByteCodecClaims(t *testing.T) {
	boundaries := []uint32{0, 1, 0xfe, 0xff, 0x100, 0x101, 0xffff, 0x10000, 0x10001,
		0xffffff, 0x1000000, 0x1000001, 0x7fffffff, 0x80000000, 0x80000001,
		0x12345678, 0x89abcdef, 0xaaaaaaaa, 0x55555555, 0xfffffffe, 0xffffffff}
	distinct := [][4]byte{{0x12, 0x34, 0x56, 0x78}, {0xff, 0, 0x80, 1}, {0, 1, 2, 3}, {255, 254, 253, 252}}
	for _, word := range boundaries {
		for _, q := range distinct {
			checkCodec(t, word, q)
		}
	}
	for bit := range 32 {
		var q [4]byte
		q[bit/8] = 1 << (bit % 8)
		checkCodec(t, uint32(1)<<bit, q)
	}
	for position := range 4 {
		for _, value := range []byte{0, 1, 127, 128, 254, 255} {
			var q [4]byte
			q[position] = value
			checkCodec(t, binary.LittleEndian.Uint32(q[:]), q)
		}
	}
	random := rand.New(rand.NewPCG(8, 3236))
	for range 512 {
		q := [4]byte{byte(random.Uint32()), byte(random.Uint32()), byte(random.Uint32()), byte(random.Uint32())}
		checkCodec(t, random.Uint32(), q)
	}
	t.Log("checked 12 laws in both APIs: 84 distinct-byte boundary cases, 32 one-hot cases, 24 byte-position boundaries and 512 seeded cases")
}

type operation struct {
	root                                               string
	input                                              codec.Value
	quad                                               codec.ByteQuad
	allocations, steps, slots, inputNodes, outputNodes uint64
}

func operations(t *testing.T) []operation {
	t.Helper()
	q := [4]byte{0x12, 0x34, 0x56, 0x78}
	quad, err := codec.NewByteQuad(q[0], q[1], q[2], q[3], codec.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	word := importValue(t, &codec.Input{Type: "UInt32", UInt32: 0x12345678})
	raw := importValue(t, quadInput(q))
	return []operation{
		{"ByteCodec.encodeLE", word, quad, 11, 12, 12, 1, 5},
		{"ByteCodec.encodeBE", word, quad, 11, 12, 12, 1, 5},
		{"ByteCodec.decodeLE", raw, quad, 13, 15, 18, 5, 1},
		{"ByteCodec.decodeBE", raw, quad, 13, 15, 18, 5, 1},
	}
}

func typedOperation(t *testing.T, op operation, limits codec.Limits, wantErr error) {
	t.Helper()
	var gotErr error
	switch op.root {
	case "ByteCodec.encodeLE", "ByteCodec.encodeBE":
		encode := codec.RunEncodeLE
		if op.root == "ByteCodec.encodeBE" {
			encode = codec.RunEncodeBE
		}
		got, err := encode(0x12345678, limits)
		gotErr = err
		if err != nil {
			if got != (codec.ByteQuad{}) {
				t.Fatal("typed failure returned a partial record")
			}
		} else {
			var want [4]byte
			order := binary.ByteOrder(binary.LittleEndian)
			if op.root == "ByteCodec.encodeBE" {
				order = binary.BigEndian
			}
			order.PutUint32(want[:], 0x12345678)
			if typedQuad(t, got) != want {
				t.Fatal("incorrect typed encoded bytes")
			}
		}
	default:
		decode := codec.RunDecodeLE
		order := binary.ByteOrder(binary.LittleEndian)
		if op.root == "ByteCodec.decodeBE" {
			decode, order = codec.RunDecodeBE, binary.BigEndian
		}
		got, err := decode(op.quad, limits)
		gotErr = err
		if err != nil && got != 0 {
			t.Fatal("typed failure returned a partial word")
		}
		q := typedQuad(t, op.quad)
		if err == nil && got != order.Uint32(q[:]) {
			t.Fatal("incorrect typed decoded word")
		}
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("typed %s: %v, want %v", op.root, gotErr, wantErr)
	}
}

func checkFailure(t *testing.T, op operation, limits codec.Limits, want error) {
	t.Helper()
	typedOperation(t, op, limits, want)
	args := []codec.Value{op.input}
	got, err := codec.RunRoot(op.root, args, limits)
	if !errors.Is(err, want) || got.Type() != "" || got.Constructor() != "" || len(got.Fields()) != 0 {
		t.Fatalf("raw %s returned a partial result or wrong error: %q %v", op.root, got.Type(), err)
	}
	if args[0] != op.input {
		t.Fatal("failed call replaced caller arguments")
	}
	if _, err := codec.RunRoot(op.root, args, codec.DefaultLimits()); err != nil {
		t.Fatalf("failure damaged owned inputs: %v", err)
	}
}

func TestByteCodecBudgets(t *testing.T) {
	for _, op := range operations(t) {
		t.Run(op.root, func(t *testing.T) {
			exact := codec.DefaultLimits()
			exact.MaxAllocations, exact.MaxSteps, exact.MaxSlots = op.allocations, op.steps, op.slots
			exact.MaxNodes, exact.MaxOutputNodes = op.inputNodes, op.outputNodes
			exact.MaxDepth, exact.MaxFrames = 2, 1
			typedOperation(t, op, exact, nil)
			actual, err := codec.RunRoot(op.root, []codec.Value{op.input}, exact)
			if err != nil {
				t.Fatalf("exact raw budgets: %v", err)
			}
			if op.input.Type() == "UInt32" {
				var want [4]byte
				order := binary.ByteOrder(binary.LittleEndian)
				if op.root == "ByteCodec.encodeBE" {
					order = binary.BigEndian
				}
				order.PutUint32(want[:], 0x12345678)
				if rawQuad(t, actual) != want {
					t.Fatal("exact budget changed raw encoding")
				}
			} else {
				order := binary.ByteOrder(binary.LittleEndian)
				if op.root == "ByteCodec.decodeBE" {
					order = binary.BigEndian
				}
				q := typedQuad(t, op.quad)
				if rawWord(t, actual) != order.Uint32(q[:]) {
					t.Fatal("exact budget changed raw decoding")
				}
			}
			for _, field := range []string{"allocations", "steps", "slots", "depth", "input", "output"} {
				limited := exact
				switch field {
				case "allocations":
					limited.MaxAllocations--
				case "steps":
					limited.MaxSteps--
				case "slots":
					limited.MaxSlots--
				case "depth":
					limited.MaxDepth--
				case "input":
					if limited.MaxNodes == 1 {
						continue
					}
					limited.MaxNodes--
				case "output":
					if limited.MaxOutputNodes == 1 {
						continue
					}
					limited.MaxOutputNodes--
				}
				t.Run(field, func(t *testing.T) { checkFailure(t, op, limited, codec.ErrResource) })
			}
			checkFailure(t, op, codec.Limits{}, codec.ErrLimits)
			invalid := codec.DefaultLimits()
			invalid.MaxSteps = 0
			checkFailure(t, op, invalid, codec.ErrLimits)
			invalid = codec.DefaultLimits()
			invalid.MaxNodes++
			checkFailure(t, op, invalid, codec.ErrLimits)
		})
	}
}

func TestByteCodecOwnedAndMalformedInputs(t *testing.T) {
	limits := codec.DefaultLimits()
	original := [4]byte{0x12, 0x34, 0x56, 0x78}
	input := quadInput(original)
	owned := importValue(t, input)
	input.Fields[0].UInt8 = 255
	input.Fields[1] = &codec.Input{Type: "UInt32", UInt32: 999}
	input.Type, input.Constructor = "wrong", "wrong"
	fields := owned.Fields()
	fields[0] = codec.Value{}
	if rawQuad(t, owned) != original || rawWord(t, runRaw(t, "decodeBE", owned)) != 0x12345678 {
		t.Fatal("caller mutation changed owned record")
	}
	scalarInput := &codec.Input{Type: "UInt32", UInt32: 0x12345678}
	scalar := importValue(t, scalarInput)
	scalarInput.UInt32 = 0
	if rawWord(t, scalar) != 0x12345678 {
		t.Fatal("caller mutation changed owned scalar")
	}
	for _, op := range operations(t) {
		wrong := owned
		if op.root == "ByteCodec.decodeLE" || op.root == "ByteCodec.decodeBE" {
			wrong = scalar
		}
		for _, args := range [][]codec.Value{nil, {op.input, op.input}, {wrong}, {{}}} {
			got, err := codec.RunRoot(op.root, args, limits)
			if !errors.Is(err, codec.ErrMalformed) || got.Type() != "" {
				t.Fatalf("malformed %s: %q %v", op.root, got.Type(), err)
			}
		}
	}
	if got, err := codec.RunRoot("ByteCodec.unknown", []codec.Value{scalar}, limits); !errors.Is(err, codec.ErrMalformed) || got.Type() != "" {
		t.Fatal("unknown root accepted")
	}
	for _, decode := range []func(codec.ByteQuad, codec.Limits) (uint32, error){codec.RunDecodeLE, codec.RunDecodeBE} {
		if got, err := decode(codec.ByteQuad{}, limits); got != 0 || !errors.Is(err, codec.ErrMalformed) {
			t.Fatal("invalid typed zero accepted")
		}
	}
	if _, ok := (codec.ByteQuad{}).B0(); ok {
		t.Fatal("invalid typed zero accessor accepted")
	}
	for _, mutation := range []string{"arity", "constructor", "width", "payload", "nil"} {
		bad := quadInput(original)
		switch mutation {
		case "arity":
			bad.Fields = bad.Fields[:3]
		case "constructor":
			bad.Constructor = "ByteQuad.wrong"
		case "width":
			bad.Fields[0] = &codec.Input{Type: "UInt32", UInt32: 18}
		case "payload":
			bad.Fields[0].UInt32 = 18
		case "nil":
			bad.Fields[0] = nil
		}
		if got, err := codec.Import(bad, limits); !errors.Is(err, codec.ErrMalformed) || got.Type() != "" {
			t.Fatalf("malformed %s record accepted: %v", mutation, err)
		}
	}
}

func TestGoOnlyEnvironment(t *testing.T) {
	if os.Getenv("LEAN_GO_DEMO_GO_ONLY") != "1" {
		t.Skip("producer tools are hidden by the repository verification gate")
	}
	for _, tool := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
		if _, err := exec.LookPath(tool); err == nil {
			t.Fatalf("producer tool available in Go-only environment: %s", tool)
		}
	}
}

func Example() {
	main()
	// Output:
	// LE(0x12345678) = [78 56 34 12] -> 0x12345678
	// BE(0x12345678) = [12 34 56 78] -> 0x12345678
}
