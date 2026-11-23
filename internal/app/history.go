package app

import (
	"context"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// HistoryPayload is attached to the event of a history job. Head is the
// commit the history starts at, as known when the job was created.
type HistoryPayload struct {
	Head    string
	Commits []domain.CommitSummary
}

// HistoryJob creates a read-only job that reads the n most recent commits of
// r for the detail panel. It runs in the inspection pool and takes no lock.
func HistoryJob(client git.Interface, r domain.Repository, n int, timeout time.Duration) jobs.Job {
	loc, head := r.Location, r.Head.Value.CommitID
	return jobs.Job{
		Kind:         jobs.KindInspect,
		RepositoryID: r.ID,
		StoreID:      r.StoreID,
		Timeout:      timeout,
		Run: func(ctx context.Context) (any, error) {
			commits, err := client.RecentCommits(ctx, loc, n)
			return HistoryPayload{Head: head, Commits: commits}, err
		},
	}
}
