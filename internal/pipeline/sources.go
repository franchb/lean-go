package pipeline

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Source snapshot limits shared by extraction and proof checking.
const (
	MaxFiles       = 256
	MaxSourceBytes = 8 * 1024 * 1024
	MaxIRBytes     = 4 * 1024 * 1024
	MaxRoots       = 64
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_']*(?:\.[A-Za-z_][A-Za-z0-9_']*)*$`)

var reservedModules = map[string]bool{"Lean": true, "Init": true, "Std": true, "LeanGo": true, "Lake": true}
var ignoredDirectories = map[string]bool{".git": true, ".lake": true, ".venv": true, "__pycache__": true}

func isPrintable(r rune) bool { return unicode.IsPrint(r) }

// ValidateName accepts the ASCII Lean identifier syntax used on the command line.
func ValidateName(name string) error {
	if !identifier.MatchString(name) {
		return exportErrorf("invalid ASCII Lean name: %s", pythonRepr(name))
	}
	return nil
}

// SelectRoots canonicalizes explicit roots, rejecting duplicates.
func SelectRoots(entries []string) ([]string, error) {
	if len(entries) < 1 || len(entries) > MaxRoots {
		return nil, exportErrorf("expected between 1 and %d roots", MaxRoots)
	}
	seen := map[string]bool{}
	for _, name := range entries {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		seen[name] = true
	}
	if len(seen) != len(entries) {
		return nil, exportError("duplicate roots are not allowed")
	}
	out := append([]string(nil), entries...)
	sort.Strings(out)
	return out, nil
}

// Source is one Lean module in a snapshot.
type Source struct {
	Module   string
	Relative string // slash-separated path relative to the snapshot root
	Data     []byte
}

// Snapshot is an ordered set of Lean modules, in directory-walk order.
type Snapshot []Source

// Lookup returns the module with the given name.
func (s Snapshot) Lookup(module string) (Source, bool) {
	for _, source := range s {
		if source.Module == module {
			return source, true
		}
	}
	return Source{}, false
}

// Modules returns the sorted module names.
func (s Snapshot) Modules() []string {
	names := make([]string, len(s))
	for i, source := range s {
		names[i] = source.Module
	}
	sort.Strings(names)
	return names
}

func absolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return lexicalPath(path), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return lexicalPath(wd + string(filepath.Separator) + path), nil
}

// lexicalPath normalizes like Python's pathlib: repeated separators and "."
// components disappear, while ".." stays for symlink-aware checks.
func lexicalPath(path string) string {
	volume := filepath.VolumeName(path)
	rest := filepath.ToSlash(path[len(volume):])
	absolute := strings.HasPrefix(rest, "/")
	var parts []string
	for _, part := range strings.Split(rest, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	joined := strings.Join(parts, "/")
	if absolute {
		joined = "/" + joined
	}
	return volume + filepath.FromSlash(joined)
}

// resolvePath follows symlinks for the existing prefix and cleans the rest,
// like Python's non-strict Path.resolve().
func resolvePath(path string) (string, error) {
	path, err := absolutePath(path)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	parent, base := filepath.Dir(path), filepath.Base(path)
	if parent == path {
		return path, nil
	}
	resolvedParent, err := resolvePath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, base), nil
}

// isRelativeTo reports whether path equals base or lies below it.
func isRelativeTo(path, base string) bool {
	if path == base {
		return true
	}
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&fs.ModeSymlink != 0
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// SourceSnapshot reads regular .lean files only and never follows symlinks.
func SourceSnapshot(sourceDir string) (Snapshot, error) {
	directory, err := absolutePath(sourceDir)
	if err != nil {
		return nil, err
	}
	resolved, err := resolvePath(directory)
	if err != nil || resolved != directory || !isDir(directory) {
		return nil, exportError("source directory must exist and must not contain symlinks")
	}
	var out Snapshot
	total := 0
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var dirs, files []string
		for _, entry := range entries {
			full := filepath.Join(dir, entry.Name())
			info, err := os.Stat(full)
			isDirectory := err == nil && info.IsDir()
			if isDirectory {
				if !ignoredDirectories[entry.Name()] {
					dirs = append(dirs, entry.Name())
				}
			} else {
				files = append(files, entry.Name())
			}
		}
		sort.Strings(dirs)
		sort.Strings(files)
		for _, name := range dirs {
			if isSymlink(filepath.Join(dir, name)) {
				return exportError("symlinked source directory is forbidden")
			}
		}
		for _, name := range files {
			if !strings.HasSuffix(name, ".lean") || name == "lakefile.lean" {
				continue
			}
			path := filepath.Join(dir, name)
			rel, err := filepath.Rel(directory, path)
			if err != nil {
				return err
			}
			relative := filepath.ToSlash(rel)
			module := strings.ReplaceAll(strings.TrimSuffix(relative, ".lean"), "/", ".")
			if err := ValidateName(module); err != nil {
				return err
			}
			if reservedModules[strings.SplitN(module, ".", 2)[0]] {
				return exportErrorf("reserved source module: %s", module)
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return exportErrorf("source must be a regular file: %s", relative)
			}
			if len(out) >= MaxFiles || total+int(info.Size()) > MaxSourceBytes {
				return exportError("source snapshot exceeds file or byte limit")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			total += len(data)
			if total > MaxSourceBytes {
				return exportError("source snapshot exceeds file or byte limit")
			}
			if _, dup := out.Lookup(module); dup {
				return exportErrorf("ambiguous source module: %s", module)
			}
			out = append(out, Source{module, relative, data})
		}
		for _, name := range dirs {
			if err := walk(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(directory); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, exportError("source directory has no Lean modules")
	}
	return out, nil
}

type headerToken struct {
	text       string
	start, end int
}

var headerIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_'.]*`)

