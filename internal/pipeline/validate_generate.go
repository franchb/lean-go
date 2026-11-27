package pipeline

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/franchb/lean-go/internal/moduleir"
)

// generateValidation normalizes the validation options of one generation
// and checks the report paths against the package and IR outputs.
func generateValidation(opts GenerateOptions, output, irOutput string) (*ValidationOptions, error) {
	validation, err := opts.Validation.Normalized()
	if err != nil || validation == nil {
		return nil, err
	}
	if validation.Report == "" {
		validation.Report = DefaultReportPath(irOutput)
	}
	if err := checkReportPaths(validation, opts.SourceDir, []string{output}, []string{irOutput}); err != nil {
		return nil, err
	}
	return validation, nil
}

// checkReportPaths resolves the report paths and keeps them away from the
// sources, from each other and from the other outputs.
func checkReportPaths(validation *ValidationOptions, sourceDir string, dirs, files []string) error {
	paths := []*string{&validation.Report}
	if validation.Diagnostics != "" {
		paths = append(paths, &validation.Diagnostics)
	}
	var seen []string
	for _, file := range files {
		resolved, err := resolvePath(file)
		if err != nil {
			return err
		}
		seen = append(seen, resolved)
	}
	for _, path := range paths {
		absolute, err := absolutePath(*path)
		if err != nil {
			return err
		}
		if err := validateOutput(absolute, sourceDir, true); err != nil {
			return err
		}
		resolved, err := resolvePath(absolute)
		if err != nil {
			return err
		}
		for _, other := range seen {
			if resolved == other {
				return exportError("validation report paths must differ from each other and from the IR output")
			}
		}
		for _, dir := range dirs {
			pkg, err := resolvePath(dir)
			if err != nil {
				return err
			}
			if resolved == pkg || isRelativeTo(resolved, pkg) {
				return exportError("validation report paths must not be inside the package output")
			}
		}
		seen = append(seen, resolved)
		*path = absolute
	}
	return nil
}

// publishValidation writes the report and, when requested, the diagnostics.
func publishValidation(validation *ValidationOptions, result *ValidationResult) error {
	if validation == nil || result == nil {
		return nil
	}
	result.ReportPath, result.DiagnosticsPath = validation.Report, validation.Diagnostics
	if err := PublishFile(validation.Report, result.ReportJSON, true); err != nil {
		return staged("emission", "output_failed", err)
	}
	if validation.Diagnostics != "" {
		if err := PublishFile(validation.Diagnostics, result.DiagnosticsJSON, true); err != nil {
			return staged("emission", "output_failed", err)
		}
	}
	return nil
}

// requireValidation fails a "require" run with an incomplete report, after
// writing the report so that it can be inspected.
func requireValidation(validation *ValidationOptions, result *ValidationResult) error {
	if validation == nil || result == nil || validation.Mode != ValidateRequire || result.Report.Complete() {
		return nil
	}
	if err := publishValidation(validation, result); err != nil {
		return err
	}
	return validationFailure(validation, result)
}

func validationFailure(validation *ValidationOptions, result *ValidationResult) error {
	unproved := result.Report.Unproved()
	message := fmt.Sprintf("translation validation incomplete (--validate require): %s; report written to %s",
		strings.Join(unproved, "; "), validation.Report)
	hint := "Inspect the report; --validate report records the result without failing."
	return stagedHint("validation", "validation_failed", &hint, exportError(message))
}

// ValidateOptions is one standalone validation of an existing IR file.
type ValidateOptions struct {
	SourceDir string
	Module    string
	Roots     []string
	// IR is the IR file the sources must export to, byte for byte.
	IR         string
	Validation ValidationOptions
	Cache      *Cache
}

// Validate exports the module again in a fresh workspace, requires that the
// result is byte-identical to the given IR file, validates it and writes the
// report. In "require" mode it fails, after writing the report, unless every
// function and root is proved.
func Validate(opts ValidateOptions) (*ValidationResult, error) {
	if opts.SourceDir == "" || opts.Module == "" || len(opts.Roots) == 0 || opts.IR == "" {
		return nil, exportError("--source-dir, --module, --root and --ir must be supplied together")
	}
	if opts.Validation.Mode == "" || opts.Validation.Mode == ValidateOff {
		opts.Validation.Mode = ValidateReport
	}
	validation, err := opts.Validation.Normalized()
	if err != nil {
		return nil, err
	}
	if validation.Report == "" {
		validation.Report = DefaultReportPath(opts.IR)
	}
	if err := checkReportPaths(validation, opts.SourceDir, nil, []string{opts.IR}); err != nil {
		return nil, err
	}
	f, err := os.Open(opts.IR)
	if err != nil {
		return nil, err
	}
	tracked, err := io.ReadAll(io.LimitReader(f, moduleir.MaxBytes+1))
	f.Close()
	if err != nil {
		return nil, err
	}
	resolvedSource, err := resolvePath(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	exported, err := ExportModule(ExportOptions{SourceDir: resolvedSource, Module: opts.Module, Roots: opts.Roots, Cache: opts.Cache, Validation: validation})
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(exported.IR, tracked) {
		return nil, exportErrorf("%s is not the IR these sources export to; regenerate it first", opts.IR)
	}
	if err := requireValidation(validation, exported.Validation); err != nil {
		return exported.Validation, err
	}
	if err := publishValidation(validation, exported.Validation); err != nil {
		return nil, err
	}
	return exported.Validation, nil
}
