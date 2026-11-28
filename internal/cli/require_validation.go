package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/franchb/lean-go/internal/pipeline"
)

// check-proofs --require-validation ties the two kernel-checked links to one
// program: the proof evidence speaks about the Lean function, the validation
// report about the IR, and a package is covered by both only when the report
// is about the IR the package was emitted from.
//
// A manifest records that IR whole, as "program". Encoded the way an IR file
// is (compact, keys sorted, one trailing newline) it is byte for byte what
// `generate --ir-output` writes, and the digest of those bytes is the
// ir_sha256 of a validation report. (The manifest's own ir_sha256 is the
// digest of another encoding of the same program and cannot be compared with
// it.) check-proofs requires the manifest's program to be a fresh export of
// the contract's sources, so a report accepted here covers the program the
// evidence is bound to.
//
// The report is read, not proved again. It must be the file this lean-go
// writes: same schema, same validation library and runner.

const (
	maxManifestBytes = 4 * 1024 * 1024
	maxReportBytes   = 16 * 1024 * 1024
)

// boundIR is the IR recorded in a manifest.
type boundIR struct {
	manifest  string
	digest    string
	module    string
	roots     []string
	functions []string
}

func readBounded(path string, limit int64) ([]byte, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// The rule check-proofs applies to its other inputs.
	if !pipeline.SymlinkFree(absolute) {
		return nil, fmt.Errorf("%s: path must not contain symlinks", path)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than %d MiB", path, limit>>20)
	}
	return data, nil
}

func stringList(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		if out[i], ok = item.(string); !ok {
			return nil, false
		}
	}
	return out, true
}

// manifestIR reads the IR a manifest records and digests it as an IR file.
func manifestIR(path string) (*boundIR, error) {
	data, err := readBounded(path, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	malformed := fmt.Errorf("%s: not a generated manifest with its program", path)
	value, err := pipeline.ParseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	manifest, ok := value.(*pipeline.Object)
	if !ok {
		return nil, malformed
	}
	program, ok := manifest.Values["program"].(*pipeline.Object)
	if !ok {
		return nil, malformed
	}
	encoded, err := pipeline.EncodeCompactSorted(program)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	ir := &boundIR{manifest: path, digest: pipeline.Digest(append(encoded, '\n'))}
	if ir.module, ok = program.Values["module"].(string); !ok {
		return nil, malformed
	}
	if ir.roots, ok = stringList(program.Values["roots"]); !ok {
		return nil, malformed
	}
	functions, ok := program.Values["functions"].([]any)
	if !ok {
		return nil, malformed
	}
	for _, item := range functions {
		function, ok := item.(*pipeline.Object)
		if !ok {
			return nil, malformed
		}
		name, ok := function.Values["name"].(string)
		if !ok {
			return nil, malformed
		}
		ir.functions = append(ir.functions, name)
	}
	return ir, nil
}

func sameSet(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

// reportFor requires path to be a complete validation report, written by this
// lean-go, of one of the bound IRs, and returns that IR and the report.
func reportFor(path string, irs []*boundIR) (*boundIR, *pipeline.ValidationReport, error) {
	data, err := readBounded(path, maxReportBytes)
	if err != nil {
		return nil, nil, err
	}
	report, err := pipeline.ParseValidationReport(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", path, err)
	}
	if !reflect.DeepEqual(report.Library, pipeline.ValidationInventory()) {
		return nil, nil, fmt.Errorf("%s: written by another lean-go version (its validation library differs); validate again with this lean-go", path)
	}
	var ir *boundIR
	var bound []string
	for _, candidate := range irs {
		bound = append(bound, candidate.digest)
		if ir == nil && candidate.digest == report.IRSHA256 {
			ir = candidate
		}
	}
	if ir == nil {
		return nil, nil, fmt.Errorf("%s: validates another IR (ir_sha256 %s; the bound IR is %s); validate the IR this package was generated from",
			path, report.IRSHA256, strings.Join(bound, " or "))
	}
	if report.Module != ir.module || !sameSet(report.Roots, ir.roots) {
		return nil, nil, fmt.Errorf("%s: its module or roots are not those of the IR in %s", path, ir.manifest)
	}
	listed := make(map[string]bool, len(report.Functions))
	for _, function := range report.Functions {
		listed[function.Name] = true
	}
	for _, name := range ir.functions {
		if !listed[name] {
			return nil, nil, fmt.Errorf("%s: does not account for IR function %s", path, name)
		}
	}
	if unproved := report.Unproved(); len(unproved) > 0 {
		return nil, nil, fmt.Errorf("%s: not fully proved (%s): %s", path, report.SummaryLine(), strings.Join(unproved, "; "))
	}
	return ir, report, nil
}

// requireValidation requires every report to be accepted by reportFor and
// every manifest's IR to have a report. It returns one line per report.
func requireValidation(manifests, reports []string) ([]string, error) {
	if len(manifests) == 0 {
		return nil, errors.New("no manifest to require a validation report for")
	}
	irs := make([]*boundIR, len(manifests))
	for i, path := range manifests {
		ir, err := manifestIR(path)
		if err != nil {
			return nil, err
		}
		irs[i] = ir
	}
	covered := map[string]bool{}
	var lines []string
	for _, path := range reports {
		ir, report, err := reportFor(path, irs)
		if err != nil {
			return nil, err
		}
		covered[ir.digest] = true
		lines = append(lines, fmt.Sprintf("%s covers the IR of module %s: %s.", path, ir.module, strings.TrimPrefix(report.SummaryLine(), "validation: ")))
	}
	for _, ir := range irs {
		if !covered[ir.digest] {
			return nil, fmt.Errorf("%s: no validation report given for its IR (ir_sha256 %s)", ir.manifest, ir.digest)
		}
	}
	return lines, nil
}
