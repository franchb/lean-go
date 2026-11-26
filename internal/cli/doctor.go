package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/franchb/lean-go/internal/pipeline"
)

// doctor checks the build toolchain, Lean, Lake and the producer cache.
func doctor(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: lean-go doctor")
		return 2
	}
	healthy := true
	report := func(ok bool, label, detail string) {
		state := "OK"
		if !ok {
			state = "FAIL"
			healthy = false
		}
		fmt.Fprintf(stdout, "%s %s: %s\n", state, label, detail)
	}
	goErr := pipeline.CheckBuildToolchain()
	detail := runtime.Version() + " (requires " + pipeline.RequiredGo + " for reproducible go/format output)"
	report(goErr == nil, "Go toolchain used to build lean-go", detail)
	compiler, err := pipeline.SelectedCompiler()
	if err != nil {
		report(false, "Lean selection", err.Error())
		return 1
	}
	workspace, err := os.MkdirTemp("", "lean-go-doctor-")
	if err != nil {
		report(false, "temporary workspace", err.Error())
		return 1
	}
	defer os.RemoveAll(workspace)
	if err := os.WriteFile(filepath.Join(workspace, "lean-toolchain"), compiler.PinFile(), 0o644); err != nil {
		report(false, "temporary workspace", err.Error())
		return 1
	}
	env := pipeline.ChildEnvironment(compiler)
	for _, probe := range []struct {
		label, name string
		args        []string
	}{
		{"Lean", "lean", []string{"--version"}},
		{"Lake environment", "lake", []string{"env", "lean", "--version"}},
	} {
		path, err := exec.LookPath(probe.name)
		if err != nil {
			report(false, probe.label, probe.name+" is missing from PATH; install Lean "+compiler.Version+" with elan")
			continue
		}
		cmd := exec.Command(path, probe.args...)
		cmd.Dir, cmd.Env = workspace, env
		out, err := cmd.CombinedOutput()
		text := strings.TrimSpace(string(out))
		if err == nil {
			if checkErr := compiler.CheckVersion(text); checkErr != nil {
				err = checkErr
			}
		}
		if err != nil {
			if text == "" {
				text = err.Error()
			}
			report(false, probe.label, text)
			continue
		}
		report(true, probe.label, text)
	}
	cache := pipeline.DefaultCache()
	if cache == nil {
		report(true, "Producer cache", "disabled")
	} else if status := pipeline.CacheHealth(cache); status != nil {
		report(false, "Producer cache", cache.Dir+": "+status.Error())
	} else {
		report(true, "Producer cache", cache.Dir)
	}
	if !healthy {
		return 1
	}
	return 0
}
