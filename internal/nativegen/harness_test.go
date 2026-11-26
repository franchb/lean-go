package nativegen

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/modulegen"
	"github.com/franchb/lean-go/internal/moduleir"
)

// meteredAPI is the part of the metered manifest's go_api the driver needs.
type meteredAPI struct {
	Entrypoint   string                       `json:"entrypoint"`
	Entrypoints  map[string]string            `json:"entrypoints"`
	Types        map[string]string            `json:"types"`
	Constructors map[string]string            `json:"constructors"`
	Accessors    map[string]map[string]string `json:"accessors"`
	Destructors  map[string]struct {
		Is string `json:"is"`
		As string `json:"as"`
	} `json:"destructors"`
	ByteFolds map[string]string `json:"byte_folds"`
}

func manifestAPI(t testing.TB, files map[string][]byte, into any) {
	t.Helper()
	var manifest struct {
		GoAPI json.RawMessage `json:"go_api"`
	}
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifest.GoAPI, into); err != nil {
		t.Fatal(err)
	}
}

// driver writes a differential test comparing the native package "nat" with
// the metered package "met" generated from the same IR. It uses only the two
// manifests' public name maps and the IR's type declarations.
type driver struct {
	p       *moduleir.Program
	met     meteredAPI
	nat     API
	index   map[string]int
	decls   map[string]moduleir.TypeDecl
	records map[string][]string
	b       strings.Builder
}

func newDriver(t testing.TB, p *moduleir.Program, metered, native map[string][]byte) *driver {
	d := &driver{p: p, index: map[string]int{}, decls: map[string]moduleir.TypeDecl{}, records: map[string][]string{}}
	manifestAPI(t, metered, &d.met)
	manifestAPI(t, native, &d.nat)
	for i, typ := range p.Types {
		d.index[typ.Name] = i
		d.decls[typ.Name] = typ
	}
	if p.API != nil {
		d.records = p.API.Records
	}
	return d
}

func (d *driver) printf(format string, args ...any) { fmt.Fprintf(&d.b, format, args...) }

func (d *driver) scalar(typ string) bool {
	kind := d.nat.Types[typ].Kind
	return kind == "scalar" || kind == "bool"
}

func (d *driver) natType(typ string) string {
	if d.scalar(typ) {
		return d.nat.Types[typ].Go
	}
	return "nat." + d.nat.Types[typ].Go
}

func (d *driver) metType(typ string) string {
	if d.scalar(typ) {
		return d.met.Types[typ]
	}
	return "met." + d.met.Types[typ]
}

func (d *driver) gen(typ, depth string) string {
	if d.nat.Types[typ].Kind == "bool" {
		return "r.Intn(2) == 0"
	}
	if d.scalar(typ) {
		return "gen" + strings.TrimPrefix(typ, "UInt") + "(r)"
	}
	return fmt.Sprintf("gen_t%d(r, %s)", d.index[typ], depth)
}

func (d *driver) toMet(typ, x string) string {
	if d.scalar(typ) {
		return x
	}
	return fmt.Sprintf("met_t%d(%s)", d.index[typ], x)
}

func (d *driver) eq(typ, a, b string) string {
	if d.scalar(typ) {
		return a + " == " + b
	}
	return fmt.Sprintf("eq_t%d(%s, %s)", d.index[typ], a, b)
}

func (d *driver) zero(typ string) string {
	switch d.nat.Types[typ].Kind {
	case "bool":
		return "false"
	case "scalar":
		return "0"
	}
	return d.natType(typ) + "{}"
}

func names(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return out
}

// natFields destructures native value a as constructor c into variables
// prefix0..., returning statements and a condition.
func (d *driver) natFields(typ string, c moduleir.Constructor, a, prefix string) (string, string) {
	api := d.nat.Types[typ]
	vars := names(prefix, len(c.Fields))
	if api.Kind == "record" {
		var b strings.Builder
		for i, field := range api.Fields {
			fmt.Fprintf(&b, "%s := %s.%s\n", vars[i], a, field)
		}
		return b.String(), "true"
	}
	if len(c.Fields) == 0 {
		return "", a + "." + api.Is[c.Name] + "()"
	}
	return fmt.Sprintf("%s, %sok := %s.%s()\n", strings.Join(vars, ", "), prefix, a, api.As[c.Name]), prefix + "ok"
}

