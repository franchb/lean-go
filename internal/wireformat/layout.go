package wireformat

import (
	"fmt"
	"slices"
	"sort"
)

func applyConstraints(path string, s *Sub, c Constraints) error {
	limit := maxOf(s.Width)
	switch {
	case c.Const != nil:
		if c.Reserved || c.Enum != nil || c.Min != nil || c.Max != nil {
			return errAt(path, "%q: \"const\" cannot be combined with reserved, enum, min or max", s.Name)
		}
		if uint64(*c.Const) > limit {
			return errAt(path, "%q: const %d does not fit in %d bits (max %d)", s.Name, uint64(*c.Const), s.Width, limit)
		}
		s.Role, s.Const = RoleConst, uint64(*c.Const)
		return nil
	case c.Reserved:
		if c.Enum != nil || c.Min != nil || c.Max != nil {
			return errAt(path, "%q: \"reserved\" cannot be combined with enum, min or max", s.Name)
		}
		s.Role = RoleReserved
		return nil
	}
	s.Role = RoleField
	switch {
	case s.Width <= 8:
		s.Type = 8
	case s.Width <= 16:
		s.Type = 16
	case s.Width <= 32:
		s.Type = 32
	default:
		s.Type = 64
	}
	if c.Enum != nil {
		if len(c.Enum) == 0 {
			return errAt(path, "%q: enum must list at least one value", s.Name)
		}
		if len(c.Enum) > 32 {
			return errAt(path, "%q: enum lists %d values; at most 32 are supported", s.Name, len(c.Enum))
		}
		seen := map[uint64]bool{}
		for _, v := range c.Enum {
			if uint64(v) > limit {
				return errAt(path, "%q: enum value %d does not fit in %d bits (max %d)", s.Name, uint64(v), s.Width, limit)
			}
			if seen[uint64(v)] {
				return errAt(path, "%q: enum value %d is listed twice", s.Name, uint64(v))
			}
			seen[uint64(v)] = true
			s.Enum = append(s.Enum, uint64(v))
		}
	}
	if c.Min != nil {
		v := uint64(*c.Min)
		if v > limit {
			return errAt(path, "%q: min %d does not fit in %d bits (max %d)", s.Name, v, s.Width, limit)
		}
		if v == 0 {
			return errAt(path, "%q: min 0 is always satisfied; remove it", s.Name)
		}
		s.Min = &v
	}
	if c.Max != nil {
		v := uint64(*c.Max)
		if v > limit {
			return errAt(path, "%q: max %d does not fit in %d bits (max %d)", s.Name, v, s.Width, limit)
		}
		if v == limit {
			return errAt(path, "%q: max %d is the largest %d-bit value and always satisfied; remove it", s.Name, v, s.Width)
		}
		s.Max = &v
	}
	if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
		return errAt(path, "%q: min %d is greater than max %d; no value is accepted", s.Name, *s.Min, *s.Max)
	}
	if s.Enum != nil && (s.Min != nil || s.Max != nil) {
		if !slices.ContainsFunc(s.Enum, s.inRange) {
			return errAt(path, "%q: no enum value lies within min/max; no value is accepted", s.Name)
		}
	}
	return nil
}

func (s *Sub) inRange(v uint64) bool {
	return (s.Min == nil || v >= *s.Min) && (s.Max == nil || v <= *s.Max)
}

