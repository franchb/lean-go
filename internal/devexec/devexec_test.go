package devexec

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func shell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	return sh
}

func TestExitStatusOutputAndStdin(t *testing.T) {
	sh := shell(t)
	result, err := Run([]string{sh, "-c", "cat; echo err >&2; exit 3"}, t.TempDir(), os.Environ(), "input", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != 3 || result.Stdout != "input" || result.Stderr != "err\n" {
		t.Fatalf("got %+v", result)
	}
}

func TestStartFailureIsAnError(t *testing.T) {
	result, err := Run([]string{"/nonexistent/lean-go-dev-command"}, t.TempDir(), nil, "", time.Minute)
	if err == nil || result.Code != -1 {
		t.Fatalf("got %+v, %v", result, err)
	}
}

// Ported from test_differential_backend.py
// ProcessTests.test_timeout_kills_grandchild_holding_output_pipe.
func TestTimeoutKillsGrandchildHoldingOutputPipe(t *testing.T) {
	sh := shell(t)
	script := "sleep 30 & echo 'child started'; sleep 30"
	start := time.Now()
	result, err := Run([]string{sh, "-c", script}, t.TempDir(), os.Environ(), "", 500*time.Millisecond)
	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("want a timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	if !strings.Contains(result.Stdout, "child started") || result.Code != -1 {
		t.Fatalf("partial output lost: %+v", result)
	}
}
