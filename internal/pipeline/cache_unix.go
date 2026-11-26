//go:build unix

package pipeline

import (
	"fmt"
	"os"
	"syscall"
)

// checkPrivate requires a plain directory owned by this user with no group or
// other permissions.
func checkPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a plain directory", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is accessible to other users", dir)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by another user", dir)
	}
	return nil
}
