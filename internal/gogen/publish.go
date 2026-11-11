package gogen

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Publish installs a complete newly generated package or verifies identical output.
// Cooperating writers serialize through an exclusive sibling lock. The caller
// must exclude unrelated mutation of the destination parent during publication.
func Publish(dir string, files map[string][]byte) error {
	if len(files) == 0 {
		return fmt.Errorf("empty generated package")
	}
	for name := range files {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") {
			return fmt.Errorf("unsafe generated filename %q", name)
		}
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty destination")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	base := filepath.Base(absolute)
	if base == "." || base == string(filepath.Separator) {
		return fmt.Errorf("unsafe destination")
	}
	parent := filepath.Dir(absolute)
	if err = plainDirectory(parent); err != nil {
		return fmt.Errorf("unsafe parent: %w", err)
	}
	lock := filepath.Join(parent, "."+base+".lean-go.lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("acquire publication lock: %w", err)
	}
	// Only this invocation's newly created empty lock is removed.
	defer os.Remove(lock)
	exists, err := matches(absolute, files)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	stage, err := os.MkdirTemp(parent, "."+base+".lean-go-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // stage is owned, never a pre-existing destination.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err = os.WriteFile(filepath.Join(stage, name), files[name], 0644); err != nil {
			return err
		}
	}
	// Final destination check while retaining the cooperating-writer lock.
	exists, err = matches(absolute, files)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err = os.Rename(stage, absolute); err != nil {
		return fmt.Errorf("publish staged package: %w", err)
	}
	return nil
}
func plainDirectory(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("not a plain directory: %s", current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}
func matches(dir string, files map[string][]byte) (bool, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("destination is not a plain directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	if len(entries) != len(files) {
		return false, fmt.Errorf("existing output file set differs")
	}
	for _, entry := range entries {
		expected, ok := files[entry.Name()]
		if !ok {
			return false, fmt.Errorf("unexpected existing output %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() || entry.Type()&fs.ModeSymlink != 0 {
			return false, fmt.Errorf("unsafe existing output %q", entry.Name())
		}
		actual, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(actual, expected) {
			return false, fmt.Errorf("existing output differs: %s", entry.Name())
		}
	}
	return true, nil
}
