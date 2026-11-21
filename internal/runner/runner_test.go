package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newTestRunner(t *testing.T, maxOutputBytes int) (*Runner, string) {
	t.Helper()
	home := t.TempDir()
	r := New("/bin/bash", home, 5*time.Second, 15*time.Second, maxOutputBytes)
	if err := r.EnsureLocalBin(); err != nil {
		t.Fatalf("EnsureLocalBin() error: %v", err)
	}
	return r, home
}

func TestRunStdoutStderrAndExitCodes(t *testing.T) {
	r, _ := newTestRunner(t, 64*1024)

	res, err := r.Run(context.Background(), Request{
		Command: "printf 'out-line\\n'; printf 'err-line\\n' >&2",
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Stdout != "out-line\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "out-line\n")
	}
	if res.Stderr != "err-line\n" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "err-line\n")
	}

	resNonZero, err := r.Run(context.Background(), Request{
		Command: "echo failing >&2; exit 42",
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if resNonZero.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", resNonZero.ExitCode)
	}
	if !strings.Contains(resNonZero.Stderr, "failing") {
		t.Errorf("Stderr = %q, expected 'failing'", resNonZero.Stderr)
	}
}

func TestRunCwdResolution(t *testing.T) {
	r, home := newTestRunner(t, 64*1024)
	subDir := filepath.Join(home, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	otherDir := t.TempDir()
	filePath := filepath.Join(home, "file.txt")
	if err := os.WriteFile(filePath, []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tests := []struct {
		name    string
		cwd     string
		want    string
		wantErr bool
	}{
		{name: "default home", cwd: "", want: home},
		{name: "tilde", cwd: "~", want: home},
		{name: "tilde subpath", cwd: "~/sub", want: subDir},
		{name: "relative path", cwd: "sub", want: subDir},
		{name: "absolute path", cwd: otherDir, want: otherDir},
		{name: "nonexistent dir", cwd: filepath.Join(home, "missing"), wantErr: true},
		{name: "file instead of dir", cwd: filePath, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r.Run(context.Background(), Request{
				Command: "pwd",
				Cwd:     tc.cwd,
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for cwd %q, got nil", tc.cwd)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}
			got := strings.TrimSpace(res.Stdout)
			if got != tc.want {
				t.Errorf("pwd = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunValidationErrors(t *testing.T) {
	r, _ := newTestRunner(t, 64*1024)

	if _, err := r.Run(context.Background(), Request{Command: "   \t\n "}); err == nil {
		t.Errorf("expected error for empty command")
	}
	if _, err := r.Run(context.Background(), Request{Command: "echo hi", TimeoutSeconds: -1}); err == nil {
		t.Errorf("expected error for negative timeout_seconds")
	}
}

func TestRunTimeoutAndProcessGroupCleanup(t *testing.T) {
	r, _ := newTestRunner(t, 64*1024)

	start := time.Now()
	res, err := r.Run(context.Background(), Request{
		Command:        "sleep 30 & echo $!; wait",
		TimeoutSeconds: 1,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", res.ExitCode)
	}
	if elapsed > 4*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}

	pidStr := strings.TrimSpace(res.Stdout)
	childPID, err := strconv.Atoi(pidStr)
	if err != nil {
		t.Fatalf("failed to parse child PID from stdout %q: %v", res.Stdout, err)
	}

	// Verify grandchild process was killed along with the process group.
	deadline := time.Now().Add(1 * time.Second)
	for {
		err := syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
			t.Fatalf("grandchild PID %d is still alive after timeout (err=%v)", childPID, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunContextCancellation(t *testing.T) {
	r, _ := newTestRunner(t, 64*1024)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	res, err := r.Run(ctx, Request{
		Command:        "sleep 20",
		TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if !res.Canceled {
		t.Errorf("Canceled = false, want true")
	}
	if res.TimedOut {
		t.Errorf("TimedOut = true, want false")
	}
}

func TestRunOutputTruncation(t *testing.T) {
	r, _ := newTestRunner(t, 64)

	res, err := r.Run(context.Background(), Request{
		Command: "head -c 1024 /dev/zero | tr '\\0' 'A'; head -c 1024 /dev/zero | tr '\\0' 'B' >&2",
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (child should not die from SIGPIPE)", res.ExitCode)
	}
	if !res.StdoutTruncated || len(res.Stdout) != 64 || res.StdoutTotalBytes != 1024 {
		t.Errorf("stdout truncation mismatch: truncated=%v len=%d total=%d", res.StdoutTruncated, len(res.Stdout), res.StdoutTotalBytes)
	}
	if !res.StderrTruncated || len(res.Stderr) != 64 || res.StderrTotalBytes != 1024 {
		t.Errorf("stderr truncation mismatch: truncated=%v len=%d total=%d", res.StderrTruncated, len(res.Stderr), res.StderrTotalBytes)
	}

	formatted := FormatResult(res)
	if !strings.Contains(formatted, "[stdout truncated: showing first 64 of 1024 bytes]") {
		t.Errorf("formatted output missing stdout truncation notice:\n%s", formatted)
	}
	if !strings.Contains(formatted, "[stderr truncated: showing first 64 of 1024 bytes]") {
		t.Errorf("formatted output missing stderr truncation notice:\n%s", formatted)
	}
}

func TestRunLocalBinInPath(t *testing.T) {
	r, home := newTestRunner(t, 64*1024)

	res, err := r.Run(context.Background(), Request{
		Command: "printf '%s' \"$PATH\"",
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	expectedBin := filepath.Join(home, ".local", "bin")
	if !strings.Contains(res.Stdout, expectedBin) {
		t.Errorf("PATH = %q, expected %q to be included", res.Stdout, expectedBin)
	}
}
