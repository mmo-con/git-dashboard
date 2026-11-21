package app

import (
	"context"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/jobs"
)

// InspectionPayload is attached to inspection job events.
type InspectionPayload struct {
	Generation uint64
	Inspection domain.Inspection
	Err        error
}

// InspectJobs creates read-only inspection jobs for the available entries.
func InspectJobs(client git.Interface, repos []domain.Repository, gen uint64, timeout time.Duration) []jobs.Job {
	var out []jobs.Job
	for _, r := range repos {
		if !r.IsAvailable() {
			continue
		}
		loc, opts := r.Location, git.InspectOptions{Untracked: r.Untracked, Branches: r.BranchPolicy.Enabled}
		out = append(out, jobs.Job{
			Kind:         jobs.KindInspect,
			RepositoryID: r.ID,
			StoreID:      r.StoreID,
			Timeout:      timeout,
			Run: func(ctx context.Context) (any, error) {
				in, err := client.Inspect(ctx, loc, opts)
				return InspectionPayload{Generation: gen, Inspection: in, Err: err}, nil
			},
		})
	}
	return out
}

// RefreshStoreFetchTimes sets LastFetch of every entry to the newest
// FETCH_HEAD time among all entries of the same store.
func RefreshStoreFetchTimes(repos []domain.Repository) {
	newest := map[domain.StoreID]domain.Known[*time.Time]{}
	for _, r := range repos {
		if r.StoreID == "" || !r.FetchHead.Valid {
			continue
		}
		cur, ok := newest[r.StoreID]
		if !ok || cur.Value == nil || (r.FetchHead.Value != nil && r.FetchHead.Value.After(*cur.Value)) {
			newest[r.StoreID] = r.FetchHead
		}
	}
	for i := range repos {
		if v, ok := newest[repos[i].StoreID]; ok {
			repos[i].LastFetch = v
		} else {
			repos[i].LastFetch = domain.Known[*time.Time]{}
		}
	}
}

// StoreMembers returns the IDs of all entries that share the given stores.
func StoreMembers(repos []domain.Repository, stores map[domain.StoreID]bool) []domain.RepositoryID {
	var ids []domain.RepositoryID
	for _, r := range repos {
		if r.StoreID != "" && stores[r.StoreID] {
			ids = append(ids, r.ID)
		}
	}
	return ids
}
