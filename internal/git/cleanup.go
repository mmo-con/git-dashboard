package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mmo-con/headquarters/internal/domain"
)

// BranchCleanup is the state of the local branches of a store that decides
// which merged branches can be deleted.
type BranchCleanup struct {
	// Remote is the remote whose HEAD names the default branch.
	Remote string
	// Default is the full name of the default branch, the target of
	// refs/remotes/<Remote>/HEAD, or "" if it is unknown; DefaultID is its
	// commit.
	Default   string
	DefaultID string
	Branches  []CleanupBranch
}

// CleanupBranch is one local branch.
type CleanupBranch struct {
	Name     string
	CommitID string
	Upstream string // short name, empty if no upstream is configured
	Gone     bool
	Ahead    int
	// Worktree is the path of the worktree that has the branch checked out.
	Worktree string
	// MergedDefault and MergedHead report whether the branch is reachable
	// from the default branch and from HEAD of the worktree that was read.
	MergedDefault bool
	MergedHead    bool
}

// ReadBranchCleanup reads the local branches of the store of loc and whether
// they are merged into the default branch of remote and into HEAD. It reads
// local refs only and never fetches.
func (c *Client) ReadBranchCleanup(ctx context.Context, loc domain.Location, remote string) (BranchCleanup, error) {
	st := BranchCleanup{Remote: remote}
	dir := loc.WorktreeRoot
	if remote != "" {
		res, err := c.run.run(ctx, dir, true, "symbolic-ref", "-q", "refs/remotes/"+remote+"/HEAD")
		var ce *CommandError
		switch {
		case err == nil:
			if ref := strings.TrimSpace(string(res.stdout)); strings.HasPrefix(ref, "refs/remotes/") {
				st.Default = ref
			}
		case errors.As(err, &ce) && ce.ExitCode == 1:
			// The ref does not exist or is not symbolic.
		default:
			return BranchCleanup{}, fmt.Errorf("reading the default branch failed: %w", err)
		}
	}
	patterns := []string{"refs/heads/"}
	if st.Default != "" {
		patterns = append(patterns, st.Default)
	}
	res, err := c.run.run(ctx, dir, true, append([]string{"for-each-ref", "--format=" + cleanupFormat}, patterns...)...)
	if err != nil {
		return BranchCleanup{}, fmt.Errorf("reading branches failed: %w", err)
	}
	attached := false
	for _, line := range strings.Split(string(res.stdout), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) != 6 {
			return BranchCleanup{}, fmt.Errorf("unexpected git for-each-ref output %q", line)
		}
		name, local := strings.CutPrefix(f[1], "refs/heads/")
		if !local {
			if f[1] == st.Default {
				st.DefaultID = f[2]
			}
			continue
		}
		b := CleanupBranch{Name: name, CommitID: f[2], Upstream: f[3], Worktree: f[5]}
		if b.Upstream != "" {
			var track domain.BranchState
			if err := parseTrack(f[4], &track); err != nil {
				return BranchCleanup{}, err
			}
			b.Gone, b.Ahead = track.Gone, track.Ahead
		}
		attached = attached || f[0] == "*"
		st.Branches = append(st.Branches, b)
	}
	if st.DefaultID == "" {
		st.Default = ""
	}
	if st.Default != "" {
		if err := c.markMerged(ctx, dir, st.Default, st.Branches, func(b *CleanupBranch) { b.MergedDefault = true }); err != nil {
			return BranchCleanup{}, err
		}
	}
	// Without a checked-out branch, HEAD is unborn or detached; no branch
	// counts as merged into it, which keeps the decision conservative.
	if attached {
		if err := c.markMerged(ctx, dir, "HEAD", st.Branches, func(b *CleanupBranch) { b.MergedHead = true }); err != nil {
			return BranchCleanup{}, err
		}
	}
	return st, nil
}

// cleanupFormat lists one ref per line with NUL-separated fields.
const cleanupFormat = "%(HEAD)%00%(refname)%00%(objectname)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(worktreepath)"

// markMerged calls mark for every branch reachable from rev.
func (c *Client) markMerged(ctx context.Context, dir, rev string, branches []CleanupBranch, mark func(*CleanupBranch)) error {
	res, err := c.run.run(ctx, dir, true, "for-each-ref", "--merged="+rev, "--format=%(refname)", "refs/heads/")
	if err != nil {
		return fmt.Errorf("reading merged branches failed: %w", err)
	}
	merged := map[string]bool{}
	for _, line := range strings.Split(string(res.stdout), "\n") {
		if name, ok := strings.CutPrefix(line, "refs/heads/"); ok {
			merged[name] = true
		}
	}
	for i := range branches {
		if merged[branches[i].Name] {
			mark(&branches[i])
		}
	}
	return nil
}

// DeleteBranch deletes a merged local branch with git branch -d, which
// refuses branches that are not fully merged.
func (c *Client) DeleteBranch(ctx context.Context, loc domain.Location, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid branch name %q", name)
	}
	_, err := c.run.run(ctx, loc.WorktreeRoot, false, "branch", "-d", "--", name)
	return err
}
