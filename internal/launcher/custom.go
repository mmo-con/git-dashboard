package launcher

import (
	"os/exec"

	"github.com/mmo-con/git-dashboard/internal/config"
)

// Placeholders are replaced when they form a complete argument: {path} by the
// worktree root and {workspace} by the configured workspace directory.
const (
	PathPlaceholder      = "{path}"
	WorkspacePlaceholder = "{workspace}"
)

// Custom is a configured launcher.
type Custom struct {
	Name string
	Path string
	Args []string
	Err  error
}

// NewCustom resolves the configured launchers once at startup.
func NewCustom(cfgs []config.Launcher) []Custom {
	out := make([]Custom, 0, len(cfgs))
	for _, c := range cfgs {
		l := Custom{Name: c.Name, Args: c.Command[1:]}
		l.Path, l.Err = Resolve(c.Command[0])
		out = append(out, l)
	}
	return out
}

// Command returns the child process, which starts in the worktree root.
func (c Custom) Command(worktreeRoot, workspace string) *exec.Cmd {
	return command(c.Path, ExpandArgs(c.Args, worktreeRoot, workspace), worktreeRoot)
}

// ExpandArgs replaces arguments that are exactly {path} or {workspace}.
func ExpandArgs(args []string, worktreeRoot, workspace string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		switch a {
		case PathPlaceholder:
			out[i] = worktreeRoot
		case WorkspacePlaceholder:
			out[i] = workspace
		default:
			out[i] = a
		}
	}
	return out
}
