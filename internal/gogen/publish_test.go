package gogen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishPreservesAndLocks(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "normalizer")
	files := map[string][]byte{"runtime.go": []byte("package normalizer\n"), "manifest.json": []byte("{}\n")}
	if err := Publish(dir, files); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "runtime.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(dir, files); err != nil {
		t.Fatal(err)
	}
	again, _ := os.Stat(filepath.Join(dir, "runtime.go"))
	if !again.ModTime().Equal(info.ModTime()) {
		t.Fatal("identical output rewritten")
	}
	changed := map[string][]byte{"runtime.go": []byte("changed"), "manifest.json": files["manifest.json"]}
	if err = Publish(dir, changed); err == nil {
		t.Fatal("changed output replaced")
	}
	actual, _ := os.ReadFile(filepath.Join(dir, "runtime.go"))
	if string(actual) != string(files["runtime.go"]) {
		t.Fatal("prior output lost")
	}
	if err = os.WriteFile(filepath.Join(dir, "extra"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err = Publish(dir, files); err == nil {
		t.Fatal("extra output accepted")
	}
	os.Remove(filepath.Join(dir, "extra"))
	lock := filepath.Join(parent, ".normalizer.lean-go.lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err = Publish(dir, files); err == nil {
		t.Fatal("occupied lock accepted")
	}
	if _, err = os.Stat(lock); err != nil {
		t.Fatal("unowned lock deleted")
	}
	os.Remove(lock)
	for _, name := range []string{"../escape", "a/b", "a\\b", ".", "..", ""} {
		if err = Publish(filepath.Join(parent, "unsafe"), map[string][]byte{name: nil}); err == nil {
			t.Fatalf("unsafe filename %q", name)
		}
	}
	for _, path := range []string{"", string(filepath.Separator), filepath.Join(parent, "missing", "normalizer")} {
		if err = Publish(path, files); err == nil {
			t.Fatalf("unsafe path %q", path)
		}
	}
	link := filepath.Join(parent, "link")
	if err = os.Symlink(dir, link); err == nil {
		if err = Publish(link, files); err == nil {
			t.Fatal("destination symlink accepted")
		}
		if err = Publish(filepath.Join(link, "child"), files); err == nil {
			t.Fatal("parent symlink accepted")
		}
	}
	os.Remove(filepath.Join(dir, "runtime.go"))
	if err = os.Symlink(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "runtime.go")); err == nil {
		if err = Publish(dir, files); err == nil {
			t.Fatal("file symlink accepted")
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "normalizer" && entry.Name() != "link" {
			t.Fatalf("owned staging/lock leaked: %s", entry.Name())
		}
	}
}
