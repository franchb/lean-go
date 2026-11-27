package wireformat

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/modulegen"
	"github.com/franchb/lean-go/internal/moduleir"
	"github.com/franchb/lean-go/internal/pipeline"
)

// The native-backed adapter agrees with the previous metered-backed adapter.
//
// For each example the test builds one temporary module holding three
// adapters: the one the generator renders today over the tracked IR emitted
// in native mode, and two copies of the same adapter with the previous
// backend (backendMetered). The oracle copy runs on the IR emitted by
// modulegen.GenerateStateMachine, the metered output without the fast path:
// the fast path runs the native emitter's own code, so only the state
// machine is independent of the native side. The other copy runs on the
// default metered output (--mode metered, fast path included). A generated
// test compares both with the native adapter on Parse, Validate,
// AppendSerialize and, for payload formats, PayloadLength and AppendFrame on
// boundary and random inputs. No Lean is needed: the IR is tracked.
//
// LEAN_GO_FORMATS_FUZZ=30s also fuzzes the comparison, and
// LEAN_GO_FORMATS_BENCH=1 runs the benchmarks of every example (native-backed,
// default metered, metered state machine and a hand-written encoding/binary
// codec), printing their output.

const differentialTest = `package differential

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"testing"

	native "{{module}}/{{name}}"
	metered "{{module}}/{{name}}metered"
	oracle "{{module}}/{{name}}statemachine"
)

type parsed struct {
	h       native.Header
	n       int
	payload []byte
	err     string
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

{{parseNative}}

type appended struct {
	out []byte
	err string
}

// compareParse and compareHeader compare the native adapter with the
// state-machine oracle and with the default metered adapter.
func compareParse(t *testing.T, data []byte) {
	t.Helper()
	compareParseOracle(t, data)
	compareParseMetered(t, data)
}

func compareHeader(t *testing.T, h native.Header) {
	t.Helper()
	compareHeaderOracle(t, h)
	compareHeaderMetered(t, h)
}

{{compareOracle}}

{{compareMetered}}

// value draws a field value biased towards bit boundaries.
func value(r *rand.Rand, bits int) uint64 {
	mask := uint64(1)<<bits - 1
	if bits == 64 {
		mask = ^uint64(0)
	}
	switch r.IntN(6) {
	case 0:
		return 0
	case 1:
		return mask
	case 2:
		k := 1 + r.IntN(bits)
		return (uint64(1)<<k - 1) & mask
	case 3:
		k := r.IntN(bits)
		return uint64(1) << k
	case 4:
		return uint64(r.IntN(17))
	}
	return r.Uint64() & mask
}

func randomHeader(r *rand.Rand) native.Header {
	var h native.Header
	v := reflect.ValueOf(&h).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		f.SetUint(value(r, f.Type().Bits()))
	}
	return h
}

func TestNativeMatchesMetered(t *testing.T) {
	r := rand.New(rand.NewPCG(1, {{seed}}))
	// Boundary inputs: every length up to two bytes past the header, all
	// zero and all ones, and every single bit set or cleared.
	for n := 0; n <= native.HeaderSize+2; n++ {
		for _, fill := range []byte{0x00, 0xff} {
			compareParse(t, bytes.Repeat([]byte{fill}, n))
		}
	}
	for bit := range native.HeaderSize * 8 {
		for _, fill := range []byte{0x00, 0xff} {
			data := bytes.Repeat([]byte{fill}, native.HeaderSize+8)
			data[bit/8] ^= 0x80 >> (bit % 8)
			compareParse(t, data)
		}
	}
	// Field boundaries, then random headers.
	var zero native.Header
	compareHeader(t, zero)
	v := reflect.ValueOf(zero)
	for i := range v.NumField() {
		bits := v.Field(i).Type().Bits()
		for k := 0; k <= bits; k++ {
			for _, x := range []uint64{uint64(1)<<k - 1, uint64(1) << k} {
				var h native.Header
				f := reflect.ValueOf(&h).Elem().Field(i)
				f.SetUint(x)
				compareHeader(t, h)
			}
		}
	}
	for range 20000 {
		compareHeader(t, randomHeader(r))
	}
	// Random bytes, and valid encodings followed by random payload bytes.
	for range 20000 {
		data := make([]byte, r.IntN(native.HeaderSize+40))
		for i := range data {
			data[i] = byte(r.Uint32())
		}
		compareParse(t, data)
		if out, err := native.AppendSerialize(nil, randomHeader(r)); err == nil {
			compareParse(t, append(out, data...))
		}
	}
}

func FuzzNativeMatchesMetered(f *testing.F) {
	f.Add(make([]byte, native.HeaderSize))
	f.Add(bytes.Repeat([]byte{0xff}, native.HeaderSize+4))
	f.Fuzz(func(t *testing.T, data []byte) {
		compareParse(t, data)
		if len(data) >= 8 {
			r := rand.New(rand.NewPCG(uint64(len(data)), uint64(data[0])|uint64(data[1])<<8|uint64(data[len(data)-1])<<16))
			compareHeader(t, randomHeader(r))
		}
	})
}
`

