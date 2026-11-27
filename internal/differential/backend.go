package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
)

// BackendError is a failed build, execution or output protocol: never an
// accepted sample.
type BackendError struct {
	Stage, Detail string
}

func (e *BackendError) Error() string { return e.Stage + ": " + e.Detail }

func backendError(stage, format string, args ...any) *BackendError {
	return &BackendError{Stage: stage, Detail: fmt.Sprintf(format, args...)}
}

// processFunc runs one command; tests replace it.
type processFunc func(command []string, dir string, env []string, stdin string, timeout time.Duration) (devexec.Result, error)

// Backend compiles each candidate from scratch with native Lean and with
// lean-go, retaining every command, its output and the artifacts.
type Backend struct {
	Repo, Work string
	Timeout    time.Duration
	// LastWork is the directory of the most recent check.
	LastWork string
	// ConsumerSource renders the Go consumer; fault-injection tests replace it.
	ConsumerSource func(width int) string
	// StateMachine writes the state-machine package of an IR file (default
	// EmitStateMachine); fault-injection tests replace it.
	StateMachine func(irPath, out string) error

	compiler pipeline.Compiler
	env      []string
	tools    map[string]string
	versions map[string]string
	process  processFunc
	lookPath func(string) (string, error)
}

// NewBackend prepares a backend for the selected compiler (LEAN_GO_TOOLCHAIN).
func NewBackend(repo, work string, timeout time.Duration) (*Backend, error) {
	if timeout <= 0 {
		return nil, errors.New("timeout must be a positive number of seconds")
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	work, err = filepath.Abs(work)
	if err != nil {
		return nil, err
	}
	compiler, err := pipeline.SelectedCompiler()
	if err != nil {
		return nil, err
	}
	b := &Backend{Repo: repo, Work: work, Timeout: timeout, ConsumerSource: ConsumerSource, StateMachine: EmitStateMachine, compiler: compiler,
		process: devexec.Run, lookPath: exec.LookPath}
	b.env = backendEnvironment(compiler)
	return b, nil
}

// backendEnvironment drops Lean and Lake overrides, keeps an explicit
// producer cache selection and pins the compiler and a pure, offline Go.
func backendEnvironment(c pipeline.Compiler) []string {
	pinned := map[string]string{
		"LEAN_GO_TOOLCHAIN": c.Version, "ELAN_TOOLCHAIN": c.Toolchain(), "CGO_ENABLED": "0",
		"GOTOOLCHAIN": "local", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOFLAGS": "",
	}
	var env []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if _, ok := pinned[key]; ok {
			continue
		}
		if (strings.HasPrefix(key, "LEAN_") || strings.HasPrefix(key, "LAKE_")) && key != "LEAN_GO_CACHE_DIR" && key != "LEAN_GO_NO_CACHE" {
			continue
		}
		env = append(env, pair)
	}
	for _, key := range sortedKeys(pinned) {
		env = append(env, key+"="+pinned[key])
	}
	return env
}

func withPath(env []string, path string) []string {
	out := make([]string, 0, len(env)+1)
	for _, pair := range env {
		if !strings.HasPrefix(pair, "PATH=") {
			out = append(out, pair)
		}
	}
	return append(out, "PATH="+path)
}

func lookup(env []string, key string) string {
	for _, pair := range env {
		if k, v, _ := strings.Cut(pair, "="); k == key {
			return v
		}
	}
	return ""
}

type commandRecord struct {
	Stage          string            `json:"stage"`
	Command        []string          `json:"command"`
	Cwd            string            `json:"cwd"`
	TimeoutSeconds float64           `json:"timeout_seconds"`
	GoOnly         bool              `json:"go_only"`
	Environment    map[string]string `json:"environment"`
	Returncode     *int              `json:"returncode,omitempty"`
	Stdout         string            `json:"stdout"`
	Stderr         string            `json:"stderr"`
	Error          string            `json:"error,omitempty"`
	ElapsedSeconds float64           `json:"elapsed_seconds"`
}

var loggedEnvironment = []string{"CGO_ENABLED", "GOTOOLCHAIN", "GOWORK", "GOPROXY", "ELAN_TOOLCHAIN", "LEAN_GO_TOOLCHAIN", "LEAN_GO_CACHE_DIR"}

