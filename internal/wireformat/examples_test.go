package wireformat

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const examplesDir = "../../examples/formats"

func exampleNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if !slices.Equal(names, []string{"dns", "header", "udp"}) {
		t.Fatalf("unexpected examples %v", names)
	}
	return names
}

// extractedFiles are produced by the pipeline rather than rendered.
func (f *Format) extractedFiles() []string {
	out := []string{f.IRPath(), f.EvidencePath()}
	for _, name := range []string{"functions.go", "manifest.json", "native.go", "types.go"} {
		out = append(out, filepath.Join(f.ExtractedDir(), name))
	}
	return out
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		out = append(out, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Without Lean: the tracked sources of every example are exactly what the
// generator renders today, and every other tracked file is either pipeline
// output or a hand-written README, spec or test.
func TestExamplesMatchGenerator(t *testing.T) {
	for _, name := range exampleNames(t) {
		dir := filepath.Join(examplesDir, name)
		f, err := Load(filepath.Join(dir, "spec.json"))
		if err != nil {
			t.Fatal(err)
		}
		if f.Name != name {
			t.Fatalf("%s: spec name %q", dir, f.Name)
		}
		files := f.Files()
		again := f.Files()
		for path, text := range files {
			if again[path] != text {
				t.Fatalf("%s: rendering %s is not deterministic", name, path)
			}
			tracked, err := os.ReadFile(filepath.Join(dir, path))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if string(tracked) != text {
				t.Errorf("%s: %s differs from the generator output; regenerate with lean-go format generate", name, path)
			}
		}
		pipeline := f.extractedFiles()
		for _, path := range listFiles(t, dir) {
			_, generated := files[path]
			switch {
			case generated, slices.Contains(pipeline, path):
			case path == "spec.json" || path == "README.md":
			// the translation-validation report lean-go regenerate tracks next to the IR
			case path == name+".validation.json":
			case filepath.Dir(path) == f.AdapterDir() && strings.HasSuffix(path, "_test.go"):
			default:
				t.Errorf("%s: unexpected file %s", name, path)
			}
		}
		for _, path := range pipeline {
			if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
				t.Errorf("%s: missing pipeline output: %v", name, err)
			}
		}
	}
}

func goBinary() string {
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(runtime.GOROOT(), "bin", name)
}

func goEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "CGO_ENABLED", "GOTOOLCHAIN", "GOWORK", "GOPROXY", "GOSUMDB", "GOFLAGS":
			continue
		}
		env = append(env, kv)
	}
	return append(append(env, "CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off",
		"GOSUMDB=off", "GOFLAGS="), extra...)
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary(), args...)
	cmd.Dir, cmd.Env = dir, goEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v in %s: %v\n%s", args, dir, err, out)
	}
}

// copyGoModule copies the Go files and go.mod of an example.
func copyGoModule(t *testing.T, src, dst string) {
	t.Helper()
	for _, path := range listFiles(t, src) {
		if path != "go.mod" && !strings.HasSuffix(path, ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, path))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dst, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The examples deploy with Go alone: copy their Go sources and run their tests.
func TestExamplesGoOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three nested modules")
	}
	for _, name := range exampleNames(t) {
		dir := filepath.Join(t.TempDir(), name)
		copyGoModule(t, filepath.Join(examplesDir, name), dir)
		runGo(t, dir, "vet", "./...")
		runGo(t, dir, "test", "-count=1", "./...")
	}
}

const equivalenceTest = `package header_test

import (
	"bytes"
	"errors"
	"testing"

	demo "example.com/lean-go-format-header/internal/demo"
	"example.com/lean-go-format-header/header"
)

// The generated header package and the hand-written examples/proofs/header
// package agree on every version and flags byte and every encodable header.
func TestSameAsProofsHeader(t *testing.T) {
	limits := demo.DefaultLimits()
	for b0 := range 256 {
		for b1 := range 256 {
			for _, length := range []uint16{0, 0x1234, 0xffff} {
				wire := [4]byte{byte(b0), byte(b1), byte(length >> 8), byte(length)}
				q, err := demo.NewByteQuadFromBytes(wire, limits)
				if err != nil {
					t.Fatal(err)
				}
				old, err := demo.RunParse(q, limits)
				if err != nil {
					t.Fatal(err)
				}
				h, _, _, err := header.Parse(wire[:])
				var e *header.Error
				reason := header.Reason(0)
				if errors.As(err, &e) {
					reason = e.Reason
				} else if err != nil {
					t.Fatal(err)
				}
				switch {
				case old.IsUnsupportedVersion():
					if reason != header.ReasonVersionMismatch {
						t.Fatalf("% x: %v", wire, err)
					}
				case old.IsReservedFlags():
					if reason != header.ReasonReservedNonzero {
						t.Fatalf("% x: %v", wire, err)
					}
				default:
					parsed, ok := old.AsParsed()
					flags, _ := parsed.Flags()
					size, _ := parsed.Length()
					if !ok || (reason != 0 && reason != header.ReasonPayloadTruncated) ||
						(reason == 0 && (h.Flags != flags || uint32(h.Length) != size)) {
						t.Fatalf("% x: %+v %v vs %d %d", wire, h, err, flags, size)
					}
				}
			}
		}
	}
	for flags := range uint8(8) {
		for length := range 1 << 16 {
			lh, err := demo.NewHeader(flags, uint32(length), limits)
			if err != nil {
				t.Fatal(err)
			}
			old, err := demo.RunEncode(lh, limits)
			if err != nil {
				t.Fatal(err)
			}
			enc, err := header.AppendSerialize(nil, header.Header{Flags: flags, Length: uint16(length)})
			q, ok := old.AsEncoded()
			if !ok {
				if err == nil || !old.IsReservedFlags() {
					t.Fatalf("flags %d length %d: %v", flags, length, err)
				}
				continue
			}
			want, _ := q.Bytes()
			if err != nil || !bytes.Equal(enc, want[:]) {
				t.Fatalf("flags %d length %d: % x, %v; want % x", flags, length, enc, err, want)
			}
		}
	}
}
`

// The generated header example behaves like the hand-written
// examples/proofs/header package it re-expresses.
func TestHeaderMatchesProofsHeader(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a nested module")
	}
	dir := filepath.Join(t.TempDir(), "header")
	copyGoModule(t, filepath.Join(examplesDir, "header"), dir)
	original := "../../examples/proofs/header/header"
	for _, name := range []string{"api.go", "control.go", "runtime.go", "schema.go"} {
		data, err := os.ReadFile(filepath.Join(original, name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "internal", "demo", name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "header", "equivalence_test.go"), []byte(equivalenceTest), 0o644); err != nil {
		t.Fatal(err)
	}
	runGo(t, dir, "test", "-count=1", "-run", "TestSameAsProofsHeader", "./header")
}
