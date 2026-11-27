package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/franchb/lean-go/internal/modulegen"
	"github.com/franchb/lean-go/internal/moduleir"
	"github.com/franchb/lean-go/internal/nativegen"
)

// EmitOptions selects how module IR becomes Go.
type EmitOptions struct {
	Mode           string // "native" (default) or "metered"
	NativeMaxDepth int    // native mode only; 0 selects the default
}

// Emit validates module IR and returns the generated package files.
func Emit(data []byte, opts EmitOptions) (map[string][]byte, error) {
	mode := opts.Mode
	if mode == "" {
		mode = "native"
	}
	if mode != "metered" && mode != "native" {
		return nil, fmt.Errorf("unknown mode %q: use metered or native", mode)
	}
	if opts.NativeMaxDepth != 0 && mode != "native" {
		return nil, fmt.Errorf("-native-max-depth requires -mode native")
	}
	if len(data) > moduleir.MaxBytes {
		return nil, fmt.Errorf("IR exceeds %d bytes", moduleir.MaxBytes)
	}
	var envelope struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("IR envelope: %w", err)
	}
	if envelope.Schema != moduleir.Schema {
		return nil, fmt.Errorf("unsupported IR schema %q; regenerate the IR with the current lean-go tool (module schema %s)", envelope.Schema, moduleir.Schema)
	}
	program, err := moduleir.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if mode == "native" {
		return nativegen.Generate(program, nativegen.Options{MaxDepth: opts.NativeMaxDepth})
	}
	return modulegen.Generate(program)
}

// EmitFile reads IR from path, emits Go and publishes the package to out.
func EmitFile(path, out string, opts EmitOptions, replace bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, moduleir.MaxBytes+1))
	if err != nil {
		return err
	}
	files, err := Emit(data, opts)
	if err != nil {
		return err
	}
	return PublishPackage(out, files, replace)
}

// GenerateOptions is one source-to-Go extraction.
type GenerateOptions struct {
	SourceDir string
	Module    string
	Roots     []string
	Out       string // Go package directory
	IROutput  string // separate IR file
	Emit      EmitOptions
	// Replace swaps an existing differing package after full success.
	Replace bool
	Cache   *Cache
	// StrictGo refuses to run unless this binary was built with RequiredGo,
	// whose go/format output the tracked artifacts use. Otherwise a binary
	// built by another Go release continues with a warning in the result.
	StrictGo bool

	harness func(module string, roots []string) string
}

// GenerateResult reports a successful extraction.
type GenerateResult struct {
	Out, IROutput string
	IR            []byte
	Files         map[string][]byte
	Cache         CacheStatus
	// Warning is a one-line note, such as a Go toolchain other than RequiredGo.
	Warning string
}

// GoWarning returns the non-strict toolchain warning, or "" for RequiredGo.
func GoWarning() string { return goWarning(goVersion()) }

func goWarning(version string) string {
	if checkGoVersion(version) == nil {
		return ""
	}
	return fmt.Sprintf("lean-go: warning: built with %s, not the pinned %s; generated Go may differ from tracked artifacts in formatting", version, RequiredGo)
}

// Summary is the human-readable success line.
func (r *GenerateResult) Summary() string {
	return fmt.Sprintf("Generated %s; recorded producer IR in %s", r.Out, r.IROutput)
}

func validateOutputPaths(output, irOutput string) error {
	pkg, err := resolvePath(output)
	if err != nil {
		return err
	}
	ir, err := resolvePath(irOutput)
	if err != nil {
		return err
	}
	if pkg == ir || isRelativeTo(ir, pkg) || isRelativeTo(pkg, ir) {
		return exportError("package and IR output paths must not overlap")
	}
	return nil
}

