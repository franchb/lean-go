package modulegen

import (
	"bytes"
	"text/template"

	"github.com/franchb/lean-go/internal/moduleir"
)

// The same runtime templates retain the earlier profiles' source shape.
func (g *generator) renderTemplate(name string) ([]byte, error) {
	source, err := templates.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}
	parsed, err := template.New(name).Delims("[[", "]]").Parse(string(source))
	if err != nil {
		return nil, err
	}
	data := struct {
		Wide       bool
		UInt64Type int
		MultiRoot  bool
	}{moduleir.HasUInt64(g.program.Schema), g.types["UInt64"], g.program.Schema == moduleir.SchemaV7}
	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
