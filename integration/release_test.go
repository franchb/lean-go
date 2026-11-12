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

const releaseModule = "github.com/franchb/lean-go"

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
	// This path must fetch the public artifact; candidate tests cannot replace it.
	runReleaseConsumer(t, version, "https://proxy.golang.org,direct", "sum.golang.org")
}

const releaseConsumerSource = `package main
import (
 "errors"
 "fmt"
 "os/exec"
 "runtime"
 c "github.com/franchb/lean-go/generated/counters"
 t "github.com/franchb/lean-go/generated/trees"
 w "github.com/franchb/lean-go/generated/wide"
 "github.com/franchb/lean-go/quota"
)
func must[T any](value T, err error) T { if err != nil { panic(err) }; return value }
func check(ok bool, message string) { if !ok { panic(message) } }
func treesAndNumbers() {
 limits := t.DefaultLimits()
 red := must(t.NewTreeLeaf(must(t.NewColorRed(limits)), limits))
 green := must(t.NewTreeLeaf(must(t.NewColorGreen(limits)), limits))
 tree := must(t.NewTreeBranch(red, green, limits))
 result := must(t.RunTyped(tree, limits))
 left, right, ok := result.AsBranch(); check(ok, "tree branch")
 leftColor, leftOK := left.AsLeaf()
 rightColor, rightOK := right.AsLeaf()
 check(leftOK && rightOK && leftColor.AsGreen() && rightColor.AsBlue(), "typed tree result")
 const maximum uint64 = 18446744073709551615
 wide := must(w.RunTyped(maximum, 65, w.DefaultLimits()))
 for _, test := range []struct { get func() (uint64, bool); want uint64 }{
  {wide.Sum, 64}, {wide.Maximum, maximum}, {wide.Wrapped, 1}, {wide.Above53, 9007199254740993},
 } {
  got, ok := test.get(); check(ok && got == test.want, "typed UInt64 result")
 }
}
func counters() {
 limits := c.DefaultLimits()
 const maximum uint32 = 4294967295
 const total uint64 = 9007199254740993
 original := must(c.RunInitial(maximum, total, true, limits))
 next := must(c.RunAdvance(original, must(c.NewCount(2, limits)), limits))
 check(must(c.RunRead(next, limits)) == 1, "composed roots UInt32 wrap")
 check(must(c.RunTotal(next, limits)) == total, "composed roots UInt64 exactness")
 check(must(c.RunRead(original, limits)) == maximum, "multi-root immutable input")
 disabled := must(c.RunInitial(42, 17, false, limits))
 unchanged := must(c.RunAdvance(disabled, must(c.NewCount(10, limits)), limits))
 check(must(c.RunRead(unchanged, limits)) == 42, "multi-root disabled branch")
 raw := must(c.Import(&c.Input{Type: "Counters.State", Constructor: "Counters.State.mk", Fields: []*c.Input{
  {Type: "Counters.Count", Constructor: "Counters.Count.mk", Fields: []*c.Input{{Type: "UInt32", UInt32: 9}}},
  {Type: "UInt64", UInt64: total}, {Type: "Bool", Constructor: "Bool.true"},
 }}, limits))
 value := must(c.RunRoot("Counters.read", []c.Value{raw}, limits))
 count, valid := value.UInt32(); check(valid && count == 9, "selected raw root")
 delta := must(c.Import(&c.Input{Type: "Counters.Count", Constructor: "Counters.Count.mk", Fields: []*c.Input{{Type: "UInt32", UInt32: 1}}}, limits))
 _, err := c.RunRoot("Counters.step", []c.Value{raw, delta}, limits)
 check(err != nil, "internal helper exposed as root")
}
func quotas() {
 const maximum uint64 = 18446744073709551615
 original := must(quota.New(maximum))
 full, accepted, err := original.Reserve(maximum)
 check(err == nil && accepted, "quota maximum reserve")
 denied, accepted, err := full.Reserve(1)
 check(err == nil && !accepted && must(denied.Used()) == maximum, "quota overflow rejection")
 empty, accepted, err := full.Release(maximum)
 check(err == nil && accepted && must(empty.Remaining()) == maximum, "quota maximum release")
 check(must(original.Used()) == 0 && must(full.Used()) == maximum, "quota immutable snapshots")
 check(must(full.Capacity()) == maximum, "quota capacity")
 denied, accepted, err = empty.Release(1)
 check(err == nil && !accepted && must(denied.Used()) == 0, "quota underflow rejection")
 zero := must(quota.New(0))
 _, accepted, err = zero.Reserve(0)
 check(err == nil && accepted, "quota zero-unit reserve")
 _, accepted, err = zero.Reserve(1)
 check(err == nil && !accepted, "quota zero capacity")
 var invalid quota.State
 _, _, err = invalid.Reserve(0)
 check(errors.Is(err, quota.ErrInvalidState), "quota invalid state")
}
func main() {
 check(runtime.Version() == "go1.27.4", "expected Go 1.27.4")
 for _, name := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
  if _, err := exec.LookPath(name); err == nil { panic("producer executable available: " + name) }
 }
 treesAndNumbers()
 counters()
 quotas()
 fmt.Println("release Go-only consumer: OK")
}
`

func runReleaseConsumer(t *testing.T, version, proxy, sumDB string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":  fmt.Sprintf("module releaseconsumer\n\ngo 1.27.4\n\nrequire %s %s\n", releaseModule, version),
		"main.go": releaseConsumerSource,
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
		"GOPROXY":    proxy,
		"GOSUMDB":    sumDB,
		"GOPRIVATE":  "",
		"GONOPROXY":  "",
		"GONOSUMDB":  "",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	// Download before restricting the consumer to Go alone; the public path may use Git.
	cmd := exec.CommandContext(ctx, goBinary(), "mod", "download", "-json", releaseModule+"@"+version)
	cmd.Dir, cmd.Env = dir, commandEnv(settings)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("download exact-version module: %v\n%s", err, output)
	}
	var downloaded struct {
		Path, Version, Sum, GoModSum string
	}
	if err := json.Unmarshal(output, &downloaded); err != nil {
		t.Fatalf("decode downloaded module: %v\n%s", err, output)
	}
	if downloaded.Path != releaseModule || downloaded.Version != version || downloaded.Sum == "" || downloaded.GoModSum == "" {
		t.Fatalf("unexpected downloaded module: %s", output)
	}
	t.Logf("downloaded %s@%s: %s; go.mod %s", downloaded.Path, downloaded.Version, downloaded.Sum, downloaded.GoModSum)

	settings["PATH"] = filepath.Dir(goBinary())
	settings["GOPROXY"] = "off"
	settings["GOSUMDB"] = "off"
	cmd = exec.CommandContext(ctx, goBinary(), "list", "-m", "-json", releaseModule)
	cmd.Dir, cmd.Env = dir, commandEnv(settings)
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
	if selected.Path != releaseModule || selected.Version != version || selected.Replace != nil {
		t.Fatalf("release module was replaced or changed version: %s", output)
	}
	cmd = exec.CommandContext(ctx, goBinary(), "run", "-buildvcs=false", "-mod=readonly", ".")
	cmd.Dir, cmd.Env = dir, commandEnv(settings)
	if output, err := cmd.CombinedOutput(); err != nil || string(output) != "release Go-only consumer: OK\n" {
		t.Fatalf("release Go-only consumer: %v\n%s", err, output)
	}
}
