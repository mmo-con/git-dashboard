package git

import (
	"context"
	"fmt"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// MergeFastForwardOnly fast-forwards the current branch to target, the full
// object name of the upstream commit that the user confirmed. It never
// contacts the network, never creates a merge commit, never stashes, and never
// overwrites untracked or ignored files.
func (c *Client) MergeFastForwardOnly(ctx context.Context, loc domain.Location, target string) error {
	if !isObjectName(target) {
		return fmt.Errorf("invalid merge target %q", target)
	}
	_, err := c.run.run(ctx, loc.WorktreeRoot, false, "merge", "--ff-only", "--no-autostash", "--no-overwrite-ignore", target)
	return err
}

// isObjectName reports whether s is a full SHA-1 or SHA-256 object name.
func isObjectName(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
