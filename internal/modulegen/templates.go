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
		Bytes      bool
		UInt8Type  int
		UInt64Type int
		MultiRoot  bool
	}{Wide: moduleir.HasUInt64(g.program.Schema), Bytes: moduleir.HasUInt8(g.program.Schema), UInt8Type: g.types["UInt8"], UInt64Type: g.types["UInt64"], MultiRoot: moduleir.HasMultipleRoots(g.program.Schema)}
	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
