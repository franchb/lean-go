package compatibility

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/franchb/lean-go/internal/artifacts"
	"github.com/franchb/lean-go/internal/cli"
	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
)

// The demo check regenerates every proof demo (artifacts.Packages under
// examples/proofs, and the checked-frame composition) with one explicit,
// supported Lean installation, freshly checks and verifies its source
// proofs, and runs each demo's Go consumer with only Go on PATH. It
// replaced the retired scripts/check_toolchain_demos.py.

// Demo is one examples/proofs directory and its tracked packages.
type Demo struct {
	Name     string
	Packages []artifacts.Package
}

const demosDir = "examples/proofs"

// Demos groups the proof-demo packages by directory, in artifacts order.
func Demos() []Demo {
	var out []Demo
	index := map[string]int{}
	for _, spec := range artifacts.Packages {
		if spec.Contract == "" || !strings.HasPrefix(spec.Package, demosDir+"/") {
			continue
		}
		name := strings.Split(strings.TrimPrefix(spec.Package, demosDir+"/"), "/")[0]
		i, ok := index[name]
		if !ok {
			i = len(out)
			index[name] = i
			out = append(out, Demo{Name: name})
		}
		out[i].Packages = append(out[i].Packages, spec)
	}
	return out
}

// DemoOptions select the installation and checkout.
type DemoOptions struct {
	LeanBin, Repo string
	Timeout       time.Duration
	Stdout        io.Writer
}

type claim struct{ Name, Expected string }

func readObject(file string) (*pipeline.Object, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	value, err := pipeline.ParseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	o, ok := value.(*pipeline.Object)
	if !ok {
		return nil, fmt.Errorf("%s is not a JSON object", file)
	}
	return o, nil
}

func claimsOf(o *pipeline.Object, nameKey string) ([]claim, error) {
	items, ok := o.Values["claims"].([]any)
	if !ok {
		return nil, fmt.Errorf("missing claims")
	}
	var out []claim
	for _, item := range items {
		c, ok := item.(*pipeline.Object)
		if !ok {
			return nil, fmt.Errorf("invalid claim")
		}
		name, _ := c.Values[nameKey].(string)
		expected, _ := c.Values["expected"].(string)
		out = append(out, claim{name, expected})
	}
	return out, nil
}

func sortedClaims(c []claim) []claim {
	out := append([]claim(nil), c...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name+"\x00"+out[i].Expected < out[j].Name+"\x00"+out[j].Expected })
	return out
}

func canonicalOf(v any) string {
	data, _ := pipeline.EncodeCompactSorted(v)
	return string(data)
}

// checkIdentity requires a record to name the observed compiler.
func checkIdentity(label string, o *pipeline.Object, identity Identity) error {
	if o == nil || o.Values["lean_version"] != identity.Version || o.Values["lean_commit"] != identity.Commit {
		return fmt.Errorf("%s: artifacts identify a different compiler", label)
	}
	return nil
}

type demoRun struct {
	o        DemoOptions
	identity Identity
	work     string
	goPath   string
	goEnv    []string
	claims   int
}

func (d *demoRun) run(label string, command []string, dir string, env []string) (string, error) {
	result, err := devexec.Run(command, dir, env, "", d.o.Timeout)
	if err == nil && result.Code != 0 {
		err = fmt.Errorf("exit %d", result.Code)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %v: %v\n%s%s", label, command, err, result.Stderr, result.Stdout)
	}
	return result.Stdout, nil
}

// copyDemo copies a demo directory except the listed generated packages.
func copyDemo(source, destination string, skip map[string]bool) error {
	return filepath.WalkDir(source, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		if skip[filepath.ToSlash(relative)] {
			return filepath.SkipDir
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", file)
		}
		return copyFile(file, target)
	})
}

// goOnly runs the consumer's tests and program with only Go on PATH and
// returns the program output.
func (d *demoRun) goOnly(label, dir string, extra []string) (string, error) {
	env := append(append([]string(nil), d.goEnv...), extra...)
	if _, err := d.run(label+" go test", []string{d.goPath, "test", "-count=1", "-buildvcs=false", "-mod=readonly", "./..."}, dir, env); err != nil {
		return "", err
	}
	return d.run(label+" go run", []string{d.goPath, "run", "-buildvcs=false", "-mod=readonly", "."}, dir, env)
}

