package app

import (
	"context"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// InspectionPayload is attached to inspection job events, also to those of a
// failed inspection, whose partial result is applied with the job error.
type InspectionPayload struct {
	Generation uint64
	Inspection domain.Inspection
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
				return InspectionPayload{Generation: gen, Inspection: in}, err
			},
		})
	}
	return out
}

// Inspections starts the inspection batches of one inventory, applies their
// results, and tracks the batches that are still running. It is not safe for
// concurrent use; its owner, such as the update loop of the user interface,
// delivers the events of its batches to Handle.
type Inspections struct {
	app *Application
	// submit starts the jobs of a batch; the caller delivers their events to
	// Handle.
	submit  func([]jobs.Job) uint64
	pending map[uint64]int
}

// NewInspections returns the inspections of a, which start their jobs with
// submit.
func NewInspections(a *Application, submit func([]jobs.Job) uint64) *Inspections {
	return &Inspections{app: a, submit: submit, pending: map[uint64]int{}}
}

// Start inspects the available entries of repos and returns the ID of the
// batch, or 0 if nothing is inspected.
func (s *Inspections) Start(repos []domain.Repository) uint64 {
	js := InspectJobs(s.app.Git, repos, s.app.NextGeneration(), s.app.Config.Git.InspectionTimeout)
	if len(js) == 0 {
		return 0
	}
	id := s.submit(js)
	s.pending[id] = len(js)
	return id
}

// Handle applies a finished inspection to the entry that find returns; a
// failed inspection still carries the state it could determine. It reports
// whether the batch of the event has no unfinished jobs left.
func (s *Inspections) Handle(ev jobs.Event, find func(domain.RepositoryID) *domain.Repository, at time.Time) (finished bool) {
	if !ev.Result.Status.Final() {
		return false
	}
	if s.pending[ev.BatchID]--; s.pending[ev.BatchID] <= 0 {
		delete(s.pending, ev.BatchID)
		finished = true
	}
	if p, ok := ev.Payload.(InspectionPayload); ok {
		if r := find(ev.Result.RepositoryID); r != nil {
			r.ApplyInspection(p.Generation, p.Inspection, ev.Result.Error, at)
		}
	}
	return finished
}

// Running reports whether the batch id has unfinished jobs.
func (s *Inspections) Running(id uint64) bool {
	_, ok := s.pending[id]
	return ok
}

// Pending returns the number of unfinished inspection jobs.
func (s *Inspections) Pending() int {
	n := 0
	for _, c := range s.pending {
		n += c
	}
	return n
}

// Forget stops tracking all batches, for example because the inventory was
// replaced, and returns their IDs so the owner can ignore their events.
func (s *Inspections) Forget() []uint64 {
	ids := make([]uint64, 0, len(s.pending))
	for id := range s.pending {
		ids = append(ids, id)
	}
	s.pending = map[uint64]int{}
	return ids
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
