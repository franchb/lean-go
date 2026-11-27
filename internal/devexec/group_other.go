//go:build !unix

package devexec

import "os/exec"

// isolate keeps the default: the context kills only the direct child.
func isolate(cmd *exec.Cmd) {}
