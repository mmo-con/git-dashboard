package app

import (
	"context"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// IncomingRange is the set of upstream commits that HEAD lacks: the commits
// reachable from Upstream but not from Head. Behind is their number. Head or
// Upstream is "" if the commit is unknown; the commits cannot be listed then.
type IncomingRange struct {
	Head     string
	Upstream string
	Behind   int
}

// Listable reports whether the range has commits that can be read.
func (r IncomingRange) Listable() bool {
	return r.Behind > 0 && r.Head != "" && r.Upstream != ""
}

// IncomingOf returns the incoming range of r as of its last inspection. It
// reports false if the state of HEAD or the upstream is unknown. A
// repository without commits, with a detached HEAD, or without an existing
// upstream has an empty range.
func IncomingOf(r *domain.Repository) (IncomingRange, bool) {
	if !r.IsAvailable() || !r.Head.Valid || !r.Upstream.Valid {
		return IncomingRange{}, false
	}
	h, u := r.Head.Value, r.Upstream.Value
	if h.Unborn || h.Detached || !u.Configured || u.Gone {
		return IncomingRange{}, true
	}
	return IncomingRange{Head: h.CommitID, Upstream: u.CommitID, Behind: u.Behind}, true
}

// IncomingPayload is attached to the event of an incoming job.
type IncomingPayload struct {
	Range   IncomingRange
	Commits []domain.CommitSummary
}

// IncomingJob creates a read-only job that reads up to n commits of rng in
// r from local objects; it never fetches. It runs in the inspection pool and
// takes no lock.
func IncomingJob(client git.Interface, r domain.Repository, rng IncomingRange, n int, timeout time.Duration) jobs.Job {
	loc := r.Location
	return jobs.Job{
		Kind:         jobs.KindInspect,
		RepositoryID: r.ID,
		StoreID:      r.StoreID,
		Timeout:      timeout,
		Run: func(ctx context.Context) (any, error) {
			commits, err := client.IncomingCommits(ctx, loc, rng.Head, rng.Upstream, n)
			return IncomingPayload{Range: rng, Commits: commits}, err
		},
	}
}
