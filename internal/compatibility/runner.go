package compatibility

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
)

// ReportSchema identifies report.json.
const ReportSchema = "lean-go-compatibility/v1"

// envKeys are the environment values a command log records; the process
// environment is never dumped.
var envKeys = []string{"PATH", "CGO_ENABLED", "GOTOOLCHAIN", "GOWORK", "GOPROXY", "GOSUMDB",
	"GOENV", "GOFLAGS", "ELAN_TOOLCHAIN", "LEAN_GO_TOOLCHAIN", "LEAN_GO_CACHE_DIR"}

// defaultPath is the fallback search path after the compiler and Go (Python's os.defpath).
const defaultPath = "/bin:/usr/bin"

// WriteJSON writes value with sorted keys, two-space indentation, unescaped
// non-ASCII and a final newline.
func WriteJSON(path string, value any) error {
	data, err := encodeJSON(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func encodeJSON(value any) ([]byte, error) {
	var b strings.Builder
	if err := EncodeJSON(&b, value); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// EncodeJSON writes value as WriteJSON does.
func EncodeJSON(w io.Writer, value any) error {
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	return e.Encode(value)
}

// Environment is the environment of every command: no Lean, Lake or elan
// overrides except an explicit producer cache selection, no cross-compiling,
// the explicit compiler first on PATH, and a pure offline Go.
func Environment(binDir, goPath string) map[string]string {
	env := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, _ := strings.Cut(pair, "=")
		if key == "LEAN_GO_CACHE_DIR" || key == "LEAN_GO_NO_CACHE" {
			env[key] = value
			continue
		}
		if strings.HasPrefix(key, "LEAN_") || strings.HasPrefix(key, "LAKE_") || strings.HasPrefix(key, "ELAN_") || key == "GOOS" || key == "GOARCH" {
			continue
		}
		env[key] = value
	}
	for key, value := range map[string]string{
		"PATH":        strings.Join([]string{binDir, filepath.Dir(goPath), defaultPath}, string(os.PathListSeparator)),
		"CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOPROXY": "off",
		"GOSUMDB": "off", "GOENV": "off", "GOFLAGS": "",
	} {
		env[key] = value
	}
	return env
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for _, key := range sortedKeys(env) {
		out = append(out, key+"="+env[key])
	}
	return out
}

func withEnv(env map[string]string, key, value string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	out[key] = value
	return out
}

// CommandResult is one finished command; Code is -1 for a timeout, signal
// or start failure.
type CommandResult struct {
	Code           int
	Stdout, Stderr string
}

// Commands runs and logs the harness commands. Run is replaceable in tests.
type Commands struct {
	Timeout time.Duration
	Run     func(stage string, argv []string, cwd string, env map[string]string, logs string) CommandResult
}

// NewCommands logs every command to commands.jsonl in its logs directory.
func NewCommands(timeout time.Duration) *Commands {
	c := &Commands{Timeout: timeout}
	c.Run = c.execute
	return c
}

type commandRecord struct {
	Stage          string            `json:"stage"`
	Argv           []string          `json:"argv"`
	Cwd            string            `json:"cwd"`
	TimeoutSeconds float64           `json:"timeout_seconds"`
	Environment    map[string]string `json:"environment"`
	Error          string            `json:"error,omitempty"`
	Returncode     int               `json:"returncode"`
	Stdout         string            `json:"stdout"`
	Stderr         string            `json:"stderr"`
	ElapsedSeconds float64           `json:"elapsed_seconds"`
}

// process is replaceable in tests.
var process = devexec.Run

func (c *Commands) execute(stage string, argv []string, cwd string, env map[string]string, logs string) CommandResult {
	record := commandRecord{Stage: stage, Argv: argv, Cwd: cwd, TimeoutSeconds: c.Timeout.Seconds(), Environment: map[string]string{}}
	for _, key := range envKeys {
		if value, ok := env[key]; ok {
			record.Environment[key] = value
		}
	}
	start := time.Now()
	result, err := process(argv, cwd, envList(env), "", c.Timeout)
	if err != nil {
		record.Error = err.Error()
		result.Code = -1
	}
	record.Returncode, record.Stdout, record.Stderr = result.Code, result.Stdout, result.Stderr
	record.ElapsedSeconds = time.Since(start).Seconds()
	if line, err := json.Marshal(record); err == nil {
		if f, err := os.OpenFile(filepath.Join(logs, "commands.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			f.Write(append(line, '\n'))
			f.Close()
		}
	}
	return CommandResult{Code: result.Code, Stdout: result.Stdout, Stderr: result.Stderr}
}

func checkedOutput(result CommandResult, label string) (string, error) {
	if result.Code != 0 {
		return "", harnessError("%s failed; inspect commands.jsonl", label)
	}
	return result.Stdout, nil
}

// ObserveCompiler records the explicit installation's identity and requires
// Lake's Lean to be the same compiler and the prefix to be the installation.
func ObserveCompiler(binDir, workspace string, env map[string]string, commands *Commands, logs string) (Identity, error) {
	direct, err := checkedOutput(commands.Run("lean-version", []string{filepath.Join(binDir, "lean"), "--version"}, workspace, env, logs), "lean identity")
	if err != nil {
		return Identity{}, err
	}
	identity, err := ParseIdentity(direct)
	if err != nil {
		return Identity{}, err
	}
	env["ELAN_TOOLCHAIN"] = "leanprover/lean4:v" + identity.Version
	env["LEAN_GO_TOOLCHAIN"] = identity.Version
	throughLake, err := checkedOutput(commands.Run("lake-lean-version", []string{filepath.Join(binDir, "lake"), "env", "lean", "--version"}, workspace, env, logs), "lake Lean identity")
	if err != nil {
		return Identity{}, err
	}
	if _, err := MatchingIdentity(direct, throughLake); err != nil {
		return Identity{}, err
	}
	prefix, err := checkedOutput(commands.Run("lean-prefix", []string{filepath.Join(binDir, "lean"), "--print-prefix"}, workspace, env, logs), "Lean prefix")
	if err != nil {
		return Identity{}, err
	}
	prefix = strings.TrimSpace(prefix)
	resolved, err := filepath.EvalSymlinks(prefix)
	if err != nil || resolved != filepath.Dir(binDir) {
		return Identity{}, harnessError("explicit compiler directory disagrees with lean --print-prefix")
	}
	lake, err := checkedOutput(commands.Run("lake-version", []string{filepath.Join(binDir, "lake"), "--version"}, workspace, env, logs), "Lake identity")
	if err != nil {
		return Identity{}, err
	}
	identity.LeanOutput, identity.LakeLeanOutput, identity.LakeOutput = direct, throughLake, lake
	identity.Bin, identity.Prefix, identity.SHA256 = binDir, prefix, map[string]string{}
	install := filepath.Dir(binDir)
	for _, name := range []string{"bin/lean", "bin/lake", "lib/lean/Init.olean", "lib/lean/Lean.olean", "lib/lean/Init/Data/UInt/Package.olean"} {
		data, err := os.ReadFile(filepath.Join(install, filepath.FromSlash(name)))
		if err != nil {
			return Identity{}, err
		}
		identity.SHA256[name] = Digest(data)
	}
	return identity, nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return harnessError("fixture %s is not a regular file", file)
		}
		return copyFile(file, target)
	})
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

// Runner holds the commands of one run and the steps tests replace.
type Runner struct {
	Commands *Commands
	observe  func(binDir, workspace string, env map[string]string, commands *Commands, logs string) (Identity, error)
	runCase  func(r *Runner, c Case, corpus, workspace, role string, identity Identity, leanGo, goPath string, env map[string]string) (Outcome, error)
}

// NewRunner returns a runner with real commands.
func NewRunner(timeout time.Duration) *Runner {
	return &Runner{Commands: NewCommands(timeout), observe: ObserveCompiler, runCase: (*Runner).RunCase}
}

// RunCase extracts one fixture and checks its admission outcome or, for an
// accepted case, its IR shapes, native oracle and independent consumer.
func (r *Runner) RunCase(c Case, corpus, workspace, role string, identity Identity, leanGo, goPath string, env map[string]string) (Outcome, error) {
	directory := filepath.Join(role, "cases", c.Name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	fixture := filepath.Join(corpus, c.Name)
	source := filepath.Join(directory, "source")
	sourceName := "source"
	for _, version := range c.SourceVersions {
		if version == identity.Version {
			sourceName = version
		}
	}
	if err := copyTree(filepath.Join(fixture, sourceName), source); err != nil {
		return nil, err
	}
	if len(c.SourceVersions) > 0 {
		// Establish that the chosen standalone caller compiles before checking admission.
		module := filepath.FromSlash(strings.ReplaceAll(c.Module, ".", "/")) + ".lean"
		control := r.Commands.Run("lean-typecheck", []string{filepath.Join(identity.Bin, "lean"), module}, source, env, directory)
		if control.Code != 0 {
			outcome := Outcome{"status": "source_typecheck_failed", "fixture_source": sourceName}
			return outcome, WriteJSON(filepath.Join(directory, "result.json"), outcome)
		}
	}
	consumer := filepath.Join(directory, "consumer")
	if err := os.MkdirAll(consumer, 0o755); err != nil {
		return nil, err
	}
	command := []string{leanGo, "generate", "--diagnostics", "json", "--source-dir", source, "--module", c.Module,
		"--out", filepath.Join(consumer, "extracted"), "--ir-output", filepath.Join(directory, "program.ir.json"),
		"--mode", "metered", "--strict-go"}
	for _, root := range c.Roots {
		command = append(command, "--root", root)
	}
	result := r.Commands.Run("extract", command, workspace, env, directory)
	outcome := ExtractionOutcome(result, c.Reject, c.RejectDeclaration)
	if outcome["status"] == "extracted" {
		var err error
		if outcome, err = r.CheckAccepted(c, fixture, directory, consumer, identity, goPath, env); err != nil {
			return nil, err
		}
	}
	if len(c.SourceVersions) > 0 {
		outcome["fixture_source"], outcome["source_typecheck"] = sourceName, "passed"
	}
	return outcome, WriteJSON(filepath.Join(directory, "result.json"), outcome)
}

// BuildNativeOracle compiles the fixture's native reference from the same
// source that produced the IR and writes consumer/oracle.bin. It returns a
// failure status, or "" on success. Lake caches stay out of the evidence.
func (r *Runner) BuildNativeOracle(fixture, directory, consumer string, identity Identity, env map[string]string) (string, error) {
	project, err := os.MkdirTemp("", "lean-go-native-oracle-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(project)
	if err := copyTree(filepath.Join(directory, "source"), project); err != nil {
		return "", err
	}
	for _, name := range []string{"Reference.lean", "lakefile.toml"} {
		if _, err := os.Stat(filepath.Join(project, name)); err == nil {
			return "", harnessError("native oracle conflicts with fixture source: %s", name)
		}
		if err := copyFile(filepath.Join(fixture, "native", name), filepath.Join(project, name)); err != nil {
			return "", err
		}
	}
	built := r.Commands.Run("native-oracle-build", []string{filepath.Join(identity.Bin, "lake"), "build", "reference"}, project, env, directory)
	if built.Code != 0 {
		return "native_oracle_build_failed", nil
	}
	binary := filepath.Join(project, ".lake", "build", "bin", "reference")
	oracle := filepath.Join(consumer, "oracle.bin")
	ran := r.Commands.Run("native-oracle-run", []string{binary, oracle}, project, env, directory)
	if info, err := os.Stat(oracle); ran.Code != 0 || err != nil || !info.Mode().IsRegular() {
		return "native_oracle_failed", nil
	}
	return "", nil
}

// CheckAccepted validates a successful extraction: compiler identity,
// canonical IR, required shapes, the native oracle and the fixture's
// independent Go consumer, run with no compiler tools on PATH.
func (r *Runner) CheckAccepted(c Case, fixture, directory, consumer string, identity Identity, goPath string, env map[string]string) (Outcome, error) {
	data, err := os.ReadFile(filepath.Join(directory, "program.ir.json"))
	if err != nil {
		return Outcome{"status": "extraction_protocol_failed", "detail": err.Error()}, nil
	}
	program, err := pipeline.ParseJSON(data)
	var canonical string
	var missing []string
	if err == nil {
		o, ok := program.(*pipeline.Object)
		if !ok || o.Values["lean_version"] != identity.Version || o.Values["lean_commit"] != identity.Commit {
			err = harnessError("successful IR has a different compiler identity")
		}
	}
	if err == nil {
		canonical, err = CanonicalIR(program)
	}
	if err == nil {
		missing, err = IRShapes(program, c.Shapes)
	}
	if err != nil {
		return Outcome{"status": "extraction_protocol_failed", "detail": err.Error()}, nil
	}
	if err := os.WriteFile(filepath.Join(directory, "program.canonical.json"), []byte(canonical), 0o644); err != nil {
		return nil, err
	}
	status := "accepted"
	if len(missing) > 0 {
		status = "fixture_shape_changed"
	}
	outcome := Outcome{"status": status, "canonical_ir_sha256": Digest([]byte(canonical)), "missing_shapes": missing}
	if info, err := os.Stat(filepath.Join(fixture, "native")); err == nil && info.IsDir() {
		failure, err := r.BuildNativeOracle(fixture, directory, consumer, identity, env)
		if err != nil {
			return nil, err
		}
		if failure != "" {
			outcome["status"] = failure
			return outcome, nil
		}
	}
	// Even a shape change retains independent behavior evidence when emission succeeded.
	if err := copyFile(filepath.Join(fixture, "consumer_test.go.txt"), filepath.Join(consumer, "consumer_test.go")); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(consumer, "go.mod"), []byte("module compatconsumer\n\ngo 1.27.4\n"), 0o644); err != nil {
		return nil, err
	}
	binary := filepath.Join(consumer, "consumer.test")
	built := r.Commands.Run("go-consumer-build", []string{goPath, "test", "-c", "-o", binary, "."}, consumer, env, directory)
	if built.Code != 0 {
		outcome["status"] = "consumer_build_failed"
		return outcome, nil
	}
	runtimeEnv := withEnv(env, "PATH", filepath.Join(consumer, "no-compiler-tools"))
	executed := r.Commands.Run("go-consumer-run", []string{binary, "-test.v"}, consumer, runtimeEnv, directory)
	if executed.Code == 0 {
		outcome["behavior"] = "passed"
	} else {
		outcome["behavior"] = "failed"
		outcome["status"] = "behavior_mismatch"
	}
	return outcome, nil
}

// RoleResult is result.json of one compiler run.
type RoleResult struct {
	Status               string             `json:"status"`
	Identity             Identity           `json:"identity"`
	Adaptations          Adaptations        `json:"adaptations"`
	Cases                map[string]Outcome `json:"cases"`
	NumericBoundaryProbe map[string]any     `json:"numeric_boundary_probe"`
}

// RunCompiler runs the producer and every case with one compiler. The
// producer is the Lean producer library and the lean-go command built from
// the snapshot; if either fails to build, every case stays not_run.
func (r *Runner) RunCompiler(role, binDir string, snapshot Snapshot, corpus string, cases []Case, goPath string, baseline bool) (*RoleResult, error) {
	if err := os.Mkdir(role, 0o755); err != nil {
		return nil, err
	}
	workspace := filepath.Join(role, "producer")
	if err := WriteSnapshot(workspace, snapshot); err != nil {
		return nil, err
	}
	env := Environment(binDir, goPath)
	identity, err := r.observe(binDir, filepath.Join(workspace, "producer"), env, r.Commands, role)
	if err != nil {
		return nil, err
	}
	if baseline && (identity.Version != Pinned.Version || identity.Commit != Pinned.Commit) {
		return nil, harnessError("baseline must be exactly the production Lean version and commit")
	}
	adapted, adaptations, patch, err := AdaptPins(snapshot, identity)
	if err != nil {
		return nil, err
	}
	for _, name := range adapted.Names() {
		if string(adapted[name]) != string(snapshot[name]) {
			if err := os.WriteFile(filepath.Join(workspace, filepath.FromSlash(name)), adapted[name], 0o644); err != nil {
				return nil, err
			}
		}
	}
	for name, value := range map[string]any{"identity.json": identity, "adaptations.json": adaptations} {
		if err := WriteJSON(filepath.Join(role, name), value); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(role, "adaptation.patch"), []byte(patch), 0o644); err != nil {
		return nil, err
	}
	result := &RoleResult{Identity: identity, Adaptations: adaptations, Cases: map[string]Outcome{}}
	for _, c := range cases {
		result.Cases[c.Name] = Outcome{"status": "not_run"}
	}
	// A focused upstream-name observation is retained even if the producer cannot build.
	probe := filepath.Join(filepath.Dir(corpus), "reproducers", "NumericBoundaryName.lean")
	if err := copyFile(probe, filepath.Join(role, filepath.Base(probe))); err != nil {
		return nil, err
	}
	observed := r.Commands.Run("numeric-boundary-probe", []string{filepath.Join(binDir, "lean"), filepath.Join(role, filepath.Base(probe))},
		filepath.Join(workspace, "producer"), env, role)
	result.NumericBoundaryProbe = map[string]any{"returncode": observed.Code, "stdout": observed.Stdout, "stderr": observed.Stderr}
	leanGo := filepath.Join(role, "lean-go"+exeSuffix())
	build := r.Commands.Run("producer-build", []string{filepath.Join(binDir, "lake"), "build", "LeanGo.Module.Export"},
		filepath.Join(workspace, "producer"), env, role)
	if build.Code == 0 {
		build = r.Commands.Run("lean-go-build", []string{goPath, "build", "-buildvcs=false", "-o", leanGo, "./cmd/lean-go"}, workspace, env, role)
	}
	if build.Code != 0 {
		result.Status = "producer_build_failed"
	} else {
		result.Status = "passed"
		for _, c := range cases {
			outcome, err := r.runCase(r, c, corpus, workspace, role, identity, leanGo, goPath, env)
			if err != nil {
				return nil, err
			}
			result.Cases[c.Name] = outcome
			if status, _ := outcome["status"].(string); !PassStatuses[status] {
				result.Status = "fixture_failed"
			}
		}
	}
	return result, WriteJSON(filepath.Join(role, "result.json"), result)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// shellQuote quotes one word for a POSIX shell.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func formatSeconds(timeout time.Duration) string {
	return strconv.FormatFloat(timeout.Seconds(), 'f', -1, 64)
}

// ReplayScript is replay.sh: it rebuilds the harness from the retained
// source snapshot next to it and reruns the same comparison with the same
// existing compiler installations into a NEW output directory. Arguments
// are single-quoted, so no recorded path is expanded by the shell.
func ReplayScript(baselineBin, candidateBin, caseName string, timeout time.Duration) string {
	arguments := []string{"--baseline-bin", baselineBin, "--timeout", formatSeconds(timeout)}
	if candidateBin != "" {
		arguments = append(arguments, "--candidate-bin", candidateBin)
	}
	if caseName != "" {
		arguments = append(arguments, "--case", caseName)
	}
	quoted := make([]string, len(arguments))
	for i, argument := range arguments {
		quoted[i] = shellQuote(argument)
	}
	return `#!/bin/sh
# Replay with the same existing tool installations and a NEW output directory.
set -eu
if [ "$#" -ne 1 ]; then
  echo "usage: sh replay.sh NEW_ARTIFACT_DIRECTORY" >&2
  exit 2
fi
case "$1" in
  /*) artifacts=$1 ;;
  *) artifacts=$(pwd)/$1 ;;
esac
root=$(cd "$(dirname "$0")" && pwd)
cd "$root/source"
CGO_ENABLED=0 GOTOOLCHAIN=local GOWORK=off GOFLAGS= exec go run -buildvcs=false ./cmd/lean-go-dev compatibility \
  ` + strings.Join(quoted, " ") + ` --artifacts "$artifacts"
`
}

// RetainedEvidence lists replay evidence without caches and executables.
func RetainedEvidence(artifacts, caseName string) ([]string, error) {
	var out []string
	for _, name := range []string{"report.json", "inputs.json"} {
		out = append(out, filepath.Join(artifacts, name))
	}
	walk := func(root string, skip func(string) bool) error {
		var files []string
		err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if entry.Type().IsRegular() && !skip(file) {
				files = append(files, file)
			}
			return nil
		})
		sort.Strings(files)
		out = append(out, files...)
		return err
	}
	if err := walk(filepath.Join(artifacts, "source"), func(string) bool { return false }); err != nil {
		return nil, err
	}
	for _, role := range []string{"baseline", "candidate"} {
		for _, name := range []string{"identity.json", "adaptations.json", "adaptation.patch", "commands.jsonl", "result.json"} {
			file := filepath.Join(artifacts, role, name)
			if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
				out = append(out, file)
			}
		}
		if caseName == "" {
			continue
		}
		skip := func(file string) bool {
			base := filepath.Base(file)
			return base == "consumer.test" || strings.Contains(filepath.ToSlash(file), "/.lake/")
		}
		if err := walk(filepath.Join(artifacts, role, "cases", caseName), skip); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func addFile(archive *zip.Writer, name string, data []byte, mode os.FileMode) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	w, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// DifferenceBundles writes differences/<case>/ for every difference: the
// difference, an IR diff for canonical drift and a small source-only
// replay.zip. Build caches and consumer executables are never included.
func DifferenceBundles(artifacts string, differences []Difference, baselineBin, candidateBin string, timeout time.Duration) error {
	directory := filepath.Join(artifacts, "differences")
	if err := os.Mkdir(directory, 0o755); err != nil {
		return err
	}
	for _, difference := range differences {
		name := difference.Case
		if name == "" {
			name = "producer"
		}
		bundle := filepath.Join(directory, name)
		if err := os.Mkdir(bundle, 0o755); err != nil {
			return err
		}
		if err := WriteJSON(filepath.Join(bundle, "difference.json"), difference); err != nil {
			return err
		}
		if difference.Kind == "canonical_ir_drift" {
			before, err := os.ReadFile(filepath.Join(artifacts, "baseline", "cases", difference.Case, "program.canonical.json"))
			if err != nil {
				return err
			}
			after, err := os.ReadFile(filepath.Join(artifacts, "candidate", "cases", difference.Case, "program.canonical.json"))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(bundle, "ir.diff"), []byte(IRDiff(string(before), string(after))), 0o644); err != nil {
				return err
			}
		}
		if err := writeBundle(artifacts, bundle, difference.Case, baselineBin, candidateBin, timeout); err != nil {
			return err
		}
	}
	return nil
}

