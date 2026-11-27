package pipeline

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = "<dir>"
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(data)
	}
	return out
}

func TestPublishPackageRefusesOrReplaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pkg")
	first := map[string][]byte{"api.go": []byte("package x // one\n"), "manifest.json": []byte(`{"schema":"lean-go-manifest/v24","generated_files":{"api.go":"0"}}`)}
	if err := PublishPackage(dir, first, false); err != nil {
		t.Fatal(err)
	}
	if err := PublishPackage(dir, first, false); err != nil {
		t.Fatalf("identical output must be a no-op: %v", err)
	}
	second := map[string][]byte{"api.go": []byte("package x // two\n"), "control.go": []byte("package x\n"), "manifest.json": first["manifest.json"]}
	err := PublishPackage(dir, second, false)
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("differing output accepted: %v", err)
	}
	if got := readDir(t, dir); got["api.go"] != "package x // one\n" || len(got) != 2 {
		t.Fatalf("refused publication changed output: %v", got)
	}
	if err := PublishPackage(dir, second, true); err != nil {
		t.Fatal(err)
	}
	if got := readDir(t, dir); got["api.go"] != "package x // two\n" || len(got) != 3 {
		t.Fatalf("replacement incomplete: %v", got)
	}
	siblings, _ := os.ReadDir(filepath.Dir(dir))
	if len(siblings) != 1 {
		t.Fatalf("staging or backup directories left behind: %v", siblings)
	}
}

// --replace swaps a native package for a metered one and back.
func TestReplaceAcceptsEitherMode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "proofs", "clamp", "clamp.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "pkg")
	for _, mode := range []string{"native", "metered", "native"} {
		files, err := Emit(data, EmitOptions{Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if err := PublishPackage(dir, files, true); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if got := readDir(t, dir); len(got) != len(files) || got["manifest.json"] != string(files["manifest.json"]) {
			t.Fatalf("%s: package holds %d files", mode, len(got))
		}
	}
	foreign := map[string][]byte{"native.go": []byte("package x\n"), "manifest.json": []byte(`{"schema":"lean-go-native-manifest/v1","generated_files":{"native.go":"0"}}`)}
	os.WriteFile(filepath.Join(dir, "doc.go"), []byte("package x\n"), 0o644)
	if err := PublishPackage(dir, foreign, true); err == nil {
		t.Fatal("replaced a native package holding a hand-written file")
	}
}

func TestReplaceRefusesForeignDirectories(t *testing.T) {
	files := map[string][]byte{"api.go": []byte("package x\n"), "manifest.json": []byte(`{"schema":"lean-go-manifest/v24","generated_files":{"api.go":"0"}}`)}
	for name, setup := range map[string]func(dir string){
		"no manifest": func(dir string) { os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644) },
		"handwritten": func(dir string) {
			os.WriteFile(filepath.Join(dir, "manifest.json"), files["manifest.json"], 0o644)
			os.WriteFile(filepath.Join(dir, "doc.go"), []byte("package x\n"), 0o644)
		},
		"subdirectory": func(dir string) {
			os.WriteFile(filepath.Join(dir, "manifest.json"), files["manifest.json"], 0o644)
			os.Mkdir(filepath.Join(dir, "sub"), 0o755)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "pkg")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			setup(dir)
			before := readDir(t, dir)
			if err := PublishPackage(dir, files, true); err == nil {
				t.Fatal("replaced a directory that lean-go did not generate")
			}
			if after := readDir(t, dir); len(after) != len(before) {
				t.Fatalf("refused replacement changed %v to %v", before, after)
			}
		})
	}
}

func TestCacheStoreVerifyAndTamper(t *testing.T) {
	cache := &Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	files := map[string][]byte{"lib/lean/LeanGo/Module/Export.olean": []byte("olean"), "ir/LeanGo/Module/Export.c": []byte("c")}
	if err := cache.store("key", files); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cache.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX permission bits (Go reports 0777 for every
	// directory); there the user profile's ACLs protect the cache directory.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory permissions %v", info.Mode())
	}
	if err := checkPrivate(cache.Dir); err != nil {
		t.Fatal(err)
	}
	got, err := cache.lookup("key")
	if err != nil || len(got) != 2 || string(got["ir/LeanGo/Module/Export.c"]) != "c" {
		t.Fatalf("lookup %v %v", got, err)
	}
	if missing, err := cache.lookup("absent"); missing != nil || err != nil {
		t.Fatalf("absent entry %v %v", missing, err)
	}
	target := filepath.Join(cache.Dir, "key", "build", "lib", "lean", "LeanGo", "Module", "Export.olean")
	if err := os.WriteFile(target, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tampered, _ := cache.lookup("key"); tampered != nil {
		t.Fatal("a modified cache entry was trusted")
	}
	os.WriteFile(target, []byte("olean"), 0o600)
	os.WriteFile(filepath.Join(cache.Dir, "key", "build", "extra"), []byte("x"), 0o600)
	if extra, _ := cache.lookup("key"); extra != nil {
		t.Fatal("an entry with an unlisted file was trusted")
	}
	// Group or world access disables the cache where POSIX permissions exist.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(cache.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := cache.lookup("key"); err == nil {
			t.Fatal("a cache directory accessible to other users was used")
		}
	}
	if cacheKey("Lean (version 4.36.0, x, commit a, Release)", false) == cacheKey("Lean (version 4.36.0, x, commit b, Release)", false) {
		t.Fatal("the cache key ignores the compiler identity")
	}
}

