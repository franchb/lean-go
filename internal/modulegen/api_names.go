package modulegen

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/franchb/lean-go/internal/moduleir"
)

// goAPI is also the manifest's complete mapping of the emitted public facade.
type goAPI struct {
	Entrypoint   string                       `json:"entrypoint"`
	Types        map[string]string            `json:"types"`
	Constructors map[string]string            `json:"constructors"`
	Accessors    map[string]map[string]string `json:"accessors"`
	Destructors  map[string]apiDestructor     `json:"destructors"`
}

type apiDestructor struct {
	Is string `json:"is"`
	As string `json:"as"`
}

type apiNameAllocator map[string]bool

func (names apiNameAllocator) allocate(preferred string) string {
	name := preferred
	for suffix := 2; names[name]; suffix++ {
		name = fmt.Sprintf("%s%d", preferred, suffix)
	}
	names[name] = true
	return name
}

// exportedAPIName never inserts source names verbatim into Go source. Unicode
// letters are preserved when legal, and scripts without case get an X prefix.
func exportedAPIName(lean string) string {
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
	first, width := utf8.DecodeRuneInString(name)
	if name == "" {
		return "X"
	}
	name = string(unicode.ToUpper(first)) + name[width:]
	first, _ = utf8.DecodeRuneInString(name)
	if !unicode.IsUpper(first) {
		name = "X" + name
	}
	if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
		return "X"
	}
	return name
}

// Keep generated implementation names reserved even when they are absent from
// a particular closure. All wrapper types are allocated before constructors.
const apiReservedNames = `Input Limits DefaultLimits ErrMalformed ErrResource ErrLimits
node Value typeName constructorName inputShape fieldType rootParameters rootResult
checkLimits Import measure budget Run frame execute RunTyped
apiMake apiNode apiMatches apiUInt32Node apiReadUInt32 apiBoolNode apiReadBool`

func newGoAPI(p *moduleir.Program) *goAPI {
	api := &goAPI{
		Entrypoint: "RunTyped", Types: map[string]string{}, Constructors: map[string]string{},
		Accessors: map[string]map[string]string{}, Destructors: map[string]apiDestructor{},
	}
	packageNames := apiNameAllocator{}
	for _, name := range strings.Fields(apiReservedNames) {
		packageNames[name] = true
	}
	scalars := apiScalars(p)
	for name, scalar := range scalars {
		api.Types[name] = scalar.GoType
	}
	for _, typ := range p.Types {
		if _, scalar := api.Types[typ.Name]; !scalar {
			api.Types[typ.Name] = packageNames.allocate(exportedAPIName(typ.Name))
		}
	}
	// Constructor tags describe semantics; source identities determine naming.
	type candidate struct{ lean, preferred string }
	var constructors []candidate
	for _, typ := range p.Types {
		if _, scalar := scalars[typ.Name]; scalar {
			continue
		}
		methods := apiNameAllocator{"v": true}
		if fields, record := p.API.Records[typ.Name]; record {
			constructors = append(constructors, candidate{typ.Constructors[0].Name, "New" + api.Types[typ.Name]})
			api.Accessors[typ.Name] = map[string]string{}
			fields = append([]string(nil), fields...)
			sort.Strings(fields)
			for _, field := range fields {
				api.Accessors[typ.Name][field] = methods.allocate(exportedAPIName(field))
			}
			continue
		}
		ctors := append([]moduleir.Constructor(nil), typ.Constructors...)
		sort.Slice(ctors, func(i, j int) bool { return ctors[i].Name < ctors[j].Name })
		for _, ctor := range ctors {
			name := exportedAPIName(ctor.Name)
			constructors = append(constructors, candidate{ctor.Name, "New" + api.Types[typ.Name] + name})
			api.Destructors[ctor.Name] = apiDestructor{Is: methods.allocate("Is" + name), As: methods.allocate("As" + name)}
		}
	}
	sort.Slice(constructors, func(i, j int) bool { return constructors[i].lean < constructors[j].lean })
	for _, ctor := range constructors {
		api.Constructors[ctor.lean] = packageNames.allocate(ctor.preferred)
	}
	return api
}
