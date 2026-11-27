// Package compatibility runs the compiler-compatibility corpus with an
// explicitly installed baseline and candidate Lean and reports differences
// in admission, canonical IR and independent Go-consumer behavior. It also
// regenerates the proof demos with one explicit installation.
//
// It is a developer harness behind cmd/lean-go-dev; see
// docs/compiler-compatibility.md.
package compatibility

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/pipeline"
)

// HarnessError is an invalid baseline, tool selection, corpus or harness
// precondition: exit status 2, no compatibility conclusion.
type HarnessError struct{ msg string }

func (e *HarnessError) Error() string { return e.msg }

func harnessError(format string, args ...any) *HarnessError {
	return &HarnessError{fmt.Sprintf(format, args...)}
}

// Pinned is the production compiler: the baseline must be exactly this.
var Pinned = pipeline.DefaultCompiler

// VersionPattern matches a Lean 4 release name.
const VersionPattern = `4\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?`

var (
	identityPattern = regexp.MustCompile(`\ALean \(version (` + VersionPattern + `), [^\r\n,]+, commit ([0-9a-f]{40}), Release\)\s*\z`)
	versionOnly     = regexp.MustCompile(`\A` + VersionPattern + `\z`)
)

// Digest is the lowercase SHA-256 of data.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Identity is one observed compiler.
type Identity struct {
	Version        string            `json:"version"`
	Commit         string            `json:"commit"`
	LeanOutput     string            `json:"lean_output,omitempty"`
	LakeLeanOutput string            `json:"lake_lean_output,omitempty"`
	LakeOutput     string            `json:"lake_output,omitempty"`
	Bin            string            `json:"bin,omitempty"`
	Prefix         string            `json:"prefix,omitempty"`
	SHA256         map[string]string `json:"sha256,omitempty"`
}

// ParseIdentity accepts exactly one release identity with a full commit.
func ParseIdentity(output string) (Identity, error) {
	m := identityPattern.FindStringSubmatch(output)
	if m == nil {
		return Identity{}, harnessError("expected one release Lean identity with a full 40-digit commit")
	}
	return Identity{Version: m[1], Commit: m[2]}, nil
}

// MatchingIdentity requires lean --version and lake env lean --version to agree.
func MatchingIdentity(direct, throughLake string) (Identity, error) {
	identity, err := ParseIdentity(direct)
	if err != nil {
		return Identity{}, err
	}
	other, err := ParseIdentity(throughLake)
	if err != nil {
		return Identity{}, err
	}
	if other.Version != identity.Version || other.Commit != identity.Commit {
		return Identity{}, harnessError("lean --version and lake env lean --version disagree")
	}
	return identity, nil
}

// CompilerBin accepts an installation or its bin directory, never an elan
// shim selection: lean and lake must be real executables next to an
// installed Lean library.
func CompilerBin(dir string) (string, error) {
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(dir, "bin")); err == nil && info.IsDir() {
		dir = filepath.Join(dir, "bin")
	}
	for _, name := range []string{"lean", "lake"} {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return "", harnessError("expected real %s executable in explicit compiler bin directory", name)
		}
	}
	for _, name := range []string{"lib/lean/Init.olean", "lib/lean/Lean.olean"} {
		info, err := os.Stat(filepath.Join(filepath.Dir(dir), filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() {
			return "", harnessError("explicit compiler must have an installed Lean library; elan shims are not installations")
		}
	}
	return dir, nil
}

// Snapshot maps checkout-relative slash paths to file contents.
type Snapshot map[string][]byte

// Names returns the sorted paths.
func (s Snapshot) Names() []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s Snapshot) clone() Snapshot {
	out := Snapshot{}
	for k, v := range s {
		out[k] = v
	}
	return out
}

const modulePath = "github.com/franchb/lean-go"

