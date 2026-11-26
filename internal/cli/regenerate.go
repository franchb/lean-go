package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/franchb/lean-go/internal/artifacts"
)

var regenerateOptions = []option{
	{name: "jobs", values: 1, meta: "N", help: "concurrent regenerations (default 2)"},
	{name: "repo", values: 1, meta: "DIR", help: "lean-go checkout (default: the one containing the working directory)"},
	{name: "no-cache", help: "build the producer from scratch instead of using the cache"},
}

const regenerateSummary = `Regenerate every tracked module artifact through the real pipeline.

Each package, IR fixture, proof-evidence file and wire-format example is first
produced in a fresh temporary tree. Tracked copies are replaced only after every
artifact succeeded; the command then lists the artifacts whose bytes changed.
The pilot normalizer is not covered.`

func regenerate(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		fmt.Fprint(stdout, usageText("regenerate", regenerateSummary, regenerateOptions))
		return 0
	}
	p, err := parse(args, regenerateOptions)
	jobs := 2
	if err == nil && p.has("jobs") {
		jobs, err = strconv.Atoi(p.one("jobs"))
	}
	if err != nil {
		return usageFailure(stderr, "regenerate", regenerateSummary, regenerateOptions, err)
	}
	repo := p.one("repo")
	if repo == "" {
		repo = "."
	}
	root, err := repositoryRoot(repo)
	if err != nil {
		fmt.Fprintf(stderr, "regeneration failed; tracked artifacts unchanged: %v\n", err)
		return 1
	}
	changed, total, err := artifacts.Regenerate(root, jobs, cacheFor(p.has("no-cache")), stdout)
	for _, path := range changed {
		fmt.Fprintf(stdout, "changed %s\n", path)
	}
	if err != nil {
		if total == 0 {
			fmt.Fprintf(stderr, "regeneration failed; tracked artifacts unchanged: %v\n", err)
		} else {
			fmt.Fprintf(stderr, "regeneration failed while replacing artifacts: %v\n", err)
		}
		return 1
	}
	fmt.Fprintf(stdout, "%d of %d artifacts changed\n", len(changed), total)
	return 0
}
