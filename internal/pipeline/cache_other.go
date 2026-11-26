//go:build !unix

package pipeline

import (
	"fmt"
	"os"
)

// checkPrivate requires a plain directory. Ownership and ACLs are left to the
// user profile's cache directory on platforms without POSIX permissions.
func checkPrivate(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a plain directory", dir)
	}
	return nil
}
