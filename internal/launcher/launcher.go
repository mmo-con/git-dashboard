// Package launcher builds interactive child processes for the terminal
// handover: lazygit, interactive fetch retries, and configured launchers.
package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mmo-con/headquarters/internal/git"
)

// Resolve finds an executable by name in PATH or validates an absolute path.
// The result must be a regular executable file.
func Resolve(command string) (string, error) {
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("%s: not found or not executable", command)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s: not a regular executable file", abs)
	}
	return abs, nil
}

// command creates a foreground child process in dir. It is deliberately not
// bound to a context: cancelling dashboard work must never terminate an
// interactive session.
func command(exe string, args []string, dir string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = git.SanitizedEnvironment()
	return cmd
}
