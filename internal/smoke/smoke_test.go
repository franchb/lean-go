package smoke

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/cli"
	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
	"github.com/franchb/lean-go/internal/proofdemos"
	"github.com/franchb/lean-go/producer"
)

// Ported from the Python verifier tests: scripts/test_clamp_demo.py and the
// tests that extend it (test_flags_demo.py, test_words_demo.py,
// test_bytes_demo.py, test_header_demo.py, test_wire16_demo.py,
// test_normalizer_demo.py) and scripts/test_release_candidate.py. They run
// the checks with a stand-in for lean-go and Go; the real commands run in
// lean-go-dev installed-cli and release-candidate.

const repo = "../.."

func demoNamed(t *testing.T, name string) proofdemos.Demo {
	t.Helper()
	for _, demo := range proofdemos.All {
		if demo.Name == name {
			return demo
		}
	}
	t.Fatalf("no demo %s", name)
	return proofdemos.Demo{}
}

// copyTracked copies a demo's tracked artifacts into a candidate layout.
func copyTracked(t *testing.T, name, candidate string) {
	t.Helper()
	tracked := filepath.Join(repo, demosDir, name)
	if err := copyTree(filepath.Join(tracked, name), filepath.Join(candidate, name)); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{name + ".ir.json", "proof-evidence.json"} {
		if err := copyFile(filepath.Join(tracked, file), filepath.Join(candidate, file)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStaleMissingAndExtraArtifactsAreRejected(t *testing.T) {
	for _, demo := range proofdemos.All {
		tracked := filepath.Join(repo, demosDir, demo.Name)
		files, err := demoArtifacts(tracked, demo.Name)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, name := range append(slices.Clone(proofdemos.GeneratedFiles), "manifest.json") {
			names = append(names, demo.Name+"/"+name)
		}
		names = append(names, demo.Name+".ir.json", "proof-evidence.json")
		got := make([]string, 0, len(files))
		for name := range files {
			got = append(got, name)
		}
		if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(names))) {
			t.Fatalf("%s artifacts %v", demo.Name, got)
		}
		for _, name := range names {
			for _, operation := range []string{"change", "remove"} {
				candidate := t.TempDir()
				copyTracked(t, demo.Name, candidate)
				target := filepath.Join(candidate, filepath.FromSlash(name))
				if operation == "change" {
					data, _ := os.ReadFile(target)
					os.WriteFile(target, append(data, "stale"...), 0o644)
				} else {
					os.Remove(target)
				}
				if compareArtifacts(tracked, candidate, demo.Name) == nil {
					t.Errorf("%s: %s %s accepted", demo.Name, operation, name)
				}
			}
		}
		candidate := t.TempDir()
		copyTracked(t, demo.Name, candidate)
		if err := compareArtifacts(tracked, candidate, demo.Name); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(candidate, demo.Name, "unexpected.go"), []byte("package extracted"), 0o644)
		if err := compareArtifacts(tracked, candidate, demo.Name); err == nil || !strings.Contains(err.Error(), "unexpected.go") {
			t.Errorf("%s: extra file: %v", demo.Name, err)
		}
	}
}

type call struct {
	command []string
	dir     string
	env     []string
}

// fakeRun answers every command with reply and records the calls.
func fakeRun(calls *[]call, reply func(command []string, dir string) (string, int)) runner {
	return func(command []string, dir string, env []string) (devexec.Result, error) {
		*calls = append(*calls, call{slices.Clone(command), dir, env})
		out, code := reply(command, dir)
		return devexec.Result{Stdout: out, Code: code}, nil
	}
}

func goChecker(run runner) *demoChecker {
	return &demoChecker{repo: repo, run: run, goPath: filepath.Join("/go", "bin", "go"), stdout: &bytes.Buffer{},
		env:    []string{"PATH=/producer/tools", "CGO_ENABLED=1", "GOPROXY=https://example.invalid", "LEAN_GO_TOOLCHAIN=4.36.0"},
		deploy: (*demoChecker).runGoDemo}
}