func writeBundle(artifacts, bundle, caseName, baselineBin, candidateBin string, timeout time.Duration) error {
	f, err := os.Create(filepath.Join(bundle, "replay.zip"))
	if err != nil {
		return err
	}
	defer f.Close()
	archive := zip.NewWriter(f)
	if err := addFile(archive, "replay.sh", []byte(ReplayScript(baselineBin, candidateBin, caseName, timeout)), 0o755); err != nil {
		return err
	}
	files, err := RetainedEvidence(artifacts, caseName)
	if err != nil {
		return err
	}
	for _, extra := range []string{"difference.json", "ir.diff"} {
		files = append(files, filepath.Join(bundle, extra))
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if errors.Is(err, fs.ErrNotExist) && strings.HasPrefix(file, bundle) {
			continue
		}
		if err != nil {
			return err
		}
		name, err := filepath.Rel(artifacts, file)
		if err != nil {
			return err
		}
		if strings.HasPrefix(file, bundle) {
			name = filepath.Base(file)
		}
		if err := addFile(archive, filepath.ToSlash(name), data, 0o644); err != nil {
			return err
		}
	}
	return archive.Close()
}

// resolveLexical resolves symlinks in the existing prefix of path.
func resolveLexical(path string) string {
	existing, rest := path, ""
	for {
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return path
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

func within(path, base string) bool {
	relative, err := filepath.Rel(base, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ValidateOutput requires a new artifact path without symlinks that does not
// overlap the checkout or a compiler installation.
func ValidateOutput(artifacts, repo string, bins []string) (string, error) {
	requested, err := filepath.Abs(artifacts)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(requested); err == nil || resolveLexical(requested) != requested {
		return "", harnessError("artifact directory must be a new path without symlinks")
	}
	protected := []string{repo}
	for _, bin := range bins {
		protected = append(protected, filepath.Dir(bin))
	}
	for _, p := range protected {
		p = resolveLexical(p)
		if within(requested, p) || within(p, requested) {
			return "", harnessError("artifact directory must not overlap source or compiler installations")
		}
	}
	return requested, nil
}

// Options are the inputs of one compatibility run.
type Options struct {
	BaselineBin, CandidateBin string
	Artifacts, Case           string
	Timeout                   time.Duration
}

// Report is report.json.
type Report struct {
	Schema                string            `json:"schema"`
	Status                string            `json:"status"`
	Differences           []Difference      `json:"differences"`
	CanonicalIRExclusions []string          `json:"canonical_ir_exclusions"`
	Scope                 string            `json:"scope"`
	SelectedCases         []string          `json:"selected_cases"`
	HostTools             map[string]string `json:"host_tools"`
	Baseline              *RoleResult       `json:"baseline,omitempty"`
	Candidate             *RoleResult       `json:"candidate,omitempty"`
	Error                 string            `json:"error,omitempty"`
	ExitCode              int               `json:"exit_code"`
}

// Run compares the candidate with the baseline (or runs only the baseline)
// and returns the exit status and the report path: 0 compatible or baseline
// passed, 1 a candidate difference, 2 a harness or baseline failure.
func Run(o Options, repo string) (int, string, error) {
	if o.Timeout <= 0 {
		return 2, "", harnessError("--timeout must be finite and positive")
	}
	baselineBin, err := CompilerBin(o.BaselineBin)
	if err != nil {
		return 2, "", err
	}
	candidateBin := ""
	if o.CandidateBin != "" {
		if candidateBin, err = CompilerBin(o.CandidateBin); err != nil {
			return 2, "", err
		}
	}
	bins := []string{baselineBin}
	if candidateBin != "" {
		bins = append(bins, candidateBin)
	}
	artifacts, err := ValidateOutput(o.Artifacts, repo, bins)
	if err != nil {
		return 2, "", err
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return 2, "", harnessError("Go 1.27.4 must be available on PATH")
	}
	if goPath, err = filepath.Abs(goPath); err == nil {
		goPath, err = filepath.EvalSymlinks(goPath)
	}
	if err != nil {
		return 2, "", err
	}
	snapshot, err := SourceSnapshot(repo)
	if err != nil {
		return 2, "", err
	}
	inputs, err := SnapshotInputs(repo, snapshot)
	if err != nil {
		return 2, "", err
	}
	manifest, err := pipeline.ParseJSON(inputs["compatibility/corpus/manifest.json"])
	if err != nil {
		return 2, "", harnessError("corpus manifest: %v", err)
	}
	cases, err := ValidateCases(manifest)
	if err != nil {
		return 2, "", err
	}
	if o.Case != "" {
		var focused []Case
		for _, c := range cases {
			if c.Name == o.Case {
				focused = append(focused, c)
			}
		}
		if focused == nil {
			return 2, "", harnessError("unknown corpus case")
		}
		cases = focused
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return 2, "", err
	}
	if err := WriteSnapshot(filepath.Join(artifacts, "source"), inputs); err != nil {
		return 2, "", err
	}
	digests := map[string]string{}
	for name, data := range inputs {
		digests[name] = Digest(data)
	}
	if err := WriteJSON(filepath.Join(artifacts, "inputs.json"), digests); err != nil {
		return 2, "", err
	}
	if err := os.WriteFile(filepath.Join(artifacts, "replay.sh"), []byte(ReplayScript(baselineBin, candidateBin, o.Case, o.Timeout)), 0o755); err != nil {
		return 2, "", err
	}
	names := make([]string, len(cases))
	for i, c := range cases {
		names[i] = c.Name
	}
	report := Report{Schema: ReportSchema, Status: "harness_error", Differences: []Difference{},
		CanonicalIRExclusions: []string{"inputs", "lean_commit", "lean_version"},
		Scope:                 "research_identity_adaptation_only", SelectedCases: names,
		HostTools: map[string]string{"go_executable": goPath}}
	if data, err := os.ReadFile(goPath); err == nil {
		report.HostTools["go_sha256"] = Digest(data)
	}
	if harness, err := os.Executable(); err == nil {
		report.HostTools["harness_executable"] = harness
		if data, err := os.ReadFile(harness); err == nil {
			report.HostTools["harness_sha256"] = Digest(data)
		}
	}
	report.ExitCode = 2
	runner := NewRunner(o.Timeout)
	if err := runner.compare(&report, artifacts, baselineBin, candidateBin, snapshot, cases, names, goPath); err != nil {
		report.Error = err.Error()
	}
	if err := WriteJSON(filepath.Join(artifacts, "report.json"), report); err != nil {
		return 2, "", err
	}
	if len(report.Differences) > 0 {
		if err := DifferenceBundles(artifacts, report.Differences, baselineBin, candidateBin, o.Timeout); err != nil {
			return 2, "", err
		}
	}
	return report.ExitCode, filepath.Join(artifacts, "report.json"), nil
}

func (r *Runner) compare(report *Report, artifacts, baselineBin, candidateBin string, snapshot Snapshot, cases []Case, names []string, goPath string) error {
	version, err := checkedOutput(r.Commands.Run("go-version", []string{goPath, "version"}, artifacts, Environment(baselineBin, goPath), artifacts), "Go identity")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(version, "go version go1.27.4 ") {
		return harnessError("Go 1.27.4 is required")
	}
	report.HostTools["go"] = version
	corpus := filepath.Join(artifacts, "source", "compatibility", "corpus")
	if report.Baseline, err = r.RunCompiler(filepath.Join(artifacts, "baseline"), baselineBin, snapshot, corpus, cases, goPath, true); err != nil {
		return err
	}
	switch {
	case report.Baseline.Status != "passed":
		report.Status = "baseline_failed"
	case candidateBin == "":
		report.Status, report.ExitCode = "baseline_passed", 0
	default:
		if report.Candidate, err = r.RunCompiler(filepath.Join(artifacts, "candidate"), candidateBin, snapshot, corpus, cases, goPath, false); err != nil {
			return err
		}
		if report.Candidate.Status == "producer_build_failed" {
			report.Differences = []Difference{{Kind: "producer_build_failed"}}
		} else {
			report.Differences = CompareCases(names, report.Baseline.Cases, report.Candidate.Cases)
		}
		report.Status, report.ExitCode = "compatible", 0
		if len(report.Differences) > 0 {
			report.Status, report.ExitCode = "different", 1
		}
	}
	return nil
}