func (d *driver) metFields(typ string, c moduleir.Constructor, b, prefix string) (string, string) {
	vars := names(prefix, len(c.Fields))
	if fields, record := d.records[typ]; record {
		var out strings.Builder
		var oks []string
		for i, field := range fields {
			fmt.Fprintf(&out, "%s, %sok%d := %s.%s()\n", vars[i], prefix, i, b, d.met.Accessors[typ][field])
			oks = append(oks, fmt.Sprintf("%sok%d", prefix, i))
		}
		return out.String(), strings.Join(oks, " && ")
	}
	destructor := d.met.Destructors[c.Name]
	if len(c.Fields) == 0 {
		return "", b + "." + destructor.Is + "()"
	}
	return fmt.Sprintf("%s, %sok := %s.%s()\n", strings.Join(vars, ", "), prefix, b, destructor.As), prefix + "ok"
}

func (d *driver) emitType(typ moduleir.TypeDecl) {
	i := d.index[typ.Name]
	api := d.nat.Types[typ.Name]
	nat, met := d.natType(typ.Name), d.metType(typ.Name)
	// Generator: random constructor while depth remains, then the zero
	// constructor, whose fields are finite by construction. Sometimes the Go
	// zero value itself.
	d.printf("func gen_t%d(r *rand.Rand, depth int) %s {\n", i, nat)
	if api.Kind == "record" {
		var fields []string
		for j, field := range typ.Constructors[0].Fields {
			fields = append(fields, fmt.Sprintf("%s: %s", api.Fields[j], d.gen(field, "depth-1")))
		}
		d.printf("if r.Intn(16) == 0 {\nreturn %s{}\n}\nreturn %s{%s}\n}\n", nat, nat, strings.Join(fields, ", "))
	} else {
		zero := slices.IndexFunc(typ.Constructors, func(c moduleir.Constructor) bool { return c.Name == api.Zero })
		// Deep requests on recursive types avoid the zero constructor, and
		// only the first structured field inherits the depth: a long spine
		// with small side values.
		spine := ""
		if api.Kind == "boxed" {
			spine = fmt.Sprintf("\nif depth > 16 && k == %d {\nk = (k + 1) %% %d\n}", zero, len(typ.Constructors))
		}
		d.printf("if depth < 16 && r.Intn(8) == 0 {\nreturn %s{}\n}\nk := %d\nif depth > 0 {\nk = r.Intn(%d)%s\n}\nswitch k {\n", nat, zero, len(typ.Constructors), spine)
		for k, c := range typ.Constructors {
			var args []string
			child := "depth-1"
			for _, field := range c.Fields {
				args = append(args, d.gen(field, child))
				if !d.scalar(field) {
					child = "min(depth-1, r.Intn(4))"
				}
			}
			d.printf("case %d:\nreturn nat.%s(%s)\n", k, api.Constructors[c.Name], strings.Join(args, ", "))
		}
		d.printf("}\npanic(\"unreachable\")\n}\n")
	}
	// Conversion into the metered package through its typed constructors.
	d.printf("func met_t%d(v %s) %s {\n", i, nat, met)
	for _, c := range typ.Constructors {
		statements, cond := d.natFields(typ.Name, c, "v", "a")
		var args []string
		for j, field := range c.Fields {
			args = append(args, d.toMet(field, fmt.Sprintf("a%d", j)))
		}
		args = append(args, "limits")
		d.printf("{\n%sif %s {\nout, err := met.%s(%s)\nif err != nil {\npanic(err)\n}\nreturn out\n}\n", statements, cond, d.met.Constructors[c.Name], strings.Join(args, ", "))
		for j := range c.Fields {
			d.printf("_ = a%d\n", j)
		}
		d.printf("}\n")
	}
	d.printf("panic(\"no constructor matched\")\n}\n")
	// Structural equality between a native and a metered value.
	d.printf("func eq_t%d(a %s, b %s) bool {\n", i, nat, met)
	for _, c := range typ.Constructors {
		natStatements, natCond := d.natFields(typ.Name, c, "a", "a")
		metStatements, metCond := d.metFields(typ.Name, c, "b", "b")
		checks := []string{metCond}
		for j, field := range c.Fields {
			checks = append(checks, d.eq(field, fmt.Sprintf("a%d", j), fmt.Sprintf("b%d", j)))
		}
		d.printf("{\n%sif %s {\n%sreturn %s\n}\n", natStatements, natCond, metStatements, strings.Join(checks, " && "))
		for j := range c.Fields {
			d.printf("_ = a%d\n", j)
		}
		d.printf("}\n")
	}
	d.printf("return false\n}\n")
}

