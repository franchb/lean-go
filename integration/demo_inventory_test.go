package integration_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/franchb/lean-go/internal/artifacts"
	"github.com/franchb/lean-go/internal/leanversion"
	"github.com/franchb/lean-go/internal/proofdemos"
)

// Each demo's tracked contract, evidence and regeneration entry must select
// exactly the independently listed module, roots and claims, so a claim
// cannot vanish from contract and evidence together.

const proofDemos = "../examples/proofs"

func readJSONObject(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return value
}

// claimPairs reads the (nameKey, "expected") pairs of a claims array.
func claimPairs(t *testing.T, document map[string]any, nameKey string) [][2]string {
	t.Helper()
	items, ok := document["claims"].([]any)
	if !ok {
		t.Fatal("claims is not an array")
	}
	var out [][2]string
	for _, item := range items {
		claim, _ := item.(map[string]any)
		name, _ := claim[nameKey].(string)
		expected, _ := claim["expected"].(string)
		out = append(out, [2]string{name, expected})
	}
	return out
}

func sortedPairs(pairs [][2]string) [][2]string {
	out := slices.Clone(pairs)
	slices.SortFunc(out, func(a, b [2]string) int { return strings.Compare(a[0]+"\x00"+a[1], b[0]+"\x00"+b[1]) })
	return out
}

func stringsOf(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%v is not an array", value)
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i], _ = item.(string)
	}
	return out
}

func TestProofDemoInventory(t *testing.T) {
	if len(proofdemos.All) != 10 {
		t.Fatalf("%d demos listed", len(proofdemos.All))
	}
	total := 0
	for _, demo := range proofdemos.All {
		t.Run(demo.Name, func(t *testing.T) {
			dir := filepath.Join(proofDemos, demo.Name)
			want := sortedPairs(demo.Claims)
			if len(slices.Compact(slices.Clone(want))) != len(want) {
				t.Fatal("the inventory lists a claim twice")
			}
			contract := readJSONObject(t, filepath.Join(dir, "proof-contract.json"))
			roots := stringsOf(t, contract["roots"])
			slices.Sort(roots)
			if contract["module"] != demo.Module || !slices.Equal(roots, demo.Roots) {
				t.Errorf("contract selects %v %v", contract["module"], roots)
			}
			// Sorted lists, not sets: a duplicated claim must fail too.
			if got := sortedPairs(claimPairs(t, contract, "theorem")); !reflect.DeepEqual(got, want) {
				t.Errorf("contract claims %v", got)
			}
			evidence := readJSONObject(t, filepath.Join(dir, "proof-evidence.json"))
			if got := sortedPairs(claimPairs(t, evidence, "name")); !reflect.DeepEqual(got, want) {
				t.Errorf("evidence must check all %d source claims, has %v", len(want), got)
			}
			// lean-go regenerate extracts the demo with the same selection.
			found := false
			for _, spec := range artifacts.Packages {
				if spec.Package == "examples/proofs/"+demo.Name+"/"+demo.Name {
					found = true
					sorted := slices.Sorted(slices.Values(spec.Roots))
					if spec.Module != demo.Module || !slices.Equal(sorted, demo.Roots) || spec.Mode != "metered" ||
						spec.Contract != "examples/proofs/"+demo.Name+"/proof-contract.json" {
						t.Errorf("regeneration entry %+v", spec)
					}
				}
			}
			if !found {
				t.Error("no regeneration entry for the demo package")
			}
			// The tracked package holds exactly the generated files.
			entries, err := os.ReadDir(filepath.Join(dir, demo.Name))
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			if wantFiles := slices.Sorted(slices.Values(append(slices.Clone(proofdemos.GeneratedFiles), "manifest.json"))); !slices.Equal(names, wantFiles) {
				t.Errorf("tracked package files %v, want %v", names, wantFiles)
			}
			for _, file := range demo.ConsumerFiles() {
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(file))); err != nil {
					t.Errorf("consumer file: %v", err)
				}
			}
			// A documented example output must be the program's output.
			source, err := os.ReadFile(filepath.Join(dir, "main_test.go"))
			if err != nil {
				t.Fatal(err)
			}
			_, example, documented := strings.Cut(string(source), "\t// Output:\n")
			if demo.Name == "normalizer" && !documented {
				t.Fatal("the normalizer consumer lost its documented output")
			}
			if documented {
				for _, line := range strings.SplitAfter(strings.TrimSuffix(demo.Output, "\n"), "\n") {
					if !strings.Contains(example, "\t// "+strings.TrimSuffix(line, "\n")+"\n") {
						t.Errorf("main_test.go does not document %q", line)
					}
				}
			}
			total += len(want)
		})
	}
	if total != 61 {
		t.Errorf("%d source claims across the demos, want 61", total)
	}
}

func TestCheckedFrameInventory(t *testing.T) {
	contract := readJSONObject(t, filepath.Join(proofDemos, "checkedframe-contract.json"))
	if got := claimPairs(t, contract, "theorem"); !reflect.DeepEqual(got, proofdemos.Frame.Claims) {
		t.Errorf("contract claims %v", got)
	}
	components, _ := contract["components"].(map[string]any)
	evidence := readJSONObject(t, filepath.Join(proofDemos, "checkedframe", "proof-evidence.json"))
	bound, _ := evidence["components"].(map[string]any)
	for label, object := range map[string]map[string]any{"contract": components, "evidence": bound} {
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, proofdemos.Frame.Components) {
			t.Errorf("%s components %v", label, keys)
		}
	}
	if !slices.Equal(artifacts.Frame.Components, []string{"wire16", "crc16"}) {
		t.Errorf("regeneration components %v", artifacts.Frame.Components)
	}
	if got := claimPairs(t, evidence, "name"); !reflect.DeepEqual(got, proofdemos.Frame.Claims) {
		t.Errorf("unexpected frame claims %v", got)
	}
	if evidence["lean_version"] != leanversion.Version || evidence["lean_commit"] != leanversion.Commit {
		t.Errorf("composition evidence names Lean %v %v", evidence["lean_version"], evidence["lean_commit"])
	}
}

// The checked frame deploys with Go alone next to copies of its two
// component demos and prints its documented output.
func TestStandaloneCheckedFrameDemo(t *testing.T) {
	workspace := t.TempDir()
	tree := filepath.Join(workspace, "copied demos")
	for _, name := range []string{"checkedframe", "wire16", "crc16"} {
		source := filepath.Join(proofDemos, name)
		err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			target := filepath.Join(tree, name, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0o700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o600)
		})
		if err != nil {
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
		command.Dir, command.Env = filepath.Join(tree, "checkedframe"), environment
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("checked frame %v: %v\n%s", arguments, err, output)
		}
		if arguments[0] == "run" && string(output) != proofdemos.Frame.Output {
			t.Fatalf("unexpected checked frame output: %q", output)
		}
	}
}
