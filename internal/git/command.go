// Package git wraps the official Git CLI. It never parses human-oriented
// output and never involves a shell.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const maxStderr = 16 * 1024

// CommandError describes a Git process that exited unsuccessfully.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if e.ExitCode >= 0 {
		if msg == "" {
			return fmt.Sprintf("git exited with status %d", e.ExitCode)
		}
		return fmt.Sprintf("git exited with status %d: %s", e.ExitCode, msg)
	}
	if msg != "" {
		return fmt.Sprintf("git: %v: %s", e.Err, msg)
	}
	return fmt.Sprintf("git: %v", e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

// runner executes non-interactive Git commands.
type runner struct {
	executable string
}

type result struct {
	stdout   []byte
	stderr   string
	exitCode int
}

// run starts git with args in dir. Standard input is the null device, output is
// captured, and the process runs isolated from the terminal. Cancellation of
// ctx terminates the complete process group.
func (r runner) run(ctx context.Context, dir string, readOnly bool, args ...string) (result, error) {
	full := append([]string{"--no-pager"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	cmd := exec.Command(r.executable, full...)
	cmd.Env = backgroundEnvironment(readOnly)
	cmd.Stdin = nil
	var stdout bytes.Buffer
	stderr := &tailBuffer{limit: maxStderr}
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	isolate(cmd)

	if err := ctx.Err(); err != nil {
		return result{exitCode: -1}, &CommandError{Args: args, ExitCode: -1, Err: err}
	}
	if err := cmd.Start(); err != nil {
		return result{exitCode: -1}, &CommandError{Args: args, ExitCode: -1, Err: err}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killGroup(cmd)
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)

	res := result{stdout: stdout.Bytes(), stderr: stderr.String(), exitCode: 0}
	if ctxErr := ctx.Err(); ctxErr != nil {
		res.exitCode = -1
		return res, &CommandError{Args: args, ExitCode: -1, Stderr: res.stderr, Err: ctxErr}
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.exitCode = exitErr.ExitCode()
			return res, &CommandError{Args: args, ExitCode: res.exitCode, Stderr: res.stderr, Err: err}
		}
		res.exitCode = -1
		return res, &CommandError{Args: args, ExitCode: -1, Stderr: res.stderr, Err: err}
	}
	return res, nil
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	limit     int
	buf       []byte
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
		t.truncated = true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	if t.truncated {
		return "…" + string(t.buf)
	}
	return string(t.buf)
}

// ExitCode extracts the exit code from an error returned by the client.
func ExitCode(err error) *int {
	var ce *CommandError
	if errors.As(err, &ce) && ce.ExitCode >= 0 {
		c := ce.ExitCode
		return &c
	}
	return nil
}

// Stderr extracts captured standard error from an error returned by the client.
func Stderr(err error) string {
	var ce *CommandError
	if errors.As(err, &ce) {
		return ce.Stderr
	}
	return ""
}
