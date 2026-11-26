package artifacts

import (
	"encoding/json"
	"os"
	"testing"
)

// Every tracked package names its emission mode, so a change of the default
// cannot change tracked bytes, and the tracked manifest is of that mode.
func TestEveryPackageNamesItsMode(t *testing.T) {
	root := checkoutRoot(t)
	native := 0
	for _, spec := range Packages {
		data, err := os.ReadFile(at(root, spec.Package+"/manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Schema string `json:"schema"`
			Mode   string `json:"mode"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		switch spec.Mode {
		case "metered":
			if manifest.Schema != "lean-go-manifest/v24" || manifest.Mode != "" {
				t.Errorf("%s: metered entry has a %s manifest", spec.Package, manifest.Schema)
			}
		case "native":
			native++
			if manifest.Schema != "lean-go-native-manifest/v1" || manifest.Mode != "native" || spec.Contract == "" {
				t.Errorf("%s: native entry has a %s manifest or no contract", spec.Package, manifest.Schema)
			}
		default:
			t.Errorf("%s: mode %q is not explicit", spec.Package, spec.Mode)
		}
	}
	if native == 0 {
		t.Error("no native package with proof evidence is tracked")
	}
}