func (d *driver) function(name string) moduleir.Function {
	for _, f := range d.p.Functions {
		if f.Name == name {
			return f
		}
	}
	panic(name)
}

func (d *driver) meteredEntry(root string) string {
	if d.met.Entrypoint != "" {
		return d.met.Entrypoint
	}
	return d.met.Entrypoints[root]
}

// boundary lists per width are shared with random generation.
const driverPrelude = `
var limits = met.DefaultLimits()

var bounds8 = []uint8{0, 1, 2, 3, 7, 8, 9, 15, 16, 31, 32, 63, 64, 127, 128, 129, 254, 255}
var bounds16 = []uint16{0, 1, 2, 7, 8, 15, 16, 17, 31, 32, 255, 256, 0x7fff, 0x8000, 0x8001, 0xfffe, 0xffff}
var bounds32 = []uint32{0, 1, 2, 3, 7, 8, 15, 16, 31, 32, 33, 63, 64, 255, 256, 65520, 65521, 65535, 65536, 1<<31 - 1, 1 << 31, 1<<32 - 2, 1<<32 - 1}
var bounds64 = []uint64{0, 1, 2, 31, 32, 63, 64, 65, 1<<32 - 1, 1 << 32, 1<<53 + 1, 1<<63 - 1, 1 << 63, 1<<64 - 2, 1<<64 - 1}

func gen8(r *rand.Rand) uint8 {
	if r.Intn(2) == 0 {
		return bounds8[r.Intn(len(bounds8))]
	}
	return uint8(r.Uint32())
}
func gen16(r *rand.Rand) uint16 {
	if r.Intn(2) == 0 {
		return bounds16[r.Intn(len(bounds16))]
	}
	return uint16(r.Uint32())
}
func gen32(r *rand.Rand) uint32 {
	if r.Intn(2) == 0 {
		return bounds32[r.Intn(len(bounds32))]
	}
	return r.Uint32()
}
func gen64(r *rand.Rand) uint64 {
	if r.Intn(2) == 0 {
		return bounds64[r.Intn(len(bounds64))]
	}
	return r.Uint64()
}

var _ = gen8
var _ = gen16
var _ = gen32
var _ = gen64
var _ = reflect.TypeOf
var _ = bytes.Repeat
var _ = errors.Is
`

const driverIterations = 1500

// source returns the complete driver test file.
func (d *driver) source() string {
	d.printf("// Code generated by the native-mode differential harness.\n\npackage diff\n\nimport (\n\"bytes\"\n\"errors\"\n\"math/rand\"\n\"reflect\"\n\"testing\"\n\nmet %q\nnat %q\n)\n", "difftest/"+d.p.Module+"/met", "difftest/"+d.p.Module+"/nat")
	d.b.WriteString(driverPrelude)
	elided := map[string]bool{}
	for _, name := range d.nat.Elided {
		elided[name] = true
	}
	var emitted []moduleir.TypeDecl
	for _, typ := range d.p.Types {
		if !elided[typ.Name] && !d.scalar(typ.Name) {
			d.emitType(typ)
			emitted = append(emitted, typ)
		}
	}
	for k, root := range moduleir.RootNames(d.p) {
		if d.nat.Functions[root].ByteFold != "" {
			d.emitFold(k, root)
		} else {
			d.emitRoot(k, root)
		}
	}
	d.emitHostile(emitted)
	return d.b.String()
}