// validateOutput keeps the IR file away from source and project configuration.
func validateOutput(output, sourceDir string, allowMissingParent bool) error {
	output, err := absolutePath(output)
	if err != nil {
		return err
	}
	if isSymlink(output) || (exists(output) && !isFile(output)) {
		return exportError("output must be a regular file, not a symlink")
	}
	if !allowMissingParent && !isDir(filepath.Dir(output)) {
		return exportError("output parent does not exist")
	}
	resolved, err := resolvePath(output)
	if err != nil {
		return err
	}
	source, err := resolvePath(sourceDir)
	if err != nil {
		return err
	}
	base := filepath.Base(resolved)
	if isRelativeTo(resolved, source) && (filepath.Ext(resolved) == ".lean" || base == "lakefile.toml" || base == "lean-toolchain") {
		return exportError("output conflicts with source or project configuration")
	}
	return nil
}

// Generate extracts Lean sources into a Go package and IR file. Nothing is
// published until the producer, the freshness checks and the emitter succeed.
func Generate(opts GenerateOptions) (*GenerateResult, error) {
	if opts.SourceDir == "" || opts.Module == "" || len(opts.Roots) == 0 {
		return nil, exportError("--source-dir, --module and --root must be supplied together")
	}
	if opts.Out == "" || opts.IROutput == "" {
		return nil, exportError("module generation requires explicit --out and --ir-output paths")
	}
	output, err := absolutePath(opts.Out)
	if err != nil {
		return nil, err
	}
	irOutput, err := absolutePath(opts.IROutput)
	if err != nil {
		return nil, err
	}
	if err := validateOutputPaths(output, irOutput); err != nil {
		return nil, err
	}
	if err := ValidateName(opts.Module); err != nil {
		return nil, err
	}
	roots, err := SelectRoots(opts.Roots)
	if err != nil {
		return nil, err
	}
	sources, err := SourceSnapshot(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	if _, ok := sources.Lookup(opts.Module); !ok {
		return nil, exportError("requested source module does not exist")
	}
	if err := validateOutput(irOutput, opts.SourceDir, true); err != nil {
		return nil, err
	}
	sourceRoot, err := absolutePath(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	resolvedOutput, err := resolvePath(output)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		path, err := resolvePath(filepath.Join(sourceRoot, filepath.FromSlash(source.Relative)))
		if err == nil && isRelativeTo(path, resolvedOutput) {
			return nil, exportError("package output overlaps source or trusted producer inputs")
		}
	}
	if _, err := SelectedCompiler(); err != nil {
		return nil, staged("toolchain", "toolchain_mismatch", err)
	}
	warning := goWarning(goVersion())
	if warning != "" && opts.StrictGo {
		return nil, staged("toolchain", "toolchain_mismatch", checkGoVersion(goVersion()))
	}
	resolvedSource, err := resolvePath(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	exported, err := ExportModule(ExportOptions{SourceDir: resolvedSource, Module: opts.Module, Roots: roots, Cache: opts.Cache, harness: opts.harness})
	if err != nil {
		return nil, err
	}
	value, err := ParseJSON(exported.IR)
	if err != nil {
		return nil, err
	}
	program, ok := value.(*Object)
	if !ok {
		return nil, exportError("module producer inputs changed after export")
	}
	if err := VerifyInputs(opts.SourceDir, program); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return nil, staged("emission", "emission_failed", err)
	}
	files, err := Emit(exported.IR, opts.Emit)
	if err != nil {
		return nil, staged("emission", "emission_failed", fmt.Errorf("lean-go: %w", err))
	}
	if err := PublishPackage(output, files, opts.Replace); err != nil {
		return nil, staged("emission", "emission_failed", fmt.Errorf("lean-go: %w", err))
	}
	if err := PublishFile(irOutput, exported.IR, true); err != nil {
		return nil, staged("emission", "emission_failed", err)
	}
	return &GenerateResult{Out: output, IROutput: irOutput, IR: exported.IR, Files: files, Cache: exported.Cache, Warning: warning}, nil
}

// goVersion is a variable so tests can simulate another build toolchain.
var goVersion = runtime.Version