// compareTemplate compares the native adapter with the adapter {{pkg}}.
const compareTemplate = `{{parse}}

func compareParse{{Pkg}}(t *testing.T, data []byte) {
	t.Helper()
	before := bytes.Clone(data)
	a, b := parseNative(data), parse{{Pkg}}(data)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Parse(% x): native %+v, {{label}} %+v", data, a, b)
	}
	if !bytes.Equal(data, before) {
		t.Fatalf("Parse modified its input")
	}
}

func compareHeader{{Pkg}}(t *testing.T, h native.Header) {
	t.Helper()
	m := {{pkg}}.Header(h)
	if a, b := errText(native.Validate(h)), errText({{pkg}}.Validate(m)); a != b {
		t.Fatalf("Validate(%+v): native %q, {{label}} %q", h, a, b)
	}
	prefix := []byte{0xa5, 0x5a}
	a, errA := native.AppendSerialize(bytes.Clone(prefix), h)
	b, errB := {{pkg}}.AppendSerialize(bytes.Clone(prefix), m)
	if !bytes.Equal(a, b) || errText(errA) != errText(errB) {
		t.Fatalf("AppendSerialize(%+v): native % x %v, {{label}} % x %v", h, a, errA, b, errB)
	}
	if errA == nil {
		compareParse(t, a[len(prefix):])
	}
	{{header}}
}
`

const parseNativePlain = `func parseNative(data []byte) parsed {
	h, n, err := native.Parse(data)
	return parsed{h: h, n: n, err: errText(err)}
}`

const parsePlain = `func parse{{Pkg}}(data []byte) parsed {
	h, n, err := {{pkg}}.Parse(data)
	return parsed{h: native.Header(h), n: n, err: errText(err)}
}`

const parseNativePayload = `func parseNative(data []byte) parsed {
	h, n, payload, err := native.Parse(data)
	if payload != nil && cap(payload) != len(payload) {
		panic("payload capacity exceeds its length")
	}
	return parsed{h: h, n: n, payload: payload, err: errText(err)}
}`

const parsePayload = `func parse{{Pkg}}(data []byte) parsed {
	h, n, payload, err := {{pkg}}.Parse(data)
	return parsed{h: native.Header(h), n: n, payload: payload, err: errText(err)}
}`

const headerPayload = `a1, e1 := native.PayloadLength(h)
	b1, f1 := {{pkg}}.PayloadLength(m)
	if a1 != b1 || errText(e1) != errText(f1) {
		t.Fatalf("PayloadLength(%+v): native %d %v, {{label}} %d %v", h, a1, e1, b1, f1)
	}
	for _, size := range []uint32{0, 1, a1 - 1, a1, a1 + 1} {
		if size > 1<<16 {
			continue
		}
		payload := bytes.Repeat([]byte{0x3c}, int(size))
		c, e2 := native.AppendFrame(bytes.Clone(prefix), h, payload)
		d, f2 := {{pkg}}.AppendFrame(bytes.Clone(prefix), m, payload)
		if !bytes.Equal(c, d) || errText(e2) != errText(f2) {
			t.Fatalf("AppendFrame(%+v, %d bytes): native %v, {{label}} %v", h, size, e2, f2)
		}
		if e2 == nil {
			compareParse(t, c[len(prefix):])
		}
	}`