func (d *driver) emitRoot(k int, root string) {
	f := d.function(root)
	native := d.nat.Functions[root]
	params := make([]string, len(f.Params))
	natArgs := names("a", len(f.Params))
	metArgs := make([]string, 0, len(f.Params)+1)
	allScalar := true
	for i, p := range f.Params {
		params[i] = fmt.Sprintf("a%d %s", i, d.natType(p.Type))
		metArgs = append(metArgs, d.toMet(p.Type, natArgs[i]))
		allScalar = allScalar && d.scalar(p.Type)
	}
	metArgs = append(metArgs, "limits")
	// check reports false, without comparing, only when the metered package
	// exhausts its resource budgets; native mode has none to exhaust.
	d.printf("\nfunc check%d(%s) bool {\nt.Helper()\n", k, strings.Join(append([]string{"t *testing.T"}, params...), ", "))
	if native.Error {
		d.printf("nv, nerr := nat.%s(%s)\n", native.Go, strings.Join(natArgs, ", "))
	} else {
		d.printf("nv := nat.%s(%s)\nvar nerr error\n", native.Go, strings.Join(natArgs, ", "))
	}
	d.printf("mv, merr := met.%s(%s)\n", d.meteredEntry(root), strings.Join(metArgs, ", "))
	d.printf("if nerr == nil && errors.Is(merr, met.ErrResource) {\nreturn false\n}\nif nerr != nil || merr != nil || !(%s) {\nt.Fatalf(\"%s%%v: native %%v, %%v; metered %%v, %%v\", []any{%s}, nv, nerr, mv, merr)\n}\nreturn true\n}\n",
		d.eq(f.Result, "nv", "mv"), root, strings.Join(natArgs, ", "))
	// Random arguments of varied depth.
	d.printf("\nfunc TestRoot%dRandom(t *testing.T) {\nr := rand.New(rand.NewSource(%d))\nfor range %d {\ndepth := r.Intn(12)\n_ = depth\n", k, 1000+k, driverIterations)
	var gens []string
	for _, p := range f.Params {
		gens = append(gens, d.gen(p.Type, "depth"))
	}
	d.printf("if !check%d(%s) {\nt.Fatal(\"metered budget exhausted\")\n}\n}\n}\n", k, strings.Join(append([]string{"t"}, gens...), ", "))
	// Values of varied large depth, within the metered default budgets.
	if !allScalar {
		d.printf("\nfunc TestRoot%dDeep(t *testing.T) {\nr := rand.New(rand.NewSource(%d))\ncompared := 0\nfor i := range 40 {\ndepth := 16 + i*i\n_ = depth\nif check%d(%s) {\ncompared++\n}\n}\nt.Logf(\"compared %%d of 40 deep values within metered budgets\", compared)\nif compared < 20 {\nt.Fatal(\"too few comparisons\")\n}\n}\n", k, 3000+k, k, strings.Join(append([]string{"t"}, gens...), ", "))
	}
	// Zero values of every argument type.
	zeros := []string{"t"}
	for _, p := range f.Params {
		zeros = append(zeros, d.zero(p.Type))
	}
	d.printf("\nfunc TestRoot%dZero(t *testing.T) {\nif !check%d(%s) {\nt.Fatal(\"metered budget exhausted\")\n}\n}\n", k, k, strings.Join(zeros, ", "))
	// Exhaustive boundary products for small scalar signatures.
	if allScalar && len(f.Params) > 0 && len(f.Params) <= 3 {
		d.printf("\nfunc TestRoot%dBoundaries(t *testing.T) {\n", k)
		for i, p := range f.Params {
			set := "[]bool{false, true}"
			if d.nat.Types[p.Type].Kind == "scalar" {
				set = "bounds" + strings.TrimPrefix(p.Type, "UInt")
			}
			d.printf("for _, a%d := range %s {\n", i, set)
		}
		d.printf("if !check%d(t, %s) {\nt.Fatal(\"metered budget exhausted\")\n}\n", k, strings.Join(natArgs, ", "))
		d.printf("%s}\n", strings.Repeat("}\n", len(f.Params)))
	}
}

