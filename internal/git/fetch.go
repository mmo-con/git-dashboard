package git

import (
	"context"
	"os/exec"

	"github.com/mmo-con/headquarters/internal/domain"
)

func fetchArgs(opts FetchOptions) []string {
	args := []string{"fetch", "--all"}
	if opts.Prune {
		args = append(args, "--prune")
	}
	return args
}

// Fetch runs a non-interactive fetch. Prompts fail instead of reaching the
// terminal.
func (c *Client) Fetch(ctx context.Context, loc domain.Location, opts FetchOptions) error {
	_, err := c.run.run(ctx, loc.WorktreeRoot, false, fetchArgs(opts)...)
	return err
}

// InteractiveFetchCommand returns a fetch command for the terminal handover.
// It is attached to the terminal and may prompt.
func (c *Client) InteractiveFetchCommand(loc domain.Location, opts FetchOptions) *exec.Cmd {
	cmd := exec.Command(c.run.executable, append([]string{"-C", loc.WorktreeRoot}, fetchArgs(opts)...)...)
	cmd.Dir = loc.WorktreeRoot
	cmd.Env = SanitizedEnvironment()
	return cmd
}
