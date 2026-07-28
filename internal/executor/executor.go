package executor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Executor runs commands with a timeout and output limit.
type Executor struct {
	Timeout     time.Duration
	OutputLimit int64
}

// Run executes command through the platform shell.
func (e Executor) Run(ctx context.Context, command string) Result {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	limit := e.OutputLimit
	if limit <= 0 {
		limit = 1 << 20
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shellCommand(ctx, command)
	stdout := &limitedBuffer{limit: limit}
	stderr := &limitedBuffer{limit: limit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	result := Result{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Truncated: stdout.Truncated() || stderr.Truncated(),
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Error = "command timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			result.ExitStatus = &code
		}
		return result
	}
	code := 0
	result.ExitStatus = &code
	return result
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:int(remaining)])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