// nativeOracle builds a demo's native Lean reference, when it has one, and
// returns the environment entry naming its output.
func (d *demoRun) nativeOracle(demo Demo) ([]string, error) {
	source := filepath.Join(d.o.Repo, filepath.FromSlash(demosDir), demo.Name)
	if _, err := os.Stat(filepath.Join(source, "native", "lakefile.toml")); err != nil {
		return nil, nil
	}
	project := filepath.Join(d.work, demo.Name+"-native")
	if err := os.Mkdir(project, 0o755); err != nil {
		return nil, err
	}
	for _, dir := range []string{"native", "source"} {
		entries, err := os.ReadDir(filepath.Join(source, dir))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && (dir == "native" || strings.HasSuffix(entry.Name(), ".lean")) {
				if err := copyFile(filepath.Join(source, dir, entry.Name()), filepath.Join(project, entry.Name())); err != nil {
					return nil, err
				}
			}
		}
	}
	env := pipeline.ChildEnvironment(pipeline.Compiler{Version: d.identity.Version, Commit: d.identity.Commit})
	if _, err := d.run(demo.Name+" native build", []string{"lake", "build", "reference"}, project, env); err != nil {
		return nil, err
	}
	oracle := filepath.Join(project, "oracle.bin")
	if _, err := d.run(demo.Name+" native oracle", []string{filepath.Join(project, ".lake", "build", "bin", "reference"), oracle}, project, env); err != nil {
		return nil, err
	}
	return []string{"LEAN_GO_" + strings.ToUpper(demo.Name) + "_NATIVE=" + oracle}, nil
}

func (d *demoRun) checkPackage(spec artifacts.Package, deployed string) (int, error) {
	repo := d.o.Repo
	at := func(relative string) string { return filepath.Join(repo, filepath.FromSlash(relative)) }
	label := spec.Package
	ir := filepath.Join(d.work, strings.ReplaceAll(spec.Package, "/", "_")+".ir.json")
	out := filepath.Join(deployed, path.Base(spec.Package))
	if _, err := pipeline.Generate(pipeline.GenerateOptions{SourceDir: at(spec.SourceDir), Module: spec.Module, Roots: spec.Roots,
		Out: out, IROutput: ir, Emit: pipeline.EmitOptions{Mode: spec.Mode}, Cache: pipeline.DefaultCache(), StrictGo: true}); err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	evidence := filepath.Join(d.work, strings.ReplaceAll(spec.Evidence, "/", "_"))
	manifest := filepath.Join(out, "manifest.json")
	for _, o := range []pipeline.CheckProofsOptions{
		{Contract: at(spec.Contract), Manifest: manifest, Output: evidence},
		{Contract: at(spec.Contract), Manifest: manifest, Evidence: evidence},
	} {
		if _, err := pipeline.CheckProofs(o); err != nil {
			return 0, fmt.Errorf("%s: %w", label, err)
		}
	}
	program, err := readObject(ir)
	if err != nil {
		return 0, err
	}
	m, err := readObject(manifest)
	if err != nil {
		return 0, err
	}
	fresh, err := readObject(evidence)
	if err != nil {
		return 0, err
	}
	embedded, _ := m.Values["program"].(*pipeline.Object)
	for _, record := range []*pipeline.Object{program, embedded, fresh} {
		if err := checkIdentity(label, record, d.identity); err != nil {
			return 0, err
		}
	}
	tracked, err := readObject(at(spec.Evidence))
	if err != nil {
		return 0, err
	}
	contract, err := readObject(at(spec.Contract))
	if err != nil {
		return 0, err
	}
	freshClaims, err := claimsOf(fresh, "name")
	if err != nil {
		return 0, err
	}
	contractClaims, err := claimsOf(contract, "theorem")
	if err != nil {
		return 0, err
	}
	if canonicalOf(fresh.Values["claims"]) != canonicalOf(tracked.Values["claims"]) ||
		!reflect.DeepEqual(sortedClaims(freshClaims), sortedClaims(contractClaims)) {
		return 0, fmt.Errorf("%s: source theorems, propositions or axioms changed", label)
	}
	return len(freshClaims), nil
}

