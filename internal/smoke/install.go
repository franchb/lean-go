package smoke

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/franchb/lean-go/internal/cli"
	"github.com/franchb/lean-go/internal/pipeline"
	"github.com/franchb/lean-go/internal/proofdemos"
)

// InstallOptions select the binaries and the depth of the installed-CLI
// smoke.
type InstallOptions struct {
	// Repo is the lean-go checkout whose examples are copied.
	Repo string
	// CLI is an already installed lean-go binary. Empty: build one with
	// `go build` from Repo and install another with `go install` from a
	// module archive of Repo's tracked files, served by a local proxy, as
	// `go install github.com/franchb/lean-go/cmd/lean-go@<tag>` would.
	CLI string
	// ExpectTag, when set, is the release tag CLI must report.
	ExpectTag string
	// WithoutLean checks the binaries, their help and the missing-tool
	// errors only.
	WithoutLean bool
	// Demos, when not empty, restricts the Lean checks to these demos
	// (FrameDemo selects the checked frame) and skips the clamp producer
	// workflow.
	Demos  []string
	Stdout io.Writer
}

// FrameDemo names the checked-frame composition among the demos.
const FrameDemo = "checkedframe"

// CheckDemoNames rejects names that are not demos.
func CheckDemoNames(names []string) error {
	for _, name := range names {
		if name != FrameDemo && !slices.ContainsFunc(proofdemos.All, func(d proofdemos.Demo) bool { return d.Name == name }) {
			return fmt.Errorf("unknown demo %q", name)
		}
	}
	return nil
}

// SmokeVersion is the module version the local proxy serves.
const SmokeVersion = "v0.0.0-smoke"

// ReleaseTag is the Go module tag of a lean-go version: 0.1.0a15 is
// v0.1.0-alpha.15.
func ReleaseTag(version string) (string, error) {
	release, alpha, ok := strings.Cut(version, "a")
	if !ok || release == "" || alpha == "" {
		return "", fmt.Errorf("version %q is not an alpha release", version)
	}
	return "v" + release + "-alpha." + alpha, nil
}