// The benchmarks: parse then serialize one message per iteration, through
// the native-backed adapter, the default metered adapter, the state-machine
// metered adapter and a hand-written
// encoding/binary codec with the same checks. Payload formats parse the
// header and its payload and append the frame again.
const benchmarkTest = `package differential

import (
	"encoding/binary"
	"errors"
	"testing"

	native "{{module}}/{{name}}"
	metered "{{module}}/{{name}}metered"
	oracle "{{module}}/{{name}}statemachine"
)

var errTruncated = errors.New("truncated")
var errInvalid = errors.New("invalid")

{{hand}}

func BenchmarkParseSerialize(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.Run("native", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			{{loopNative}}
		}
	})
	b.Run("metered", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			{{loopMetered}}
		}
	})
	b.Run("metered-state-machine", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			{{loopStateMachine}}
		}
	})
	b.Run("handwritten", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			{{loopHand}}
		}
	})
}
`

const loopPlain = `h, _, err := {{pkg}}.Parse(message)
			if err != nil {
				b.Fatal(err)
			}
			if buf, err = {{pkg}}.AppendSerialize(buf[:0], h); err != nil {
				b.Fatal(err)
			}`

const loopPayload = `h, _, payload, err := {{pkg}}.Parse(message)
			if err != nil {
				b.Fatal(err)
			}
			if buf, err = {{pkg}}.AppendFrame(buf[:0], h, payload); err != nil {
				b.Fatal(err)
			}`

const loopHandPlain = `h, err := parseHand(message)
			if err != nil {
				b.Fatal(err)
			}
			if buf, err = appendHand(buf[:0], h); err != nil {
				b.Fatal(err)
			}`

const loopHandPayload = `h, payload, err := parseHand(message)
			if err != nil {
				b.Fatal(err)
			}
			if buf, err = appendHand(buf[:0], h, payload); err != nil {
				b.Fatal(err)
			}`