func (b *Backend) log(record commandRecord) error {
	if b.LastWork == "" {
		return errors.New("no check directory")
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(b.LastWork, "commands.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// run executes one stage, logs it and turns any failure into a BackendError.
// A go-only command runs with a PATH that contains no producer tools.
func (b *Backend) run(stage string, command []string, dir, stdin string, goOnly bool) (string, error) {
	env := b.env
	if goOnly {
		env = withPath(env, filepath.Join(dir, "no-producer-tools"))
	}
	record := commandRecord{Stage: stage, Command: command, Cwd: dir, TimeoutSeconds: b.Timeout.Seconds(), GoOnly: goOnly,
		Environment: map[string]string{}}
	for _, key := range loggedEnvironment {
		record.Environment[key] = lookup(env, key)
	}
	start := time.Now()
	result, err := b.process(command, dir, env, stdin, b.Timeout)
	record.ElapsedSeconds = time.Since(start).Seconds()
	record.Stdout, record.Stderr = result.Stdout, result.Stderr
	if err != nil {
		record.Error = err.Error()
		if logErr := b.log(record); logErr != nil {
			return "", logErr
		}
		return "", backendError(stage, "%v", err)
	}
	code := result.Code
	record.Returncode = &code
	if err := b.log(record); err != nil {
		return "", err
	}
	if code != 0 {
		return "", backendError(stage, "exit %d: %s\n%s", code, strings.TrimSpace(result.Stderr), strings.TrimSpace(result.Stdout))
	}
	return result.Stdout, nil
}

// preflight finds the tools, checks the exact Lean and Go releases and builds
// this checkout's lean-go command once.
func (b *Backend) preflight() error {
	if b.tools != nil {
		return nil
	}
	tools := map[string]string{}
	for _, name := range []string{"go", "lean", "lake"} {
		path, err := b.lookPath(name)
		if err != nil || path == "" {
			return backendError("toolchain", "%s executable is unavailable", name)
		}
		tools[name] = path
	}
	lean, err := b.run("lean-version", []string{tools["lean"], "--version"}, b.LastWork, "", false)
	if err != nil {
		return err
	}
	if err := b.compiler.CheckVersion(lean); err != nil {
		return backendError("toolchain", "%v", err)
	}
	goVersion, err := b.run("go-version", []string{tools["go"], "version"}, b.LastWork, "", false)
	if err != nil {
		return err
	}
	goVersion = strings.TrimSpace(goVersion)
	if !strings.HasPrefix(goVersion, "go version go1.27.4 ") {
		return backendError("toolchain", "expected Go 1.27.4, got %s", goVersion)
	}
	binary := filepath.Join(b.Work, "lean-go"+exeSuffix())
	if _, err := b.run("lean-go-build", []string{tools["go"], "build", "-buildvcs=false", "-o", binary, "./cmd/lean-go"}, b.Repo, "", false); err != nil {
		return err
	}
	tools["lean-go"] = binary
	b.tools = tools
	b.versions = map[string]string{"lean": strings.TrimSpace(lean), "go": goVersion}
	return nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

var integerResult = func(v any) (uint64, *big.Int, bool) {
	n, ok := natural(v)
	if !ok || !n.IsUint64() {
		return 0, n, false
	}
	return n.Uint64(), n, true
}

// CheckedValues requires exactly count unsigned integers below 2^width,
// rejecting booleans and non-integer or rounded numbers.
func CheckedValues(value any, count, width int, stage string) ([]uint64, error) {
	items, ok := value.([]any)
	if !ok || len(items) != count {
		return nil, backendError(stage, "expected %d output values", count)
	}
	out := make([]uint64, count)
	for i, item := range items {
		n, _, ok := integerResult(item)
		if !ok || checkInput(width, n, stage) != nil {
			return nil, backendError(stage, "expected exact unsigned %d-bit integers", width)
		}
		out[i] = n
	}
	return out, nil
}

// goChannels are the consumer's result keys.
var goChannels = []string{"raw", "typed", "raw_state_machine", "typed_state_machine", "native_go"}

// DecodeResults parses the native Lean and Go consumer outputs.
func DecodeResults(native, goOutput string, count, width int) (map[string][]uint64, error) {
	leanValues, err := decodeJSON([]byte(native))
	if err != nil {
		return nil, backendError("output", "invalid result JSON: %v", err)
	}
	goValue, err := decodeJSON([]byte(goOutput))
	if err != nil {
		return nil, backendError("output", "invalid result JSON: %v", err)
	}
	goValues, ok := goValue.(map[string]any)
	if !ok || !exactKeys(goValues, goChannels...) {
		return nil, backendError("output", "Go result must contain exactly the %v arrays", goChannels)
	}
	out := map[string][]uint64{}
	if out["native"], err = CheckedValues(leanValues, count, width, "native-output"); err != nil {
		return nil, err
	}
	for _, name := range goChannels {
		if out[name], err = CheckedValues(goValues[name], count, width, name+"-output"); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ReferenceSource is the native Lean executable printing Generated.run for
// every input; IO code stays outside the extracted sources.
func ReferenceSource(width int, inputs []Pair) string {
	pairs := make([]string, len(inputs))
	for i, pair := range inputs {
		pairs[i] = fmt.Sprintf("(%d, %d)", pair[0], pair[1])
	}
	return fmt.Sprintf(`import Generated

def samples : List (UInt%[1]d × UInt%[1]d) := [
  %[2]s]

def main : IO Unit := do
  let values := samples.map (fun pair => toString (Generated.run pair.1 pair.2).toNat)
  IO.println ("[" ++ String.intercalate "," values ++ "]")
`, width, strings.Join(pairs, ",\n  "))
}

// ConsumerSource is the independent Go consumer: it runs every input
// through the raw (Value) and typed APIs of the metered package users get
// (x, whose statically bounded roots take the fast path under covering
// limits) and of the state-machine package generated from the same IR
// without the fast path (s), and through the native-mode package (n). It
// prints uint64 results (encoding/json would write a []uint8 as base64).
func ConsumerSource(width int) string {
	return strings.ReplaceAll(`package main

import (
	"encoding/json"
	"os"

	x "differentialconsumer/extracted"
	n "differentialconsumer/native"
	s "differentialconsumer/statemachine"
)

func main() {
	var inputs [][2]uintW
	if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {
		panic(err)
	}
	count := len(inputs)
	result := struct {
		Raw               []uint64 `+"`json:\"raw\"`"+`
		Typed             []uint64 `+"`json:\"typed\"`"+`
		RawStateMachine   []uint64 `+"`json:\"raw_state_machine\"`"+`
		TypedStateMachine []uint64 `+"`json:\"typed_state_machine\"`"+`
		NativeGo          []uint64 `+"`json:\"native_go\"`"+`
	}{make([]uint64, count), make([]uint64, count), make([]uint64, count), make([]uint64, count), make([]uint64, count)}
	limits := x.DefaultLimits()
	stateLimits := s.DefaultLimits()
	for i, pair := range inputs {
		args := make([]x.Value, 2)
		for j, value := range pair {
			v, err := x.Import(&x.Input{Type: "UIntW", UIntW: value}, limits)
			if err != nil {
				panic(err)
			}
			args[j] = v
		}
		raw, err := x.Run(args, limits)
		if err != nil {
			panic(err)
		}
		value, ok := raw.UIntW()
		if !ok || raw.Type() != "UIntW" || len(raw.Fields()) != 0 {
			panic("invalid scalar result")
		}
		result.Raw[i] = uint64(value)
		typed, err := x.RunTyped(pair[0], pair[1], limits)
		if err != nil {
			panic(err)
		}
		result.Typed[i] = uint64(typed)

		stateArgs := make([]s.Value, 2)
		for j, value := range pair {
			v, err := s.Import(&s.Input{Type: "UIntW", UIntW: value}, stateLimits)
			if err != nil {
				panic(err)
			}
			stateArgs[j] = v
		}
		stateRaw, err := s.Run(stateArgs, stateLimits)
		if err != nil {
			panic(err)
		}
		stateValue, ok := stateRaw.UIntW()
		if !ok || stateRaw.Type() != "UIntW" || len(stateRaw.Fields()) != 0 {
			panic("invalid state-machine scalar result")
		}
		result.RawStateMachine[i] = uint64(stateValue)
		stateTyped, err := s.RunTyped(pair[0], pair[1], stateLimits)
		if err != nil {
			panic(err)
		}
		result.TypedStateMachine[i] = uint64(stateTyped)

		result.NativeGo[i] = uint64(n.Run(pair[0], pair[1]))
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
`, "W", fmt.Sprint(width))
}

const referenceLakefile = "name = \"differential_reference\"\nversion = \"0.0.0\"\n" +
	"[[lean_lib]]\nname = \"Generated\"\n" +
	"[[lean_exe]]\nname = \"reference\"\nroot = \"Reference\"\n"

func writeFiles(files map[string]string) error {
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Check builds and runs one candidate in a fresh directory and returns the
// decoded results of every channel.
func (b *Backend) Check(p Program, inputs []Pair) (map[string][]uint64, error) {
	if err := ValidateInputs(p.Width, inputs); err != nil {
		return nil, err
	}
	source, err := RenderLean(p)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(b.Work, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(b.Work, "check-")
	if err != nil {
		return nil, err
	}
	b.LastWork = work
	sourceDir, native, consumer := filepath.Join(work, "source"), filepath.Join(work, "native"), filepath.Join(work, "consumer")
	for _, dir := range []string{sourceDir, native, consumer} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return nil, err
		}
	}
	wire, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	if err := writeFiles(map[string]string{
		filepath.Join(sourceDir, "Generated.lean"): source,
		filepath.Join(native, "Generated.lean"):    source,
		filepath.Join(native, "Reference.lean"):    ReferenceSource(p.Width, inputs),
		filepath.Join(native, "lean-toolchain"):    string(b.compiler.PinFile()),
		filepath.Join(native, "lakefile.toml"):     referenceLakefile,
		filepath.Join(work, "inputs.json"):         string(wire) + "\n",
	}); err != nil {
		return nil, err
	}
	if err := b.preflight(); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(work, "toolchain.json"), b.versions); err != nil {
		return nil, err
	}
	if _, err := b.run("native-build", []string{b.tools["lake"], "build", "reference"}, native, "", false); err != nil {
		return nil, err
	}
	nativeOutput, err := b.run("native-run", []string{filepath.Join(native, ".lake", "build", "bin", "reference"+exeSuffix())}, native, "", false)
	if err != nil {
		return nil, err
	}
	ir := filepath.Join(work, "program.ir.json")
	if _, err := b.run("extract", []string{b.tools["lean-go"], "generate", "--source-dir", sourceDir, "--module", "Generated",
		"--root", "Generated.run", "--out", filepath.Join(consumer, "extracted"), "--ir-output", ir,
		"--mode", "metered", "--strict-go"}, b.Repo, "", false); err != nil {
		return nil, err
	}
	if _, err := b.run("native-emit", []string{b.tools["lean-go"], "emit", "--in", ir, "--out", filepath.Join(consumer, "native"),
		"--mode", "native"}, b.Repo, "", false); err != nil {
		return nil, err
	}
	if err := b.emitStateMachine(ir, filepath.Join(consumer, "statemachine")); err != nil {
		return nil, err
	}
	if err := writeFiles(map[string]string{
		filepath.Join(consumer, "go.mod"):  "module differentialconsumer\n\ngo 1.27.4\n",
		filepath.Join(consumer, "main.go"): b.ConsumerSource(p.Width),
	}); err != nil {
		return nil, err
	}
	binary := filepath.Join(consumer, "consumer"+exeSuffix())
	if _, err := b.run("go-build", []string{b.tools["go"], "build", "-buildvcs=false", "-o", binary, "."}, consumer, "", false); err != nil {
		return nil, err
	}
	goOutput, err := b.run("go-run", []string{binary}, consumer, string(wire), true)
	if err != nil {
		return nil, err
	}
	results, err := DecodeResults(nativeOutput, goOutput, len(inputs), p.Width)
	if err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(work, "results.json"), results); err != nil {
		return nil, err
	}
	return results, nil
}
