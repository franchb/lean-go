package wireformat

import (
	"fmt"
	"strings"
)

// Runtime Lean source. It stays inside the extractor's admitted subset on the
// current integer matrix: UInt16 values are only built from bytes with OR and
// shifts and only compared after UInt16.toUInt32, all arithmetic happens in
// UInt32 or UInt64, and there is no division. With a complete UInt16 matrix
// (comparison, arithmetic, UInt32.toUInt16) the u16/to16 helpers and the
// widening in checks could be dropped.

func cap1(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

func leanType(bits int) string { return fmt.Sprintf("UInt%d", bits) }

func allowedName(s *Sub) string { return "allowed" + cap1(s.Name) }
func wordName(w *Word) string   { return fmt.Sprintf("word%d", w.Index) }
func getterName(s *Sub) string  { return "get" + cap1(s.Name) }

// readerWord reads a word in its wide type.
func readerWord(w *Word) string {
	bytes := make([]string, len(w.Order))
	for p, i := range w.Order {
		bytes[p] = fmt.Sprintf("w.b%d", i)
	}
	switch w.Size {
	case 1:
		return bytes[0] + ".toUInt32"
	case 2:
		return "be16w " + strings.Join(bytes, " ")
	case 4:
		return "be32w " + strings.Join(bytes, " ")
	default:
		return "be64w " + strings.Join(bytes, " ")
	}
}

// getterBody extracts a member from its word value x.
func getterBody(s *Sub) string {
	if s.Shift == 0 {
		return fmt.Sprintf("x &&& %d", s.Modulus()-1)
	}
	return fmt.Sprintf("(x >>> %d) &&& %d", s.Shift, s.Modulus()-1)
}

// readExpr is the wide value of a member of the wire value w.
func readExpr(s *Sub) string {
	if s.Whole() {
		return readerWord(s.Word)
	}
	return fmt.Sprintf("%s (%s)", getterName(s), readerWord(s.Word))
}

// headerValue is the Header field value parse stores for a member.
func headerValue(s *Sub) string {
	switch s.Type {
	case 8:
		return fmt.Sprintf("(%s).toUInt8", readExpr(s))
	case 16:
		if s.Whole() {
			hi, lo := s.Word.Order[0], s.Word.Order[1]
			return fmt.Sprintf("u16 w.b%d w.b%d", hi, lo)
		}
		return fmt.Sprintf("to16 (%s)", readExpr(s))
	default:
		return readExpr(s)
	}
}

// widened is the header field in its word's wide type.
func widened(s *Sub, h string) string {
	v := h + "." + s.Name
	if s.Type < 32 {
		return v + ".toUInt32"
	}
	return v
}

// runtimeCond is the Lean condition for a check on a wide value.
func runtimeCond(c *Check, value string) string {
	s := c.Sub
	switch c.Kind {
	case CheckConst:
		return fmt.Sprintf("%s = %d", value, s.Const)
	case CheckReserved:
		return value + " = 0"
	case CheckWidth:
		return fmt.Sprintf("%s < %d", value, s.Modulus())
	case CheckEnum:
		return fmt.Sprintf("%s %s = true", allowedName(s), paren(value, !strings.Contains(value, " ")))
	case CheckMin:
		return fmt.Sprintf("%d ≤ %s", *s.Min, value)
	default:
		return fmt.Sprintf("%s ≤ %d", value, *s.Max)
	}
}

func parseRuntimeCond(c *Check) string { return runtimeCond(c, readExpr(c.Sub)) }
func validRuntimeCond(c *Check) string { return runtimeCond(c, widened(c.Sub, "h")) }

// wordParams are the fields a bit-field word serializer takes; passing them
// one by one keeps the extracted function from unpacking the whole Header.
func wordParams(w *Word) string {
	var out string
	for _, s := range w.Subs {
		if s.Role == RoleField {
			out += fmt.Sprintf(" (%s : %s)", s.Name, leanType(s.Type))
		}
	}
	return out
}

func wordCall(w *Word) string {
	out := wordName(w)
	for _, s := range w.Subs {
		if s.Role == RoleField {
			out += " h." + s.Name
		}
	}
	return out
}

// serWordTerms is the runtime sum for a bit-field word, constants folded.
func serWordBody(w *Word) string {
	var parts []string
	var constant uint64
	for _, s := range w.Subs {
		switch s.Role {
		case RoleConst:
			constant += s.Const << s.Shift
		case RoleField:
			v := s.Name
			if s.Type < 32 {
				v += ".toUInt32"
			}
			if s.Shift == 0 {
				parts = append(parts, v)
			} else {
				parts = append(parts, fmt.Sprintf("%s * %d", v, s.Scale()))
			}
		}
	}
	if constant != 0 {
		parts = append([]string{fmt.Sprint(constant)}, parts...)
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " + ")
}

// serByte is the runtime expression of wire byte p (0 = most significant) of a word.
func serByte(w *Word, p int) string {
	shift := 8 * (w.Size - 1 - p)
	s := w.Subs[0]
	if !w.Group {
		switch s.Role {
		case RoleConst:
			return fmt.Sprint((s.Const >> shift) & 0xff)
		case RoleReserved:
			return "0"
		}
		v := "h." + s.Name
		switch s.Type {
		case 8:
			return v
		case 64:
			if shift == 0 {
				return v + ".toUInt32.toUInt8"
			}
			return fmt.Sprintf("(%s >>> %d).toUInt32.toUInt8", v, shift)
		}
		if shift == 0 {
			return v + ".toUInt8"
		}
		return fmt.Sprintf("(%s >>> %d).toUInt8", v, shift)
	}
	if shift == 0 {
		return fmt.Sprintf("(%s).toUInt8", wordCall(w))
	}
	return fmt.Sprintf("(%s >>> %d).toUInt8", wordCall(w), shift)
}

func (f *Format) wireByteExpr() []string {
	out := make([]string, f.Size)
	for _, w := range f.Words {
		for p, i := range w.Order {
			out[i] = serByte(w, p)
		}
	}
	return out
}

// RuntimeLean renders source/<Module>.lean.
func (f *Format) RuntimeLean() string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	p("-- Code generated by lean-go-format from %s.json; DO NOT EDIT.\n", f.Name)
	p("-- Ordinary first-order Lean for the %s header (%d bytes).\n\n", f.Name, f.Size)
	p("/-- The header bytes in wire order. -/\nstructure Wire where\n")
	for i := range f.Size {
		p("  b%d : UInt8\n", i)
	}
	p("\n/-- The decoded header: every field that is neither constant nor reserved. -/\nstructure Header where\n")
	for _, s := range f.Fields {
		p("  %s : %s\n", s.Name, leanType(s.Type))
	}
	p("\ninductive ParseResult where\n  | ok (header : Header)\n")
	for _, c := range f.Parse {
		p("  | %s\n", c.Reason)
	}
	p("\ninductive Validity where\n  | valid\n")
	for _, c := range f.Valid {
		p("  | %s\n", c.Reason)
	}
	if f.Payload != nil {
		p("\ninductive Availability where\n  | needMore (missing : UInt32)\n  | complete (consumed : UInt32)\n")
	}
	p("\nnamespace %s\n\n", f.Module)
	p(runtimeHelpers)
	for _, w := range f.Words {
		for _, s := range w.Subs {
			if !s.Whole() {
				p("def %s (x : %s) : %s := %s\n", getterName(s), w.Wide(), w.Wide(), getterBody(s))
			}
		}
	}
	for _, s := range f.Subs {
		if s.Enum == nil {
			continue
		}
		p("\ndef %s (v : %s) : Bool :=\n", allowedName(s), s.Word.Wide())
		for _, v := range s.Enum {
			p("  if v = %d then true else\n", v)
		}
		p("  false\n")
	}
	p("\n/-- Checks run in wire order; the first failure is reported. -/\n")
	p("def parse (w : Wire) : ParseResult :=\n")
	var fields []string
	for _, s := range f.Fields {
		fields = append(fields, fmt.Sprintf("%s := %s", s.Name, headerValue(s)))
	}
	ok := ".ok { " + strings.Join(fields, ",\n"+strings.Repeat(" ", 2*len(f.Parse)+8)) + " }"
	writeChain(&b, f.Parse, parseRuntimeCond, ok)
	p("\n/-- Every Header the format can represent; serialize is specified on these. -/\n")
	if len(f.Valid) == 0 {
		p("def validate (_h : Header) : Validity :=\n")
	} else {
		p("def validate (h : Header) : Validity :=\n")
	}
	writeChain(&b, f.Valid, validRuntimeCond, ".valid")
	for _, w := range f.Words {
		if w.Group {
			p("\ndef %s%s : UInt32 := %s\n", wordName(w), wordParams(w), serWordBody(w))
		}
	}
	p("\ndef serialize (h : Header) : Wire :=\n  { ")
	exprs := f.wireByteExpr()
	for i, e := range exprs {
		if i > 0 {
			p(",\n    ")
		}
		p("b%d := %s", i, e)
	}
	p(" }\n")
	if f.Payload != nil {
		s := f.Payload
		total := widened(s, "")[1:]
		p("\n/-- The payload byte count the header declares. -/\n")
		if f.HeaderBytes == 0 {
			p("def payloadLength (%s : %s) : UInt32 := %s\n", s.Name, leanType(s.Type), total)
		} else {
			p("def payloadLength (%s : %s) : UInt32 :=\n  if %s < %d then 0 else %s - %d\n", s.Name, leanType(s.Type), total, f.HeaderBytes, total, f.HeaderBytes)
		}
		p("\n/-- Classify the payload bytes available after the header. -/\n")
		p("def classify (h : Header) (available : UInt32) : Availability :=\n")
		p("  if available < payloadLength h.%s then .needMore (payloadLength h.%s - available)\n", s.Name, s.Name)
		p("  else .complete (payloadLength h.%s)\n", s.Name)
	}
	p("\nend %s\n", f.Module)
	return b.String()
}