func TestFailedGoTestsStopExecution(t *testing.T) {
	var calls []call
	d := goChecker(fakeRun(&calls, func([]string, string) (string, int) { return "test failure", 1 }))
	if err := d.runGoDemo("project", "workspace", "output"); err == nil || len(calls) != 1 || calls[0].command[1] != "test" {
		t.Fatalf("%v after %d calls", err, len(calls))
	}
}

func TestWrongOutputIsRejected(t *testing.T) {
	var calls []call
	d := goChecker(fakeRun(&calls, func(command []string, _ string) (string, int) {
		if command[1] == "run" {
			return "wrong output", 0
		}
		return "ok", 0
	}))
	if err := d.runGoDemo("project", "workspace", "output\n"); err == nil || !strings.Contains(err.Error(), "unexpected output") {
		t.Fatal(err)
	}
}

func TestGoConsumerDisablesProducerToolsAndDownloads(t *testing.T) {
	var calls []call
	demo := demoNamed(t, "words")
	d := goChecker(fakeRun(&calls, func(command []string, _ string) (string, int) {
		if command[1] == "run" {
			return demo.Output, 0
		}
		return "ok", 0
	}))
	if err := d.runGoDemo("project", "workspace", demo.Output); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].command[1] != "test" || calls[1].command[1] != "run" {
		t.Fatalf("calls %v", calls)
	}
	want := map[string]string{"PATH": filepath.Dir(d.goPath), "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOENV": "off",
		"GOFLAGS": "", "GOPROXY": "off", "GOSUMDB": "off", "LEAN_GO_DEMO_GO_ONLY": "1"}
	for _, c := range calls {
		if c.command[0] != d.goPath || !slices.Contains(c.command, "-mod=readonly") || c.dir != "project" {
			t.Errorf("command %v in %s", c.command, c.dir)
		}
		for key, value := range want {
			if got, _ := lookup(c.env, key); got != value {
				t.Errorf("%s=%q, want %q", key, got, value)
			}
		}
		if _, ok := lookup(c.env, "LEAN_GO_TOOLCHAIN"); ok {
			t.Error("the Go-only consumer sees Lean settings")
		}
	}
}

// generationFake stands in for lean-go: generate copies the tracked
// package and IR, check-proofs --output writes evidence.
func generationFake(t *testing.T, workspace string, demo proofdemos.Demo, evidence []byte, fail string) func([]string, string) (string, int) {
	return func(command []string, _ string) (string, int) {
		phase := "validate"
		switch {
		case command[1] == "generate":
			phase = "generate"
		case slices.Contains(command, "--output"):
			phase = "prove"
		}
		if phase == fail {
			return "simulated " + phase + " failure", 1
		}
		candidate := filepath.Join(workspace, "candidate")
		switch phase {
		case "generate":
			if command[slices.Index(command, "--module")+1] != demo.Module {
				t.Errorf("module %v", command)
			}
			var roots []string
			for i, argument := range command {
				if argument == "--root" {
					roots = append(roots, command[i+1])
				}
			}
			if !slices.Equal(roots, demo.Roots) || command[slices.Index(command, "--mode")+1] != "metered" {
				t.Errorf("roots %v in %v", roots, command)
			}
			copyTracked(t, demo.Name, candidate)
			os.Remove(filepath.Join(candidate, "proof-evidence.json"))
		case "prove":
			os.WriteFile(filepath.Join(candidate, "proof-evidence.json"), evidence, 0o644)
		}
		return "", 0
	}
}

