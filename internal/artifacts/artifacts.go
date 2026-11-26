// Package artifacts lists every tracked module artifact of the checkout and
// regenerates them through the in-process pipeline.
package artifacts

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/franchb/lean-go/internal/pipeline"
	"github.com/franchb/lean-go/internal/wireformat"
)

// Package is one generated package, its IR fixture and optional bound evidence.
// Paths are slash-separated and relative to the checkout root.
type Package struct {
	Package, IR, SourceDir, Module string
	Roots                          []string
	Contract, Evidence             string
}

// Name is the package directory's base name.
func (p Package) Name() string { return filepath.Base(filepath.FromSlash(p.Package)) }

func fixture(name, module string, roots ...string) Package {
	return Package{Package: "generated/" + name, IR: "testdata/" + name + ".ir.json", SourceDir: "examples/modules", Module: module, Roots: roots}
}

func demo(name, module string, roots ...string) Package {
	base := "examples/proofs/" + name
	return Package{Package: base + "/" + name, IR: base + "/" + name + ".ir.json", SourceDir: base + "/source",
		Module: module, Roots: roots, Contract: base + "/proof-contract.json", Evidence: base + "/proof-evidence.json"}
}

// Packages are the tracked module packages, in scripts/regenerate_all.py order.
var Packages = []Package{
	fixture("choice", "Choice.Transform", "Choice.rotate"),
	fixture("counters", "Counters", "Counters.advance", "Counters.initial", "Counters.read", "Counters.total"),
	fixture("joins", "Joins", "Joins.update"),
	fixture("local", "Local", "Local.run"),
	fixture("numbers", "Numbers", "Numbers.compute"),
	fixture("records", "Records", "Records.update"),
	fixture("reduced", "Reduced", "Reduced.run"),
	fixture("trees", "Trees", "Trees.mirror"),
	fixture("wide", "Wide", "Wide.compute"),
	{Package: "generated/quota", IR: "testdata/quota.ir.json", SourceDir: "libraries/quota/source", Module: "Quota",
		Roots:    []string{"Quota.create", "Quota.release", "Quota.remaining", "Quota.reserve"},
		Contract: "libraries/quota/proof-contract.json", Evidence: "libraries/quota/proof-evidence.json"},
	demo("clamp", "Clamp", "Clamp.clamp"),
	demo("flags", "Flags", "Flags.clear", "Flags.contains", "Flags.set", "Flags.toggle"),
	demo("words", "Words", "Words.high", "Words.low", "Words.narrow", "Words.pack", "Words.widen"),
	demo("bytes", "ByteCodec", "ByteCodec.decodeBE", "ByteCodec.decodeLE", "ByteCodec.encodeBE", "ByteCodec.encodeLE"),
	demo("header", "HeaderCodec", "HeaderCodec.encode", "HeaderCodec.parse"),
	demo("wire16", "Wire16", "Wire16.classify", "Wire16.decodeBE", "Wire16.encode", "Wire16.encodeBE", "Wire16.parse"),
	demo("crc16", "Crc16", "Crc16.update", "Crc16.updateByte"),
	demo("adler32", "Adler32", "Adler32.checksum", "Adler32.initial", "Adler32.update"),
}

// Frame is the checked-frame composition over two demo packages.
var Frame = struct {
	Contract, Evidence string
	Components         []string
}{"examples/proofs/checkedframe-contract.json", "examples/proofs/checkedframe/proof-evidence.json", []string{"wire16", "crc16"}}

// FormatsDir holds the wire-format examples, one spec.json per directory.
const FormatsDir = "examples/formats"

// FormatNames lists the wire-format example directories.
func FormatNames(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(FormatsDir)))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func at(root, relative string) string { return filepath.Join(root, filepath.FromSlash(relative)) }

// GeneratePackage extracts one package (and its evidence) under stage.
func GeneratePackage(root, stage string, spec Package, cache *pipeline.Cache) error {
	if _, err := pipeline.Generate(pipeline.GenerateOptions{
		SourceDir: at(root, spec.SourceDir), Module: spec.Module, Roots: spec.Roots,
		Out: at(stage, spec.Package), IROutput: at(stage, spec.IR), Cache: cache,
	}); err != nil {
		return fmt.Errorf("%s: %w", spec.Package, err)
	}
	if spec.Contract == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(at(stage, spec.Evidence)), 0o755); err != nil {
		return err
	}
	if _, err := pipeline.CheckProofs(pipeline.CheckProofsOptions{
		Contract: at(root, spec.Contract), Manifest: at(stage, spec.Package+"/manifest.json"), Output: at(stage, spec.Evidence),
	}); err != nil {
		return fmt.Errorf("%s: %w", spec.Evidence, err)
	}
	return nil
}

