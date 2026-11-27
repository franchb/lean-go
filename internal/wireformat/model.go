package wireformat

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Limits imposed by the extractor and the proof checker.
const (
	maxHeaderBytes = 64 // record constructors take at most 64 fields
	maxFields      = 64
	maxClaims      = 64 // proof contracts carry at most 64 claims
)

// Role says how a member of a word is treated.
type Role int

const (
	RoleField    Role = iota // a semantic Header field
	RoleConst                // must equal a constant; not stored in Header
	RoleReserved             // must be zero; not stored in Header
)

// CheckKind identifies one declared constraint.
type CheckKind int

const (
	CheckConst CheckKind = iota
	CheckReserved
	CheckWidth // validation only: the value fits its bit width
	CheckEnum
	CheckMin
	CheckMax
)

// Format is the validated, normalized model every generator consumes.
type Format struct {
	Name        string // adapter Go package and directory name
	Module      string // Lean module name
	Description string
	Size        int     // header bytes
	Words       []*Word // in wire order
	Fields      []*Sub  // semantic fields, wire order: the Header structure
	Subs        []*Sub  // every member, wire order
	Payload     *Sub    // payload-length field, if any
	HeaderBytes int     // bytes the length field counts beyond the payload
	Parse       []*Check
	Valid       []*Check
}

// Word is a run of bytes read as one big- or little-endian unsigned value.
type Word struct {
	Index  int
	Offset int   // first wire byte
	Size   int   // bytes: 1, 2, 4 or 8
	Order  []int // wire byte indices, most significant first
	Group  bool  // declared as a bit-field word
	Subs   []*Sub
	Little bool
}

func (w *Word) Bits() int { return 8 * w.Size }

// Wide is the Lean type used to read the word: UInt32 up to 32 bits.
func (w *Word) Wide() string {
	if w.Size == 8 {
		return "UInt64"
	}
	return "UInt32"
}

// Sub is one member of a word.
type Sub struct {
	Name  string
	Word  *Word
	Shift int
	Width int
	Role  Role
	Const uint64
	Enum  []uint64
	Min   *uint64 // effective minimum (includes an implicit header length)
	Max   *uint64
	Type  int // Lean/Go width in bits for RoleField: 8, 16, 32 or 64
}

// Whole reports whether the member spans its entire word.
func (s *Sub) Whole() bool { return s.Shift == 0 && s.Width == s.Word.Bits() }

// Narrow reports whether the member's type admits values above its width.
func (s *Sub) Narrow() bool { return s.Role == RoleField && s.Type > s.Width }

func (s *Sub) Modulus() uint64 { return pow2(s.Width) }
func (s *Sub) Scale() uint64   { return pow2(s.Shift) }

// Check is one parse rejection or validation failure.
type Check struct {
	Sub    *Sub
	Kind   CheckKind
	Reason string // Lean constructor name, also the Go reason suffix
}

func pow2(n int) uint64 {
	if n >= 64 {
		return 0 // callers use pow2Str for 2^64
	}
	return uint64(1) << n
}

// pow2Str renders 2^n in decimal, including 2^64.
func pow2Str(n int) string {
	if n == 64 {
		return "18446744073709551616"
	}
	return fmt.Sprint(pow2(n))
}

func maxOf(width int) uint64 {
	if width >= 64 {
		return ^uint64(0)
	}
	return pow2(width) - 1
}

var (
	packagePattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,30}$`)
	modulePattern  = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,40}$`)
	fieldPattern   = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,30}$`)
)

// Words that cannot be field names: Lean and Go keywords, Lean structure
// auxiliaries, and identifiers the generated code uses.
var reservedNames = func() map[string]bool {
	words := `
