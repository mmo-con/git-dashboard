package launcher

import (
	"os/exec"

	"github.com/mmo-con/git-dashboard/internal/config"
)

// PathPlaceholder is replaced by the worktree root when it forms a complete
// argument.
const PathPlaceholder = "{path}"

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

// Command returns the child process for the worktree.
func (c Custom) Command(worktreeRoot string) *exec.Cmd {
	return command(c.Path, ExpandArgs(c.Args, worktreeRoot), worktreeRoot)
}

// ExpandArgs replaces arguments that are exactly {path}.
func ExpandArgs(args []string, worktreeRoot string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == PathPlaceholder {
			out[i] = worktreeRoot
		} else {
			out[i] = a
		}
	}
	return out
}
