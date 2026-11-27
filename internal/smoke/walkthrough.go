package smoke

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/franchb/lean-go/internal/cli"
	"github.com/franchb/lean-go/internal/pipeline"
)

// The release-candidate walkthrough copies the clamp proof demo into a
// temporary external project, generates fresh Go from it, checks and then
// verifies its three source claims, and runs a separate Go-only consumer
// against an independent model. On success it prints a JSON receipt; a
// failed stage prints nothing on standard output.

// walkthroughSource identifies the verifier in the receipt.
//
//go:embed walkthrough.go
var walkthroughSource []byte

// ReceiptSchema names the receipt format.
const ReceiptSchema = "lean-go-release-walkthrough/v1"

const clampFixture = "examples/proofs/clamp/proof-contract.json"

// copyFixture copies the clamp demo's contract, runtime and proof modules
// into destination and returns the copied contract.
func copyFixture(repo, destination string) (*pipeline.Contract, error) {
	contract, err := pipeline.LoadContract(filepath.Join(repo, filepath.FromSlash(clampFixture)))
	if err != nil {
		return nil, err
	}
	if contract.Module != "Clamp" || strings.Join(contract.Roots, ",") != "Clamp.clamp" {
		return nil, errors.New("release walkthrough fixture no longer selects Clamp.clamp")
	}
	project, err := pipeline.SnapshotProject(contract)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(contract.Path)
	for directory, modules := range map[string]map[string]pipeline.Source{contract.SourceDir: project.Runtime, contract.ProofDir: project.Proofs} {
		relative, err := filepath.Rel(parent, directory)
		if err != nil {
			return nil, err
		}
		for _, module := range modules {
			target := filepath.Join(destination, relative, filepath.FromSlash(module.Relative))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(target, module.Data, 0o644); err != nil {
				return nil, err
			}
		}
	}
	target := filepath.Join(destination, "proof-contract.json")
	if err := os.WriteFile(target, contract.Raw, 0o644); err != nil {
		return nil, err
	}
	return pipeline.LoadContract(target)
}

type clampCase struct{ value, limit, want uint64 }

// clampSamples are 49 boundary pairs and 128 pairs from a fixed seed, with
// the expected result of an independent model.
func clampSamples() []clampCase {
	bounds := []uint64{0, 1, 31, 1<<32 - 1, 1<<53 + 1, 1 << 63, 1<<64 - 1}
	var pairs [][2]uint64
	for _, value := range bounds {
		for _, limit := range bounds {
			pairs = append(pairs, [2]uint64{value, limit})
		}
	}
	rng := rand.New(rand.NewSource(35027))
	for range 128 {
		pairs = append(pairs, [2]uint64{rng.Uint64(), rng.Uint64()})
	}
	out := make([]clampCase, len(pairs))
	for i, pair := range pairs {
		out[i] = clampCase{pair[0], pair[1], min(pair[0], pair[1])}
	}
	return out
}

func consumerSource(cases []clampCase) []byte {
	rows := make([]string, len(cases))
	for i, c := range cases {
		rows[i] = fmt.Sprintf("{%d, %d, %d}", c.value, c.limit, c.want)
	}
	return []byte(`package main
import (
 "fmt"
 "os/exec"
 "runtime"
 clamp "example.com/proved-clamp/clamp"
)
func main() {
 if runtime.Version() != "go1.27.4" { panic("expected Go 1.27.4") }
 for _, tool := range []string{"lean", "lake", "uv", "python", "python3", "python3.15", "py", "cc", "gcc", "clang", "cl"} {
  if _, err := exec.LookPath(tool); err == nil { panic("producer executable available: " + tool) }
 }
 cases := [][3]uint64{
` + strings.Join(rows, ",\n") + `,
 }
 for _, test := range cases {
  got, err := clamp.RunTyped(test[0], test[1], clamp.DefaultLimits())
  if err != nil || got != test[2] { panic("clamp result differs from independent model") }
 }
 fmt.Printf("clamp Go-only consumer: %d cases OK\n", len(cases))
}
`)
}

// runClampConsumer deploys only the generated Go sources of package with a
// separate consumer and runs it offline with only Go on PATH. It returns
// the case count and the digest of the consumer source.
func runClampConsumer(run runner, workspace, pkg, goPath string, env []string) (int, string, error) {
	consumer := filepath.Join(workspace, "consumer")
	for _, name := range []string{"api.go", "control.go", "runtime.go", "schema.go"} {
		data, err := readRegular(filepath.Join(pkg, name))
		if err != nil {
			return 0, "", err
		}
		if err := os.MkdirAll(filepath.Join(consumer, "clamp"), 0o755); err != nil {
			return 0, "", err
		}
		if err := os.WriteFile(filepath.Join(consumer, "clamp", name), data, 0o644); err != nil {
			return 0, "", err
		}
	}
	if err := os.WriteFile(filepath.Join(consumer, "go.mod"), []byte("module example.com/proved-clamp\n\ngo 1.27.4\n"), 0o644); err != nil {
		return 0, "", err
	}
	cases := clampSamples()
	source := consumerSource(cases)
	if err := os.WriteFile(filepath.Join(consumer, "main.go"), source, 0o644); err != nil {
		return 0, "", err
	}
	isolated := withEnv(env, map[string]string{"PATH": filepath.Dir(goPath), "GOPROXY": "off", "GOSUMDB": "off",
		"GOPRIVATE": "", "GONOPROXY": "", "GONOSUMDB": "", "GOPATH": filepath.Join(workspace, "gopath"),
		"GOMODCACHE": filepath.Join(workspace, "modules"), "GOCACHE": filepath.Join(workspace, "go-cache")})
	output, err := must(run, []string{goPath, "run", "-buildvcs=false", "-mod=readonly", "."}, consumer, isolated)
	if err != nil {
		return 0, "", err
	}
	if output != fmt.Sprintf("clamp Go-only consumer: %d cases OK\n", len(cases)) {
		return 0, "", fmt.Errorf("release consumer returned an unexpected result: %q", output)
	}
	return len(cases), pipeline.Digest(source), nil
}

