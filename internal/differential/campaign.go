package differential

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

// CampaignSchema identifies a campaign report.
const CampaignSchema = "lean-go-differential-campaign/v1"

// Options select the programs of a campaign. Nil Seeds and Widths mean the
// defaults (seeds 0 and 1, all four widths).
type Options struct {
	Seeds          []uint64
	Widths         []int
	Depth          int
	Cases          int
	Artifacts      string
	Replay         string
	Shrink         bool
	ShrinkAttempts int
	Timeout        time.Duration
}

// DefaultOptions are the options of a campaign without arguments.
func DefaultOptions() Options {
	return Options{Depth: 3, Cases: 32, Artifacts: ".differential", ShrinkAttempts: 20, Timeout: 300 * time.Second}
}

// Checker runs one candidate on every channel.
type Checker interface {
	Check(Program, []Pair) (map[string][]uint64, error)
}

// Campaign runs generated or replayed programs. The hooks default to the
// model and the real backend; tests replace them.
type Campaign struct {
	Options Options
	Repo    string
	Stdout  io.Writer

	generate   func(seed uint64, width, depth int) (Program, error)
	inputs     func(seed uint64, width, count int) ([]Pair, error)
	newBackend func(repo, work string, timeout time.Duration) (Checker, error)
}

// NewCampaign prepares a campaign over the checkout at repo.
func NewCampaign(options Options, repo string, stdout io.Writer) *Campaign {
	return &Campaign{Options: options, Repo: repo, Stdout: stdout, generate: Generate, inputs: Inputs,
		newBackend: func(repo, work string, timeout time.Duration) (Checker, error) {
			return NewBackend(repo, work, timeout)
		}}
}

type programCase struct {
	program Program
	inputs  []Pair
}

func (c *Campaign) selected() ([]programCase, error) {
	o := c.Options
	if o.Replay != "" {
		if o.Seeds != nil || o.Widths != nil {
			return nil, errors.New("--replay cannot be combined with --seed or --width")
		}
		p, pairs, err := LoadCase(o.Replay)
		if err != nil {
			return nil, err
		}
		return []programCase{{p, pairs}}, nil
	}
	seeds, widths := o.Seeds, o.Widths
	if seeds == nil {
		seeds = []uint64{0, 1}
	}
	if widths == nil {
		widths = Widths
	}
	var out []programCase
	for _, seed := range seeds {
		for _, width := range widths {
			p, err := c.generate(seed, width, o.Depth)
			if err != nil {
				return nil, err
			}
			pairs, err := c.inputs(seed, width, o.Cases)
			if err != nil {
				return nil, err
			}
			out = append(out, programCase{p, pairs})
		}
	}
	return out, nil
}

// CaseResult is one line of the campaign output and report.
type CaseResult struct {
	Status         string   `json:"status"`
	Inputs         int      `json:"inputs,omitempty"`
	Error          string   `json:"error,omitempty"`
	Channels       []string `json:"channels,omitempty"`
	Case           string   `json:"case,omitempty"`
	Minimized      string   `json:"minimized,omitempty"`
	Attempts       *int     `json:"attempts,omitempty"`
	RejectedErrors *int     `json:"rejected_errors,omitempty"`
	Seed           uint64   `json:"seed"`
	Width          int      `json:"width"`
}

// Report is report.json of a campaign directory.
type Report struct {
	Schema            string       `json:"schema"`
	Results           []CaseResult `json:"results"`
	ElapsedSeconds    float64      `json:"elapsed_seconds"`
	PlannedPrograms   int          `json:"planned_programs"`
	CompletedPrograms int          `json:"completed_programs"`
}

