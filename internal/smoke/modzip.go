package smoke

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// modulePath is the lean-go module.
const modulePath = "github.com/franchb/lean-go"

// moduleFiles lists the files the module zip of a tag of repo contains:
// the tracked files (`git ls-files`), without those in directories that
// hold their own go.mod (the examples are separate modules) or under a
// vendor directory. Contents come from the working tree.
func moduleFiles(repo string) ([]string, error) {
	cmd := exec.Command("git", "-C", repo, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files (the module archive is built from the tracked files): %w", err)
	}
	tracked := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var submodules []string
	for _, file := range tracked {
		if path.Base(file) == "go.mod" && file != "go.mod" {
			submodules = append(submodules, path.Dir(file)+"/")
		}
	}
	var files []string
	for _, file := range tracked {
		if file == "" || strings.HasPrefix(file, "vendor/") || strings.Contains(file, "/vendor/") {
			continue
		}
		if slices.ContainsFunc(submodules, func(prefix string) bool { return strings.HasPrefix(file, prefix) }) {
			continue
		}
		info, err := os.Lstat(filepath.Join(repo, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		files = append(files, file)
	}
	slices.Sort(files)
	return files, nil
}

// writeModuleProxy writes a GOPROXY directory serving one version of the
// module built from repo's tracked files.
func writeModuleProxy(repo, proxy, version string) error {
	files, err := moduleFiles(repo)
	if err != nil {
		return err
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(file)))
		if err != nil {
			return err
		}
		entry, err := writer.Create(modulePath + "@" + version + "/" + file)
		if err != nil {
			return err
		}
		if _, err := entry.Write(data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	mod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return err
	}
	info, err := json.Marshal(struct{ Version string }{version})
	if err != nil {
		return err
	}
	versions := filepath.Join(proxy, filepath.FromSlash(modulePath), "@v")
	if err := os.MkdirAll(versions, 0o755); err != nil {
		return err
	}
	for name, data := range map[string][]byte{"list": []byte(version + "\n"), version + ".info": info, version + ".mod": mod, version + ".zip": archive.Bytes()} {
		if err := os.WriteFile(filepath.Join(versions, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// fileURL is the file:// GOPROXY URL of a local directory, on Linux and
// Windows (file:///C:/...).
func fileURL(dir string) string {
	slashed := filepath.ToSlash(dir)
	u := url.URL{Scheme: "file"}
	if strings.HasPrefix(slashed, "//") {
		u.Host, slashed, _ = strings.Cut(slashed[2:], "/")
		slashed = "/" + slashed
	} else if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	u.Path = slashed
	return u.String()
}