// InstalledCLI runs the installed-CLI smoke.
func InstalledCLI(o InstallOptions) error {
	env := BaseEnvironment()
	goPath, err := pinnedGo(runCommand, o.Repo, env)
	if err != nil {
		return err
	}
	workspace, cleanup, err := tempWorkspace("lean-go-install-smoke-")
	if err != nil {
		return err
	}
	defer cleanup()
	var binaries []string
	if o.CLI != "" {
		cliPath, err := filepath.Abs(o.CLI)
		if err != nil {
			return err
		}
		binaries = []string{cliPath}
	} else {
		built, err := buildBinary(o.Repo, goPath, env, workspace)
		if err != nil {
			return err
		}
		installed, err := installFromModuleArchive(o.Repo, goPath, env, workspace)
		if err != nil {
			return err
		}
		binaries = []string{built, installed}
		fmt.Fprintf(o.Stdout, "built %s and installed %s@%s from a module archive of the tracked files\n",
			filepath.Base(built), "github.com/franchb/lean-go/cmd/lean-go", SmokeVersion)
	}
	for index, binary := range binaries {
		label := []string{"go build", "go install from the module archive"}[min(index, 1)]
		if o.CLI != "" {
			label = o.CLI
		}
		if err := checkBinary(o, binary, label, index, goPath, env, workspace); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	return nil
}

// buildBinary is `go build ./cmd/lean-go` into a directory with a space.
func buildBinary(repo, goPath string, env []string, workspace string) (string, error) {
	target := filepath.Join(workspace, "built lean-go", exe("lean-go"))
	_, err := must(runCommand, []string{goPath, "build", "-buildvcs=false", "-o", target, "./cmd/lean-go"}, repo,
		withEnv(env, map[string]string{"GOCACHE": filepath.Join(workspace, "go-cache")}))
	return target, err
}

// installFromModuleArchive serves a module archive of the tracked files of
// repo from a file:// proxy and runs `go install module/cmd/lean-go@version`
// outside the checkout, as a user of a published tag would.
func installFromModuleArchive(repo, goPath string, env []string, workspace string) (string, error) {
	proxy := filepath.Join(workspace, "module proxy")
	if err := writeModuleProxy(repo, proxy, SmokeVersion); err != nil {
		return "", err
	}
	bin := filepath.Join(workspace, "installed lean-go")
	caller := filepath.Join(workspace, "outside the checkout")
	if err := os.MkdirAll(caller, 0o755); err != nil {
		return "", err
	}
	installEnv := withEnv(env, map[string]string{"GOPROXY": fileURL(proxy), "GOSUMDB": "off", "GONOSUMDB": "", "GOPRIVATE": "",
		"GONOPROXY": "", "GOFLAGS": "-modcacherw", "GOBIN": bin, "GOPATH": filepath.Join(workspace, "gopath"),
		"GOMODCACHE": filepath.Join(workspace, "modules"), "GOCACHE": filepath.Join(workspace, "go-cache")})
	if _, err := must(runCommand, []string{goPath, "install", modulePath + "/cmd/lean-go@" + SmokeVersion}, caller, installEnv); err != nil {
		return "", err
	}
	return filepath.Join(bin, exe("lean-go")), nil
}

// checkBinary copies one binary into a read-only directory and exercises it
// from directories outside the checkout.
func checkBinary(o InstallOptions, binary, label string, index int, goPath string, env []string, workspace string) error {
	directory := filepath.Join(workspace, fmt.Sprintf("installed %d", index))
	installed := filepath.Join(directory, "bin with spaces", exe("lean-go"))
	if err := copyFile(binary, installed); err != nil {
		return err
	}
	if err := os.Chmod(installed, 0o555); err != nil {
		return err
	}
	before, err := os.ReadFile(installed)
	if err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(installed), 0o555); err != nil {
		return err
	}
	defer os.Chmod(filepath.Dir(installed), 0o755)
	defer os.Chmod(installed, 0o755)
	caller := filepath.Join(directory, "caller")
	if err := os.MkdirAll(caller, 0o755); err != nil {
		return err
	}
	noTools := withEnv(env, map[string]string{"PATH": ""})
	version, err := must(runCommand, []string{installed, "--version"}, caller, noTools)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(version, "lean-go "+cli.Version+" ") || !strings.HasSuffix(strings.TrimSpace(version), " pipeline "+pipeline.SourceDigest()[:12]) {
		return fmt.Errorf("unexpected version %q: want lean-go %s with pipeline %s", strings.TrimSpace(version), cli.Version, pipeline.SourceDigest()[:12])
	}
	if o.ExpectTag != "" {
		tag, err := ReleaseTag(cli.Version)
		if err != nil {
			return err
		}
		if o.ExpectTag != tag {
			return fmt.Errorf("release tag %s does not name lean-go %s (%s)", o.ExpectTag, cli.Version, tag)
		}
		if !strings.HasPrefix(version, "lean-go "+cli.Version+" ("+tag+") ") {
			return fmt.Errorf("installed binary reports %q, not module version %s", strings.TrimSpace(version), tag)
		}
	}
	for _, arguments := range [][]string{{"--help"}, {"version"}, {"generate", "--help"}, {"check-proofs", "--help"}} {
		if _, err := must(runCommand, append([]string{installed}, arguments...), caller, noTools); err != nil {
			return err
		}
	}
	missing, err := mustFail(runCommand, []string{installed, "doctor"}, caller, noTools)
	if err != nil {
		return err
	}
	if !strings.Contains(missing, "missing from PATH") {
		return fmt.Errorf("doctor without tools did not name the missing tools:\n%s", missing)
	}
	summary := "producer checks explicitly skipped"
	if !o.WithoutLean {
		count := 0
		if len(o.Demos) == 0 {
			if count, err = producerWorkflow(o.Repo, installed, directory, goPath, env, index == 0); err != nil {
				return err
			}
		}
		copied := filepath.Join(directory, "checkout copy")
		demos := filepath.Join(copied, filepath.FromSlash(demosDir))
		if err := copyTree(filepath.Join(o.Repo, filepath.FromSlash(demosDir)), demos); err != nil {
			return err
		}
		checker := newDemoChecker(copied, goPath, env, o.Stdout)
		checked := 0
		for _, demo := range proofdemos.All {
			if len(o.Demos) != 0 && !slices.Contains(o.Demos, demo.Name) {
				continue
			}
			demoWorkspace := filepath.Join(directory, "standalone "+demo.Name+" demo")
			if err := checker.verifyDemo(installed, demoWorkspace, demo); err != nil {
				return err
			}
			checked++
		}
		frame := len(o.Demos) == 0 || slices.Contains(o.Demos, FrameDemo)
		if frame {
			if err := checker.verifyFrame(installed, demos, directory); err != nil {
				return err
			}
		}
		summary = fmt.Sprintf("%d standalone demos", checked)
		if frame {
			summary += " and the checked frame"
		}
		summary += " passed"
		if len(o.Demos) == 0 {
			summary = fmt.Sprintf("%d Go-only consumer cases, %s", count, summary)
		}
	}
	after, err := os.ReadFile(installed)
	if err != nil {
		return err
	}
	if string(after) != string(before) {
		return errors.New("installed binary was changed")
	}
	fmt.Fprintf(o.Stdout, "%s: installed binary run outside the checkout; %s\n", label, summary)
	return nil
}

