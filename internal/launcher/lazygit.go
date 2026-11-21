package launcher

import (
	"errors"
	"os/exec"

	"github.com/mmo-con/git-dashboard/internal/config"
)

// Lazygit starts lazygit in a worktree.
type Lazygit struct {
	Path string
	Args []string
	// Err is set if lazygit is disabled or unavailable.
	Err error
}

// NewLazygit resolves the configured lazygit executable once at startup.
func NewLazygit(cfg config.Lazygit, configFile string) *Lazygit {
	if !cfg.Enabled {
		return &Lazygit{Err: errors.New("lazygit is disabled in [lazygit].enabled")}
	}
	path, err := Resolve(cfg.Command)
	if err != nil {
		where := "[lazygit].command"
		if configFile != "" {
			where += " in " + configFile
		}
		return &Lazygit{Err: errors.New("cannot start lazygit: " + err.Error() + "; configure the executable with " + where)}
	}
	return &Lazygit{Path: path, Args: cfg.Args}
}

// Available reports whether lazygit can be started.
func (l *Lazygit) Available() bool { return l.Err == nil }

// Command returns the child process for the worktree. lazygit discovers the
// repository from its working directory.
func (l *Lazygit) Command(worktreeRoot string) *exec.Cmd {
	return command(l.Path, l.Args, worktreeRoot)
}