func (c *Campaign) runCase(p Program, pairs []Pair, dir string, backend Checker) (CaseResult, error) {
	if err := os.Mkdir(dir, 0o755); err != nil {
		return CaseResult{}, err
	}
	if err := SaveCase(filepath.Join(dir, "case.json"), p, pairs, nil, nil); err != nil {
		return CaseResult{}, err
	}
	source, err := RenderLean(p)
	if err != nil {
		return CaseResult{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "Generated.lean"), []byte(source), 0o644); err != nil {
		return CaseResult{}, err
	}
	check := func(candidate Program, values []Pair) (Observation, error) {
		outputs, err := backend.Check(candidate, values)
		if err != nil {
			return Observation{}, err
		}
		return Compare(candidate, values, outputs)
	}
	failure := filepath.Join(dir, "failure.json")
	observation, err := check(p, pairs)
	if err != nil {
		if saveErr := SaveCase(failure, p, pairs, nil, err); saveErr != nil {
			return CaseResult{}, saveErr
		}
		return CaseResult{Status: "error", Error: err.Error(), Case: failure}, nil
	}
	if len(observation.Signature()) == 0 {
		if err := SaveCase(filepath.Join(dir, "case.json"), p, pairs, &observation, nil); err != nil {
			return CaseResult{}, err
		}
		return CaseResult{Status: "passed", Inputs: len(pairs)}, nil
	}
	if err := SaveCase(failure, p, pairs, &observation, nil); err != nil {
		return CaseResult{}, err
	}
	result := CaseResult{Status: "mismatch", Channels: observation.Signature(), Case: failure}
	if c.Options.Shrink {
		reduced, err := ReduceCase(p, pairs, observation, check, c.Options.ShrinkAttempts)
		if err != nil {
			return CaseResult{}, err
		}
		path := filepath.Join(dir, "minimized.json")
		if err := SaveCase(path, reduced.Program, reduced.Inputs, &reduced.Observation, nil); err != nil {
			return CaseResult{}, err
		}
		minimized, err := RenderLean(reduced.Program)
		if err != nil {
			return CaseResult{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "Minimized.lean"), []byte(minimized), 0o644); err != nil {
			return CaseResult{}, err
		}
		result.Minimized = path
		result.Attempts = &reduced.Attempts
		result.RejectedErrors = &reduced.RejectedErrors
	}
	return result, nil
}

// Run executes the campaign. It stops at the first program that does not
// pass and returns exit status 1 when any program failed.
func (c *Campaign) Run() (int, error) {
	cases, err := c.selected()
	if err != nil {
		return 1, err
	}
	if err := os.MkdirAll(c.Options.Artifacts, 0o755); err != nil {
		return 1, err
	}
	parent, err := filepath.Abs(c.Options.Artifacts)
	if err != nil {
		return 1, err
	}
	campaign, err := os.MkdirTemp(parent, "campaign-")
	if err != nil {
		return 1, err
	}
	backend, err := c.newBackend(c.Repo, filepath.Join(campaign, "backend"), c.Options.Timeout)
	if err != nil {
		return 1, err
	}
	started := time.Now()
	results := []CaseResult{}
	for index, item := range cases {
		result, err := c.runCase(item.program, item.inputs, filepath.Join(campaign, fmt.Sprintf("case-%04d", index)), backend)
		if err != nil {
			return 1, err
		}
		result.Seed, result.Width = item.program.Seed, item.program.Width
		results = append(results, result)
		line, err := json.Marshal(result)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(c.Stdout, string(line))
		if result.Status != "passed" {
			break
		}
	}
	report := Report{Schema: CampaignSchema, Results: results,
		ElapsedSeconds:  math.Round(time.Since(started).Seconds()*1000) / 1000,
		PlannedPrograms: len(cases), CompletedPrograms: len(results)}
	if err := writeJSON(filepath.Join(campaign, "report.json"), report); err != nil {
		return 1, err
	}
	fmt.Fprintf(c.Stdout, "Artifacts: %s\n", campaign)
	for _, result := range results {
		if result.Status != "passed" {
			return 1, nil
		}
	}
	return 0, nil
}