// CheckFrame writes the composition evidence for packages generated under stage.
func CheckFrame(root, stage, manifests string) error {
	var pairs [][2]string
	for _, name := range Frame.Components {
		for _, spec := range Packages {
			if spec.Name() == name {
				pairs = append(pairs, [2]string{name, at(manifests, spec.Package+"/manifest.json")})
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(at(stage, Frame.Evidence)), 0o755); err != nil {
		return err
	}
	_, err := pipeline.CheckProofs(pipeline.CheckProofsOptions{Contract: at(root, Frame.Contract), ComponentManifests: pairs, Output: at(stage, Frame.Evidence)})
	if err != nil {
		return fmt.Errorf("%s: %w", Frame.Evidence, err)
	}
	return nil
}

// GenerateFormat renders one wire-format example into stage/examples/formats/name.
func GenerateFormat(root, stage, name string, cache *pipeline.Cache) error {
	f, err := wireformat.Load(at(root, FormatsDir+"/"+name+"/spec.json"))
	if err != nil {
		return err
	}
	if err := wireformat.Render(f, at(stage, FormatsDir+"/"+name), cache); err != nil {
		return fmt.Errorf("%s/%s: %w", FormatsDir, name, err)
	}
	return nil
}

// tree reads a file or a directory tree; nil when absent.
func tree(path string) (map[string][]byte, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		return map[string][]byte{"": data}, err
	}
	out := map[string][]byte{}
	err = filepath.WalkDir(path, func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(path, file)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(file)
		out[filepath.ToSlash(rel)] = data
		return err
	})
	return out, err
}

func sameTree(a, b map[string][]byte) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}

// replace installs a fresh artifact (file or whole directory) if it changed.
func replace(root, stage, relative string) (bool, error) {
	fresh, err := tree(at(stage, relative))
	if err != nil {
		return false, err
	}
	tracked, err := tree(at(root, relative))
	if err != nil {
		return false, err
	}
	if sameTree(fresh, tracked) {
		return false, nil
	}
	target := at(root, relative)
	if _, single := fresh[""]; single && len(fresh) == 1 {
		return true, pipeline.PublishFile(target, fresh[""], true)
	}
	if err := os.RemoveAll(target); err != nil {
		return false, err
	}
	for name, data := range fresh {
		path := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Regenerate rebuilds every tracked artifact in a fresh tree and replaces
// tracked copies only after every artifact succeeded. It returns the
// artifacts whose bytes changed, in a fixed order.
func Regenerate(root string, jobs int, cache *pipeline.Cache, progress io.Writer) ([]string, int, error) {
	stage, err := os.MkdirTemp("", "lean-go-regenerate-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(stage)
	formats, err := FormatNames(root)
	if err != nil {
		return nil, 0, err
	}
	if jobs < 1 {
		jobs = 1
	}
	var tasks []func() (string, error)
	for _, spec := range Packages {
		tasks = append(tasks, func() (string, error) { return spec.Package, GeneratePackage(root, stage, spec, cache) })
	}
	for _, name := range formats {
		tasks = append(tasks, func() (string, error) { return FormatsDir + "/" + name, GenerateFormat(root, stage, name, cache) })
	}
	errs := make([]error, len(tasks))
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, jobs)
	for i, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			label, err := task()
			errs[i] = err
			if err == nil {
				mu.Lock()
				fmt.Fprintf(progress, "regenerated %s\n", label)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, 0, err
		}
	}
	if err := CheckFrame(root, stage, stage); err != nil {
		return nil, 0, err
	}
	fmt.Fprintf(progress, "regenerated %s\n", Frame.Evidence)
	var artifacts []string
	for _, spec := range Packages {
		artifacts = append(artifacts, spec.Package, spec.IR)
		if spec.Evidence != "" {
			artifacts = append(artifacts, spec.Evidence)
		}
	}
	artifacts = append(artifacts, Frame.Evidence)
	for _, name := range formats {
		files, err := tree(at(stage, FormatsDir+"/"+name))
		if err != nil {
			return nil, 0, err
		}
		names := make([]string, 0, len(files))
		for file := range files {
			names = append(names, file)
		}
		sort.Strings(names)
		for _, file := range names {
			artifacts = append(artifacts, FormatsDir+"/"+name+"/"+file)
		}
	}
	var changed []string
	for _, artifact := range artifacts {
		ok, err := replace(root, stage, artifact)
		if err != nil {
			return changed, len(artifacts), err
		}
		if ok {
			changed = append(changed, artifact)
		}
	}
	return changed, len(artifacts), nil
}
