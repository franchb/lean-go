package modulegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/moduleir"
)

// trackedPrograms are the IR files of every tracked metered package and of
// the wire-format examples (native in the checkout; the metered benchmark of
// internal/wireformat emits them in metered mode).
var trackedPrograms = []string{
	"../../testdata/choice.ir.json", "../../testdata/counters.ir.json", "../../testdata/joins.ir.json",
	"../../testdata/local.ir.json", "../../testdata/numbers.ir.json", "../../testdata/quota.ir.json",
	"../../testdata/records.ir.json", "../../testdata/reduced.ir.json", "../../testdata/trees.ir.json",
	"../../testdata/wide.ir.json",
	"../../examples/proofs/adler32/adler32.ir.json", "../../examples/proofs/bytes/bytes.ir.json",
	"../../examples/proofs/clamp/clamp.ir.json", "../../examples/proofs/crc16/crc16.ir.json",
	"../../examples/proofs/flags/flags.ir.json", "../../examples/proofs/header/header.ir.json",
	"../../examples/proofs/normalizer/normalizer.ir.json", "../../examples/proofs/wire16/wire16.ir.json",
	"../../examples/proofs/words/words.ir.json",
	"../../examples/formats/dns/dns.ir.json", "../../examples/formats/header/header.ir.json",
	"../../examples/formats/udp/udp.ir.json",
}

// fastRoots lists, per tracked program, the roots that take the fast path.
// A root dropping out of this list (or joining it) must be a deliberate
// change of the eligibility rule, not a silent one.
var fastRoots = map[string][]string{
	"choice":     {"Choice.rotate"},
	"counters":   {"Counters.advance", "Counters.initial", "Counters.read", "Counters.total"},
	"joins":      {"Joins.update"},
	"local":      {"Local.run"},
	"numbers":    {"Numbers.compute"},
	"quota":      {"Quota.create", "Quota.release", "Quota.remaining", "Quota.reserve"},
	"records":    {"Records.update"},
	"reduced":    {"Reduced.run"},
	"trees":      nil, // recursive type and function
	"wide":       {"Wide.compute"},
	"adler32":    {"Adler32.checksum", "Adler32.initial"}, // Adler32.update is a byte fold
	"bytes":      {"ByteCodec.decodeBE", "ByteCodec.decodeLE", "ByteCodec.encodeBE", "ByteCodec.encodeLE"},
	"clamp":      {"Clamp.clamp"},
	"crc16":      {"Crc16.updateByte"}, // Crc16.update is a byte fold
	"flags":      {"Flags.clear", "Flags.contains", "Flags.set", "Flags.toggle"},
	"header":     {"HeaderCodec.encode", "HeaderCodec.parse"},
	"normalizer": nil, // recursive type and function
	"wire16":     {"Wire16.classify", "Wire16.decodeBE", "Wire16.encode", "Wire16.encodeBE", "Wire16.parse"},
	"words":      {"Words.high", "Words.low", "Words.narrow", "Words.pack", "Words.widen"},
	"dns":        {"DnsHeader.parse", "DnsHeader.serialize", "DnsHeader.validate"},
	"udp":        {"UdpHeader.classify", "UdpHeader.parse", "UdpHeader.serialize", "UdpHeader.validate"},
	"header2":    {"DemoHeader.classify", "DemoHeader.parse", "DemoHeader.serialize", "DemoHeader.validate"},
}

func trackedName(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".ir.json")
	if strings.Contains(path, "/formats/header/") {
		return "header2"
	}
	return name
}