// goList is a variable so tests can count calls.
var goList = func(repo string, packages ...string) ([]byte, error) {
	command := exec.Command(goTool(), append([]string{"list", "-deps", "-json"}, packages...)...)
	command.Dir = repo
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func goTool() string {
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	return "go"
}

// buildClosure lists every file of this module that a build of packages
// reads: Go files for every platform and embedded files, plus go.mod.
func buildClosure(repo string, packages ...string) ([]string, error) {
	out, err := goList(repo, packages...)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	set := map[string]bool{"go.mod": true}
	d := json.NewDecoder(bytes.NewReader(out))
	for d.More() {
		var p struct {
			ImportPath, Dir                      string
			Module                               *struct{ Path string }
			GoFiles, IgnoredGoFiles, EmbedFiles  []string
			CgoFiles, SFiles, OtherFiles, HFiles []string
		}
		if err := d.Decode(&p); err != nil {
			return nil, err
		}
		if p.Module == nil || p.Module.Path != modulePath {
			continue
		}
		dir := p.Dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
		relative, err := filepath.Rel(root, dir)
		if err != nil || strings.HasPrefix(relative, "..") {
			return nil, harnessError("package %s is outside the checkout", p.ImportPath)
		}
		for _, group := range [][]string{p.GoFiles, p.IgnoredGoFiles, p.EmbedFiles, p.CgoFiles, p.SFiles, p.OtherFiles, p.HFiles} {
			for _, name := range group {
				if strings.HasSuffix(name, "_test.go") {
					continue
				}
				set[path.Join(filepath.ToSlash(relative), filepath.ToSlash(name))] = true
			}
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, path.Clean(name))
	}
	sort.Strings(names)
	return names, nil
}

func readSnapshot(repo string, names []string) (Snapshot, error) {
	out := Snapshot{}
	for _, name := range names {
		if path.IsAbs(name) || strings.HasPrefix(name, "../") || name == ".." {
			return nil, harnessError("invalid trusted snapshot path: %s", name)
		}
		file := filepath.Join(repo, filepath.FromSlash(name))
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			return nil, harnessError("invalid trusted snapshot path: %s", name)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		out[name] = data
	}
	return out, nil
}

// producerExtras are producer inputs no Go build embeds: the Lake
// configuration the producer-build probe uses.
var producerExtras = []string{"producer/lakefile.toml"}

// SourceSnapshot is the reviewed closure the compiler runs use: every file a
// build of the lean-go command reads (its Go sources and the embedded
// producer and templates) and the producer's Lake configuration. Caches and
// other checkout files are not copied.
func SourceSnapshot(repo string) (Snapshot, error) {
	names, err := buildClosure(repo, "./cmd/lean-go")
	if err != nil {
		return nil, err
	}
	return readSnapshot(repo, append(names, producerExtras...))
}

// SnapshotInputs adds the harness itself (the build closure of
// cmd/lean-go-dev) and every compatibility input to the snapshot: the
// retained source tree of a run, from which replay.sh rebuilds the harness.
func SnapshotInputs(repo string, snapshot Snapshot) (Snapshot, error) {
	out := snapshot.clone()
	names, err := buildClosure(repo, "./cmd/lean-go-dev")
	if err != nil {
		return nil, err
	}
	harness, err := readSnapshot(repo, names)
	if err != nil {
		return nil, err
	}
	for name, data := range harness {
		out[name] = data
	}
	base := filepath.Join(repo, "compatibility")
	err = filepath.WalkDir(base, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return harnessError("compatibility inputs must not contain symlinks")
		}
		if entry.Type().IsRegular() {
			relative, err := filepath.Rel(repo, file)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(relative)] = data
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WriteSnapshot creates destination and writes every file below it.
func WriteSnapshot(destination string, snapshot Snapshot) error {
	if err := os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	for _, name := range snapshot.Names() {
		file := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, snapshot[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Substitution replaces one exact identity literal.
type Substitution struct{ Old, New string }

// PinSubstitutions lists the exact known identity literals a research run
// may replace: no producer semantics or numeric audit edits. A supported
// compiler needs no adaptation; any other identity replaces the production
// pin in the toolchain file and the Go identity table.
func PinSubstitutions(identity Identity) (map[string][]Substitution, error) {
	if _, err := ParseIdentity(fmt.Sprintf("Lean (version %s, research, commit %s, Release)", identity.Version, identity.Commit)); err != nil {
		return nil, err
	}
	target := pipeline.Compiler{Version: identity.Version, Commit: identity.Commit}
	for _, c := range pipeline.SupportedCompilers {
		if c == target {
			target = Pinned
		}
	}
	return map[string][]Substitution{
		"producer/lean-toolchain": {{string(Pinned.PinFile()), "leanprover/lean4:v" + target.Version + "\n"}},
		"internal/leanversion/identity.go": {
			{`Version = "` + Pinned.Version + `"`, `Version = "` + target.Version + `"`},
			{`Commit  = "` + Pinned.Commit + `"`, `Commit  = "` + target.Commit + `"`},
		},
	}, nil
}

// AdaptedFile records one adapted file.
type AdaptedFile struct {
	Path         string `json:"path"`
	BeforeSHA256 string `json:"before_sha256"`
	AfterSHA256  string `json:"after_sha256"`
}

// Adaptations is adaptations.json of a compiler run.
type Adaptations struct {
	Kind                string        `json:"kind"`
	Files               []AdaptedFile `json:"files"`
	PatchSHA256         string        `json:"patch_sha256"`
	SemanticAdaptations []string      `json:"semantic_adaptations"`
}

// AdaptPins validates every anchor before producing any edit, same-pin runs
// included, and returns the adapted snapshot, its evidence and a unified patch.
func AdaptPins(snapshot Snapshot, identity Identity) (Snapshot, Adaptations, string, error) {
	substitutions, err := PinSubstitutions(identity)
	if err != nil {
		return nil, Adaptations{}, "", err
	}
	adapted := snapshot.clone()
	evidence := Adaptations{Kind: "research_identity_only", Files: []AdaptedFile{}, SemanticAdaptations: []string{}}
	var patch strings.Builder
	for _, name := range sortedKeys(substitutions) {
		data, ok := snapshot[name]
		if !ok {
			return nil, Adaptations{}, "", harnessError("pin adaptation anchor drift: %s is missing", name)
		}
		original := string(data)
		modified := original
		for _, s := range substitutions[name] {
			if strings.Count(original, s.Old) != 1 {
				return nil, Adaptations{}, "", harnessError("pin adaptation anchor drift: %s: %q", name, s.Old)
			}
			modified = strings.Replace(modified, s.Old, s.New, 1)
		}
		if modified != original {
			adapted[name] = []byte(modified)
			evidence.Files = append(evidence.Files, AdaptedFile{Path: name, BeforeSHA256: Digest(data), AfterSHA256: Digest(adapted[name])})
			patch.WriteString(UnifiedDiff(original, modified, "a/"+name, "b/"+name))
		}
	}
	evidence.PatchSHA256 = Digest([]byte(patch.String()))
	return adapted, evidence, patch.String(), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var errNoRepo = errors.New("no lean-go checkout")

// RepositoryRoot finds the lean-go checkout containing dir.
func RepositoryRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for current := dir; ; current = filepath.Dir(current) {
		data, err := os.ReadFile(filepath.Join(current, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module "+modulePath+"\n") {
			return current, nil
		}
		if filepath.Dir(current) == current {
			return "", fmt.Errorf("%w above %s; run inside a checkout or pass --repo", errNoRepo, dir)
		}
	}
}
