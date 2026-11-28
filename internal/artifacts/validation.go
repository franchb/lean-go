package artifacts

import (
	"fmt"
	"os"
	"strings"

	"github.com/franchb/lean-go/internal/pipeline"
)

// Every tracked module program has a tracked translation-validation report
// next to its IR, written by Regenerate with the default budgets in "report"
// mode: an unproved obligation is recorded, never an error, and a change in
// proof coverage shows up as a diff of the report.

func trackedValidation(report string) *pipeline.ValidationOptions {
	return &pipeline.ValidationOptions{Mode: pipeline.ValidateReport, Report: report}
}

// FormatIR and FormatValidation are a wire-format example's IR and report.
func FormatIR(name string) string         { return FormatsDir + "/" + name + "/" + name + ".ir.json" }
func FormatValidation(name string) string { return pipeline.DefaultReportPath(FormatIR(name)) }

// ValidateFormat validates the wire-format example rendered under
// stage/examples/formats/name against its rendered sources and IR.
func ValidateFormat(stage, name string, cache *pipeline.Cache) error {
	data, err := os.ReadFile(at(stage, FormatIR(name)))
	if err != nil {
		return err
	}
	value, err := pipeline.ParseJSON(data)
	if err != nil {
		return err
	}
	program, ok := value.(*pipeline.Object)
	if !ok {
		return fmt.Errorf("%s is not a JSON object", FormatIR(name))
	}
	module, _ := program.Values["module"].(string)
	var roots []string
	list, _ := program.Values["roots"].([]any)
	for _, root := range list {
		if s, ok := root.(string); ok {
			roots = append(roots, s)
		}
	}
	_, err = pipeline.Validate(pipeline.ValidateOptions{
		SourceDir: at(stage, FormatsDir+"/"+name+"/source"), Module: module, Roots: roots,
		IR: at(stage, FormatIR(name)), Validation: *trackedValidation(at(stage, FormatValidation(name))), Cache: cache,
	})
	return err
}

// CoverageRow is one tracked program's validation coverage.
type CoverageRow struct {
	Program, Report    string
	Proved, Functions  int
	RootsProved, Roots int
	RootsWitnessed     int
	Unproved           []string
	// Unwitnessed lists the established roots whose witnesses are not
	// checked, with their status and reason class.
	Unwitnessed []string
	Status      string
}

// Coverage reads every tracked validation report, packages first, then the
// wire-format examples.
func Coverage(root string) ([]CoverageRow, error) {
	type entry struct{ program, report string }
	var entries []entry
	for _, spec := range Packages {
		if spec.Validates() {
			entries = append(entries, entry{spec.Name(), spec.Validation()})
		}
	}
	formats, err := FormatNames(root)
	if err != nil {
		return nil, err
	}
	for _, name := range formats {
		entries = append(entries, entry{"format " + name, FormatValidation(name)})
	}
	var rows []CoverageRow
	for _, e := range entries {
		data, err := os.ReadFile(at(root, e.report))
		if err != nil {
			return nil, err
		}
		report, err := pipeline.ParseValidationReport(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.report, err)
		}
		var unwitnessed []string
		for _, p := range report.Programs {
			if p.Status != "proved" || p.Witnesses.Status == pipeline.WitnessChecked {
				continue
			}
			unwitnessed = append(unwitnessed, fmt.Sprintf("`%s`: %s (%s)", p.Root,
				strings.ReplaceAll(p.Witnesses.Status, "_", " "), strings.ReplaceAll(p.Witnesses.ReasonClass, "_", " ")))
		}
		rows = append(rows, CoverageRow{
			Program: e.program, Report: e.report, Status: report.Status,
			Proved: report.Summary.Proved, Functions: report.Summary.Functions,
			RootsProved: report.Summary.RootsProved, Roots: report.Summary.Roots,
			RootsWitnessed: report.Summary.RootsWitnessed,
			Unproved:       report.Unproved(), Unwitnessed: unwitnessed,
		})
	}
	return rows, nil
}

// CoverageTable renders the rows as the Markdown table of
// docs/research/translation-validation.md §9.
func CoverageTable(rows []CoverageRow) string {
	var b strings.Builder
	b.WriteString("| Program | Report | Functions proved / total | Roots established / total | Roots witnessed / total | Not proved | Not witnessed |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	var proved, functions, rootsProved, roots, witnessed int
	for _, row := range rows {
		unproved := "–"
		var names []string
		for _, line := range row.Unproved {
			if !strings.HasPrefix(line, "root ") {
				names = append(names, "`"+strings.Replace(line, ": ", "`: ", 1))
			}
		}
		if len(names) > 0 {
			unproved = strings.Join(names, "; ")
		}
		unwitnessed := "–"
		if len(row.Unwitnessed) > 0 {
			unwitnessed = strings.Join(row.Unwitnessed, "; ")
		}
		if row.Status != pipeline.ValidationCompleted {
			unproved, unwitnessed = row.Status, row.Status
		}
		fmt.Fprintf(&b, "| %s | `%s` | %d / %d | %d / %d | %d / %d | %s | %s |\n", row.Program, row.Report,
			row.Proved, row.Functions, row.RootsProved, row.Roots, row.RootsWitnessed, row.Roots, unproved, unwitnessed)
		proved, functions, rootsProved, roots = proved+row.Proved, functions+row.Functions, rootsProved+row.RootsProved, roots+row.Roots
		witnessed += row.RootsWitnessed
	}
	fmt.Fprintf(&b, "| **total** | %d programs | **%d / %d** | **%d / %d** | **%d / %d** | | |\n", len(rows), proved, functions,
		rootsProved, roots, witnessed, roots)
	return b.String()
}

// Markers delimiting the generated coverage table in the design note.
const (
	CoverageDoc   = "docs/research/translation-validation.md"
	CoverageBegin = "<!-- validation-coverage:begin (lean-go regenerate --coverage) -->\n"
	CoverageEnd   = "<!-- validation-coverage:end -->\n"
)

// DocumentedCoverage returns the table between the markers of CoverageDoc.
func DocumentedCoverage(root string) (string, error) {
	data, err := os.ReadFile(at(root, CoverageDoc))
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, rest, ok := strings.Cut(text, CoverageBegin)
	if !ok {
		return "", fmt.Errorf("%s has no coverage begin marker", CoverageDoc)
	}
	table, _, ok := strings.Cut(rest, CoverageEnd)
	if !ok {
		return "", fmt.Errorf("%s has no coverage end marker", CoverageDoc)
	}
	return table, nil
}