func TestFreshGenerationChecksExactClaimsThenDeploysOnlyGo(t *testing.T) {
	for _, demo := range proofdemos.All {
		workspace := t.TempDir()
		evidence, _ := os.ReadFile(filepath.Join(repo, demosDir, demo.Name, "proof-evidence.json"))
		var calls []call
		d := goChecker(fakeRun(&calls, generationFake(t, workspace, demo, evidence, "")))
		var deployed []string
		d.deploy = func(_ *demoChecker, project, _, output string) error {
			files, err := snapshot(project)
			for file := range files {
				deployed = append(deployed, file)
			}
			if output != demo.Output {
				t.Errorf("%s: deployed with output %q", demo.Name, output)
			}
			return err
		}
		if err := d.verifyDemo("/tools/lean-go", workspace, demo); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 3 || calls[0].command[1] != "generate" || !slices.Contains(calls[1].command, "--output") ||
			!slices.Contains(calls[2].command, "--evidence") {
			t.Fatalf("%s: calls %v", demo.Name, calls)
		}
		if !slices.Equal(slices.Sorted(slices.Values(deployed)), slices.Sorted(slices.Values(demo.ConsumerFiles()))) {
			t.Errorf("%s: deployed %v", demo.Name, deployed)
		}
	}
}

func TestFailedGenerationProofOrEvidenceCheckStopsDeployment(t *testing.T) {
	demo := demoNamed(t, "words")
	evidence, _ := os.ReadFile(filepath.Join(repo, demosDir, demo.Name, "proof-evidence.json"))
	for phase, count := range map[string]int{"generate": 1, "prove": 2, "validate": 3} {
		workspace := t.TempDir()
		var calls []call
		d := goChecker(fakeRun(&calls, generationFake(t, workspace, demo, evidence, phase)))
		d.deploy = func(*demoChecker, string, string, string) error { t.Errorf("%s failure deployed", phase); return nil }
		if err := d.verifyDemo("/tools/lean-go", workspace, demo); err == nil || !strings.Contains(err.Error(), "simulated "+phase+" failure") {
			t.Errorf("%s: %v", phase, err)
		}
		if len(calls) != count {
			t.Errorf("%s: %d calls", phase, len(calls))
		}
	}
}

func TestWrongClaimInventoryIsRejected(t *testing.T) {
	for _, name := range []string{"flags", "words", "bytes", "normalizer"} {
		demo := demoNamed(t, name)
		original, _ := os.ReadFile(filepath.Join(repo, demosDir, name, "proof-evidence.json"))
		var evidence map[string]any
		json.Unmarshal(original, &evidence)
		claims := evidence["claims"].([]any)
		clone := func() []any {
			var out []any
			data, _ := json.Marshal(claims)
			json.Unmarshal(data, &out)
			return out
		}
		mutations := map[string][]any{}
		mutations["missing"] = clone()[:len(claims)-1]
		mutations["extra"] = append(clone(), map[string]any{"name": "Proofs.extra", "expected": "Contract.extra"})
		duplicate := clone()
		duplicate[len(duplicate)-1] = duplicate[0]
		mutations["duplicate"] = duplicate
		for _, field := range []string{"name", "expected"} {
			changed := clone()
			changed[0].(map[string]any)[field] = "Unrelated.claim"
			mutations["wrong "+field] = changed
		}
		for label, mutated := range mutations {
			evidence["claims"] = mutated
			fresh, _ := json.Marshal(evidence)
			// The copied project carries the same evidence, so only the
			// inventory, not the byte comparison, can reject it.
			fakeRepo := t.TempDir()
			project := filepath.Join(fakeRepo, demosDir, name)
			if err := copyTree(filepath.Join(repo, demosDir, name), project); err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(project, "proof-evidence.json"), fresh, 0o644)
			workspace := t.TempDir()
			var calls []call
			fake := generationFake(t, workspace, demo, fresh, "")
			d := goChecker(fakeRun(&calls, func(command []string, dir string) (string, int) {
				out, code := fake(command, dir)
				if command[1] == "generate" {
					os.WriteFile(filepath.Join(workspace, "candidate", "proof-evidence.json"), fresh, 0o644)
				}
				return out, code
			}))
			d.repo = fakeRepo
			d.deploy = func(*demoChecker, string, string, string) error { t.Errorf("%s %s deployed", name, label); return nil }
			err := d.verifyDemo("/tools/lean-go", workspace, demo)
			if want := "all " + strconv.Itoa(len(demo.Claims)) + " source claims"; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s %s: %v", name, label, err)
			}
		}
	}
}

