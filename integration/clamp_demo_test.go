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
	testStandaloneDemo(t, "clamp", "clamp(7, 42) = 7\nclamp(100, 42) = 42\nclamp(18446744073709551615, 42) = 42\n")
}

func testStandaloneDemo(t *testing.T, name, expected string) {
	t.Helper()
	workspace := t.TempDir()
	project := filepath.Join(workspace, "copied "+name+" demo")
	if err := os.MkdirAll(filepath.Join(project, name), 0700); err != nil {
		t.Fatal(err)
	}
	// Deploy only Go sources; no Lean inputs, manifests or proof artifacts.
	for _, file := range []string{
		"go.mod", "main.go", "main_test.go",
		name + "/api.go", name + "/control.go", name + "/runtime.go", name + "/schema.go",
	} {
		relative := filepath.FromSlash(file)
		data, err := os.ReadFile(filepath.Join("../examples/proofs", name, relative))
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
		if arguments[0] == "run" && string(output) != expected {
			t.Fatalf("unexpected demo output: %s", output)
		}
	}
}