func loadProgram(t *testing.T, path string) *moduleir.Program {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := moduleir.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func planOf(t *testing.T, p *moduleir.Program) (*generator, *fastPlan) {
	t.Helper()
	if err := moduleir.Validate(p); err != nil {
		t.Fatal(err)
	}
	g := newGenerator(p)
	return g, g.planFastPath()
}

func TestFastPathTrackedRoots(t *testing.T) {
	for _, path := range trackedPrograms {
		p := loadProgram(t, path)
		name := trackedName(path)
		want, listed := fastRoots[name]
		if !listed {
			t.Fatalf("%s: not listed in fastRoots", name)
		}
		_, plan := planOf(t, p)
		var got []string
		if plan != nil {
			got = plan.roots
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: fast-path roots %q, want %q", name, got, want)
		}
	}
}

// The bounds of a few roots, worked out by hand from the IR, pin the
// accounting itself.
func TestFastPathBoundByHand(t *testing.T) {
	// Clamp.clamp: let call, return (2 steps); Clamp.Bounds.cap: let
	// primitive, cases, return (3 steps, 1 allocation). Slots 3 + 3, two
	// frames, two UInt64 argument nodes, the Bool result of one primitive.
	_, plan := planOf(t, loadProgram(t, "../../examples/proofs/clamp/clamp.ir.json"))
	if got, want := plan.bounds["Clamp.clamp"], [dimensions]uint64{2, 1, 1, 1, 5, 2, 6}; got != want {
		t.Fatalf("Clamp.clamp bound %v, want %v", got, want)
	}
	// The numeric test program: nine primitives and a constructor (10 lets),
	// one return; 12 slots; the result record has ten nodes and depth 2.
	g, plan := planOf(t, declare(numericProgram()))
	if plan == nil {
		t.Fatal("numeric program not eligible")
	}
	root := moduleir.RootNames(g.program)[0]
	if got, want := plan.bounds[root], [dimensions]uint64{2, 2, 10, 10, 11, 1, 12}; got != want {
		t.Fatalf("%s bound %v, want %v", root, got, want)
	}
}

// The bound analysis and fastCovers use the caps of DefaultLimits.
func TestLimitCapsAreDefaultLimits(t *testing.T) {
	files, err := Generate(loadProgram(t, "../../examples/proofs/clamp/clamp.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, c := range limitCaps {
		parts = append(parts, fmt.Sprint(c))
	}
	if want := "return Limits{" + strings.Join(parts, ", ") + "}"; !bytes.Contains(files["runtime.go"], []byte(want)) {
		t.Fatalf("runtime.go does not contain %q", want)
	}
}

// The test switch only adds the fastDisabled variable and its checks.
func TestFastPathSwitchIsTestOnly(t *testing.T) {
	p := loadProgram(t, "../../examples/formats/dns/dns.ir.json")
	production, err := Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	switched, err := generateWith(loadProgram(t, "../../examples/formats/dns/dns.ir.json"), generateOptions{fastSwitch: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(production["api.go"], []byte("fastDisabled")) {
		t.Fatal("production output mentions fastDisabled")
	}
	stripped := strings.ReplaceAll(string(switched["api.go"]), "\tif fastDisabled {\n\t\treturn false\n\t}\n", "")
	stripped = strings.Replace(stripped, "\n// fastDisabled turns the fast path off (test builds only).\nvar fastDisabled bool\n", "", 1)
	if stripped != string(production["api.go"]) {
		t.Fatal("test-switch output differs from production beyond fastDisabled")
	}
	for _, file := range []string{"runtime.go", "schema.go", "control.go"} {
		if !bytes.Equal(production[file], switched[file]) {
			t.Fatalf("%s differs with the test switch", file)
		}
	}
}

// A scalar root whose bound exceeds the caps keeps the metered scalar entry:
// exact metering still runs the cheap inputs natively.
func TestScalarEntryKeptAboveCaps(t *testing.T) {
	p := exponentialScalarProgram(24)
	g, plan := planOf(t, p)
	if plan.eligible("Exp.f23") {
		t.Fatal("exponential root is on the fast path")
	}
	if bound, ok := newBoundAnalysis(g).rootBound("Exp.f23"); !ok || withinCaps(bound) {
		t.Fatalf("bound %v %v", bound, ok)
	}
	files, err := generate(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["api.go"], []byte("runs the scalar root with native Go values")) {
		t.Fatal("scalar entry not kept")
	}
	runGenerated(t, files, `package extracted
import "testing"
func TestExponential(t *testing.T){
	args:=[]Value{{apiUInt32Node(1)},{apiBoolNode(false)}}
	for _,cheap:=range []bool{false,true}{
		args[1]=Value{apiBoolNode(cheap)}
		for _,l:=range []Limits{DefaultLimits(),{MaxNodes:2,MaxDepth:1,MaxOutputNodes:1,MaxAllocations:100,MaxSteps:300,MaxFrames:30,MaxSlots:500}}{
			got,err:=RunTyped(1,cheap,l)
			v,wantErr:=Run(args,l)
			want,_:=v.UInt32()
			if err!=wantErr||got!=want{t.Fatal(cheap,l,got,err,want,wantErr)}
			if cheap&&err!=nil{t.Fatal("cheap input failed",err)}
		}
	}
}`)
}

// exponentialScalarProgram: f0 returns its argument; f(i) calls f(i-1)
// twice unless its Bool argument is true. The root's bound doubles per level.
func exponentialScalarProgram(levels int) *moduleir.Program {
	p := testProgram()
	p.Module = "Exp"
	p.Types = []moduleir.TypeDecl{{Name: "Bool", Constructors: []moduleir.Constructor{{Name: "Bool.false", Fields: []string{}}, {Name: "Bool.true", Tag: 1, Fields: []string{}}}}}
	params := []moduleir.Param{{ID: 1, Type: "UInt32"}, {ID: 2, Type: "Bool"}}
	p.Functions = []moduleir.Function{{Name: "Exp.f00", Params: params, Result: "UInt32", Body: &moduleir.Code{Op: "return", Var: 1}}}
	p.Provenance = []moduleir.Provenance{{Name: "Exp.f00", Dependencies: []string{}, Rewrites: []moduleir.Rewrite{}}}
	for i := 1; i < levels; i++ {
		name, prev := fmt.Sprintf("Exp.f%02d", i), fmt.Sprintf("Exp.f%02d", i-1)
		call := func(id, arg int, next *moduleir.Code) *moduleir.Code {
			return &moduleir.Code{Op: "let", Bind: &moduleir.Param{ID: id, Type: "UInt32"}, Value: &moduleir.Value{Kind: "call", Name: prev, Args: []int{arg, 2}}, Next: next}
		}
		twice := call(3, 1, call(4, 3, &moduleir.Code{Op: "return", Var: 4}))
		body := &moduleir.Code{Op: "cases", Var: 2, Type: "Bool", Result: "UInt32", Alts: []moduleir.Alt{
			{Constructor: "Bool.false", Params: []moduleir.Param{}, Body: twice},
			{Constructor: "Bool.true", Params: []moduleir.Param{}, Body: &moduleir.Code{Op: "return", Var: 1}}}}
		p.Functions = append(p.Functions, moduleir.Function{Name: name, Params: params, Result: "UInt32", Body: body})
		p.Provenance = append(p.Provenance, moduleir.Provenance{Name: name, Dependencies: []string{prev}, Rewrites: []moduleir.Rewrite{}})
	}
	p.Functions[len(p.Functions)-1].Name = "Exp.f23"
	p.Provenance[len(p.Provenance)-1].Name = "Exp.f23"
	p.Roots = []string{"Exp.f23"}
	return declare(p)
}

// unreachedStates are the state-machine states of tracked fast-path call
// trees that no input reaches. Joins.update: join 24 matches its Bool
// parameter 25 (for `!state.enabled.value`), but both of its jumps sit in
// the `Bool.true` alternative of `cases 45` and pass 45, so the `Bool.false`
// alternative (a jump to join 17) is dead code of the compiled IR.
var unreachedStates = map[string][]string{"joins": {"function 1 pc 19"}}

// TestFastPathSoundAndEquivalent runs, for every eligible root of every
// tracked program and of random programs, a generated in-package driver
// (fastDriver) and then checks that the inputs reached every IR node of the
// roots' call trees (all of them for tracked programs).
func TestFastPathSoundAndEquivalent(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a package per program")
	}
	type program struct {
		name    string
		p       *moduleir.Program
		tracked bool
	}
	var programs []program
	for _, path := range trackedPrograms {
		programs = append(programs, program{trackedName(path), loadProgram(t, path), true})
	}
	for seed := int64(1); seed <= 40; seed++ {
		programs = append(programs, program{fmt.Sprintf("random%d", seed), randomProgram(seed), false})
	}
	roots := 0
	for _, prog := range programs {
		t.Run(prog.name, func(t *testing.T) {
			if err := moduleir.Validate(prog.p); err != nil {
				t.Fatal(err)
			}
			files, err := generateWith(prog.p, generateOptions{fastSwitch: true})
			if err != nil {
				t.Fatal(err)
			}
			g := newGenerator(prog.p)
			g.fast = g.planFastPath()
			if g.fast == nil {
				if !prog.tracked {
					t.Fatal("random program has no fast-path root")
				}
				return
			}
			roots += len(g.fast.roots)
			driver := fastDriver(t, g, files["manifest.json"])
			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for name, data := range map[string]string{"go.mod": "module example.test/extracted\n\ngo 1.27.4\n", "fast_test.go": driver} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			goBin, err := exec.LookPath("go")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(goBin, "test", "-count=1", "-v", "-coverprofile=cover.out", ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("driver: %v\n%s", err, out)
			}
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "bound [") {
					t.Log(strings.TrimSpace(line))
				}
			}
			missed, total := uncoveredNodes(t, g, dir)
			t.Logf("IR nodes of the fast-path call trees reached by the state machine: %d of %d", total-len(missed), total)
			if prog.tracked && !slices.Equal(missed, unreachedStates[prog.name]) {
				t.Errorf("inputs did not reach %v, expected only %v", missed, unreachedStates[prog.name])
			}
		})
	}
	t.Logf("%d programs, %d fast-path roots", len(programs), roots)
}

type driverAPI struct {
	GoAPI struct {
		Entrypoint  string            `json:"entrypoint"`
		Entrypoints map[string]string `json:"entrypoints"`
		Types       map[string]string `json:"types"`
	} `json:"go_api"`
}

// fastDriver writes the in-package test of a generated package. For each
// fast-path root and many generated arguments it checks:
//   - soundness: the state machine alone (fastDisabled) succeeds with limits
//     equal to the bound, and the least value of each limit that succeeds,
//     with the others at their caps, is at most the bound;
//   - equivalence: with limits at the bound and at the defaults, the raw and
//     typed entry points with the fast path return exactly the state
//     machine's result (types, tags, scalars, sizes and depths);
//   - fallback: with one limit below the bound, with random limit vectors,
//     and with malformed arguments, both modes agree on result and error.
func fastDriver(t *testing.T, g *generator, manifest []byte) string {
	t.Helper()
	var api driverAPI
	if err := json.Unmarshal(manifest, &api); err != nil {
		t.Fatal(err)
	}
	scalars := apiScalars(g.program)
	pools := literalPools(g.program)
	var b strings.Builder
	b.WriteString(driverPrelude)
	fmt.Fprintf(&b, "var pools = map[string][]uint64{\n")
	for _, width := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
		fmt.Fprintf(&b, "%q: {", width)
		for _, v := range pools[width] {
			fmt.Fprintf(&b, "%d,", v)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\nvar fastRoots = []fastRoot{\n")
	for _, root := range g.fast.roots {
		f := g.functions[root]
		bound := g.fast.bounds[root]
		fmt.Fprintf(&b, "{name: %q, bound: Limits{%d, %d, %d, %d, %d, %d, %d}, params: []int{", root, bound[0], bound[1], bound[2], bound[3], bound[4], bound[5], bound[6])
		for _, p := range f.decl.Params {
			fmt.Fprintf(&b, "%d,", g.types[p.Type])
		}
		b.WriteString("},\n")
		if moduleir.HasMultipleRoots(g.program) {
			fmt.Fprintf(&b, "raw: func(args []Value, l Limits) (Value, error) { return RunRoot(%q, args, l) },\n", root)
		} else {
			b.WriteString("raw: Run,\n")
		}
		entry := api.GoAPI.Entrypoint
		if entry == "" {
			entry = api.GoAPI.Entrypoints[root]
		}
		var args []string
		for i, p := range f.decl.Params {
			switch {
			case p.Type == "Bool":
				args = append(args, fmt.Sprintf("args[%d].tag == 1", i))
			case scalars[p.Type].GoType != "":
				args = append(args, fmt.Sprintf("args[%d].%s", i, scalarField(p.Type)))
			default:
				args = append(args, fmt.Sprintf("%s{v: Value{args[%d]}}", api.GoAPI.Types[p.Type], i))
			}
		}
		back := "r.v.n"
		if scalar, ok := scalars[f.decl.Result]; ok {
			back = scalar.Node + "(r)"
		}
		fmt.Fprintf(&b, "typed: func(args []*node, l Limits) (*node, error) { r, err := %s(%sl); if err != nil { return nil, err }; return %s, nil },\n},\n", entry, strings.Join(append(args, ""), ", "), back)
	}
	b.WriteString("}\n")
	return b.String()
}

// literalPools collects, per width, every literal of the program and its
// neighbours, so that comparisons with constants take both branches.
func literalPools(p *moduleir.Program) map[string][]uint64 {
	masks := map[string]uint64{"UInt8": 0xff, "UInt16": 0xffff, "UInt32": 0xffffffff, "UInt64": ^uint64(0)}
	sets := map[string]map[uint64]bool{}
	for width, mask := range masks {
		sets[width] = map[uint64]bool{0: true, 1: true, 2: true, mask: true, mask - 1: true, mask >> 1: true, mask>>1 + 1: true}
	}
	for _, f := range p.Functions {
		walkGenCode(f.Body, func(c *moduleir.Code) {
			if c.Op != "let" {
				return
			}
			width := map[string]string{"uint8": "UInt8", "uint16": "UInt16", "uint32": "UInt32", "uint64": "UInt64"}[c.Value.Kind]
			if width == "" {
				return
			}
			v, err := strconv.ParseUint(c.Value.Name, 10, 64)
			if err != nil {
				return
			}
			for _, w := range []uint64{v - 1, v, v + 1} {
				// Widths convert into each other: offer it to every pool.
				for other, mask := range masks {
					sets[other][w&mask] = true
				}
			}
		})
	}
	out := map[string][]uint64{}
	for width, set := range sets {
		for v := range set {
			out[width] = append(out[width], v)
		}
		slices.Sort(out[width])
	}
	return out
}

const driverPrelude = `package extracted

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
)

type fastRoot struct {
	name   string
	bound  Limits
	params []int
	raw    func([]Value, Limits) (Value, error)
	typed  func([]*node, Limits) (*node, error)
}

func dims(l Limits) [7]uint64 { return [7]uint64{l.MaxNodes, l.MaxDepth, l.MaxOutputNodes, l.MaxAllocations, l.MaxSteps, l.MaxFrames, l.MaxSlots} }
func fromDims(d [7]uint64) Limits { return Limits{d[0], d[1], d[2], d[3], d[4], d[5], d[6]} }

// scalarMode selects how genNode picks scalars: 0 mixes full-width random,
// small and pool values; 1 uses the pool only; 2 only 0 and 1, which passes
// chains of range checks at once.
var scalarMode int

func scalarValue(r *rand.Rand, width string) uint64 {
	pool := pools[width]
	switch scalarMode {
	case 1:
		return pool[r.Intn(len(pool))]
	case 2:
		return uint64(r.Intn(2))
	}
	switch r.Intn(4) {
	case 0:
		return r.Uint64()
	case 1:
		return uint64(r.Intn(17))
	default:
		return pool[r.Intn(len(pool))]
	}
}

// genNode builds a well-formed random node of type typ.
func genNode(r *rand.Rand, typ int) *node {
	switch name := typeName(typ); name {
	case "UInt8":
		return apiUInt8Node(uint8(scalarValue(r, name)))
	case "UInt16":
		return apiUInt16Node(uint16(scalarValue(r, name)))
	case "UInt32":
		return apiUInt32Node(uint32(scalarValue(r, name)))
	case "UInt64":
		return apiUInt64Node(scalarValue(r, name))
	}
	constructors := 0
	for constructorName(typ, constructors) != "" {
		constructors++
	}
	tag := r.Intn(constructors)
	var fields []*node
	for i := 0; fieldType(typ, tag, i) != -1; i++ {
		fields = append(fields, genNode(r, fieldType(typ, tag, i)))
	}
	size, depth, err := measure(fields, 1<<62, 1<<62)
	if err != nil {
		panic(err)
	}
	return &node{typ: typ, tag: tag, fields: fields, size: size, depth: depth}
}

// mutate returns n with one leaf replaced: a fresh value of its type, or a
// scalar moved by one.
func mutate(r *rand.Rand, n *node) *node {
	if len(n.fields) == 0 || r.Intn(len(n.fields)+1) == 0 {
		if len(n.fields) == 0 && r.Intn(2) == 0 {
			c := *n
			delta := uint64(1)
			if r.Intn(2) == 0 {
				delta = ^uint64(0)
			}
			c.scalar8 += uint8(delta)
			c.scalar16 += uint16(delta)
			c.scalar += uint32(delta)
			c.scalar64 += delta
			switch typeName(n.typ) {
			case "UInt8":
				return apiUInt8Node(c.scalar8)
			case "UInt16":
				return apiUInt16Node(c.scalar16)
			case "UInt32":
				return apiUInt32Node(c.scalar)
			case "UInt64":
				return apiUInt64Node(c.scalar64)
			}
		}
		return genNode(r, n.typ)
	}
	fields := append([]*node(nil), n.fields...)
	k := r.Intn(len(fields))
	fields[k] = mutate(r, fields[k])
	size, depth, err := measure(fields, 1<<62, 1<<62)
	if err != nil {
		panic(err)
	}
	return &node{typ: n.typ, tag: n.tag, fields: fields, size: size, depth: depth}
}

func sameNode(a, b *node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.typ != b.typ || a.tag != b.tag || a.scalar8 != b.scalar8 || a.scalar16 != b.scalar16 || a.scalar != b.scalar || a.scalar64 != b.scalar64 ||
		a.size != b.size || a.depth != b.depth || len(a.fields) != len(b.fields) {
		return false
	}
	for i := range a.fields {
		if !sameNode(a.fields[i], b.fields[i]) {
			return false
		}
	}
	return true
}

func show(n *node) string {
	if n == nil {
		return "<nil>"
	}
	s := fmt.Sprintf("%d/%d[%d %d %d %d s%d d%d]", n.typ, n.tag, n.scalar8, n.scalar16, n.scalar, n.scalar64, n.size, n.depth)
	for _, f := range n.fields {
		s += " " + show(f)
	}
	return "(" + s + ")"
}

type outcome struct {
	raw, typed       *node
	rawErr, typedErr error
}

func run(root fastRoot, nodes []*node, l Limits, disabled bool) outcome {
	fastDisabled = disabled
	defer func() { fastDisabled = false }()
	args := make([]Value, len(nodes))
	for i, n := range nodes {
		args[i] = Value{n}
	}
	v, err := root.raw(args, l)
	var o outcome
	o.raw, o.rawErr = v.n, err
	o.typed, o.typedErr = root.typed(nodes, l)
	return o
}

func same(a, b outcome) bool {
	return a.rawErr == b.rawErr && a.typedErr == b.typedErr && sameNode(a.raw, b.raw) && sameNode(a.typed, b.typed)
}

func compareModes(t *testing.T, root fastRoot, nodes []*node, l Limits, what string) {
	t.Helper()
	slow, fast := run(root, nodes, l, true), run(root, nodes, l, false)
	if !same(slow, fast) {
		t.Fatalf("%s %s %+v: state machine %s/%v typed %s/%v; package %s/%v typed %s/%v", root.name, what, l,
			show(slow.raw), slow.rawErr, show(slow.typed), slow.typedErr, show(fast.raw), fast.rawErr, show(fast.typed), fast.typedErr)
	}
}

// least returns the smallest value of limit d, the others at their caps,
// with which the state machine alone succeeds.
func least(t *testing.T, root fastRoot, nodes []*node, d int) uint64 {
	caps := dims(DefaultLimits())
	ok := func(v uint64) bool {
		l := caps
		l[d] = v
		o := run(root, nodes, fromDims(l), true)
		if o.rawErr != nil && !errors.Is(o.rawErr, ErrResource) {
			t.Fatalf("%s: unexpected error %v", root.name, o.rawErr)
		}
		return o.rawErr == nil
	}
	if !ok(caps[d]) {
		t.Fatalf("%s: fails at the caps", root.name)
	}
	lo, hi := uint64(1), caps[d]
	for lo < hi {
		mid := lo + (hi-lo)/2
		if ok(mid) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

func TestFastPath(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	caps := dims(DefaultLimits())
	for _, root := range fastRoots {
		bound := dims(root.bound)
		var seen [7]uint64
		// Inputs are fresh random arguments or mutations of earlier inputs
		// that took a path with a new (steps, allocations) signature, so
		// chains of checks are passed one at a time.
		var corpus [][]*node
		signatures := map[[2]uint64]bool{}
		for i := 0; i < 1000; i++ {
			nodes := make([]*node, len(root.params))
			if len(corpus) > 0 && len(nodes) > 0 && r.Intn(3) != 0 {
				// Half the time extend the newest path, the frontier.
				pick := len(corpus) - 1
				if r.Intn(2) == 0 {
					pick = r.Intn(len(corpus))
				}
				copy(nodes, corpus[pick])
				j := r.Intn(len(nodes))
				scalarMode = 0
				nodes[j] = mutate(r, nodes[j])
			} else {
				scalarMode = r.Intn(3)
				for j, typ := range root.params {
					nodes[j] = genNode(r, typ)
				}
			}
			slow := run(root, nodes, root.bound, true)
			if slow.rawErr != nil || slow.typedErr != nil {
				t.Fatalf("%s: state machine with limits at the bound %+v: %v / %v", root.name, root.bound, slow.rawErr, slow.typedErr)
			}
			if !sameNode(slow.raw, slow.typed) {
				t.Fatalf("%s: raw and typed state machine differ", root.name)
			}
			var use [7]uint64
			for d := range 7 {
				use[d] = least(t, root, nodes, d)
				if use[d] > bound[d] {
					t.Fatalf("%s: dimension %d uses %d above the bound %d", root.name, d, use[d], bound[d])
				}
				seen[d] = max(seen[d], use[d])
			}
			if signature := [2]uint64{use[4], use[3]}; !signatures[signature] {
				signatures[signature] = true
				corpus = append(corpus, nodes)
			}
			for _, l := range []Limits{root.bound, DefaultLimits()} {
				fast := run(root, nodes, l, false)
				if !same(slow, fast) {
					t.Fatalf("%s %+v: state machine %s, fast %s/%v typed %s/%v", root.name, l, show(slow.raw), show(fast.raw), fast.rawErr, show(fast.typed), fast.typedErr)
				}
			}
			for d := range 7 {
				if bound[d] < 2 {
					continue
				}
				for _, v := range []uint64{bound[d] - 1, 1 + r.Uint64()%(bound[d]-1)} {
					for _, base := range [][7]uint64{bound, caps} {
						l := base
						l[d] = v
						compareModes(t, root, nodes, fromDims(l), "below bound")
					}
				}
			}
			for k := 0; k < 4; k++ {
				var l [7]uint64
				for d := range 7 {
					switch r.Intn(5) {
					case 0:
						l[d] = caps[d]
					case 1:
						l[d] = bound[d]
					case 2:
						l[d] = bound[d] - 1
					case 3:
						l[d] = caps[d] + 1
					default:
						l[d] = uint64(r.Int63n(int64(caps[d]))) + 1
					}
				}
				compareModes(t, root, nodes, fromDims(l), "random limits")
			}
		}
		// Malformed arguments: no handle, a value of the wrong type, an
		// unknown tag, a zero size. Each argument in turn, the others valid.
		for j, typ := range root.params {
			if typeName(typ) == "UInt8" || typeName(typ) == "UInt16" || typeName(typ) == "UInt32" || typeName(typ) == "UInt64" || typeName(typ) == "Bool" {
				continue
			}
			constructors := 0
			for constructorName(typ, constructors) != "" {
				constructors++
			}
			for _, bad := range []*node{nil, apiUInt32Node(7), {typ: typ, tag: constructors, size: 1, depth: 1}, {typ: typ, size: 0, depth: 1}} {
				nodes := make([]*node, len(root.params))
				for k, typ := range root.params {
					nodes[k] = genNode(r, typ)
				}
				nodes[j] = bad
				for _, l := range []Limits{root.bound, DefaultLimits()} {
					compareModes(t, root, nodes, l, "malformed argument")
				}
			}
		}
		t.Logf("%s: bound %v, largest use observed %v, %d paths by (steps, allocations): %v", root.name, bound, seen, len(signatures), signatures)
	}
}
`

// uncoveredNodes maps the coverage profile of control.go back to IR nodes:
// every state (function id, pc) of a function reachable from a fast-path
// root whose first statement never ran.
func uncoveredNodes(t *testing.T, g *generator, dir string) ([]string, int) {
	t.Helper()
	profile, err := os.ReadFile(filepath.Join(dir, "cover.out"))
	if err != nil {
		t.Fatal(err)
	}
	type block struct{ l1, c1, l2, c2, count int }
	var blocks []block
	for _, line := range strings.Split(string(profile), "\n") {
		file, rest, ok := strings.Cut(line, ":")
		if !ok || !strings.HasSuffix(file, "/control.go") {
			continue
		}
		var bl block
		var stmts int
		if _, err := fmt.Sscanf(rest, "%d.%d,%d.%d %d %d", &bl.l1, &bl.c1, &bl.l2, &bl.c2, &stmts, &bl.count); err != nil {
			t.Fatalf("cover line %q: %v", line, err)
		}
		blocks = append(blocks, bl)
	}
	covered := func(pos token.Position) bool {
		for _, bl := range blocks {
			after := pos.Line > bl.l1 || pos.Line == bl.l1 && pos.Column >= bl.c1
			before := pos.Line < bl.l2 || pos.Line == bl.l2 && pos.Column < bl.c2
			if after && before && bl.count > 0 {
				return true
			}
		}
		return false
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "control.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	isSwitchOn := func(n ast.Node, field string) (*ast.SwitchStmt, bool) {
		s, ok := n.(*ast.SwitchStmt)
		if !ok {
			return nil, false
		}
		sel, ok := s.Tag.(*ast.SelectorExpr)
		return s, ok && sel.Sel.Name == field
	}
	wanted := map[int]bool{}
	analysis := newBoundAnalysis(g)
	for _, root := range g.fast.roots {
		functions, _ := analysis.reachable(root)
		for _, f := range functions {
			wanted[f.id] = true
		}
	}
	var missed []string
	total := 0
	ast.Inspect(file, func(n ast.Node) bool {
		outer, ok := isSwitchOn(n, "function")
		if !ok {
			return true
		}
		for _, clause := range outer.Body.List {
			cc := clause.(*ast.CaseClause)
			if len(cc.List) == 0 {
				continue
			}
			id, _ := strconv.Atoi(cc.List[0].(*ast.BasicLit).Value)
			if !wanted[id] {
				continue
			}
			for _, stmt := range cc.Body {
				inner, ok := isSwitchOn(stmt, "pc")
				if !ok {
					continue
				}
				for _, pcClause := range inner.Body.List {
					pc := pcClause.(*ast.CaseClause)
					if len(pc.List) == 0 || len(pc.Body) == 0 {
						continue
					}
					total++
					if !covered(fset.Position(pc.Body[0].Pos())) {
						missed = append(missed, fmt.Sprintf("function %d pc %s", id, pc.List[0].(*ast.BasicLit).Value))
					}
				}
			}
		}
		return false
	})
	sort.Strings(missed)
	return missed, total
}