// An entry that does not verify used to stay in place: lookup treated it as a
// miss, store could not rename onto it and reported success, and every later
// run rebuilt the producer. store now replaces it.
func TestCacheStoreRepairsAnEntryThatDoesNotVerify(t *testing.T) {
	files := map[string][]byte{"lib/lean/LeanGo/Module/Export.olean": []byte("olean"), "ir/LeanGo/Module/Export.c": []byte("c")}
	breakages := map[string]func(entry string) error{
		"missing manifest": func(entry string) error { return os.Remove(filepath.Join(entry, manifestName)) },
		"truncated manifest": func(entry string) error {
			return os.WriteFile(filepath.Join(entry, manifestName), []byte("not a manifest\n"), 0o600)
		},
		"modified file": func(entry string) error {
			return os.WriteFile(filepath.Join(entry, "build", "ir", "LeanGo", "Module", "Export.c"), []byte("tampered"), 0o600)
		},
		"missing file": func(entry string) error {
			return os.Remove(filepath.Join(entry, "build", "ir", "LeanGo", "Module", "Export.c"))
		},
		"empty entry": func(entry string) error {
			if err := os.RemoveAll(entry); err != nil {
				return err
			}
			return os.Mkdir(entry, 0o700)
		},
	}
	for name, breakEntry := range breakages {
		t.Run(name, func(t *testing.T) {
			cache := &Cache{Dir: filepath.Join(t.TempDir(), "cache")}
			if err := cache.store("key", files); err != nil {
				t.Fatal(err)
			}
			if err := breakEntry(filepath.Join(cache.Dir, "key")); err != nil {
				t.Fatal(err)
			}
			if broken, err := cache.lookup("key"); broken != nil || err != nil {
				t.Fatalf("the broken entry was not a plain miss: %v %v", broken, err)
			}
			if err := cache.store("key", files); err != nil {
				t.Fatal(err)
			}
			got, err := cache.lookup("key")
			if err != nil || len(got) != 2 || string(got["ir/LeanGo/Module/Export.c"]) != "c" {
				t.Fatalf("the entry was not repaired: %v %v", got, err)
			}
			entries, err := os.ReadDir(cache.Dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "key" {
				t.Fatalf("repair left files behind: %v %v", entries, err)
			}
		})
	}
	// A valid entry is left alone: a concurrent identical store wins.
	cache := &Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	if err := cache.store("key", files); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cache.Dir, "key", manifestName)
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.store("key", files); err != nil {
		t.Fatal(err)
	}
	if after, err := os.Stat(marker); err != nil || !os.SameFile(before, after) {
		t.Fatalf("a valid entry was replaced: %v", err)
	}
}

// A published package is an ordinary directory, not the private one the
// staging step creates.
func TestPublishedPackageHasAnOrdinaryDirectoryMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX permission bits")
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.Mkdir(plain, 0o777); err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(plain)
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "pkg")
	if err := PublishPackage(pkg, map[string][]byte{"a.go": []byte("package a\n")}, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode().Perm() != want.Mode().Perm() || got.Mode().Perm()&0o700 != 0o700 {
		t.Fatalf("published package mode %v, an ordinary directory here has %v", got.Mode().Perm(), want.Mode().Perm())
	}
	entries, err := os.ReadDir(pkg)
	if err != nil || len(entries) != 1 || entries[0].Name() != "a.go" {
		t.Fatalf("published package contents %v %v", entries, err)
	}
}

// A lock left by an interrupted run names itself and says what to do.
func TestStalePublicationLockIsExplained(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, ".pkg.lean-go.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	err := PublishPackage(filepath.Join(dir, "pkg"), map[string][]byte{"a.go": []byte("package a\n")}, false)
	if err == nil || !strings.Contains(err.Error(), "acquire publication lock") || !strings.Contains(err.Error(), lock) ||
		!strings.Contains(err.Error(), "remove it if no run is active") {
		t.Fatalf("stale lock error: %v", err)
	}
	if exists(filepath.Join(dir, "pkg")) || !exists(lock) {
		t.Fatal("a held lock was ignored or removed")
	}
}

func TestInventoryHasNoMachinePaths(t *testing.T) {
	inventory := ProducerInventory()
	if _, ok := inventory[ToolInput]; !ok {
		t.Fatal("pipeline digest missing")
	}
	for key := range inventory {
		if key != ToolInput && !strings.HasPrefix(key, "producer/LeanGo/Module/") {
			t.Fatalf("unexpected inventory key %s", key)
		}
	}
	if _, ok := inventory["producer/LeanGo/Module/Export.lean"]; !ok {
		t.Fatal("producer export module missing")
	}
	if SourceDigest() == ProofSourceDigest() || SourceDigest() != SourceDigest() {
		t.Fatal("tool digests are not distinct and deterministic")
	}
}