// producerWorkflow runs generate and check-proofs on a copy of the clamp
// demo through relative paths with spaces, deploys the result with Go
// alone, and requires that a rejected source changes no earlier output.
func producerWorkflow(repo, cliPath, directory, goPath string, env []string, multipleRoots bool) (int, error) {
	contract, err := copyFixture(repo, filepath.Join(directory, "project with spaces"))
	if err != nil {
		return 0, err
	}
	caller := filepath.Join(directory, "caller")
	pkg := filepath.Join(directory, "generated with spaces")
	ir := filepath.Join(directory, "program with spaces.json")
	proof := filepath.Join(directory, "proof with spaces.json")
	relative := func(path string) string {
		out, err := filepath.Rel(caller, path)
		if err != nil {
			return path
		}
		return out
	}
	if _, err := must(runCommand, []string{cliPath, "doctor"}, caller, env); err != nil {
		return 0, err
	}
	generation := []string{cliPath, "generate", "--source-dir", relative(contract.SourceDir), "--module", contract.Module,
		"--root", contract.Roots[0], "--out", relative(pkg), "--ir-output", relative(ir), "--mode", "metered"}
	if _, err := must(runCommand, generation, caller, env); err != nil {
		return 0, err
	}
	before, err := snapshot(pkg)
	if err != nil {
		return 0, err
	}
	checker := []string{cliPath, "check-proofs", "--contract", relative(contract.Path), "--manifest", relative(filepath.Join(pkg, "manifest.json"))}
	if _, err := must(runCommand, append(slices.Clone(checker), "--output", relative(proof)), caller, env); err != nil {
		return 0, err
	}
	if _, err := must(runCommand, append(slices.Clone(checker), "--evidence", relative(proof)), caller, env); err != nil {
		return 0, err
	}
	claims, err := evidenceClaims(proof)
	if err != nil {
		return 0, err
	}
	if len(claims) != 3 {
		return 0, fmt.Errorf("the clamp evidence checks %d claims, want 3", len(claims))
	}
	count, _, err := runClampConsumer(runCommand, directory, pkg, goPath, env)
	if err != nil {
		return 0, err
	}
	if multipleRoots {
		if err := multiRootWorkflow(cliPath, directory, contract, goPath, env); err != nil {
			return 0, err
		}
	}
	// A selected unsupported source must not replace any successful output.
	previousIR, err := os.ReadFile(ir)
	if err != nil {
		return 0, err
	}
	previousProof, err := os.ReadFile(proof)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(contract.SourceDir, "Unsupported.lean"), []byte("def bad {α : Type} (x : α) : α := x\n"), 0o644); err != nil {
		return 0, err
	}
	failure, err := mustFail(runCommand, []string{cliPath, "generate", "--diagnostics=json", "--source-dir", relative(contract.SourceDir),
		"--module", "Unsupported", "--root", "bad", "--out", relative(pkg), "--ir-output", relative(ir)}, caller, env)
	if err != nil {
		return 0, err
	}
	var report struct {
		Schema      string
		Success     *bool
		Diagnostics []struct {
			Stage, Code string
			Declaration *string
			Location    *struct{ Path string }
		}
	}
	if err := json.Unmarshal([]byte(failure), &report); err != nil {
		return 0, fmt.Errorf("the rejection is not one JSON report: %v\n%s", err, failure)
	}
	if report.Schema != "lean-go-diagnostics/v1" || report.Success == nil || *report.Success || len(report.Diagnostics) != 1 {
		return 0, fmt.Errorf("unexpected rejection report:\n%s", failure)
	}
	item := report.Diagnostics[0]
	if item.Stage != "admission" || item.Code != "unsupported_type" || item.Declaration == nil || *item.Declaration != "bad" ||
		item.Location == nil || item.Location.Path != "Unsupported.lean" {
		return 0, fmt.Errorf("unexpected rejection diagnostic:\n%s", failure)
	}
	after, err := snapshot(pkg)
	if err != nil {
		return 0, err
	}
	currentIR, err := os.ReadFile(ir)
	if err != nil {
		return 0, err
	}
	if len(changed(before, after)) != 0 || string(currentIR) != string(previousIR) {
		return 0, errors.New("a rejected generation changed the earlier package or IR")
	}
	if _, err := mustFail(runCommand, append(slices.Clone(checker), "--evidence", relative(proof)), caller, env); err != nil {
		return 0, err
	}
	currentProof, err := os.ReadFile(proof)
	if err != nil {
		return 0, err
	}
	if string(currentProof) != string(previousProof) {
		return 0, errors.New("a failed evidence check changed the evidence")
	}
	return count, nil
}

