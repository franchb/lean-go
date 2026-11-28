package producer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded validation files are exactly the validation library on disk,
// and none of them is a module producer file (whose digests every manifest
// records).
func TestValidationFilesCoverTheLibrary(t *testing.T) {
	onDisk := map[string]bool{}
	err := filepath.WalkDir("LeanGo", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := filepath.ToSlash(path)
		if strings.HasSuffix(name, ".lean") && (name == "LeanGo/Validate.lean" || strings.HasPrefix(name, "LeanGo/Validate/")) {
			onDisk[name] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	embedded := ValidationFiles()
	if len(embedded) != len(onDisk) || len(embedded) == 0 {
		t.Fatalf("embedded %d validation files, %d on disk", len(embedded), len(onDisk))
	}
	for name, data := range embedded {
		disk, err := os.ReadFile(filepath.FromSlash(name))
		if !onDisk[name] || err != nil || string(disk) != string(data) {
			t.Errorf("%s is not the file on disk", name)
		}
		if _, ok := ModuleFiles()[name]; ok {
			t.Errorf("%s is also a module producer file", name)
		}
	}
}
