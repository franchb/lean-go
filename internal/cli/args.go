package cli

import (
	"fmt"
	"strings"
)

// A small argparse-compatible parser: "--name value", "--name=value",
// repeatable options, two-value options and boolean switches. Both "-name"
// and "--name" spellings are accepted for the flag-style emit command.

type option struct {
	name    string
	values  int // 0 for a switch
	repeat  bool
	choices []string
	help    string
	meta    string
}

type parsed struct {
	values map[string][][]string
	set    map[string]bool
}

func (p *parsed) one(name string) string {
	v := p.values[name]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1][0]
}

func (p *parsed) all(name string) []string {
	var out []string
	for _, v := range p.values[name] {
		out = append(out, v[0])
	}
	return out
}

func (p *parsed) pairs(name string) [][2]string {
	var out [][2]string
	for _, v := range p.values[name] {
		out = append(out, [2]string{v[0], v[1]})
	}
	return out
}

func (p *parsed) has(name string) bool { return p.set[name] }

// usageError is reported with usage text and exit status 2.
type usageError struct{ text string }

func (e *usageError) Error() string { return e.text }

func parse(args []string, options []option) (*parsed, error) {
	byName := map[string]*option{}
	for i := range options {
		byName[options[i].name] = &options[i]
	}
	p := &parsed{values: map[string][][]string{}, set: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			return nil, &usageError{fmt.Sprintf("unrecognized arguments: %s", arg)}
		}
		name, inline, hasInline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		opt, ok := byName[name]
		if !ok {
			return nil, &usageError{fmt.Sprintf("unrecognized arguments: %s", arg)}
		}
		var values []string
		switch {
		case opt.values == 0:
			if hasInline {
				return nil, &usageError{fmt.Sprintf("argument --%s: ignored explicit argument %q", name, inline)}
			}
		case hasInline && opt.values == 1:
			values = []string{inline}
		default:
			if hasInline {
				return nil, &usageError{fmt.Sprintf("argument --%s: expected %d arguments", name, opt.values)}
			}
			if i+opt.values > len(args)-1 {
				return nil, &usageError{fmt.Sprintf("argument --%s: expected %s", name, plural(opt.values))}
			}
			values = args[i+1 : i+1+opt.values]
			for _, v := range values {
				if strings.HasPrefix(v, "-") && v != "-" {
					return nil, &usageError{fmt.Sprintf("argument --%s: expected %s", name, plural(opt.values))}
				}
			}
			i += opt.values
		}
		if len(opt.choices) > 0 && !contains(opt.choices, values[0]) {
			return nil, &usageError{fmt.Sprintf("argument --%s: invalid choice: %q (choose from %s)", name, values[0], strings.Join(opt.choices, ", "))}
		}
		if p.set[name] && !opt.repeat && opt.values == 0 {
			continue
		}
		p.set[name] = true
		p.values[name] = append(p.values[name], values)
	}
	return p, nil
}

func plural(n int) string {
	if n == 1 {
		return "one argument"
	}
	return fmt.Sprintf("%d arguments", n)
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func usageText(command, summary string, options []option) string {
	var b strings.Builder
	fmt.Fprintf(&b, "usage: lean-go %s [options]\n\n%s\n\noptions:\n", command, summary)
	for _, opt := range options {
		flag := "--" + opt.name
		if opt.meta != "" {
			flag += " " + opt.meta
		}
		fmt.Fprintf(&b, "  %-32s %s\n", flag, opt.help)
	}
	return b.String()
}
