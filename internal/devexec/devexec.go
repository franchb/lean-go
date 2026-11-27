// Package devexec runs the external commands of the developer harnesses
// (cmd/lean-go-dev) with a hard timeout and captured output.
//
// On POSIX systems the child runs in its own process group and a timeout
// kills the whole group, so a compiler that started grandchildren cannot keep
// the output pipes open past the deadline. Elsewhere only the direct child is
// killed; descendants may survive and the timeout is not a hard bound there.
package devexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Result is one finished (or timed out) command.
type Result struct {
	Stdout, Stderr string
	// Code is the exit status, or -1 when the command did not exit normally
	// (signal, timeout or start failure).
	Code int
}

// TimeoutError reports a command killed at its deadline. Result keeps the
// output produced before the kill.
type TimeoutError struct {
	Command []string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("command %q timed out after %s", e.Command, e.Timeout)
}

// waitDelay bounds how long Run waits for the output pipes after the kill.
const waitDelay = 2 * time.Second

// Run executes command in dir with exactly env, feeding stdin when it is not
// empty. A nonzero exit is not an error: it is reported in Result.Code. The
// error is a *TimeoutError at the deadline, or the start failure.
func Run(command []string, dir string, env []string, stdin string, timeout time.Duration) (Result, error) {
	if len(command) == 0 {
		return Result{Code: -1}, errors.New("empty command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	isolate(cmd)
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), Code: -1}
	if ctx.Err() == context.DeadlineExceeded {
		return result, &TimeoutError{Command: command, Timeout: timeout}
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		result.Code = 0
	case errors.As(err, &exit):
		if code := exit.ExitCode(); code >= 0 {
			result.Code = code
		}
	default:
		return result, err
	}
	return result, nil
}