func (d *driver) emitFold(k int, root string) {
	f := d.function(root)
	native := d.nat.Functions[root]
	state := f.Params[0].Type
	if _, ok := d.met.ByteFolds[root]; !ok {
		panic("metered package lacks a byte-slice API for " + root)
	}
	d.printf("\nfunc check%d(t *testing.T, state %s, data []byte, chunk int) {\nt.Helper()\n", k, d.natType(state))
	if native.Error {
		d.printf("nv, nerr := nat.%s(state, data)\nif nerr != nil {\nt.Fatal(nerr)\n}\n", native.Go)
	} else {
		d.printf("nv := nat.%s(state, data)\n", native.Go)
	}
	d.printf(`before := bytes.Clone(data)
mv := %s
// The metered package bounds each call by its resource budgets; halve the
// chunk until a call fits, as the examples carry state between calls.
for first := true; first || len(data) > 0; first = false {
	n := min(len(data), chunk)
	next, err := met.%s(mv, data[:n], limits)
	if errors.Is(err, met.ErrResource) && n > 1 {
		chunk, first = n/2, true
		continue
	}
	if err != nil {
		t.Fatalf("metered chunk of %%d bytes: %%v", n, err)
	}
	mv = next
	data = data[n:]
}
if !(%s) {
	t.Fatalf("%s over %%d bytes: native %%v, metered %%v", len(before), nv, mv)
}
}

func TestRoot%dFold(t *testing.T) {
	r := rand.New(rand.NewSource(%d))
	for _, n := range []int{0, 1, 255, 256, 257, 4095, 4096, 1 << 20} {
		data := make([]byte, n)
		r.Read(data)
		for i := range 4 {
			state := %s
			if i == 0 {
				state = %s
			}
			chunk := []int{4095, 255, 1, 4000}[i]
			if n == 1<<20 && chunk < 4000 {
				chunk = 4095
			}
			check%d(t, state, data, chunk)
		}
		check%d(t, %s, bytes.Repeat([]byte{0xff}, n), 4095)
	}
}
`, d.toMet(state, "state"), d.meteredEntry(root), d.eq(state, "nv", "mv"), root, k, 2000+k, d.gen(state, "4"), d.zero(state), k, k, d.gen(state, "4"))
}

// emitHostile checks that no generated sum or recursive type exposes a field
// that could hold an invalid tag, and that each zero value denotes the
// documented constructor.
func (d *driver) emitHostile(types []moduleir.TypeDecl) {
	d.printf("\nfunc TestHostileRepresentation(t *testing.T) {\n")
	for _, typ := range types {
		api := d.nat.Types[typ.Name]
		if api.Kind == "record" {
			d.printf("if !(eq_t%d(%s{}, met_t%d(%s{}))) {\nt.Fatal(%q)\n}\n", d.index[typ.Name], d.natType(typ.Name), d.index[typ.Name], d.natType(typ.Name), typ.Name)
			continue
		}
		d.printf("{\nrt := reflect.TypeOf(%s{})\nrv := reflect.New(rt).Elem()\nfor i := range rt.NumField() {\nif rt.Field(i).IsExported() || rv.Field(i).CanSet() {\nt.Fatalf(\"%%s exposes field %%s\", rt, rt.Field(i).Name)\n}\n}\n", d.natType(typ.Name))
		d.printf("if !(%s{}).%s() {\nt.Fatal(\"zero %s is not %s\")\n}\n}\n", d.natType(typ.Name), api.Is[api.Zero], typ.Name, api.Zero)
	}
	d.printf("}\n")
}