func buildBits(path string, word *Word, bits []BitSpec, claim func(path, name string) error) error {
	total := word.Bits()
	if len(bits) == 0 {
		return errAt(path, "a bitfield word needs at least one entry in \"bits\"")
	}
	explicit := 0
	for _, b := range bits {
		if b.LSB != nil {
			explicit++
		}
	}
	if explicit != 0 && explicit != len(bits) {
		return errAt(path, "give \"lsb\" for every bit-field of the word or for none")
	}
	type span struct {
		lsb, width int
		path       string
		name       string
	}
	var spans []span
	next := total
	for j, b := range bits {
		bpath := fmt.Sprintf("%s.bits[%d]", path, j)
		if err := claim(bpath, b.Name); err != nil {
			return err
		}
		if b.Width < 1 || b.Width > total {
			return errAt(bpath, "%q: width %d must be between 1 and %d", b.Name, b.Width, total)
		}
		lsb := next - b.Width
		if b.LSB != nil {
			lsb = *b.LSB
			if lsb < 0 || lsb+b.Width > total {
				return errAt(bpath, "%q: bits %d..%d lie outside the %d-bit word", b.Name, lsb, lsb+b.Width-1, total)
			}
		} else if lsb < 0 {
			return errAt(bpath, "%q: the bit-fields so far need %d bits, more than the %d-bit word holds",
				b.Name, total-next+b.Width, total)
		}
		next = lsb
		spans = append(spans, span{lsb, b.Width, bpath, b.Name})
		sub := &Sub{Name: b.Name, Word: word, Shift: lsb, Width: b.Width}
		if err := applyConstraints(bpath, sub, b.Constraints); err != nil {
			return err
		}
		word.Subs = append(word.Subs, sub)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].lsb > spans[j].lsb })
	sort.Slice(word.Subs, func(i, j int) bool { return word.Subs[i].Shift > word.Subs[j].Shift })
	for i := 1; i < len(spans); i++ {
		if spans[i].lsb+spans[i].width > spans[i-1].lsb {
			return errAt(spans[i].path, "%q (bits %d..%d) overlaps %q", spans[i].name, spans[i].lsb,
				spans[i].lsb+spans[i].width-1, spans[i-1].name)
		}
	}
	top := total
	for _, sp := range spans {
		end := sp.lsb + sp.width
		if end > top {
			return errAt(sp.path, "%q (bits %d..%d) overlaps another bit-field", sp.name, sp.lsb, end-1)
		}
		if end < top {
			return errAt(path, "bits %d..%d of the %d-bit word are not covered; declare them, for example as reserved", end, top-1, total)
		}
		top = sp.lsb
	}
	if top != 0 {
		return errAt(path, "bits 0..%d of the %d-bit word are not covered; declare them, for example as reserved", top-1, total)
	}
	if len(word.Subs) == 1 {
		return errAt(path, "a bitfield word with a single member is an ordinary integer; use \"type\" instead")
	}
	return nil
}

func reasonName(s *Sub, k CheckKind) string {
	switch k {
	case CheckConst:
		return s.Name + "Mismatch"
	case CheckReserved:
		return s.Name + "Nonzero"
	case CheckWidth:
		return s.Name + "TooWide"
	case CheckEnum:
		return s.Name + "NotAllowed"
	case CheckMin:
		return s.Name + "TooSmall"
	default:
		return s.Name + "TooLarge"
	}
}

// impliedWidth reports whether enum or max already bounds a field by its width.
func (s *Sub) impliedWidth() bool {
	if s.Enum != nil {
		return true // every enum value fits the width
	}
	return s.Max != nil
}

func (f *Format) buildChecks() {
	add := func(list *[]*Check, s *Sub, k CheckKind) {
		*list = append(*list, &Check{Sub: s, Kind: k, Reason: reasonName(s, k)})
	}
	for _, s := range f.Subs {
		switch s.Role {
		case RoleConst:
			add(&f.Parse, s, CheckConst)
		case RoleReserved:
			add(&f.Parse, s, CheckReserved)
		case RoleField:
			if s.Narrow() && !s.impliedWidth() {
				add(&f.Valid, s, CheckWidth)
			}
			if s.Enum != nil {
				add(&f.Parse, s, CheckEnum)
				add(&f.Valid, s, CheckEnum)
			}
			if s.Min != nil {
				add(&f.Parse, s, CheckMin)
				add(&f.Valid, s, CheckMin)
			}
			if s.Max != nil {
				add(&f.Parse, s, CheckMax)
				add(&f.Valid, s, CheckMax)
			}
		}
	}
}

// ClaimCount is the number of theorems the proof contract lists.
func (f *Format) ClaimCount() int {
	n := 1 + len(f.Parse) + 1 // parse_ok_iff, rejections, accepts_iff
	n += 1 + len(f.Valid)     // validate_valid_iff and failures
	n += 3                    // parse_serialize, serialize_parse, parse_validate
	if f.Payload != nil {
		n += 2
	}
	return n
}

// Roots lists the extracted Lean functions.
func (f *Format) Roots() []string {
	roots := []string{f.Module + ".parse", f.Module + ".serialize", f.Module + ".validate"}
	if f.Payload != nil {
		roots = append([]string{f.Module + ".classify"}, roots...)
	}
	return roots
}