func TestReleaseTagNamesTheModuleVersion(t *testing.T) {
	if cli.Version != "0.1.0a15" {
		t.Fatalf("the receipt names candidate %s", cli.Version)
	}
	if tag, err := ReleaseTag(cli.Version); err != nil || tag != "v0.1.0-alpha.15" {
		t.Fatalf("%s %v", tag, err)
	}
	for _, bad := range []string{"0.1.0", "a15", "0.1.0a"} {
		if _, err := ReleaseTag(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// fakeWalkthrough replaces Go, generation and the proof check.
func fakeWalkthrough(t *testing.T, goVersion string, failAt int) (*walkthrough, *[]string) {
	t.Helper()
	stages := new([]string)
	w := newWalkthrough(repo)
	w.run = func(command []string, dir string, env []string) (devexec.Result, error) {
		*stages = append(*stages, "go "+command[1])
		info, _ := json.Marshal(map[string]string{"GOVERSION": goVersion, "GOROOT": runtime.GOROOT()})
		return devexec.Result{Stdout: string(info)}, nil
	}
	stage := func(name string) error {
		*stages = append(*stages, name)
		if len(*stages)-2 == failAt {
			return errors.New("stage failed")
		}
		return nil
	}
	w.generate = func(pipeline.GenerateOptions) (*pipeline.GenerateResult, error) { return nil, stage("generate") }
	w.checkProofs = func(o pipeline.CheckProofsOptions) (string, error) {
		if o.Output != "" {
			return "", stage("prove")
		}
		return "", stage("verify")
	}
	old := lookGo
	t.Cleanup(func() { lookGo = old })
	lookGo = func() (string, error) { return "go", nil }
	return w, stages
}

func TestMissingOrWrongGoStopsBeforeGeneration(t *testing.T) {
	w, stages := fakeWalkthrough(t, GoVersion, -1)
	lookGo = func() (string, error) { return "", errors.New("not found") }
	if err := w.write(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "Go 1.27.4") || len(*stages) != 0 {
		t.Fatalf("%v after %v", err, *stages)
	}
	w, stages = fakeWalkthrough(t, "go1.28.0", -1)
	if err := w.write(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "Go 1.27.4") || !slices.Equal(*stages, []string{"go env"}) {
		t.Fatalf("%v after %v", err, *stages)
	}
}

func TestFailedGenerationOrProofCheckCleansWorkspaceWithoutReceipt(t *testing.T) {
	for failing, want := range [][]string{{"go env", "generate"}, {"go env", "generate", "prove"}, {"go env", "generate", "prove", "verify"}} {
		w, stages := fakeWalkthrough(t, GoVersion, failing)
		var stdout bytes.Buffer
		if err := w.write(&stdout); err == nil || err.Error() != "stage failed" {
			t.Fatalf("stage %d: %v", failing, err)
		}
		if stdout.Len() != 0 || !slices.Equal(*stages, want) || len(w.workspaces) != 1 {
			t.Fatalf("stage %d: printed %q after %v", failing, stdout.String(), *stages)
		}
		if _, err := os.Stat(w.workspaces[0]); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stage %d: workspace %s remains", failing, w.workspaces[0])
		}
	}
}

func TestConsumerRunsOfflineWithOnlyGoAndRejectsWrongOutput(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "generated")
	for _, name := range proofdemos.GeneratedFiles {
		if err := copyFile(filepath.Join(repo, demosDir, "clamp", "clamp", name), filepath.Join(pkg, name)); err != nil {
			t.Fatal(err)
		}
	}
	goPath := filepath.Join(root, "go-bin", "go")
	var calls []call
	run := fakeRun(&calls, func([]string, string) (string, int) { return "unexpected", 0 })
	_, _, err := runClampConsumer(run, root, pkg, goPath, BaseEnvironment())
	if err == nil || !strings.Contains(err.Error(), "unexpected result") || len(calls) != 1 {
		t.Fatalf("%v after %d calls", err, len(calls))
	}
	c := calls[0]
	if !slices.Equal(c.command, []string{goPath, "run", "-buildvcs=false", "-mod=readonly", "."}) {
		t.Fatal(c.command)
	}
	for key, value := range map[string]string{"PATH": filepath.Dir(goPath), "CGO_ENABLED": "0", "GOPROXY": "off", "GOSUMDB": "off", "GOWORK": "off"} {
		if got, _ := lookup(c.env, key); got != value {
			t.Errorf("%s=%q, want %q", key, got, value)
		}
	}
	for _, absent := range []string{"proof", "source"} {
		if _, err := os.Stat(filepath.Join(c.dir, absent)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("consumer has %s", absent)
		}
	}
	if len(clampSamples()) != 177 {
		t.Fatalf("%d consumer cases", len(clampSamples()))
	}
}

// The module archive go install fetches for a tag holds the embedded
// producer and leaves out the example modules.
func TestModuleArchiveCarriesTheEmbeddedProducer(t *testing.T) {
	files, err := moduleFiles(repo)
	if err != nil {
		t.Skipf("no git checkout: %v", err)
	}
	for _, name := range append(producer.ModuleNames(), producer.ToolchainFile, producer.CheckerFile, "embed.go") {
		if !slices.Contains(files, "producer/"+name) {
			t.Errorf("module archive lacks producer/%s", name)
		}
	}
	for _, file := range files {
		if strings.HasPrefix(file, "examples/oracle/") || slices.ContainsFunc(proofdemos.All, func(d proofdemos.Demo) bool {
			return strings.HasPrefix(file, "examples/proofs/"+d.Name+"/")
		}) {
			t.Errorf("module archive holds the separate module file %s", file)
		}
	}
	for _, required := range []string{"go.mod", "LICENSE", "cmd/lean-go/main.go"} {
		if !slices.Contains(files, required) {
			t.Errorf("module archive lacks %s", required)
		}
	}
}

func TestFileURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := fileURL(`C:\a b\proxy`); got != "file:///C:/a%20b/proxy" {
			t.Fatal(got)
		}
		return
	}
	if got := fileURL("/tmp/a b/proxy"); got != "file:///tmp/a%20b/proxy" {
		t.Fatal(got)
	}
}

// The real binaries, without Lean: go build, go install from the module
// archive, help and version with no tools on PATH, doctor's missing-tool
// report (check_cli_install.py --without-lean).
func TestInstalledCLIWithoutLean(t *testing.T) {
	if _, err := moduleFiles(repo); err != nil {
		t.Skipf("no git checkout: %v", err)
	}
	if _, err := pinnedGo(runCommand, repo, BaseEnvironment()); err != nil {
		t.Skip(err)
	}
	temporary := t.TempDir()
	for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(name, temporary)
	}
	var stdout bytes.Buffer
	if err := InstalledCLI(InstallOptions{Repo: repo, WithoutLean: true, Stdout: &stdout}); err != nil {
		t.Fatalf("%v\n%s", err, stdout.String())
	}
	if strings.Count(stdout.String(), "installed binary run outside the checkout; producer checks explicitly skipped") != 2 {
		t.Fatal(stdout.String())
	}
}

func TestDemoNamesAreChecked(t *testing.T) {
	if err := CheckDemoNames([]string{"clamp", "clock", FrameDemo}); err != nil {
		t.Fatal(err)
	}
	if err := CheckDemoNames([]string{"nope"}); err == nil {
		t.Fatal("an unknown demo was accepted")
	}
}
