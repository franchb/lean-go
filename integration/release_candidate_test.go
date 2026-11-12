package integration_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const releaseCandidateVersion = "v0.1.0-alpha.3"

func TestReleaseCandidateConsumer(t *testing.T) {
	// This checks the candidate's public Go libraries without publishing a tag.
	// The focused archive is not a claim about a future public module's checksum.
	proxy := candidateModuleProxy(t)
	runReleaseConsumer(t, releaseCandidateVersion, proxy, "off")
}

func candidateModuleProxy(t *testing.T) string {
	t.Helper()
	// A space exercises URL escaping on both Linux and Windows.
	root := filepath.Join(t.TempDir(), "candidate proxy")
	versions := filepath.Join(root, filepath.FromSlash(releaseModule), "@v")
	if err := os.MkdirAll(versions, 0700); err != nil {
		t.Fatal(err)
	}
	sources := candidateLibrarySources(t)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		entry, err := writer.Create(releaseModule + "@" + releaseCandidateVersion + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(sources[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := json.Marshal(struct{ Version string }{releaseCandidateVersion})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		".info": info,
		".mod":  sources["go.mod"],
		".zip":  archive.Bytes(),
	}
	for extension, data := range files {
		if err := os.WriteFile(filepath.Join(versions, releaseCandidateVersion+extension), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.ToSlash(root)
	proxy := url.URL{Scheme: "file"}
	if strings.HasPrefix(path, "//") {
		proxy.Host, path, _ = strings.Cut(path[2:], "/")
		path = "/" + path
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path // Windows drive paths need file:///C:/..., not file://C:/....
	}
	proxy.Path = path
	return proxy.String()
}

func candidateLibrarySources(t *testing.T) map[string][]byte {
	t.Helper()
	root := ".."
	sources := map[string][]byte{}
	read := func(relative string) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		sources[relative] = data
	}
	read("go.mod")
	read("LICENSE")
	for _, library := range []string{"generated", "quota"} {
		err := filepath.WalkDir(filepath.Join(root, library), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			if !entry.Type().IsRegular() {
				t.Fatalf("candidate library input is not a regular file: %s", path)
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			read(filepath.ToSlash(relative))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return sources
}
