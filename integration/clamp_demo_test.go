package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStandaloneClampDemo(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "copied clamp demo")
	if err := os.MkdirAll(filepath.Join(project, "clamp"), 0700); err != nil {
		t.Fatal(err)
	}
	// Deploy only Go sources; no Lean inputs, manifests or proof artifacts.
	for _, name := range []string{
		"go.mod", "main.go", "main_test.go",
		"clamp/api.go", "clamp/control.go", "clamp/runtime.go", "clamp/schema.go",
	} {
		relative := filepath.FromSlash(name)
		data, err := os.ReadFile(filepath.Join("../examples/proofs/clamp", relative))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, relative), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	environment := commandEnv(map[string]string{
		"PATH": filepath.Dir(goBinary()), "GOENV": "off", "LEAN_GO_DEMO_GO_ONLY": "1",
		"GOPATH":     filepath.Join(workspace, "gopath"),
		"GOMODCACHE": filepath.Join(workspace, "modules"),
		"GOCACHE":    filepath.Join(workspace, "go-cache"),
		"GOPRIVATE":  "", "GONOPROXY": "", "GONOSUMDB": "",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	for _, arguments := range [][]string{
		{"test", "-count=1", "-buildvcs=false", "-mod=readonly", "./..."},
		{"run", "-buildvcs=false", "-mod=readonly", "."},
	} {
		command := exec.CommandContext(ctx, goBinary(), arguments...)
		command.Dir, command.Env = project, environment
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("copied standalone demo %v: %v\n%s", arguments, err, output)
		}
		if arguments[0] == "run" && string(output) != "clamp(7, 42) = 7\nclamp(100, 42) = 42\nclamp(18446744073709551615, 42) = 42\n" {
			t.Fatalf("unexpected demo output: %s", output)
		}
	}
}