// handCodecs are the hand-written codecs, each with a test that it agrees
// with the native adapter on the benchmark message.
var handCodecs = map[string]string{
	"dns": `var message = []byte{0xab, 0xcd, 0x81, 0x80, 0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x01, 0x07, 'e', 'x'}

type header struct {
	ID                                uint16
	QR, Opcode, AA, TC, RD, RA, Rcode uint8
	QD, AN, NS, AR                    uint16
}

func parseHand(b []byte) (header, error) {
	if len(b) < 12 {
		return header{}, errTruncated
	}
	flags := binary.BigEndian.Uint16(b[2:4])
	if flags&0x0070 != 0 {
		return header{}, errInvalid
	}
	return header{binary.BigEndian.Uint16(b[0:2]), uint8(flags >> 15), uint8(flags>>11) & 0xf, uint8(flags>>10) & 1,
		uint8(flags>>9) & 1, uint8(flags>>8) & 1, uint8(flags>>7) & 1, uint8(flags) & 0xf,
		binary.BigEndian.Uint16(b[4:6]), binary.BigEndian.Uint16(b[6:8]), binary.BigEndian.Uint16(b[8:10]), binary.BigEndian.Uint16(b[10:12])}, nil
}

func appendHand(dst []byte, h header) ([]byte, error) {
	if h.QR > 1 || h.Opcode > 15 || h.AA > 1 || h.TC > 1 || h.RD > 1 || h.RA > 1 || h.Rcode > 15 {
		return dst, errInvalid
	}
	flags := uint16(h.QR)<<15 | uint16(h.Opcode)<<11 | uint16(h.AA)<<10 | uint16(h.TC)<<9 | uint16(h.RD)<<8 | uint16(h.RA)<<7 | uint16(h.Rcode)
	dst = binary.BigEndian.AppendUint16(dst, h.ID)
	dst = binary.BigEndian.AppendUint16(dst, flags)
	dst = binary.BigEndian.AppendUint16(dst, h.QD)
	dst = binary.BigEndian.AppendUint16(dst, h.AN)
	dst = binary.BigEndian.AppendUint16(dst, h.NS)
	return binary.BigEndian.AppendUint16(dst, h.AR), nil
}

func TestHandMatchesNative(t *testing.T) {
	h, _, err := native.Parse(message)
	g, err2 := parseHand(message)
	a, _ := native.AppendSerialize(nil, h)
	b, _ := appendHand(nil, g)
	if err != nil || err2 != nil || string(a) != string(b) || string(a) != string(message[:12]) {
		t.Fatalf("% x vs % x: %v %v", a, b, err, err2)
	}
}`,
	"udp": `var message = []byte{0x12, 0x34, 0x00, 0x35, 0x00, 0x0b, 0xab, 0xcd, 'x', 'y', 'z', 0xee}

type header struct{ Src, Dst, Length, Checksum uint16 }

func parseHand(b []byte) (header, []byte, error) {
	if len(b) < 8 {
		return header{}, nil, errTruncated
	}
	h := header{binary.BigEndian.Uint16(b[0:2]), binary.BigEndian.Uint16(b[2:4]), binary.BigEndian.Uint16(b[4:6]), binary.BigEndian.Uint16(b[6:8])}
	if h.Length < 8 {
		return header{}, nil, errInvalid
	}
	end := int(h.Length)
	if len(b) < end {
		return header{}, nil, errTruncated
	}
	return h, b[8:end:end], nil
}

func appendHand(dst []byte, h header, payload []byte) ([]byte, error) {
	if h.Length < 8 || len(payload) != int(h.Length)-8 {
		return dst, errInvalid
	}
	dst = binary.BigEndian.AppendUint16(dst, h.Src)
	dst = binary.BigEndian.AppendUint16(dst, h.Dst)
	dst = binary.BigEndian.AppendUint16(dst, h.Length)
	dst = binary.BigEndian.AppendUint16(dst, h.Checksum)
	return append(dst, payload...), nil
}

func TestHandMatchesNative(t *testing.T) {
	h, _, p, err := native.Parse(message)
	g, q, err2 := parseHand(message)
	a, _ := native.AppendFrame(nil, h, p)
	b, _ := appendHand(nil, g, q)
	if err != nil || err2 != nil || string(a) != string(b) || string(a) != string(message[:11]) {
		t.Fatalf("% x vs % x: %v %v", a, b, err, err2)
	}
}`,
	"header": `var message = []byte{0x01, 0x02, 0x00, 0x03, 'a', 'b', 'c', 0xee}

type header struct {
	Flags  uint8
	Length uint16
}

func parseHand(b []byte) (header, []byte, error) {
	if len(b) < 4 {
		return header{}, nil, errTruncated
	}
	if b[0] != 1 || b[1]&0xfc != 0 {
		return header{}, nil, errInvalid
	}
	h := header{b[1] & 3, binary.BigEndian.Uint16(b[2:4])}
	end := 4 + int(h.Length)
	if len(b) < end {
		return header{}, nil, errTruncated
	}
	return h, b[4:end:end], nil
}

func appendHand(dst []byte, h header, payload []byte) ([]byte, error) {
	if h.Flags > 3 || len(payload) != int(h.Length) {
		return dst, errInvalid
	}
	dst = append(dst, 1, h.Flags)
	dst = binary.BigEndian.AppendUint16(dst, h.Length)
	return append(dst, payload...), nil
}

func TestHandMatchesNative(t *testing.T) {
	h, _, p, err := native.Parse(message)
	g, q, err2 := parseHand(message)
	a, _ := native.AppendFrame(nil, h, p)
	b, _ := appendHand(nil, g, q)
	if err != nil || err2 != nil || string(a) != string(b) || string(a) != string(message[:7]) {
		t.Fatalf("% x vs % x: %v %v", a, b, err, err2)
	}
}`,
}

func goOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), goBinary(), args...)
	cmd.Dir, cmd.Env = dir, goEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func expand(text string, vars map[string]string) string {
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{{"+k+"}}", v)
	}
	return text
}

