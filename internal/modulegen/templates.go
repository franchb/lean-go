package modulegen

import (
	"bytes"
	"text/template"

	"github.com/franchb/lean-go/internal/moduleir"
)

func (g *generator) renderTemplate(name string) ([]byte, error) {
	source, err := templates.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}
	parsed, err := template.New(name).Delims("[[", "]]").Parse(string(source))
	if err != nil {
		return nil, err
	}
	// Every package has the full scalar runtime. Only the root count still
	// selects between the Run and RunRoot entrypoint shapes.
	data := struct {
		UInt8Type  int
		UInt16Type int
		UInt64Type int
		MultiRoot  bool
		Fast       bool
	}{
		UInt8Type:  g.types["UInt8"],
		UInt16Type: g.types["UInt16"],
		UInt64Type: g.types["UInt64"],
		MultiRoot:  moduleir.HasMultipleRoots(g.program),
		Fast:       g.fast != nil,
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