func writeChain(b *strings.Builder, checks []*Check, cond func(*Check) string, leaf string) {
	for i, c := range checks {
		fmt.Fprintf(b, "%sif %s then\n", strings.Repeat("  ", i+1), cond(c))
	}
	fmt.Fprintf(b, "%s%s\n", strings.Repeat("  ", len(checks)+1), leaf)
	for i := len(checks) - 1; i >= 0; i-- {
		fmt.Fprintf(b, "%selse .%s\n", strings.Repeat("  ", i+1), checks[i].Reason)
	}
}

const runtimeHelpers = `def be16w (a b : UInt8) : UInt32 := a.toUInt32 * 256 + b.toUInt32
def be32w (a b c d : UInt8) : UInt32 :=
  a.toUInt32 * 16777216 + b.toUInt32 * 65536 + c.toUInt32 * 256 + d.toUInt32
def be64w (a b c d e f g h : UInt8) : UInt64 :=
  (be32w a b c d).toUInt64 * 4294967296 + (be32w e f g h).toUInt64
def u16 (a b : UInt8) : UInt16 := (a.toUInt16 <<< 8) ||| b.toUInt16
def to16 (v : UInt32) : UInt16 := ((v >>> 8).toUInt8.toUInt16 <<< 8) ||| v.toUInt8.toUInt16

`
