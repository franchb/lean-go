package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// Go module versions must be canonical: no leading zeros or build metadata.
var releaseTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

func TestReleaseTagValidation(t *testing.T) {
	for _, tag := range []string{"v0.1.0-alpha.1", "v1.2.3", "v2.0.0-rc.0", "v0.0.0-0", "v1.2.3-01alpha"} {
		if !releaseTagPattern.MatchString(tag) {
			t.Errorf("valid release tag rejected: %q", tag)
		}
	}
	for _, tag := range []string{
		"", "latest", "main", "0.1.0-alpha.1", "v1", "v1.2", "v01.2.3", "v1.02.3", "v1.2.03",
		"v1.2.3-", "v1.2.3-alpha..1", "v1.2.3-alpha.01", "v1.2.3-01", "v1.2.3+build",
		" v1.2.3", "v1.2.3\n", "v1.2.3\nreplace github.com/franchb/lean-go => ..", "v1.2.3;echo injected",
	} {
		if releaseTagPattern.MatchString(tag) {
			t.Errorf("invalid release tag accepted: %q", tag)
		}
	}
}

func TestTaggedReleaseConsumer(t *testing.T) {
	version := os.Getenv("LEAN_GO_RELEASE_VERSION")
	if version == "" {
		t.Skip("set LEAN_GO_RELEASE_VERSION to an exact published release tag")
	}
	if !releaseTagPattern.MatchString(version) {
		t.Fatalf("invalid LEAN_GO_RELEASE_VERSION %q: expected a canonical vMAJOR.MINOR.PATCH release tag", version)
	}
	const module = "github.com/franchb/lean-go"
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": fmt.Sprintf("module releaseconsumer\n\ngo 1.27.4\n\nrequire %s %s\n", module, version),
		"main.go": `package main
import (
 "fmt"
 "os/exec"
 "runtime"
 t "github.com/franchb/lean-go/generated/trees"
 w "github.com/franchb/lean-go/generated/wide"
)
func must[T any](value T, err error) T { if err != nil { panic(err) }; return value }
func main() {
 if runtime.Version() != "go1.27.4" { panic("expected Go 1.27.4") }
 for _, name := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
  if _, err := exec.LookPath(name); err == nil { panic("producer executable available: " + name) }
 }
 limits := t.DefaultLimits()
 red := must(t.NewTreeLeaf(must(t.NewColorRed(limits)), limits))
 green := must(t.NewTreeLeaf(must(t.NewColorGreen(limits)), limits))
 tree := must(t.NewTreeBranch(red, green, limits))
 result := must(t.RunTyped(tree, limits))
 left, right, ok := result.AsBranch(); if !ok { panic("tree branch") }
 leftColor, leftOK := left.AsLeaf()
 rightColor, rightOK := right.AsLeaf()
 if !leftOK || !rightOK || !leftColor.AsGreen() || !rightColor.AsBlue() { panic("typed tree result") }
 const maximum uint64 = 18446744073709551615
 wide := must(w.RunTyped(maximum, 65, w.DefaultLimits()))
 for _, check := range []struct { get func() (uint64, bool); want uint64 }{
  {wide.Sum, 64}, {wide.Maximum, maximum}, {wide.Wrapped, 1}, {wide.Above53, 9007199254740993},
 } {
  got, ok := check.get(); if !ok || got != check.want { panic("typed UInt64 result") }
 }
 fmt.Println("tagged Go-only consumer: OK")
}`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cache := t.TempDir()
	settings := map[string]string{
		"GOENV": "off",
		// The cache belongs to t.TempDir and must remain removable by the test.
		"GOFLAGS":    "-modcacherw",
		"GOPATH":     filepath.Join(cache, "gopath"),
		"GOMODCACHE": filepath.Join(cache, "modules"),
		"GOCACHE":    filepath.Join(cache, "build"),
		"GOPROXY":    "https://proxy.golang.org,direct",
		"GOSUMDB":    "sum.golang.org",
		"GOPRIVATE":  "",
		"GONOPROXY":  "",
		"GONOSUMDB":  "",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	// Download using the normal PATH, before restricting the consumer to Go alone.
	cmd := exec.CommandContext(ctx, goBinary(), "mod", "download", "-json", module+"@"+version)
	cmd.Dir = dir
	cmd.Env = commandEnv(settings)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("download published module: %v\n%s", err, output)
	}
	var downloaded struct {
		Path, Version, Sum, GoModSum string
	}
	if err := json.Unmarshal(output, &downloaded); err != nil {
		t.Fatalf("decode downloaded module: %v\n%s", err, output)
	}
	if downloaded.Path != module || downloaded.Version != version || downloaded.Sum == "" || downloaded.GoModSum == "" {
		t.Fatalf("unexpected downloaded module: %s", output)
	}
	t.Logf("downloaded %s@%s: %s; go.mod %s", downloaded.Path, downloaded.Version, downloaded.Sum, downloaded.GoModSum)

	settings["PATH"] = filepath.Dir(goBinary())
	settings["GOPROXY"] = "off"
	settings["GOSUMDB"] = "off"
	cmd = exec.CommandContext(ctx, goBinary(), "list", "-m", "-json", module)
	cmd.Dir = dir
	cmd.Env = commandEnv(settings)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve downloaded module offline: %v\n%s", err, output)
	}
	var selected struct {
		Path, Version string
		Replace       *json.RawMessage
	}
	if err := json.Unmarshal(output, &selected); err != nil {
		t.Fatalf("decode selected module: %v\n%s", err, output)
	}
	if selected.Path != module || selected.Version != version || selected.Replace != nil {
		t.Fatalf("release module was replaced or changed version: %s", output)
	}
	cmd = exec.CommandContext(ctx, goBinary(), "run", "-mod=readonly", ".")
	cmd.Dir = dir
	cmd.Env = commandEnv(settings)
	if output, err := cmd.CombinedOutput(); err != nil || string(output) != "tagged Go-only consumer: OK\n" {
		t.Fatalf("tagged Go-only consumer: %v\n%s", err, output)
	}
}
