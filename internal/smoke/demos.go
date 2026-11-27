package smoke

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/franchb/lean-go/internal/leanversion"
	"github.com/franchb/lean-go/internal/proofdemos"
)

// A demo check copies one standalone proof demo, generates fresh artifacts
// with a lean-go binary, compares package, IR and evidence bytes with the
// tracked ones, verifies the tracked evidence against the fresh package,
// requires exactly the independently listed claims and then deploys only the
// consumer and the generated Go sources, running the tests and the program
// offline with only Go on PATH.

const demosDir = "examples/proofs"

// demoChecker runs demo checks; tests replace run and deploy.
type demoChecker struct {
	repo   string
	run    runner
	goPath string
	env    []string
	stdout io.Writer
	// deploy runs the Go-only consumer in project and requires output.
	deploy func(d *demoChecker, project, workspace, output string) error
}

func newDemoChecker(repo, goPath string, env []string, stdout io.Writer) *demoChecker {
	return &demoChecker{repo: repo, run: runCommand, goPath: goPath, env: env, stdout: stdout, deploy: (*demoChecker).runGoDemo}
}

// demoArtifacts reads the generated package, the IR and the proof evidence
// of a demo laid out as in examples/proofs/<name>.
func demoArtifacts(project, name string) (map[string]string, error) {
	files, err := snapshot(filepath.Join(project, name))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for file, data := range files {
		out[name+"/"+file] = data
	}
	for _, file := range []string{name + ".ir.json", "proof-evidence.json"} {
		data, err := os.ReadFile(filepath.Join(project, file))
		if err != nil {
			return nil, err
		}
		out[file] = string(data)
	}
	return out, nil
}

// compareArtifacts requires byte-identical artifacts, with no file missing
// or added on either side.
func compareArtifacts(tracked, candidate, name string) error {
	expected, err := demoArtifacts(tracked, name)
	if err != nil {
		return err
	}
	actual, err := demoArtifacts(candidate, name)
	if err != nil {
		return err
	}
	if differ := changed(expected, actual); len(differ) != 0 {
		return fmt.Errorf("%s demo artifacts differ: %s", name, strings.Join(differ, ", "))
	}
	return nil
}

// runGoDemo runs the consumer's tests and program with only Go available.
func (d *demoChecker) runGoDemo(project, workspace, output string) error {
	env := goOnlyEnvironment(d.env, d.goPath, workspace)
	if _, err := must(d.run, []string{d.goPath, "test", "-count=1", "-buildvcs=false", "-mod=readonly", "./..."}, project, env); err != nil {
		return err
	}
	actual, err := must(d.run, []string{d.goPath, "run", "-buildvcs=false", "-mod=readonly", "."}, project, env)
	if err != nil {
		return err
	}
	if actual != output {
		return fmt.Errorf("standalone demo returned unexpected output: %q", actual)
	}
	return nil
}

func evidenceClaims(path string) ([][2]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var evidence struct {
		Claims []struct{ Name, Expected string }
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	var out [][2]string
	for _, claim := range evidence.Claims {
		out = append(out, [2]string{claim.Name, claim.Expected})
	}
	return out, nil
}

func sortedClaims(claims [][2]string) [][2]string {
	out := slices.Clone(claims)
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0]+"\x00"+a[1], b[0]+"\x00"+b[1]) })
	return out
}

