package wireformat

import (
	"fmt"
	"strings"
)

// The natural-number formulas below are shared by the contract, the proofs
// and their lemma library. Proof steps rewrite one formula into another, so
// every formula of a given shape must be printed by exactly one function.

func paren(expr string, atomic bool) string {
	if atomic {
		return expr
	}
	return "(" + expr + ")"
}

func bytePow(p int) uint64 { return pow2(8 * p) }

// wordValue combines the bytes of a word, most significant first:
// a0 * 256^(K-1) + ... + a(K-1). It is atomic only for one byte.
func wordValue(atoms []string) (string, bool) {
	k := len(atoms)
	parts := make([]string, k)
	for i, a := range atoms {
		if i == k-1 {
			parts[i] = a
		} else {
			parts[i] = fmt.Sprintf("%s * %d", a, bytePow(k-1-i))
		}
	}
	return strings.Join(parts, " + "), k == 1
}

// memberValue extracts a member from the value of its word.
func memberValue(s *Sub, x string, atomic bool) string {
	switch {
	case s.Whole():
		return x
	case s.Shift == 0:
		return fmt.Sprintf("%s %% %d", paren(x, atomic), s.Modulus())
	default:
		return fmt.Sprintf("%s / %d %% %d", paren(x, atomic), s.Scale(), s.Modulus())
	}
}

// byteOf is byte p (0 = most significant) of a K-byte value.
func byteOf(x string, atomic bool, k, p int) string {
	if q := bytePow(k - 1 - p); q != 1 {
		return fmt.Sprintf("%s / %d %% 256", paren(x, atomic), q)
	}
	return paren(x, atomic) + " % 256"
}

// packed is v0 * S0 + ... + vn * Sn over the members of a word, in member
// order (most significant first). It is never atomic.
func packed(w *Word, values []string) string {
	parts := make([]string, len(w.Subs))
	for j, s := range w.Subs {
		parts[j] = fmt.Sprintf("%s * %d", values[j], s.Scale())
	}
	return strings.Join(parts, " + ")
}

// wireAtoms returns `w.bI.toNat` for the bytes of a word in significance order.
func wireAtoms(w *Word, v string) []string {
	atoms := make([]string, len(w.Order))
	for p, i := range w.Order {
		atoms[p] = fmt.Sprintf("%s.b%d.toNat", v, i)
	}
	return atoms
}

// wireMember is the formula of a member in terms of wire bytes of `v`.
func wireMember(s *Sub, v string) string {
	x, atomic := wordValue(wireAtoms(s.Word, v))
	return memberValue(s, x, atomic)
}

// fieldNat is `h.f.toNat`.
func fieldNat(s *Sub, h string) string { return fmt.Sprintf("%s.%s.toNat", h, s.Name) }

// predicate renders a check on a value formula.
func predicate(c *Check, value string) string {
	s := c.Sub
	switch c.Kind {
	case CheckConst:
		return fmt.Sprintf("%s = %d", value, s.Const)
	case CheckReserved:
		return value + " = 0"
	case CheckWidth:
		return fmt.Sprintf("%s < %d", value, s.Modulus())
	case CheckEnum:
		parts := make([]string, len(s.Enum))
		for i, v := range s.Enum {
			parts[i] = fmt.Sprintf("%s = %d", value, v)
		}
		return strings.Join(parts, " ∨ ")
	case CheckMin:
		return fmt.Sprintf("%d ≤ %s", *s.Min, value)
	default:
		return fmt.Sprintf("%s ≤ %d", value, *s.Max)
	}
}

// predicateTerm parenthesizes disjunctions for use inside conjunctions.
func predicateTerm(c *Check, value string) string {
	p := predicate(c, value)
	if c.Kind == CheckEnum && len(c.Sub.Enum) > 1 {
		return "(" + p + ")"
	}
	return p
}

func parseCond(c *Check, w string) string { return predicateTerm(c, wireMember(c.Sub, w)) }
func validCond(c *Check, h string) string { return predicateTerm(c, fieldNat(c.Sub, h)) }

func conj(items []string) string {
	if len(items) == 0 {
		return "True"
	}
	return strings.Join(items, " ∧ ")
}

// tuple builds an anonymous constructor for a right-nested conjunction.
func tuple(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return "⟨" + strings.Join(items, ", ") + "⟩"
}

// proj selects conjunct i (0-based) of a right-nested conjunction of n items.
func proj(x string, i, n int) string {
	if n == 1 {
		return x
	}
	out := x + strings.Repeat(".2", i)
	if i < n-1 {
		out += ".1"
	}
	return out
}

// decodedFields is the conjunction of `h.f.toNat = formula` for every field.
func decodedFields(f *Format, w, h string) []string {
	var out []string
	for _, s := range f.Fields {
		out = append(out, fmt.Sprintf("%s = %s", fieldNat(s, h), wireMember(s, w)))
	}
	return out
}

func parseConds(f *Format, w string) []string {
	var out []string
	for _, c := range f.Parse {
		out = append(out, parseCond(c, w))
	}
	return out
}

func validConds(f *Format, h string) []string {
	var out []string
	for _, c := range f.Valid {
		out = append(out, validCond(c, h))
	}
	return out
}

// payloadNat is the payload byte count the length field declares.
func payloadNat(f *Format, h string) string {
	if f.HeaderBytes == 0 {
		return fieldNat(f.Payload, h)
	}
	return fmt.Sprintf("%s - %d", fieldNat(f.Payload, h), f.HeaderBytes)
}
