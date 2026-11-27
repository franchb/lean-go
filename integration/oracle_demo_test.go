package integration_test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStandaloneOracleExample copies the differential-oracle example, the
// tracked native model packages included, into an isolated workspace and
// runs its tests with only Go available and no module proxy.
func TestStandaloneOracleExample(t *testing.T) {
	source := filepath.Join("..", "examples", "oracle")
	workspace := t.TempDir()
	project := filepath.Join(workspace, "copied oracle example")
	copied := 0
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(project, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			t.Fatalf("unexpected non-regular file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		copied++
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"go.mod", "model/adler32/functions.go", "model/crc16/functions.go", "fast/adler32.go", "fast/crc16.go"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(required))); err != nil {
			t.Fatalf("copied example lacks %s: %v", required, err)
		}
	}
	environment := commandEnv(map[string]string{
		"PATH": filepath.Dir(goBinary()), "GOENV": "off",
		"GOPATH":     filepath.Join(workspace, "gopath"),
		"GOMODCACHE": filepath.Join(workspace, "modules"),
		"GOCACHE":    filepath.Join(workspace, "go-cache"),
		"GOPRIVATE":  "", "GONOPROXY": "", "GONOSUMDB": "",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	for _, arguments := range [][]string{
		{"vet", "-mod=readonly", "./..."},
		{"test", "-count=1", "-buildvcs=false", "-mod=readonly", "./..."},
	} {
		command := exec.CommandContext(ctx, goBinary(), arguments...)
		command.Dir, command.Env = project, environment
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("copied oracle example %v (%d files): %v\n%s", arguments, copied, err, output)
		}
	}
}
