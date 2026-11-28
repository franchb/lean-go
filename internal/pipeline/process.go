package pipeline

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const processTimeout = 300 * time.Second

// ChildEnvironment is the environment every Lean and Lake child receives.
func ChildEnvironment(c Compiler) []string { return childEnvironment(c) }

// childEnvironment removes Lean and Lake path overrides, then pins the compiler.
func childEnvironment(c Compiler) []string {
	var env []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if strings.HasPrefix(key, "LEAN_") || strings.HasPrefix(key, "LAKE_") {
			continue
		}
		switch key {
		case "ELAN_TOOLCHAIN", "CGO_ENABLED", "GOTOOLCHAIN":
			continue
		}
		env = append(env, pair)
	}
	return append(env, "ELAN_TOOLCHAIN="+c.Toolchain(), "CGO_ENABLED=0", "GOTOOLCHAIN=local")
}

// universal translates CRLF and CR line ends to LF, the text-mode newline
// translation the recorded diagnostics were written with.
func universal(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// run executes one producer process with captured output. Failures carry the
// child's complete diagnostics in the message.
// run and lookPath are variables so tests can stand in for Lean and Lake.
var (
	run      = runProcess
	lookPath = findExecutable
)

func runProcess(command []string, dir string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out, errText := universal(stdout.String()), universal(stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		return out, exportErrorf("producer process failed: Command %s timed out after %d seconds", quotedList(command), int(processTimeout.Seconds()))
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		if code < 0 {
			code = -1
		}
		return out, exportErrorf("producer process failed (%d): %s\n%s", code, strings.TrimSpace(errText), strings.TrimSpace(out))
	}
	if err != nil {
		return out, exportErrorf("producer process failed: %v", err)
	}
	return out, nil
}

// quotedList renders a command line for an error message.
func quotedList(items []string) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = quoted(item)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// lookPath finds a required executable on PATH.
func findExecutable(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}
