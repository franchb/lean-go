package pipeline

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/franchb/lean-go/producer"
)

// The producer cache holds Lake's build output for the embedded producer
// modules, keyed by a digest of everything that determines it: the cache
// format, the exact `lean --version` text (release, commit and target), the
// fixed Lake library stanza and every embedded producer module (plus, for a
// build that includes it, the translation-validation library). Entries are
// built only in a producer-only workspace, never next to user sources, and
// are verified against their recorded digests before use. Cached files are
// trusted like any local build cache: anyone who can write the cache
// directory can change what the producer does.

const cacheFormat = "lean-go-producer-cache/v1"

// producerLakefile is the Lake configuration of the producer-only workspace.
// The user workspace lists the same package name and LeanGo library.
var producerLakefile = []byte("name = \"lean_go_module_producer\"\nversion = \"0.0.0\"\n\n[[lean_lib]]\nname = \"LeanGo\"\n")

// Cache is a directory of verified producer builds. A nil *Cache disables caching.
type Cache struct {
	Dir string
}

// DefaultCacheDir is lean-go/producer under the user cache directory.
func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "lean-go", "producer"), nil
}

// cacheKey identifies one producer build. An entry that also holds the
// translation-validation library is keyed by those files in addition; the key
// of a build without them is unchanged by the validation files.
func cacheKey(leanVersionText string, validation bool) string {
	h := sha256.New()
	frame := func(label string, data []byte) {
		fmt.Fprintf(h, "%d:%s%d:", len(label), label, len(data))
		h.Write(data)
	}
	frame("format", []byte(cacheFormat))
	frame("lean", []byte(strings.TrimSpace(leanVersionText)))
	frame("lakefile", producerLakefile)
	for _, name := range producer.ModuleNames() {
		frame(name, producer.ModuleFiles()[name])
	}
	if validation {
		frame("validation", []byte(strings.Join(validationTargets, " ")))
		files := producer.ValidationFiles()
		for _, name := range producer.ValidationNames() {
			frame(name, files[name])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// producerTargets are the Lake targets of a producer build.
func producerTargets(validation bool) []string {
	targets := []string{"LeanGo.Module.Export"}
	if validation {
		targets = append(targets, validationTargets...)
	}
	return targets
}

// producerFiles are the embedded files a producer workspace receives.
func producerFiles(validation bool) map[string][]byte {
	files := producer.ModuleFiles()
	if validation {
		for name, data := range producer.ValidationFiles() {
			files[name] = data
		}
	}
	return files
}

// secureDirectory creates dir (and missing parents) with owner-only
// permissions and requires that it is a plain directory this user owns.
func secureDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return checkPrivate(dir)
}

// CacheHealth creates the cache directory if needed and checks its permissions.
func CacheHealth(c *Cache) error { return secureDirectory(c.Dir) }

const manifestName = "MANIFEST"

// lookup returns the verified file set of an entry, or nil when absent or invalid.
func (c *Cache) lookup(key string) (map[string][]byte, error) {
	if err := secureDirectory(c.Dir); err != nil {
		return nil, err
	}
	entry := filepath.Join(c.Dir, key)
	info, err := os.Lstat(entry)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || checkPrivate(entry) != nil {
		return nil, fmt.Errorf("producer cache entry is not a private directory")
	}
	manifest, err := os.ReadFile(filepath.Join(entry, manifestName))
	if err != nil {
		return nil, nil
	}
	expected := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(manifest))
	for scanner.Scan() {
		digest, name, ok := strings.Cut(scanner.Text(), "  ")
		if !ok || len(digest) != 64 || !filepath.IsLocal(name) || strings.Contains(name, `\`) {
			return nil, nil
		}
		expected[name] = digest
	}
	files := map[string][]byte{}
	root := filepath.Join(entry, "build")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 || !(d.IsDir() || d.Type().IsRegular()) {
			return fmt.Errorf("unexpected cache entry type")
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil || len(files) != len(expected) || len(files) == 0 {
		return nil, nil
	}
	for name, data := range files {
		if expected[name] != Digest(data) {
			return nil, nil
		}
	}
	return files, nil
}

// store publishes a complete entry atomically; a concurrent identical entry wins.
func (c *Cache) store(key string, files map[string][]byte) error {
	if err := secureDirectory(c.Dir); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(c.Dir, ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var manifest bytes.Buffer
	for _, name := range names {
		target := filepath.Join(stage, "build", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, files[name], 0o600); err != nil {
			return err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", Digest(files[name]), name)
	}
	if err := os.WriteFile(filepath.Join(stage, manifestName), manifest.Bytes(), 0o600); err != nil {
		return err
	}
	entry := filepath.Join(c.Dir, key)
	if err := os.Rename(stage, entry); err != nil {
		if exists(entry) {
			return nil
		}
		return err
	}
	return nil
}

// readTree returns every regular file below dir, keyed by slash path.
func readTree(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("unexpected symlink in producer build: %s", path)
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

func writeTree(dir string, files map[string][]byte) error {
	for name, data := range files {
		target := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// DefaultCache is the cache a command uses unless told otherwise:
// LEAN_GO_NO_CACHE=1 disables it, LEAN_GO_CACHE_DIR moves it, and otherwise
// it lives under the user cache directory. It returns nil when disabled or
// when no user cache directory exists.
func DefaultCache() *Cache {
	if os.Getenv("LEAN_GO_NO_CACHE") == "1" {
		return nil
	}
	if dir := os.Getenv("LEAN_GO_CACHE_DIR"); dir != "" {
		return &Cache{Dir: dir}
	}
	dir, err := DefaultCacheDir()
	if err != nil {
		return nil
	}
	return &Cache{Dir: dir}
}

// CacheStatus describes how one extraction obtained the producer build.
type CacheStatus string

const (
	CacheDisabled CacheStatus = "disabled"
	CacheHit      CacheStatus = "hit"
	CacheStored   CacheStatus = "stored"
	CacheSkipped  CacheStatus = "unavailable"
)

// prepareProducer returns the cached producer build for this compiler,
// building and storing it in a producer-only workspace on a miss. Any cache
// problem degrades to an uncached build in the user workspace.
func (c *Cache) prepareProducer(lake, leanVersionText string, compiler Compiler, env []string, validation bool) (map[string][]byte, CacheStatus) {
	if c == nil || c.Dir == "" {
		return nil, CacheDisabled
	}
	key := cacheKey(leanVersionText, validation)
	files, err := c.lookup(key)
	if err != nil {
		return nil, CacheSkipped
	}
	if files != nil {
		return files, CacheHit
	}
	workspace, err := os.MkdirTemp("", "lean-go-producer-")
	if err != nil {
		return nil, CacheSkipped
	}
	defer os.RemoveAll(workspace)
	if err := os.WriteFile(filepath.Join(workspace, "lean-toolchain"), compiler.PinFile(), 0o600); err != nil {
		return nil, CacheSkipped
	}
	if err := os.WriteFile(filepath.Join(workspace, "lakefile.toml"), producerLakefile, 0o600); err != nil {
		return nil, CacheSkipped
	}
	if err := writeTree(workspace, producerFiles(validation)); err != nil {
		return nil, CacheSkipped
	}
	if _, err := run(append([]string{lake, "build"}, producerTargets(validation)...), workspace, env); err != nil {
		return nil, CacheSkipped
	}
	files, err = readTree(filepath.Join(workspace, ".lake", "build"))
	if err != nil || len(files) == 0 {
		return nil, CacheSkipped
	}
	if err := c.store(key, files); err != nil {
		return nil, CacheSkipped
	}
	return files, CacheStored
}
