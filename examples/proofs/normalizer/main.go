package main

import (
	"fmt"
	"io"
	"os"

	normalizer "example.com/proved-normalizer/normalizer"
)

// builder constructs generated expressions and keeps the first error.
type builder struct {
	limits normalizer.Limits
	err    error
}

func (b *builder) tri(value byte) normalizer.Tri {
	var result normalizer.Tri
	var err error
	switch value {
	case 'F':
		result, err = normalizer.NewTriF(b.limits)
	case 'U':
		result, err = normalizer.NewTriU(b.limits)
	default:
		result, err = normalizer.NewTriT(b.limits)
	}
	b.keep(err)
	return result
}

func (b *builder) keep(err error) {
	if b.err == nil {
		b.err = err
	}
}

func (b *builder) lit(value byte) normalizer.Expr {
	e, err := normalizer.NewExprLit(b.tri(value), b.limits)
	b.keep(err)
	return e
}

func (b *builder) variable(id uint32) normalizer.Expr {
	e, err := normalizer.NewExprVar(id, b.limits)
	b.keep(err)
	return e
}

func (b *builder) not(a normalizer.Expr) normalizer.Expr {
	e, err := normalizer.NewExprNeg(a, b.limits)
	b.keep(err)
	return e
}

func (b *builder) and(x, y normalizer.Expr) normalizer.Expr {
	e, err := normalizer.NewExprConj(x, y, b.limits)
	b.keep(err)
	return e
}

func (b *builder) or(x, y normalizer.Expr) normalizer.Expr {
	e, err := normalizer.NewExprDisj(x, y, b.limits)
	b.keep(err)
	return e
}

// show renders an expression with F, U and T for the three truth values.
func show(e normalizer.Expr) string {
	if value, ok := e.AsLit(); ok {
		switch {
		case value.IsF():
			return "F"
		case value.IsU():
			return "U"
		case value.IsT():
			return "T"
		}
	}
	if id, ok := e.AsVar(); ok {
		return fmt.Sprintf("x%d", id)
	}
	if a, ok := e.AsNeg(); ok {
		return "not " + show(a)
	}
	if a, b, ok := e.AsConj(); ok {
		return "(" + show(a) + " and " + show(b) + ")"
	}
	if a, b, ok := e.AsDisj(); ok {
		return "(" + show(a) + " or " + show(b) + ")"
	}
	return "<invalid>"
}

func demonstrate(output io.Writer) error {
	b := &builder{limits: normalizer.DefaultLimits()}
	inputs := []normalizer.Expr{
		b.not(b.and(b.variable(^uint32(0)), b.lit('U'))),
		b.not(b.not(b.lit('F'))),
		b.not(b.or(b.lit('F'), b.variable(0))),
	}
	if b.err != nil {
		return fmt.Errorf("construct input: %w", b.err)
	}
	for _, input := range inputs {
		result, err := normalizer.RunTyped(input, b.limits)
		if err != nil {
			return fmt.Errorf("normalize %s: %w", show(input), err)
		}
		if _, err := fmt.Fprintf(output, "normalize(%s) = %s\n", show(input), show(result)); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := demonstrate(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