abbrev at attribute axiom by calc catch class deriving do else end example exists extends
for forall fun have if import in inductive infix instance let local macro match mutual
namespace noncomputable nomatch nofun notation obtain open opaque partial prefix private
protected return section set_option show structure suffices syntax termination then theorem
this try universe unsafe variable where with from mut break continue unless finally
Type Prop Sort
break case chan const continue default defer else fallthrough func go goto import interface
map package range select struct switch type var
mk rec casesOn recOn noConfusion noConfusionType below brecOn binductionOn ibelow sizeOf
ctorIdx toCtorIdx injEq inj ext ofNat toNat toBitVec val
w h e v x hv hp ha hb
`
	m := map[string]bool{}
	for _, word := range strings.Fields(words) {
		m[word] = true
	}
	return m
}()

type specError struct {
	path string
	msg  string
}

func (e *specError) Error() string { return "spec: " + e.path + ": " + e.msg }

func errAt(path, format string, args ...any) error {
	return &specError{path: path, msg: fmt.Sprintf(format, args...)}
}

func typeBits(t string) (int, bool) {
	switch t {
	case "uint8":
		return 8, true
	case "uint16":
		return 16, true
	case "uint32":
		return 32, true
	case "uint64":
		return 64, true
	}
	return 0, false
}

// Build validates a decoded spec and produces the normalized model.
func Build(spec *Spec) (*Format, error) {
	f := &Format{Name: spec.Name, Module: spec.Module, Description: spec.Description}
	if !packagePattern.MatchString(f.Name) || reservedNames[f.Name] {
		return nil, errAt("name", "%q must be a lower-case Go package name (letters and digits, starting with a letter)", f.Name)
	}
	switch f.Name {
	case "extracted", "internal", "testing", "main":
		return nil, errAt("name", "%q is reserved", f.Name)
	}
	if f.Module == "" {
		f.Module = strings.ToUpper(f.Name[:1]) + f.Name[1:] + "Header"
	}
	if !modulePattern.MatchString(f.Module) {
		return nil, errAt("module", "%q must be a Lean module name: an upper-case letter followed by letters and digits", f.Module)
	}
	if strings.ContainsAny(f.Description, "\r\x00") {
		return nil, errAt("description", "must not contain carriage returns or NUL")
	}
	if len(spec.Fields) == 0 {
		return nil, errAt("fields", "at least one field is required")
	}
	names, folded := map[string]string{}, map[string]string{}
	claim := func(path, name string) error {
		if !fieldPattern.MatchString(name) {
			return errAt(path, "name %q must start with a lower-case letter and contain only letters and digits (at most 31)", name)
		}
		if reservedNames[name] || (len(name) >= 2 && name[0] == 'b' && strings.Trim(name[1:], "0123456789") == "") {
			return errAt(path, "name %q is reserved", name)
		}
		if prev, ok := names[name]; ok {
			return errAt(path, "duplicate name %q (first used at %s)", name, prev)
		}
		if prev, ok := folded[strings.ToLower(name)]; ok {
			return errAt(path, "name %q differs only in case from the name at %s", name, prev)
		}
		names[name] = path
		folded[strings.ToLower(name)] = path
		return nil
	}
	offset := 0
	includesHeader, payloadPath := false, ""
	for i, fs := range spec.Fields {
		path := fmt.Sprintf("fields[%d]", i)
		word := &Word{Index: len(f.Words), Offset: offset}
		switch {
		case fs.Type != "" && fs.Bitfield != "":
			return nil, errAt(path, "a field has either \"type\" or \"bitfield\", not both")
		case fs.Type != "":
			if len(fs.Bits) != 0 {
				return nil, errAt(path, "\"bits\" requires \"bitfield\"")
			}
			bits, ok := typeBits(fs.Type)
			if !ok {
				return nil, errAt(path, "type %q must be uint8, uint16, uint32 or uint64", fs.Type)
			}
			word.Size = bits / 8
			switch fs.Endian {
			case "", "big":
			case "little":
				if bits == 8 {
					return nil, errAt(path, "endian is meaningless for uint8")
				}
				word.Little = true
			default:
				return nil, errAt(path, "endian %q must be \"big\" or \"little\"", fs.Endian)
			}
			if bits == 8 && fs.Endian != "" {
				return nil, errAt(path, "endian is meaningless for uint8")
			}
			if err := claim(path, fs.Name); err != nil {
				return nil, err
			}
			sub := &Sub{Name: fs.Name, Word: word, Width: bits}
			if err := applyConstraints(path, sub, fs.Constraints); err != nil {
				return nil, err
			}
			if fs.PayloadLength != nil {
				if sub.Role != RoleField {
					return nil, errAt(path, "a payload length cannot be constant or reserved")
				}
				if bits == 64 {
					return nil, errAt(path, "a payload length must be at most 32 bits")
				}
				if f.Payload != nil {
					return nil, errAt(path, "only one payload_length field is allowed (first: %q)", f.Payload.Name)
				}
				f.Payload, includesHeader, payloadPath = sub, fs.PayloadLength.IncludesHeader, path
			}
			word.Subs = []*Sub{sub}
		case fs.Bitfield != "":
			if fs.Name != "" || fs.Endian != "" || fs.PayloadLength != nil || fs.Const != nil || fs.Reserved ||
				fs.Enum != nil || fs.Min != nil || fs.Max != nil {
				return nil, errAt(path, "a bitfield word takes only \"bitfield\" and \"bits\"; put names and constraints on its bits")
			}
			bits, ok := typeBits(fs.Bitfield)
			if !ok || bits == 64 {
				return nil, errAt(path, "bitfield %q must be uint8, uint16 or uint32 (stored big-endian)", fs.Bitfield)
			}
			word.Size, word.Group = bits/8, true
			if err := buildBits(path, word, fs.Bits, claim); err != nil {
				return nil, err
			}
		default:
			return nil, errAt(path, "a field needs \"type\" (an integer) or \"bitfield\" (a word of bit-fields)")
		}
		word.Order = make([]int, word.Size)
		for p := range word.Size {
			if word.Little {
				word.Order[p] = offset + word.Size - 1 - p
			} else {
				word.Order[p] = offset + p
			}
		}
		offset += word.Size
		f.Words = append(f.Words, word)
		for _, s := range word.Subs {
			f.Subs = append(f.Subs, s)
			if s.Role == RoleField {
				f.Fields = append(f.Fields, s)
			}
		}
	}
	f.Size = offset
	if f.Size > maxHeaderBytes {
		return nil, errAt("fields", "the header is %d bytes; at most %d are supported", f.Size, maxHeaderBytes)
	}
	if len(f.Fields) == 0 {
		return nil, errAt("fields", "at least one field must be neither constant nor reserved")
	}
	if len(f.Fields) > maxFields {
		return nil, errAt("fields", "%d header fields; at most %d are supported", len(f.Fields), maxFields)
	}
	if f.Payload != nil && includesHeader {
		// The field counts the header too: the payload is value - Size, so
		// any accepted value must be at least Size.
		f.HeaderBytes = f.Size
		p := f.Payload
		if uint64(f.Size) > maxOf(p.Width) {
			return nil, errAt(payloadPath, "the header size %d does not fit the %d-bit length", f.Size, p.Width)
		}
		if p.Max != nil && *p.Max < uint64(f.Size) {
			return nil, errAt(payloadPath, "max %d is below the header size %d that the length includes", *p.Max, f.Size)
		}
		if p.Min == nil || *p.Min < uint64(f.Size) {
			m := uint64(f.Size)
			p.Min = &m
		}
		if p.Enum != nil && !slices.ContainsFunc(p.Enum, func(v uint64) bool { return v >= uint64(f.Size) }) {
			return nil, errAt(payloadPath, "no enum value is at least the header size %d", f.Size)
		}
	}
	f.buildChecks()
	if n := f.ClaimCount(); n > maxClaims {
		return nil, errAt("fields", "the format needs %d theorems, but a proof contract holds at most %d; reduce the number of constraints", n, maxClaims)
	}
	return f, nil
}