// multiRootWorkflow generates one package with two roots of the clamp
// source and runs a consumer of both with Go alone.
func multiRootWorkflow(cliPath, directory string, contract *pipeline.Contract, goPath string, env []string) error {
	consumer := filepath.Join(directory, "multi consumer")
	pkg := filepath.Join(consumer, "clamp")
	if _, err := must(runCommand, []string{cliPath, "generate", "--source-dir", contract.SourceDir, "--module", contract.Module,
		"--root", "Clamp.clamp", "--root", "Clamp.Bounds.cap", "--out", pkg, "--ir-output", filepath.Join(directory, "multi.json"),
		"--mode", "metered"}, directory, env); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(pkg, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest struct{ Program struct{ Roots []string } }
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if !slices.Equal(manifest.Program.Roots, []string{"Clamp.Bounds.cap", "Clamp.clamp"}) {
		return fmt.Errorf("multi-root manifest roots %v", manifest.Program.Roots)
	}
	api, err := os.ReadFile(filepath.Join(pkg, "api.go"))
	if err != nil {
		return err
	}
	if strings.Contains(string(api), "func RunTyped(") {
		return errors.New("a multi-root package exposes the single-root RunTyped")
	}
	files := map[string]string{"go.mod": "module example.com/cli-multi\n\ngo 1.27.4\n", "main.go": `package main
import clamp "example.com/cli-multi/clamp"
func main() {
 limits := clamp.DefaultLimits()
 got, err := clamp.RunClamp(100, 7, limits)
 if err != nil || got != 7 { panic("clamp mismatch") }
 got, err = clamp.RunCap(^uint64(0), 9, limits)
 if err != nil || got != 9 { panic("cap mismatch") }
}
`}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(consumer, name), []byte(text), 0o644); err != nil {
			return err
		}
	}
	isolated := withEnv(env, map[string]string{"PATH": filepath.Dir(goPath), "GOPROXY": "off", "GOSUMDB": "off",
		"GOPATH": filepath.Join(directory, "gopath"), "GOMODCACHE": filepath.Join(directory, "modules"),
		"GOCACHE": filepath.Join(directory, "go-cache")})
	_, err = must(runCommand, []string{goPath, "run", "-buildvcs=false", "-mod=readonly", "."}, consumer, isolated)
	return err
}
