package moduleir

import "fmt"

// RootNames returns the selected roots of an already validated program.
// The returned slice must not be modified by callers.
func RootNames(p *Program) []string { return p.Roots }

// HasMultipleRoots reports whether a validated program selects more than one
// root. One root keeps the Run/RunTyped API; several share RunRoot.
func HasMultipleRoots(p *Program) bool { return len(p.Roots) > 1 }

func validateRoots(p *Program) error {
	if len(p.Roots) < 1 || len(p.Roots) > MaxRoots {
		return fmt.Errorf("program requires 1..%d roots", MaxRoots)
	}
	previous := ""
	for _, root := range p.Roots {
		if !validName(root) || root <= previous {
			return fmt.Errorf("invalid, duplicate or unordered root %q", root)
		}
		previous = root
	}
	return nil
}
