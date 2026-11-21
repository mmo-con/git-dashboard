package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// Inspect reads the state of one worktree with three Git processes; a
// detached HEAD needs a fourth one for the latest commit.
// Fields that could not be determined stay invalid; the returned error
// describes the first failure.
func (c *Client) Inspect(ctx context.Context, loc domain.Location, opts InspectOptions) (domain.Inspection, error) {
	var in domain.Inspection
	root := loc.WorktreeRoot

	in.Operation = domain.KnownValue(operationInProgress(loc.Markers))
	in.FetchHead = fetchHeadTime(loc.Markers.FetchHead)

	untracked := opts.Untracked
	if untracked == "" {
		untracked = domain.UntrackedNormal
	}
	res, err := c.run.run(ctx, root, true, "status", "--porcelain=v2", "--branch", "-z",
		"--untracked-files="+string(untracked))
	if err != nil {
		return in, fmt.Errorf("inspection failed: %w", err)
	}
	st, err := ParseStatus(res.stdout, untracked != domain.UntrackedNo)
	if err != nil {
		return in, fmt.Errorf("inspection failed: %w", err)
	}
	in.Head = domain.KnownValue(st.Head)
	in.Worktree = domain.KnownValue(st.Worktree)
	in.Upstream = domain.KnownValue(st.Upstream)

	var firstErr error
	var commit *domain.CommitSummary
	if opts.Branches || (!st.Head.Unborn && !st.Head.Detached) {
		pattern := "refs/heads"
		if !opts.Branches {
			pattern += "/" + st.Head.Branch
		}
		res, err := c.run.run(ctx, root, true, "for-each-ref", "--format="+branchFormat, pattern)
		if err == nil {
			var branches []domain.BranchState
			branches, commit, err = parseBranches(res.stdout)
			if err == nil && opts.Branches {
				in.Branches = domain.KnownValue(branches)
			}
		}
		if err != nil {
			firstErr = fmt.Errorf("reading branches failed: %w", err)
		}
	}
	switch {
	case st.Head.Unborn:
		in.LastCommit = domain.KnownValue[*domain.CommitSummary](nil)
	case commit != nil:
		in.LastCommit = domain.KnownValue(commit)
	default:
		// Detached HEAD, or the branch changed after status ran.
		res, err := c.run.run(ctx, root, true, "log", "-1", "--no-color", "--format=%H%x00%h%x00%an%x00%ct%x00%s")
		if err == nil {
			commit, err = parseCommit(res.stdout)
			if err == nil {
				in.LastCommit = domain.KnownValue(commit)
			}
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("reading latest commit failed: %w", err)
		}
	}

	res, err = c.run.run(ctx, root, true, "config", "-z", "--get-regexp", `^remote\..*\.url$`)
	switch {
	case err == nil:
		in.Remotes = domain.KnownValue(parseRemotes(res.stdout))
	case res.exitCode == 1:
		in.Remotes = domain.KnownValue[[]domain.Remote](nil)
	default:
		if firstErr == nil {
			firstErr = fmt.Errorf("reading remotes failed: %w", err)
		}
	}
	return in, firstErr
}

func parseCommit(out []byte) (*domain.CommitSummary, error) {
	parts := strings.SplitN(strings.TrimSuffix(string(out), "\n"), "\x00", 5)
	if len(parts) != 5 {
		return nil, errors.New("unexpected git log output")
	}
	ts, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("unexpected commit timestamp %q", parts[3])
	}
	return &domain.CommitSummary{
		ID: parts[0], ShortID: parts[1], Author: parts[2],
		Time: time.Unix(ts, 0), Subject: parts[4],
	}, nil
}

// branchFormat lists one local branch per line with NUL-separated fields.
// Branch names, author names, and subjects cannot contain a newline.
const branchFormat = "%(HEAD)%00%(refname:lstrip=2)%00%(upstream:short)%00%(upstream:track,nobracket)%00" +
	"%(worktreepath)%00%(objectname)%00%(objectname:short)%00%(authorname)%00%(committerdate:unix)%00%(subject)"

// parseBranches parses for-each-ref output in branchFormat. It also returns
// the latest commit of the branch checked out in this worktree, if any.
func parseBranches(out []byte) ([]domain.BranchState, *domain.CommitSummary, error) {
	var branches []domain.BranchState
	var current *domain.CommitSummary
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) != 10 {
			return nil, nil, fmt.Errorf("unexpected git for-each-ref output %q", line)
		}
		ts, err := strconv.ParseInt(f[8], 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("unexpected commit timestamp %q", f[8])
		}
		b := domain.BranchState{
			Name: f[1], Upstream: f[2], CommitTime: time.Unix(ts, 0),
			Current: f[0] == "*", Worktree: f[4],
		}
		if b.Current {
			b.Worktree = ""
			current = &domain.CommitSummary{ID: f[5], ShortID: f[6], Author: f[7], Time: b.CommitTime, Subject: f[9]}
		}
		if b.Upstream != "" {
			if err := parseTrack(f[3], &b); err != nil {
				return nil, nil, err
			}
		}
		branches = append(branches, b)
	}
	return branches, current, nil
}

// parseTrack parses %(upstream:track,nobracket): empty, "gone", or
// "ahead N", "behind N", "ahead N, behind M".
func parseTrack(s string, b *domain.BranchState) error {
	if s == "" {
		return nil
	}
	if s == "gone" {
		b.Gone = true
		return nil
	}
	for _, part := range strings.Split(s, ", ") {
		word, num, _ := strings.Cut(part, " ")
		n, err := strconv.Atoi(num)
		if err != nil {
			return fmt.Errorf("unexpected tracking state %q", s)
		}
		switch word {
		case "ahead":
			b.Ahead = n
		case "behind":
			b.Behind = n
		default:
			return fmt.Errorf("unexpected tracking state %q", s)
		}
	}
	return nil
}
