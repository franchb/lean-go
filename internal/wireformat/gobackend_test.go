package wireformat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franchb/lean-go/internal/pipeline"
)

// checkNativeAPI accepts the native packages of the tracked examples and
// reports any name lean.go relies on that the manifest does not provide.
func TestCheckNativeAPI(t *testing.T) {
	for _, name := range exampleNames(t) {
		f, err := Load(filepath.Join(examplesDir, name, "spec.json"))
		if err != nil {
			t.Fatal(err)
		}
		ir, err := os.ReadFile(filepath.Join(examplesDir, name, f.IRPath()))
		if err != nil {
			t.Fatal(err)
		}
		native, err := pipeline.Emit(ir, pipeline.EmitOptions{Mode: "native"})
		if err != nil {
			t.Fatal(err)
		}
		manifest := string(native["manifest.json"])
		if err := checkNativeAPI(f, []byte(manifest)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, change := range [][2]string{
			{`"go": "Parse"`, `"go": "Parse2"`},
			{`"go": "Header"`, `"go": "Header2"`},
			{`"B0"`, `"Byte0"`},
			{`"IsOk"`, `"IsOK"`},
			{`"AsOk"`, `"AsOK"`},
			{`"IsValid"`, `"IsValid2"`},
			{`"mode": "native"`, `"mode": "metered"`},
		} {
			if !strings.Contains(manifest, change[0]) {
				t.Fatalf("%s: manifest lacks %s", name, change[0])
			}
			if checkNativeAPI(f, []byte(strings.Replace(manifest, change[0], change[1], 1))) == nil {
				t.Errorf("%s: %s accepted", name, change[1])
			}
		}
		metered, err := pipeline.Emit(ir, pipeline.EmitOptions{Mode: "metered"})
		if err != nil {
			t.Fatal(err)
		}
		if checkNativeAPI(f, metered["manifest.json"]) == nil {
			t.Errorf("%s: metered manifest accepted", name)
		}
	}
}
