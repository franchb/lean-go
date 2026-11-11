package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/gogen"
	"github.com/franchb/lean-go/internal/ir"
)

func goBinary() string {
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(runtime.GOROOT(), "bin", name)
}
func commandEnv(extra map[string]string) []string {
	settings := map[string]string{"CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOFLAGS": ""}
	for k, v := range extra {
		settings[k] = v
	}
	out := []string{}
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if _, ok := settings[key]; !ok {
			out = append(out, pair)
		}
	}
	keys := []string{}
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+settings[k])
	}
	return out
}
func directoryFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			t.Fatalf("unexpected entry %s", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = string(data)
	}
	return out
}
func TestGoOnlyConsumer(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "normalizer")
	if err := os.Mkdir(pkg, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range directoryFiles(t, "../generated/normalizer") {
		if strings.HasSuffix(name, ".go") {
			if err := os.WriteFile(filepath.Join(pkg, name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module pilotconsumer\n\ngo 1.27.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package main
import("fmt";"os/exec";n "pilotconsumer/normalizer")
func main(){
for _,name:=range []string{"lean","lake","python3","cc","gcc"}{if _,err:=exec.LookPath(name);err==nil{panic("unexpected producer executable in consumer PATH")}}
input:=&n.Input{Kind:n.Not,Left:&n.Input{Kind:n.And,Left:&n.Input{Kind:n.Variable,ID:^uint32(0)},Right:&n.Input{Kind:n.Literal,Value:n.Unknown}}}
owned,err:=n.Import(input,n.DefaultLimits());if err!=nil{panic(err)}
input.Left.Left.ID=0
out,err:=n.Normalize(owned,n.DefaultLimits());if err!=nil{panic(err)}
left,right:=out.Children();variable,_:=left.Children();id,ok:=variable.Variable();tri,lit:=right.Literal()
if out.Kind()!=n.Or||left.Kind()!=n.Not||!ok||id!=^uint32(0)||!lit||tri!=n.Unknown{panic("consumer result mismatch")}
fmt.Println("pure-Go consumer: OK")
}`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBinary(), "run", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(map[string]string{"PATH": filepath.Dir(goBinary())})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated consumer: %v\n%s", err, output)
	}
	if string(output) != "pure-Go consumer: OK\n" {
		t.Fatalf("unexpected consumer output: %s", output)
	}
}
func readProgram(t *testing.T) *ir.Program {
	t.Helper()
	data, err := os.ReadFile("../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ir.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func mutateNegation(c *ir.Code) bool {
	if c == nil {
		return false
	}
	if c.Op == "let" && c.Value.Kind == "constructor" && c.Value.Name == "LeanGo.Feasibility.Tri.t" {
		c.Value.Name = "LeanGo.Feasibility.Tri.f"
		return true
	}
	if mutateNegation(c.Next) {
		return true
	}
	for i := range c.Alts {
		if mutateNegation(c.Alts[i].Body) {
			return true
		}
	}
	return false
}
func TestConformanceRejectsWrongPrimitive(t *testing.T) {
	program := readProgram(t)
	changed := false
	for i := range program.Functions {
		if program.Functions[i].Name == "LeanGo.Feasibility.Tri.neg" {
			changed = mutateNegation(program.Functions[i].Body)
		}
	}
	if !changed {
		t.Fatal("mutation target absent")
	}
	files, err := gogen.Generate(program)
	if err != nil {
		t.Fatalf("typed mutation unexpectedly inadmissible: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "mutant")
	if err = gogen.Publish(dir, files); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module mutant\n\ngo 1.27.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package normalizer
import "testing"
func TestNegationConformance(t *testing.T){
e,err:=Import(&Input{Kind:Not,Left:&Input{Kind:Literal,Value:False}},DefaultLimits());if err!=nil{t.Fatal(err)}
out,err:=Normalize(e,DefaultLimits());if err!=nil{t.Fatal(err)}
v,ok:=out.Literal();if !ok||v!=True{t.Fatal("negation conformance mismatch")}
}`
	if err = os.WriteFile(filepath.Join(dir, "conformance_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBinary(), "test", ".", "-run", "^TestNegationConformance$")
	cmd.Dir = dir
	cmd.Env = commandEnv(nil)
	output, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("negation conformance mismatch")) || bytes.Contains(output, []byte("build failed")) {
		t.Fatalf("mutation did not fail intended conformance check: %v\n%s", err, output)
	}
}
func TestRegeneration(t *testing.T) {
	if os.Getenv("LEAN_GO_REGENERATE") != "1" {
		t.Skip("set LEAN_GO_REGENERATE=1 for two fresh producer builds")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var first map[string]string
	var firstIR []byte
	for i := 0; i < 2; i++ {
		out := filepath.Join(dir, fmt.Sprintf("package%d", i))
		irPath := filepath.Join(dir, fmt.Sprintf("ir%d.json", i))
		cmd := exec.Command("uv", "run", "--locked", "python", filepath.Join(root, "scripts/generate.py"), "--out", out, "--ir-output", irPath)
		cmd.Dir = root
		cmd.Env = commandEnv(nil)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("regeneration %d: %v\n%s", i, err, output)
		}
		files := directoryFiles(t, out)
		body, err := os.ReadFile(irPath)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = files
			firstIR = body
		} else {
			sameFiles(t, first, files)
			if !bytes.Equal(firstIR, body) {
				t.Fatal("fresh IR bytes differ")
			}
		}
	}
	sameFiles(t, first, directoryFiles(t, "../generated/normalizer"))
	fixture, err := os.ReadFile("../testdata/normalizer.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstIR, fixture) {
		t.Fatal("tracked IR fixture is stale")
	}
}
func sameFiles(t *testing.T, a, b map[string]string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatal("generated file set differs")
	}
	for name, data := range a {
		if b[name] != data {
			t.Fatalf("generated bytes differ: %s", name)
		}
	}
}
func TestFailedGenerationPreservesPackage(t *testing.T) {
	p := readProgram(t)
	files, err := gogen.Generate(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "package")
	if err = gogen.Publish(out, files); err != nil {
		t.Fatal(err)
	}
	before := directoryFiles(t, out)
	// Valid JSON but an unsupported reachable operation must fail before publication.
	p.Functions[0].Body.Op = "unreachable"
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bad.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBinary(), "run", "./cmd/lean-go", "-in", path, "-out", out)
	cmd.Dir = root
	cmd.Env = commandEnv(nil)
	output, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("lean-go:")) {
		t.Fatalf("invalid IR unexpectedly succeeded or failed outside CLI: %v\n%s", err, output)
	}
	sameFiles(t, before, directoryFiles(t, out))
}