// headerTokenizer reads tokens lazily, respecting nested comments, like the
// Python generator: an error is reported only when the caller reaches it.
// Offsets index code points.
type headerTokenizer struct {
	text []rune
	i    int
}

func (t *headerTokenizer) has(prefix string) bool {
	j := t.i
	for _, r := range prefix {
		if j >= len(t.text) || t.text[j] != r {
			return false
		}
		j++
	}
	return true
}

func (t *headerTokenizer) next() (*headerToken, error) {
	for t.i < len(t.text) {
		switch {
		case unicode.IsSpace(t.text[t.i]) || isPythonSpace(t.text[t.i]):
			t.i++
		case t.has("--"):
			for t.i < len(t.text) && t.text[t.i] != '\n' {
				t.i++
			}
			if t.i < len(t.text) {
				t.i++
			}
		case t.has("/-"):
			depth := 1
			t.i += 2
			for depth > 0 && t.i < len(t.text) {
				switch {
				case t.has("/-"):
					depth++
					t.i += 2
				case t.has("-/"):
					depth--
					t.i += 2
				default:
					t.i++
				}
			}
			if depth > 0 {
				return nil, exportError("unterminated Lean header comment")
			}
		default:
			start := t.i
			end := start + 1
			if headerIdentifier.MatchString(string(t.text[start:end])) {
				for end < len(t.text) && isHeaderIdentRune(t.text[end]) {
					end++
				}
			}
			t.i = end
			return &headerToken{string(t.text[start:end]), start, end}, nil
		}
	}
	return nil, nil
}

func isHeaderIdentRune(r rune) bool {
	return r == '_' || r == '\'' || r == '.' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

// isPythonSpace adds the separators str.isspace() accepts beyond unicode.IsSpace.
func isPythonSpace(r rune) bool { return r >= 0x1c && r <= 0x1f }

// instrumentSource injects the producer audit import after the header and
// returns the exact mapping back to the original bytes.
func instrumentSource(data []byte) (*SourceMap, []string, error) {
	if !utf8.Valid(data) {
		return nil, nil, exportError("Lean source must be UTF-8")
	}
	text := []rune(string(data))
	tokenizer := &headerTokenizer{text: text}
	var failure error
	next := func() *headerToken {
		if failure != nil {
			return nil
		}
		t, err := tokenizer.next()
		if err != nil {
			failure = err
			return nil
		}
		return t
	}
	token := next()
	modern := token != nil && token.text == "module"
	end := 0
	if modern {
		end = token.end
		token = next()
	}
	if token != nil && token.text == "prelude" {
		end = token.end
		token = next()
	}
	var imports []string
	for token != nil {
		for token != nil && (token.text == "public" || token.text == "private" || token.text == "meta") {
			token = next()
		}
		if token == nil || token.text != "import" {
			break
		}
		token = next()
		if token != nil && token.text == "all" {
			token = next()
		}
		if failure != nil {
			return nil, nil, failure
		}
		if token == nil {
			return nil, nil, exportError("missing imported module name")
		}
		if err := ValidateName(token.text); err != nil {
			return nil, nil, err
		}
		imports = append(imports, token.text)
		end = token.end
		token = next()
	}
	if failure != nil {
		return nil, nil, failure
	}
	audit := "import"
	if modern {
		audit = "public meta import"
	}
	injection := "\n" + audit + " LeanGo.Module.Audit\nset_option compiler.postponeCompile false\n"
	prefix := string(text[:end])
	instrumented := prefix + injection + string(text[end:])
	return &SourceMap{Original: data, Instrumented: []byte(instrumented), Insertion: len(prefix), InsertedBytes: len(injection)}, imports, nil
}

// SourceMap maps Lean's Unicode positions through one explicit UTF-8 insertion.
type SourceMap struct {
	Original      []byte
	Instrumented  []byte
	Insertion     int
	InsertedBytes int
}

// Position maps a 1-based line and code-point column in the instrumented
// file to the original file, or nil inside the insertion or out of range.
func (m *SourceMap) Position(line, column int64) *Object {
	if line < 1 || column < 0 {
		return nil
	}
	lines := strings.Split(string(m.Instrumented), "\n")
	if line > int64(len(lines)) {
		return nil
	}
	current := []rune(lines[line-1])
	if column > int64(len(current)) {
		return nil
	}
	offset := 0
	for _, l := range lines[:line-1] {
		offset += len(l) + 1
	}
	offset += len(string(current[:column]))
	if m.Insertion <= offset && offset < m.Insertion+m.InsertedBytes {
		return nil
	}
	if offset >= m.Insertion+m.InsertedBytes {
		offset -= m.InsertedBytes
	}
	prefix := m.Original[:offset]
	if !utf8.Valid(prefix) {
		return nil
	}
	tail := prefix
	if i := bytes.LastIndexByte(prefix, '\n'); i >= 0 {
		tail = prefix[i+1:]
	}
	tailRunes := []rune(string(tail))
	p := NewObject()
	p.Set("line", numberOf(bytes.Count(prefix, []byte("\n"))+1))
	p.Set("column", numberOf(len(tailRunes)))
	p.Set("utf16_column", numberOf(len(utf16.Encode(tailRunes))))
	p.Set("byte_offset", numberOf(offset))
	return p
}

func numberOf(n int) json.Number { return json.Number(strconv.Itoa(n)) }
