package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// RecentCommits returns up to n commits reachable from HEAD, newest first,
// with one Git process. A repository without commits must not be asked.
// Subjects and author names cannot contain a newline, so each commit is one
// line with NUL-separated fields.
func (c *Client) RecentCommits(ctx context.Context, loc domain.Location, n int) ([]domain.CommitSummary, error) {
	if n < 1 {
		return nil, nil
	}
	res, err := c.run.run(ctx, loc.WorktreeRoot, true, "log", "--max-count="+strconv.Itoa(n), "--no-color",
		"--no-show-signature", "--format=%H%x00%h%x00%an%x00%ct%x00%s", "HEAD", "--")
	if err != nil {
		return nil, fmt.Errorf("reading recent commits failed: %w", err)
	}
	var out []domain.CommitSummary
	for _, line := range strings.Split(strings.TrimSuffix(string(res.stdout), "\n"), "\n") {
		if line == "" {
			continue
		}
		commit, err := parseCommit([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("reading recent commits failed: %w", err)
		}
		out = append(out, *commit)
	}
	return out, nil
}
