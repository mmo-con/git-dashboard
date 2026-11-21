package app

import (
	"context"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/jobs"
)

// FetchJobs creates one fetch job per repository store among the targets.
// Each job lists every entry of its store, including worktrees that are not
// targeted, so that all of them are reinspected afterward.
func FetchJobs(client git.Interface, all, targets []domain.Repository, opts git.FetchOptions, timeout time.Duration) []jobs.Job {
	var out []jobs.Job
	seen := map[domain.StoreID]bool{}
	for _, t := range targets {
		if !t.IsAvailable() || t.StoreID == "" || seen[t.StoreID] {
			continue
		}
		seen[t.StoreID] = true
		loc := t.Location
		out = append(out, jobs.Job{
			Kind:         jobs.KindFetch,
			Operation:    domain.OperationFetch,
			RepositoryID: t.ID,
			Repositories: StoreMembers(all, map[domain.StoreID]bool{t.StoreID: true}),
			StoreID:      t.StoreID,
			Mutating:     true,
			Timeout:      timeout,
			Run: func(ctx context.Context) (any, error) {
				return nil, client.Fetch(ctx, loc, opts)
			},
		})
	}
	return out
}

// StoreCount returns the number of distinct available stores among repos.
func StoreCount(repos []domain.Repository) int {
	seen := map[domain.StoreID]bool{}
	for _, r := range repos {
		if r.IsAvailable() && r.StoreID != "" {
			seen[r.StoreID] = true
		}
	}
	return len(seen)
}
