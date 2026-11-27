package modulegen

import (
	"bytes"
	"strings"
	"testing"
)

// GenerateStateMachine is the oracle of the differential tests: it must
// contain no fast-path or native code, and equal Generate wherever Generate
// has no fast-path root.
func TestGenerateStateMachine(t *testing.T) {
	without := 0
	for _, path := range trackedPrograms {
		files, err := GenerateStateMachine(loadProgram(t, path))
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			for _, marker := range []string{"fastCovers", "fastRun", "func " + fastNativePrefix, "type " + fastNativePrefix} {
				if bytes.Contains(data, []byte(marker)) {
					t.Fatalf("%s %s: state-machine output contains %q", trackedName(path), name, marker)
				}
			}
		}
		if _, plan := planOf(t, loadProgram(t, path)); plan != nil {
			continue
		}
		without++
		production, err := Generate(loadProgram(t, path))
		if err != nil {
			t.Fatal(err)
		}
		if len(production) != len(files) {
			t.Fatalf("%s: file sets differ", trackedName(path))
		}
		for name := range production {
			if !bytes.Equal(production[name], files[name]) {
				t.Fatalf("%s %s: differs from Generate although no root is eligible", trackedName(path), name)
			}
		}
	}
	if without == 0 {
		t.Fatal("no tracked program without fast-path roots")
	}
	// A scalar root keeps the exactly metered scalar entry of earlier releases.
	files, err := GenerateStateMachine(loadProgram(t, "../../examples/proofs/clamp/clamp.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["api.go"], []byte("runs the scalar root with native Go values")) {
		t.Fatal("clamp state-machine output lacks the metered scalar entry")
	}
}