func (d *demoRun) checkDemo(demo Demo) error {
	source := filepath.Join(d.o.Repo, filepath.FromSlash(demosDir), demo.Name)
	deployed := filepath.Join(d.work, "fresh", demo.Name)
	skip := map[string]bool{}
	for _, spec := range demo.Packages {
		skip[path.Base(spec.Package)] = true
	}
	if err := copyDemo(source, deployed, skip); err != nil {
		return err
	}
	if err := copyDemo(source, filepath.Join(d.work, "tracked", demo.Name), nil); err != nil {
		return err
	}
	claims := 0
	for _, spec := range demo.Packages {
		n, err := d.checkPackage(spec, deployed)
		if err != nil {
			return err
		}
		claims += n
	}
	extra, err := d.nativeOracle(demo)
	if err != nil {
		return err
	}
	if err := d.compareConsumers(demo.Name, extra); err != nil {
		return err
	}
	d.claims += claims
	fmt.Fprintf(d.o.Stdout, "%s: %d fresh source proofs, Go-only tests and program passed\n", demo.Name, claims)
	return nil
}

// compareConsumers runs the consumer against the fresh packages and against
// the tracked ones: the tests must pass and the program outputs must agree.
func (d *demoRun) compareConsumers(name string, extra []string) error {
	fresh, err := d.goOnly(name+" (fresh)", filepath.Join(d.work, "fresh", name), extra)
	if err != nil {
		return err
	}
	tracked, err := d.goOnly(name+" (tracked)", filepath.Join(d.work, "tracked", name), extra)
	if err != nil {
		return err
	}
	if fresh != tracked {
		return fmt.Errorf("%s: program output with fresh packages %q differs from the tracked packages' %q", name, fresh, tracked)
	}
	return nil
}

