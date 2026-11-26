package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mmo-con/headquarters/internal/domain"
)

// RecentCommits returns up to n commits reachable from HEAD, newest first,
// with one Git process. A repository without commits must not be asked.
func (c *Client) RecentCommits(ctx context.Context, loc domain.Location, n int) ([]domain.CommitSummary, error) {
	commits, err := c.log(ctx, loc, n, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("reading recent commits failed: %w", err)
	}
	return commits, nil
}

// IncomingCommits returns up to n commits reachable from upstream but not
// from head, newest first: the commits a fast-forward from head to upstream
// would bring. Both are commit IDs, so only local objects are read.
func (c *Client) IncomingCommits(ctx context.Context, loc domain.Location, head, upstream string, n int) ([]domain.CommitSummary, error) {
	if !isObjectID(head) || !isObjectID(upstream) {
		return nil, fmt.Errorf("reading incoming commits failed: invalid commit ID")
	}
	commits, err := c.log(ctx, loc, n, upstream, "^"+head)
	if err != nil {
		return nil, fmt.Errorf("reading incoming commits failed: %w", err)
	}
	return commits, nil
}

// log runs git log for revs with one Git process. Subjects and author names
// cannot contain a newline, so each commit is one line with NUL-separated
// fields.
func (c *Client) log(ctx context.Context, loc domain.Location, n int, revs ...string) ([]domain.CommitSummary, error) {
	if n < 1 {
		return nil, nil
	}
	args := append([]string{"log", "--max-count=" + strconv.Itoa(n), "--no-color",
		"--no-show-signature", "--format=%H%x00%h%x00%an%x00%ct%x00%s"}, revs...)
	res, err := c.run.run(ctx, loc.WorktreeRoot, true, append(args, "--")...)
	if err != nil {
		return nil, err
	}
	var out []domain.CommitSummary
	for _, line := range strings.Split(strings.TrimSuffix(string(res.stdout), "\n"), "\n") {
		if line == "" {
			continue
		}
		commit, err := parseCommit([]byte(line))
		if err != nil {
			return nil, err
		}
		out = append(out, *commit)
	}
	return out, nil
}

// isObjectID reports whether s is a full hexadecimal object ID, which can
// be passed to Git without being taken for an option or a revision range.
func isObjectID(s string) bool {
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