// walkthrough holds the steps tests replace.
type walkthrough struct {
	repo        string
	run         runner
	generate    func(pipeline.GenerateOptions) (*pipeline.GenerateResult, error)
	checkProofs func(pipeline.CheckProofsOptions) (string, error)
	// workspaces records the temporary workspaces for tests.
	workspaces []string
}

func newWalkthrough(repo string) *walkthrough {
	return &walkthrough{repo: repo, run: runCommand, generate: pipeline.Generate, checkProofs: pipeline.CheckProofs}
}

// Walkthrough runs the release-candidate walkthrough of the checkout at
// repo and writes the receipt to stdout.
func Walkthrough(repo string, stdout io.Writer) error { return newWalkthrough(repo).write(stdout) }

// write prints the receipt only after every stage passed.
func (w *walkthrough) write(stdout io.Writer) error {
	receipt, err := w.receipt()
	if err != nil {
		return err
	}
	data, err := pipeline.EncodeIndentedSorted(receipt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", data)
	return err
}

func (w *walkthrough) receipt() (*pipeline.Object, error) {
	env := BaseEnvironment()
	goPath, err := pinnedGo(w.run, w.repo, env)
	if err != nil {
		return nil, err
	}
	workspace, cleanup, err := tempWorkspace("lean-go-release-walkthrough-")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	w.workspaces = append(w.workspaces, workspace)
	contract, err := copyFixture(w.repo, filepath.Join(workspace, "project"))
	if err != nil {
		return nil, err
	}
	pkg, ir := filepath.Join(workspace, "generated"), filepath.Join(workspace, "clamp.ir.json")
	proof := filepath.Join(workspace, "proof-evidence.json")
	// The checkout's lean-go generate and check-proofs, in process: metered,
	// as the tracked demo, and with the pinned Go release.
	if _, err := w.generate(pipeline.GenerateOptions{SourceDir: contract.SourceDir, Module: contract.Module, Roots: contract.Roots,
		Out: pkg, IROutput: ir, Emit: pipeline.EmitOptions{Mode: "metered"}, Cache: pipeline.DefaultCache(), StrictGo: true}); err != nil {
		return nil, err
	}
	manifest := filepath.Join(pkg, "manifest.json")
	if _, err := w.checkProofs(pipeline.CheckProofsOptions{Contract: contract.Path, Manifest: manifest, Output: proof}); err != nil {
		return nil, err
	}
	if _, err := w.checkProofs(pipeline.CheckProofsOptions{Contract: contract.Path, Manifest: manifest, Evidence: proof}); err != nil {
		return nil, err
	}
	proofBytes, err := readRegular(proof)
	if err != nil {
		return nil, err
	}
	value, err := pipeline.ParseJSON(proofBytes)
	evidence, ok := value.(*pipeline.Object)
	if err != nil || !ok {
		return nil, fmt.Errorf("invalid proof evidence: %v", err)
	}
	cases, consumerDigest, err := runClampConsumer(w.run, workspace, pkg, goPath, env)
	if err != nil {
		return nil, err
	}
	irBytes, err := readRegular(ir)
	if err != nil {
		return nil, err
	}
	receipt := pipeline.NewObject()
	for _, field := range []struct {
		key   string
		value any
	}{
		{"schema", ReceiptSchema}, {"candidate", cli.Version}, {"status", "passed"}, {"go_version", GoVersion},
		{"lean_version", evidence.Values["lean_version"]}, {"lean_commit", evidence.Values["lean_commit"]},
		{"module", contract.Module}, {"roots", contract.Roots}, {"claims", evidence.Values["claims"]},
		{"consumer_cases", cases}, {"contract_sha256", evidence.Values["contract_sha256"]},
		{"project_inputs", evidence.Values["project_inputs"]}, {"tool_inputs", evidence.Values["tool_inputs"]},
		{"generated_manifest_sha256", evidence.Values["generated_manifest_sha256"]},
		{"proof_evidence_sha256", pipeline.Digest(proofBytes)}, {"ir_sha256", pipeline.Digest(irBytes)},
		{"verifier_sha256", pipeline.Digest(walkthroughSource)}, {"lean_go_pipeline", pipeline.SourceDigest()},
		{"consumer_source_sha256", consumerDigest}, {"assurance", evidence.Values["assurance"]},
	} {
		receipt.Set(field.key, field.value)
	}
	return receipt, nil
}
