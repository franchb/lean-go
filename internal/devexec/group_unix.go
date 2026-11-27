//go:build unix

package devexec

import (
	"os/exec"
	"syscall"
)

// isolate starts the child in a new process group and kills the whole group
// when the context expires.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