// verifyDemo checks one demo with the lean-go binary cli in workspace.
func (d *demoChecker) verifyDemo(cli, workspace string, demo proofdemos.Demo) error {
	project := filepath.Join(workspace, "copied "+demo.Name+" demo")
	if err := copyTree(filepath.Join(d.repo, filepath.FromSlash(demosDir), demo.Name), project); err != nil {
		return err
	}
	candidate := filepath.Join(workspace, "candidate")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		return err
	}
	command := []string{cli, "generate", "--source-dir", filepath.Join(project, "source"), "--module", demo.Module,
		"--out", filepath.Join(candidate, demo.Name), "--ir-output", filepath.Join(candidate, demo.Name+".ir.json"), "--mode", "metered"}
	for _, root := range demo.Roots {
		command = append(command, "--root", root)
	}
	if _, err := must(d.run, command, project, d.env); err != nil {
		return err
	}
	checker := []string{cli, "check-proofs", "--contract", filepath.Join(project, "proof-contract.json"),
		"--manifest", filepath.Join(candidate, demo.Name, "manifest.json")}
	if _, err := must(d.run, append(slices.Clone(checker), "--output", filepath.Join(candidate, "proof-evidence.json")), project, d.env); err != nil {
		return err
	}
	if err := compareArtifacts(project, candidate, demo.Name); err != nil {
		return err
	}
	if _, err := must(d.run, append(slices.Clone(checker), "--evidence", filepath.Join(project, "proof-evidence.json")), project, d.env); err != nil {
		return err
	}
	claims, err := evidenceClaims(filepath.Join(candidate, "proof-evidence.json"))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(sortedClaims(claims), sortedClaims(demo.Claims)) {
		return fmt.Errorf("%s demo must check all %d source claims", demo.Name, len(demo.Claims))
	}
	deployed := filepath.Join(workspace, "Go-only "+demo.Name+" demo")
	for _, file := range demo.ConsumerFiles() {
		if err := copyFile(filepath.Join(project, filepath.FromSlash(file)), filepath.Join(deployed, filepath.FromSlash(file))); err != nil {
			return err
		}
	}
	if err := d.deploy(d, deployed, workspace, demo.Output); err != nil {
		return err
	}
	fmt.Fprintf(d.stdout, "standalone %s demo: identical artifacts, %d fresh proofs, Go-only tests and executable passed\n",
		demo.Name, len(claims))
	return nil
}

// verifyFrame checks the checked-frame composition against the component
// manifests of a copy of examples/proofs, then runs its consumer with Go
// alone. With the pinned Lean the fresh evidence must equal the tracked
// bytes; with another supported release, its claims must.
func (d *demoChecker) verifyFrame(cli, demos, workspace string) error {
	output := filepath.Join(workspace, "checkedframe-evidence.json")
	command := []string{cli, "check-proofs", "--contract", filepath.Join(demos, "checkedframe-contract.json")}
	for _, name := range []string{"wire16", "crc16"} {
		command = append(command, "--component-manifest", name, filepath.Join(demos, name, name, "manifest.json"))
	}
	if _, err := must(d.run, append(command, "--output", output), workspace, d.env); err != nil {
		return err
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return err
	}
	var evidence struct {
		LeanVersion string `json:"lean_version"`
		Components  map[string]any
		Claims      []map[string]any
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		return err
	}
	selected := leanversion.Version
	if value, ok := lookup(d.env, "LEAN_GO_TOOLCHAIN"); ok && value != "" {
		selected = value
	}
	if evidence.LeanVersion != selected {
		return fmt.Errorf("unexpected compiler in composition evidence: %s", evidence.LeanVersion)
	}
	claims, err := evidenceClaims(output)
	if err != nil {
		return err
	}
	components := make([]string, 0, len(evidence.Components))
	for name := range evidence.Components {
		components = append(components, name)
	}
	slices.Sort(components)
	if !slices.Equal(components, proofdemos.Frame.Components) || !reflect.DeepEqual(claims, proofdemos.Frame.Claims) {
		return fmt.Errorf("unexpected frame claims or bound components")
	}
	tracked, err := os.ReadFile(filepath.Join(demos, "checkedframe", "proof-evidence.json"))
	if err != nil {
		return err
	}
	if evidence.LeanVersion == leanversion.Version {
		if string(data) != string(tracked) {
			return fmt.Errorf("checksummed frame composition evidence changed")
		}
	} else {
		var stable struct{ Claims []map[string]any }
		if err := json.Unmarshal(tracked, &stable); err != nil || !reflect.DeepEqual(stable.Claims, evidence.Claims) {
			return fmt.Errorf("composition claims differ from stable Lean")
		}
	}
	if err := d.deploy(d, filepath.Join(demos, "checkedframe"), workspace, proofdemos.Frame.Output); err != nil {
		return err
	}
	fmt.Fprintln(d.stdout, "checked frame: round trip with arbitrary suffix and independent CRC acceptance proved against both component manifests; Go-only tests and executable passed")
	return nil
}
