// Package smoke holds the developer checks that run the lean-go command the
// way a user does: the installed-CLI smoke (a built and a module-installed
// lean-go binary, run from outside the checkout against copies of the proof
// demos) and the release-candidate walkthrough that prints a JSON receipt.
// cmd/lean-go-dev exposes them as the installed-cli and release-candidate
// commands.
package smoke

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/franchb/lean-go/internal/devexec"
	"github.com/franchb/lean-go/internal/pipeline"
)

// GoVersion is the Go release the checks require.
const GoVersion = pipeline.RequiredGo

// commandTimeout bounds every external command.
const commandTimeout = 15 * time.Minute

// runner executes one command with exactly env. Tests replace it.
type runner func(command []string, dir string, env []string) (devexec.Result, error)

func runCommand(command []string, dir string, env []string) (devexec.Result, error) {
	return devexec.Run(command, dir, env, "", commandTimeout)
}

// must runs command and returns its standard output; a nonzero exit is an
// error carrying both output streams.
func must(run runner, command []string, dir string, env []string) (string, error) {
	result, err := run(command, dir, env)
	if err == nil && result.Code != 0 {
		err = fmt.Errorf("exit status %d", result.Code)
	}
	if err != nil {
		return "", fmt.Errorf("%s %s: %v\n%s%s", filepath.Base(command[0]), strings.Join(command[1:], " "), err,
			result.Stderr, result.Stdout)
	}
	return result.Stdout, nil
}

// mustFail runs command, requires a nonzero exit and returns its standard
// output followed by its standard error.
func mustFail(run runner, command []string, dir string, env []string) (string, error) {
	result, err := run(command, dir, env)
	if err != nil {
		return "", err
	}
	if result.Code == 0 {
		return "", fmt.Errorf("%s %s succeeded unexpectedly:\n%s%s", filepath.Base(command[0]), strings.Join(command[1:], " "),
			result.Stdout, result.Stderr)
	}
	return result.Stdout + result.Stderr, nil
}

func sameKey(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// withEnv returns env with the settings replacing any entries of the same
// name, in sorted order after the inherited ones.
func withEnv(env []string, settings map[string]string) []string {
	var out []string
	for _, pair := range env {
		key, _, _ := strings.Cut(pair, "=")
		replaced := false
		for name := range settings {
			replaced = replaced || sameKey(key, name)
		}
		if !replaced {
			out = append(out, pair)
		}
	}
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+settings[key])
	}
	return out
}

// lookup returns the value of name in env.
func lookup(env []string, name string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		key, value, _ := strings.Cut(env[i], "=")
		if sameKey(key, name) {
			return value, true
		}
	}
	return "", false
}

// BaseEnvironment is this process's environment with the build settings
// the checkout pins: no cgo, the local toolchain, no workspace or user Go
// configuration.
func BaseEnvironment() []string {
	return withEnv(os.Environ(), map[string]string{"CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOENV": "off", "GOFLAGS": ""})
}

// goOnlyEnvironment has only Go on PATH, cgo, downloads and the checksum
// database disabled, no Lean or Lake settings and private Go directories
// under workspace.
func goOnlyEnvironment(env []string, goPath, workspace string) []string {
	var kept []string
	for _, pair := range env {
		key, _, _ := strings.Cut(pair, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "LEAN_") && !strings.HasPrefix(strings.ToUpper(key), "LAKE_") {
			kept = append(kept, pair)
		}
	}
	return withEnv(kept, map[string]string{
		"PATH": filepath.Dir(goPath), "CGO_ENABLED": "0", "GOTOOLCHAIN": "local", "GOWORK": "off", "GOENV": "off",
		"GOFLAGS": "", "GOPROXY": "off", "GOSUMDB": "off", "GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "",
		"LEAN_GO_DEMO_GO_ONLY": "1", "GOPATH": filepath.Join(workspace, "gopath"),
		"GOMODCACHE": filepath.Join(workspace, "modules"), "GOCACHE": filepath.Join(workspace, "go-cache"),
	})
}

// lookGo finds go on PATH; tests replace it.
var lookGo = func() (string, error) { return exec.LookPath("go") }

// pinnedGo returns the go executable of the pinned release found on PATH.
func pinnedGo(run runner, dir string, env []string) (string, error) {
	goPath, err := lookGo()
	if err != nil {
		return "", fmt.Errorf("Go 1.27.4 must be available on PATH")
	}
	out, err := must(run, []string{goPath, "env", "-json", "GOVERSION", "GOROOT"}, dir, env)
	if err != nil {
		return "", err
	}
	var info struct{ GOVERSION, GOROOT string }
	if json.Unmarshal([]byte(out), &info) != nil || info.GOVERSION != GoVersion || info.GOROOT == "" {
		return "", fmt.Errorf("the walkthrough requires Go 1.27.4, found %s", strings.TrimSpace(out))
	}
	executable := filepath.Join(info.GOROOT, "bin", exe("go"))
	if stat, err := os.Stat(executable); err != nil || !stat.Mode().IsRegular() {
		return "", fmt.Errorf("pinned Go installation has no Go executable")
	}
	return filepath.Abs(executable)
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// copyTree copies the regular files under source to destination.
func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		return copyFile(path, target)
	})
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

// snapshot maps the slash-separated path of every regular file under dir to
// its bytes.
func snapshot(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		out[filepath.ToSlash(relative)] = string(data)
		return err
	})
	return out, err
}

// changed lists the keys whose values differ between a and b, sorted.
func changed(a, b map[string]string) []string {
	var out []string
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			out = append(out, key)
		}
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// readRegular reads a regular file and refuses symlinks and other kinds.
func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}

// tempWorkspace creates a private, symlink-free temporary directory.
func tempWorkspace(prefix string) (string, func(), error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return resolved, cleanup, nil
}