func (d *demoRun) checkFrame() error {
	repo := d.o.Repo
	at := func(relative string) string { return filepath.Join(repo, filepath.FromSlash(relative)) }
	var pairs [][2]string
	for _, name := range artifacts.Frame.Components {
		if _, ok := artifacts.FrameComponent(name); !ok {
			return fmt.Errorf("component %s is not a tracked demo package", name)
		}
		pairs = append(pairs, [2]string{name, filepath.Join(d.work, "fresh", name, name, "manifest.json")})
	}
	evidence := filepath.Join(d.work, "checkedframe-evidence.json")
	for _, o := range []pipeline.CheckProofsOptions{
		{Contract: at(artifacts.Frame.Contract), ComponentManifests: pairs, Output: evidence},
		{Contract: at(artifacts.Frame.Contract), ComponentManifests: pairs, Evidence: evidence},
	} {
		if _, err := pipeline.CheckProofs(o); err != nil {
			return fmt.Errorf("checked frame: %w", err)
		}
	}
	fresh, err := readObject(evidence)
	if err != nil {
		return err
	}
	if err := checkIdentity("checked frame", fresh, d.identity); err != nil {
		return err
	}
	contract, err := readObject(at(artifacts.Frame.Contract))
	if err != nil {
		return err
	}
	freshClaims, err := claimsOf(fresh, "name")
	if err != nil {
		return err
	}
	contractClaims, err := claimsOf(contract, "theorem")
	if err != nil {
		return err
	}
	components, _ := fresh.Values["components"].(*pipeline.Object)
	if components == nil || !reflect.DeepEqual(sortedStrings(components.Keys), sortedStrings(artifacts.Frame.Components)) ||
		!reflect.DeepEqual(freshClaims, contractClaims) {
		return fmt.Errorf("unexpected frame claims or bound components")
	}
	trackedData, err := os.ReadFile(at(artifacts.Frame.Evidence))
	if err != nil {
		return err
	}
	freshData, _ := os.ReadFile(evidence)
	if d.identity.Version == Pinned.Version && d.identity.Commit == Pinned.Commit {
		if !bytes.Equal(freshData, trackedData) {
			return fmt.Errorf("checksummed frame composition evidence changed")
		}
	} else {
		tracked, err := readObject(at(artifacts.Frame.Evidence))
		if err != nil {
			return err
		}
		if canonicalOf(fresh.Values["claims"]) != canonicalOf(tracked.Values["claims"]) {
			return fmt.Errorf("composition claims differ from stable Lean")
		}
	}
	name := path.Base(path.Dir(artifacts.Frame.Evidence))
	for _, tree := range []string{"fresh", "tracked"} {
		if err := copyDemo(filepath.Join(repo, filepath.FromSlash(demosDir), name), filepath.Join(d.work, tree, name), nil); err != nil {
			return err
		}
	}
	if err := d.compareConsumers(name, nil); err != nil {
		return err
	}
	d.claims += len(freshClaims)
	fmt.Fprintf(d.o.Stdout, "checked frame: %d composition proofs against the fresh component manifests, Go-only tests and program passed\n", len(freshClaims))
	return nil
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// CheckDemos runs every proof demo with the installation at LeanBin. It
// changes this process's PATH and LEAN_GO_TOOLCHAIN to select it.
func CheckDemos(o DemoOptions) error {
	bin, err := CompilerBin(o.LeanBin)
	if err != nil {
		return err
	}
	output, err := exec.Command(filepath.Join(bin, "lean"), "--version").Output()
	if err != nil {
		return fmt.Errorf("lean --version: %w", err)
	}
	identity, err := ParseIdentity(string(output))
	if err != nil {
		return err
	}
	supported := false
	for _, c := range pipeline.SupportedCompilers {
		supported = supported || (c.Version == identity.Version && c.Commit == identity.Commit)
	}
	if !supported {
		return harnessError("Lean %s (%s) is not a supported compiler; qualify it with the compatibility corpus first", identity.Version, identity.Commit)
	}
	os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.Setenv("LEAN_GO_TOOLCHAIN", identity.Version)
	if code := cli.Main([]string{"doctor"}, o.Stdout, o.Stdout); code != 0 {
		return fmt.Errorf("lean-go doctor failed")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return harnessError("Go 1.27.4 must be available on PATH")
	}
	version, err := exec.Command(goPath, "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "go version go1.27.4 ") {
		return harnessError("Go 1.27.4 is required, got %s", strings.TrimSpace(string(version)))
	}
	work, err := os.MkdirTemp("", "lean-go-compiler-demos-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if work, err = filepath.EvalSymlinks(work); err != nil {
		return err
	}
	d := &demoRun{o: o, identity: identity, work: work, goPath: goPath, goEnv: goOnlyEnvironment(goPath, work)}
	for _, demo := range Demos() {
		if err := d.checkDemo(demo); err != nil {
			return err
		}
	}
	if err := d.checkFrame(); err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "Lean %s (%s): %d source claim checks passed (clamp's claims count once per package)\n", identity.Version, identity.Commit, d.claims)
	return nil
}

// goOnlyEnvironment has only Go on PATH, no proxy and private Go caches.
func goOnlyEnvironment(goPath, work string) []string {
	settings := map[string]string{
		"PATH": filepath.Dir(goPath), "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOENV": "off",
		"GOFLAGS": "", "GOPROXY": "off", "GOSUMDB": "off", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "",
		"LEAN_GO_DEMO_GO_ONLY": "1", "GOPATH": filepath.Join(work, "gopath"), "GOMODCACHE": filepath.Join(work, "modules"),
		"GOCACHE": filepath.Join(work, "go-cache"),
	}
	var env []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if _, ok := settings[key]; !ok && !strings.HasPrefix(key, "LEAN_") && !strings.HasPrefix(key, "LAKE_") {
			env = append(env, pair)
		}
	}
	for _, key := range sortedKeys(settings) {
		env = append(env, key+"="+settings[key])
	}
	return env
}
