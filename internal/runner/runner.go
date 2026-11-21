package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Runner executes stateless bash commands with bounded output and process-group timeouts.
type Runner struct {
	bashPath       string
	homeDir        string
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	maxOutputBytes int
}

// Request specifies a single bash command invocation.
type Request struct {
	Command        string
	Cwd            string
	TimeoutSeconds int
}

// Result contains the captured outcome of a bash command invocation.
type Result struct {
	ExitCode         int
	Stdout           string
	Stderr           string
	StdoutTruncated  bool
	StdoutTotalBytes int
	StderrTruncated  bool
	StderrTotalBytes int
	TimedOut         bool
	Canceled         bool
	Timeout          time.Duration
	Duration         time.Duration
}

// New creates a new Runner instance.
func New(bashPath, homeDir string, defaultTimeout, maxTimeout time.Duration, maxOutputBytes int) *Runner {
	return &Runner{
		bashPath:       bashPath,
		homeDir:        homeDir,
		defaultTimeout: defaultTimeout,
		maxTimeout:     maxTimeout,
		maxOutputBytes: maxOutputBytes,
	}
}

// EnsureLocalBin creates $HOME/.local/bin so user-installed executables are ready on PATH.
func (r *Runner) EnsureLocalBin() error {
	return os.MkdirAll(filepath.Join(r.homeDir, ".local", "bin"), 0o755)
}

// Run executes req.Command in a fresh `bash -c` process group.
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Command) == "" {
		return Result{}, errors.New("command must not be empty")
	}
	if req.TimeoutSeconds < 0 {
		return Result{}, fmt.Errorf("timeout_seconds (%d) must be >= 0", req.TimeoutSeconds)
	}

	cwd, err := r.resolveCwd(req.Cwd)
	if err != nil {
		return Result{}, err
	}

	timeout := r.defaultTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	if timeout > r.maxTimeout {
		timeout = r.maxTimeout
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdoutBuf := &limitedBuffer{limit: r.maxOutputBytes}
	stderrBuf := &limitedBuffer{limit: r.maxOutputBytes}

	cmd := exec.CommandContext(execCtx, r.bashPath, "-c", req.Command)
	cmd.Dir = cwd
	cmd.Env = buildEnv(r.homeDir)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second

	waitDone := make(chan struct{})
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pgid := cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			select {
			case <-waitDone:
				return
			case <-time.After(500 * time.Millisecond):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}()
		return nil
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		close(waitDone)
		return Result{}, fmt.Errorf("start bash: %w", err)
	}

	waitErr := cmd.Wait()
	close(waitDone)
	duration := time.Since(start)

	stdout, stdoutTotal, stdoutTrunc := stdoutBuf.snapshot()
	stderr, stderrTotal, stderrTrunc := stderrBuf.snapshot()

	res := Result{
		Stdout:           stdout,
		Stderr:           stderr,
		StdoutTruncated:  stdoutTrunc,
		StdoutTotalBytes: stdoutTotal,
		StderrTruncated:  stderrTrunc,
		StderrTotalBytes: stderrTotal,
		Timeout:          timeout,
		Duration:         duration,
	}

	switch {
	case errors.Is(execCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.ExitCode = -1
	case errors.Is(ctx.Err(), context.Canceled):
		res.Canceled = true
		res.ExitCode = -1
	case waitErr == nil:
		res.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		} else if errors.Is(waitErr, exec.ErrWaitDelay) {
			if cmd.ProcessState != nil {
				res.ExitCode = cmd.ProcessState.ExitCode()
			}
		} else {
			return Result{}, fmt.Errorf("wait bash: %w", waitErr)
		}
	}

	return res, nil
}

func (r *Runner) resolveCwd(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = r.homeDir
	} else if cwd == "~" {
		cwd = r.homeDir
	} else if strings.HasPrefix(cwd, "~/") {
		cwd = filepath.Join(r.homeDir, cwd[2:])
	} else if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(r.homeDir, cwd)
	}

	info, err := os.Stat(cwd)
	if err != nil {
		return "", fmt.Errorf("invalid cwd %q: %w", cwd, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("invalid cwd %q: not a directory", cwd)
	}
	return cwd, nil
}

func buildEnv(homeDir string) []string {
	localBin := filepath.Clean(filepath.Join(homeDir, ".local", "bin"))
	rawEnv := os.Environ()
	env := make([]string, 0, len(rawEnv)+2)

	hasHome := false
	hasPath := false

	for _, entry := range rawEnv {
		switch {
		case strings.HasPrefix(entry, "HOME="):
			env = append(env, "HOME="+homeDir)
			hasHome = true
		case strings.HasPrefix(entry, "PATH="):
			pathVal := strings.TrimPrefix(entry, "PATH=")
			env = append(env, "PATH="+prependPathIfMissing(pathVal, localBin))
			hasPath = true
		default:
			env = append(env, entry)
		}
	}

	if !hasHome {
		env = append(env, "HOME="+homeDir)
	}
	if !hasPath {
		env = append(env, "PATH="+localBin)
	}
	return env
}

func prependPathIfMissing(pathVal, dir string) string {
	for _, p := range filepath.SplitList(pathVal) {
		if filepath.Clean(p) == dir {
			return pathVal
		}
	}
	if pathVal == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + pathVal
}

// limitedBuffer captures up to limit bytes while continuing to drain all writes
// so child processes never encounter SIGPIPE due to output truncation.
type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	total     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.total += len(p)
	if b.buf.Len() < b.limit {
		rem := b.limit - b.buf.Len()
		if len(p) <= rem {
			b.buf.Write(p)
		} else {
			b.buf.Write(p[:rem])
			b.truncated = true
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) snapshot() (string, int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String(), b.total, b.truncated
}

// FormatResult formats a Result as a concise plain-text report for the MCP client.
func FormatResult(res Result) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "exit_code: %d\n", res.ExitCode)
	if res.TimedOut {
		fmt.Fprintf(&sb, "timed_out: true (limit: %s)\n", res.Timeout)
	}
	if res.Canceled {
		sb.WriteString("canceled: true\n")
	}

	if res.Stdout != "" {
		sb.WriteString("stdout:\n")
		sb.WriteString(res.Stdout)
		if !strings.HasSuffix(res.Stdout, "\n") {
			sb.WriteByte('\n')
		}
		if res.StdoutTruncated {
			fmt.Fprintf(&sb, "[stdout truncated: showing first %d of %d bytes]\n", len(res.Stdout), res.StdoutTotalBytes)
		}
	}

	if res.Stderr != "" {
		sb.WriteString("stderr:\n")
		sb.WriteString(res.Stderr)
		if !strings.HasSuffix(res.Stderr, "\n") {
			sb.WriteByte('\n')
		}
		if res.StderrTruncated {
			fmt.Fprintf(&sb, "[stderr truncated: showing first %d of %d bytes]\n", len(res.Stderr), res.StderrTotalBytes)
		}
	}

	if res.Stdout == "" && res.Stderr == "" {
		sb.WriteString("(no output)\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}
