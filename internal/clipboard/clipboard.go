// Package clipboard copies text to the system clipboard through the native
// command-line tools of the platform. It emits no terminal escape sequences.
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

// DefaultTimeout bounds one clipboard tool.
const DefaultTimeout = 2 * time.Second

// Tool is a clipboard command that reads the text from stdin.
type Tool struct {
	Name string
	Args []string
}

// Candidates returns the clipboard tools for the platform and environment in
// the order they are tried.
func Candidates(goos string, getenv func(string) string) []Tool {
	if goos == "darwin" {
		return []Tool{{Name: "pbcopy"}}
	}
	var tools []Tool
	if getenv("WAYLAND_DISPLAY") != "" {
		tools = append(tools, Tool{Name: "wl-copy"})
	}
	if getenv("DISPLAY") != "" {
		tools = append(tools,
			Tool{Name: "xclip", Args: []string{"-selection", "clipboard"}},
			Tool{Name: "xsel", Args: []string{"--clipboard", "--input"}})
	}
	return tools
}

// Clipboard copies text with the first usable tool.
type Clipboard struct {
	Tools []Tool
	// Timeout bounds each tool; zero means DefaultTimeout.
	Timeout time.Duration
}

// System returns the clipboard of the current platform and environment.
func System() Clipboard {
	return Clipboard{Tools: Candidates(runtime.GOOS, os.Getenv), Timeout: DefaultTimeout}
}

// Check reports why text must not be copied. Control characters, including
// line breaks, are rejected because pasting them into a shell could run
// commands; invalid UTF-8 is rejected because it cannot be pasted reliably.
func Check(text string) error {
	switch {
	case text == "":
		return errors.New("the value is empty")
	case !utf8.ValidString(text):
		return errors.New("the value is not valid UTF-8")
	case strings.IndexFunc(text, unicode.IsControl) >= 0:
		return errors.New("the value contains control characters")
	}
	return nil
}

// Copy writes text to the clipboard and returns the name of the tool used.
// A tool that is missing, fails, or times out is skipped for the next one.
func (c Clipboard) Copy(ctx context.Context, text string) (string, error) {
	if err := Check(text); err != nil {
		return "", err
	}
	if len(c.Tools) == 0 {
		return "", errors.New("no clipboard available: neither WAYLAND_DISPLAY nor DISPLAY is set")
	}
	var last error
	var names []string
	for _, t := range c.Tools {
		names = append(names, t.Name)
		path, err := exec.LookPath(t.Name)
		if err != nil {
			continue
		}
		name := filepath.Base(t.Name)
		if err := c.run(ctx, path, t.Args, text); err != nil {
			last = fmt.Errorf("%s: %w", name, err)
			continue
		}
		return name, nil
	}
	if last != nil {
		return "", last
	}
	return "", fmt.Errorf("no clipboard tool found (%s)", strings.Join(names, ", "))
}

// run starts one tool without a shell. Tools such as wl-copy and xclip leave
// a process behind that serves the selection and inherits stdout and stderr,
// so neither may be a pipe that Wait would wait for: stdout is discarded and
// stderr goes to a temporary file.
func (c Clipboard) run(ctx context.Context, path string, args []string, text string) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stderr, err := os.CreateTemp("", "headquarters-clipboard-*")
	if err != nil {
		return err
	}
	defer os.Remove(stderr.Name())
	defer stderr.Close()

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stderr = stderr
	// A new session keeps the tool away from the terminal; on timeout the
	// whole process group is killed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %v", timeout)
	}
	if err != nil {
		if msg := firstLine(stderr); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// firstLine returns the first line of a tool's error output, bounded.
func firstLine(f *os.File) string {
	buf := make([]byte, 256)
	n, _ := f.ReadAt(buf, 0)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	return strings.TrimSpace(line)
}
