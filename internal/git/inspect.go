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

// Inspect reads the state of one worktree with at most three Git processes.
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
	if st.Head.Unborn {
		in.LastCommit = domain.KnownValue[*domain.CommitSummary](nil)
	} else {
		res, err := c.run.run(ctx, root, true, "log", "-1", "--no-color", "--format=%H%x00%h%x00%an%x00%ct%x00%s")
		if err == nil {
			var commit *domain.CommitSummary
			commit, err = parseCommit(res.stdout)
			if err == nil {
				in.LastCommit = domain.KnownValue(commit)
			}
		}
		if err != nil {
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