func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// differentialModule writes the three adapters and the comparison into dir.
func differentialModule(t *testing.T, name, dir string) {
	t.Helper()
	f, err := Load(filepath.Join(examplesDir, name, "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	ir, err := os.ReadFile(filepath.Join(examplesDir, name, f.IRPath()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for path, text := range f.Files() {
		if path == "go.mod" || filepath.Dir(path) == f.AdapterDir() && !strings.HasSuffix(path, "_test.go") {
			files[filepath.ToSlash(path)] = []byte(text)
		}
	}
	for mode, sub := range map[string]string{"native": "internal/extracted", "metered": "internal/meteredextracted"} {
		extracted, err := pipeline.Emit(ir, pipeline.EmitOptions{Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		for file, data := range extracted {
			if strings.HasSuffix(file, ".go") {
				files[sub+"/"+file] = data
			}
		}
	}
	// The oracle: the same IR through the state machine alone, so that it
	// shares no generated code with the native package.
	program, err := moduleir.Decode(bytes.NewReader(ir))
	if err != nil {
		t.Fatal(err)
	}
	stateMachine, err := modulegen.GenerateStateMachine(program)
	if err != nil {
		t.Fatal(err)
	}
	for file, data := range stateMachine {
		if strings.HasSuffix(file, ".go") {
			files["internal/statemachineextracted/"+file] = data
		}
	}
	// The previous adapter: the same <name>.go with the metered backend, once
	// over each metered package.
	for suffix, sub := range map[string]string{"metered": "internal/meteredextracted", "statemachine": "internal/statemachineextracted"} {
		rename := func(text string) []byte {
			text = strings.Replace(text, "\npackage "+f.Name+"\n", "\npackage "+f.Name+suffix+"\n", 1)
			return []byte(strings.Replace(text, f.ExtractedImport(), f.GoModulePath()+"/"+sub, 1))
		}
		files[f.Name+suffix+"/"+f.Name+".go"] = rename(f.adapterGo())
		files[f.Name+suffix+"/lean.go"] = rename(gofmt("lean.go", backendMetered(f)))
	}
	parseNative, parse, header := parseNativePlain, parsePlain, ""
	if f.Payload != nil {
		parseNative, parse, header = parseNativePayload, parsePayload, headerPayload
	}
	compare := func(pkg, Pkg, label string) string {
		text := strings.ReplaceAll(compareTemplate, "{{parse}}", parse)
		text = strings.ReplaceAll(text, "{{header}}", header)
		return expand(text, map[string]string{"pkg": pkg, "Pkg": Pkg, "label": label})
	}
	vars := map[string]string{"module": f.GoModulePath(), "name": f.Name, "parseNative": parseNative, "seed": fmt.Sprint(len(name)),
		"compareOracle": compare("oracle", "Oracle", "metered state machine"), "compareMetered": compare("metered", "Metered", "metered")}
	files["differential/differential_test.go"] = []byte(gofmt("differential_test.go", expand(differentialTest, vars)))
	if hand, ok := handCodecs[name]; ok {
		loop, loopHand := loopPlain, loopHandPlain
		if f.Payload != nil {
			loop, loopHand = loopPayload, loopHandPayload
		}
		bench := expand(benchmarkTest, map[string]string{"hand": hand, "loopNative": strings.ReplaceAll(loop, "{{pkg}}", "native"),
			"loopMetered": strings.ReplaceAll(loop, "{{pkg}}", "metered"), "loopStateMachine": strings.ReplaceAll(loop, "{{pkg}}", "oracle"), "loopHand": loopHand})
		files["differential/bench_test.go"] = []byte(gofmt("bench_test.go", expand(bench, vars)))
	}
	writeTree(t, dir, files)
}

func TestNativeAdapterMatchesMeteredAdapter(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three nested modules")
	}
	for _, name := range exampleNames(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			differentialModule(t, name, dir)
			runGo(t, dir, "vet", "./...")
			runGo(t, dir, "test", "-count=1", "./...")
			if fuzz := os.Getenv("LEAN_GO_FORMATS_FUZZ"); fuzz != "" {
				t.Log(goOutput(t, dir, "test", "-run", "^$", "-fuzz", "^FuzzNativeMatchesMetered$", "-fuzztime", fuzz, "./differential"))
			}
			if os.Getenv("LEAN_GO_FORMATS_BENCH") != "" {
				t.Log(goOutput(t, dir, "test", "-run", "^$", "-bench", ".", "-benchmem", "-cpu", "1", "-count", "5", "./differential"))
			}
		})
	}
}
