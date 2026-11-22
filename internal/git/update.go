package git

import (
	"context"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// MergeFastForwardOnly fast-forwards the current branch to its already
// fetched upstream. It never contacts the network, never creates a merge
// commit, never stashes, and never overwrites untracked or ignored files.
func (c *Client) MergeFastForwardOnly(ctx context.Context, loc domain.Location) error {
	_, err := c.run.run(ctx, loc.WorktreeRoot, false, "merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", "@{upstream}")
	return err
}
