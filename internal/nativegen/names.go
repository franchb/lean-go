package nativegen

import (
	"fmt"
	"go/token"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// allocator hands out deterministic, collision-free Go identifiers. The first
// request for a name receives it unchanged; later requests get a numeric suffix.
type allocator map[string]bool

func (a allocator) allocate(preferred string) string {
	name := preferred
	for suffix := 2; a[name]; suffix++ {
		name = fmt.Sprintf("%s%d", preferred, suffix)
	}
	a[name] = true
	return name
}

// exportedName derives an exported Go identifier from the last component of a
// Lean name. Source text is never inserted verbatim: characters that are not
// letters, digits or underscores become underscores, and names whose first
// letter has no upper case get an X prefix.
func exportedName(lean string) string {
	lean = lean[strings.LastIndex(lean, ".")+1:]
	var b strings.Builder
	for _, r := range lean {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" {
		return "X"
	}
	first, width := utf8.DecodeRuneInString(name)
	name = string(unicode.ToUpper(first)) + name[width:]
	first, _ = utf8.DecodeRuneInString(name)
	if !unicode.IsUpper(first) {
		name = "X" + name
	}
	if !token.IsIdentifier(name) || !token.IsExported(name) {
		return "X"
	}
	return name
}

// lowerFirst turns an exported identifier into an unexported one.
func lowerFirst(name string) string {
	first, width := utf8.DecodeRuneInString(name)
	lower := string(unicode.ToLower(first)) + name[width:]
	if token.IsExported(lower) || !token.IsIdentifier(lower) {
		return "x" + name
	}
	return lower
}

// Generated function bodies use these local names; package-level unexported
// identifiers must not shadow or be shadowed by them.
var localPattern = regexp.MustCompile(`^([vnj][0-9]+|f[0-9]+_[0-9]+)$`)

const reservedInternal = `break case chan const continue default defer else fallthrough for func go goto if
import interface map package range return select struct switch type var
any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string
uint uint8 uint16 uint32 uint64 uintptr true false iota nil
append cap clear close complex copy delete imag len make max min new panic print println real recover
errors d err state data b i n v ok zero next tag p node`

func newInternalAllocator() allocator {
	a := allocator{}
	for _, name := range strings.Fields(reservedInternal) {
		a[name] = true
	}
	return a
}

func (a allocator) allocateInternal(preferred string) string {
	if localPattern.MatchString(preferred) {
		preferred = "fn" + preferred
	}
	name := preferred
	for suffix := 2; a[name] || localPattern.MatchString(name); suffix++ {
		name = fmt.Sprintf("%s%d", preferred, suffix)
	}
	a[name] = true
	return name
}