// harnessModule generates metered and native packages plus a driver for each
// tracked program into dir, a fresh Go module named difftest.
func harnessModule(t testing.TB, dir string, opts Options) []string {
	t.Helper()
	writePackage(t, dir, map[string][]byte{"go.mod": []byte("module difftest\n\ngo 1.27\n")})
	var modules []string
	var programs []*moduleir.Program
	for _, path := range trackedIR(t) {
		programs = append(programs, loadIR(t, path))
		if variant := reversedConstructors(loadIR(t, path)); variant != nil {
			programs = append(programs, variant)
		}
	}
	programs = append(programs, collidingTrees(t))
	for _, p := range programs {
		path := p.Module
		metered, err := modulegen.Generate(clone(t, p))
		if err != nil {
			t.Fatalf("%s metered: %v", path, err)
		}
		native, err := Generate(clone(t, p), opts)
		if err != nil {
			t.Fatalf("%s native: %v", path, err)
		}
		base := dir + "/" + p.Module
		writePackage(t, base+"/met", metered)
		writePackage(t, base+"/nat", native)
		source := newDriver(t, p, metered, native).source()
		writePackage(t, base, map[string][]byte{"diff_test.go": []byte(source)})
		modules = append(modules, p.Module)
	}
	return modules
}

func clone(t testing.TB, p *moduleir.Program) *moduleir.Program {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var out moduleir.Program
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

// reversedConstructors returns a variant of p, still valid IR, whose
// inductive types (other than Bool) list their constructors in reverse. It
// moves base cases away from tag 0, so zero values denote a later
// constructor, and it turns List UInt8 folds into ordinary recursion over a
// generated list type. It returns nil when nothing would change.
func reversedConstructors(p *moduleir.Program) *moduleir.Program {
	changed := false
	for i, typ := range p.Types {
		if typ.Name == "Bool" || len(typ.Constructors) < 2 {
			continue
		}
		slices.Reverse(typ.Constructors)
		for tag := range typ.Constructors {
			typ.Constructors[tag].Tag = tag
		}
		p.Types[i] = typ
		changed = true
	}
	if !changed {
		return nil
	}
	p.Module += "Reversed"
	return p
}

// renamed returns p with every JSON string equal to a key replaced, and the
// IR's sorted lists sorted again.
func renamed(t testing.TB, p *moduleir.Program, names map[string]string) *moduleir.Program {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(v any) any {
		switch v := v.(type) {
		case string:
			if replacement, ok := names[v]; ok {
				return replacement
			}
		case []any:
			for i := range v {
				v[i] = walk(v[i])
			}
		case map[string]any:
			for k := range v {
				v[k] = walk(v[k])
			}
		}
		return v
	}
	data, err = json.Marshal(walk(tree))
	if err != nil {
		t.Fatal(err)
	}
	var out moduleir.Program
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(out.Types, func(a, b moduleir.TypeDecl) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(out.Functions, func(a, b moduleir.Function) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(out.Provenance, func(a, b moduleir.Provenance) int { return strings.Compare(a.Name, b.Name) })
	for _, r := range out.Provenance {
		slices.Sort(r.Dependencies)
	}
	slices.Sort(out.Roots)
	if err := moduleir.Validate(clone(t, &out)); err != nil {
		t.Fatal(err)
	}
	return &out
}

// collidingTrees renames the Trees program so that a type, a constructor
// function, a root and an internal function all compete for Go names.
func collidingTrees(t testing.TB) *moduleir.Program {
	p := loadIR(t, repoRoot(t)+"/testdata/trees.ir.json")
	return renamed(t, p, map[string]string{
		"Trees":         "Collide",
		"Trees.Color":   "Trees.Mirror",
		"Trees.Tree":    "Other.MirrorRed",
		"Trees.recolor": "Trees.v1",
	})
}
