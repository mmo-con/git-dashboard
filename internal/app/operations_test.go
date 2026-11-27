package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// offlineOperations returns operations whose batches never run; finish
// delivers their results.
func offlineOperations(t *testing.T) *Operations {
	t.Helper()
	a := New("", config.Default(), git.New("git"))
	t.Cleanup(a.Close)
	next := uint64(0)
	return NewOperations(a, func([]jobs.Job) uint64 { next++; return next }, time.Now)
}

func finish(o *Operations, i int, s domain.OperationStatus, err error) {
	b := o.Batch()
	o.Handle(jobs.Event{BatchID: b.ID, JobID: i, Result: domain.OperationResult{Status: s, Error: err}})
}

func TestConfirmedUpdateWaitsForFetchAndInspections(t *testing.T) {
	o := offlineOperations(t)
	repos := []domain.Repository{behindRepo()}
	if _, ok := o.ConfirmUpdate(repos, []domain.RepositoryID{"r"}); !ok || !o.FetchForUpdate() {
		t.Fatal("a confirmed update must fetch first")
	}
	if _, ok := o.Advance(repos, func(uint64) bool { return false }); ok {
		t.Fatal("the update must wait for its fetch")
	}
	finish(o, 0, domain.StatusSucceeded, nil)
	o.AwaitInspection(42)
	if _, ok := o.Advance(repos, func(id uint64) bool { return id == 42 }); ok || !o.UpdatePending() {
		t.Fatal("the update must wait for the inspections after its fetch")
	}
	if _, ok := o.Advance(repos, func(uint64) bool { return false }); !ok || o.UpdatePending() {
		t.Fatal("the update must start once the inspections are finished")
	}
	if b := o.Batch(); b.Op != domain.OperationUpdate || len(b.Rows) != 1 || b.Rows[0].Result.Status != domain.StatusQueued {
		t.Fatalf("batch = %+v", b)
	}
}

// TestUpdateRowsKeepTheirPlannedIncomingRange checks that every update row,
// also a blocked one, keeps the range planned from the fetched state, so the
// operations view can list it after HEAD has moved.
func TestUpdateRowsKeepTheirPlannedIncomingRange(t *testing.T) {
	o := offlineOperations(t)
	dirty := behindRepo()
	dirty.ID, dirty.StoreID = "d", "d"
	dirty.Worktree.Value.Modified = 1
	repos := []domain.Repository{behindRepo(), dirty}
	o.ConfirmUpdate(repos, []domain.RepositoryID{"r", "d"})
	finish(o, 0, domain.StatusSucceeded, nil)
	finish(o, 1, domain.StatusSucceeded, nil)
	repos[0].Upstream.Value.CommitID, repos[0].Upstream.Value.Behind = "up2", 3
	if _, ok := o.Advance(repos, func(uint64) bool { return false }); !ok {
		t.Fatal("the update must start")
	}
	want := map[domain.RepositoryID]IncomingRange{
		"r": {Head: "abc", Upstream: "up2", Behind: 3},
		"d": {Head: "abc", Upstream: "up1", Behind: 2},
	}
	for _, row := range o.Batch().Rows {
		if got := row.Incoming; got != want[row.Job.RepositoryID] {
			t.Errorf("%s: incoming = %+v, want %+v", row.Job.RepositoryID, got, want[row.Job.RepositoryID])
		}
	}
	finish(o, 0, domain.StatusFailed, errors.New("merge failed"))
	if _, ok := o.Retry(); !ok {
		t.Fatal("the failed update must be retried")
	}
	if rows := o.Batch().Rows; len(rows) != 1 || rows[0].Incoming != want["r"] {
		t.Fatalf("a retried update row must keep its planned range: %+v", rows)
	}
}

func TestFetchOutcomeDecidesUpdate(t *testing.T) {
	upToDate := behindRepo()
	upToDate.Upstream.Value.Behind = 0
	tests := []struct {
		name    string
		repo    domain.Repository
		fetch   domain.OperationStatus
		err     error
		started bool
		notice  string
		skip    string
	}{
		{"failed fetch blocks the update", behindRepo(), domain.StatusFailed, errors.New("remote unavailable\ndetails"),
			true, "nothing to update; 1 blocked repositories listed", "fetch failed: remote unavailable"},
		{"failed fetch outweighs an up-to-date state", upToDate, domain.StatusFailed, nil,
			true, "nothing to update; 1 blocked repositories listed", "fetch failed"},
		{"cancelled fetch cancels the update", behindRepo(), domain.StatusCancelled, nil,
			false, "update cancelled during the fetch; press u to try again", ""},
		{"up to date after the fetch", upToDate, domain.StatusSucceeded, nil,
			false, "all 1 repositories are up to date", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := offlineOperations(t)
			repos := []domain.Repository{tc.repo}
			o.ConfirmUpdate(repos, []domain.RepositoryID{tc.repo.ID})
			finish(o, 0, tc.fetch, tc.err)
			n, started := o.Advance(repos, func(uint64) bool { return false })
			if started != tc.started || n.Text != tc.notice || o.UpdatePending() {
				t.Fatalf("started=%v notice=%q pending=%v", started, n.Text, o.UpdatePending())
			}
			if tc.skip == "" {
				return
			}
			b := o.Batch()
			if b.Op != domain.OperationUpdate || len(b.Rows) != 1 || b.Rows[0].Result.Status != domain.StatusSkipped ||
				b.Rows[0].Result.Error.Error() != tc.skip {
				t.Fatalf("rows = %+v", b.Rows)
			}
		})
	}
}

// drain hands the events of the scheduler to o until its batch is finished.
func drain(o *Operations) {
	for !o.Batch().Done() {
		for _, e := range o.app.Scheduler.Events.Next() {
			o.Handle(e)
		}
	}
}

func TestRetryAndCancelBatch(t *testing.T) {
	a := New("", config.Default(), git.New("git"))
	t.Cleanup(a.Close)
	o := NewOperations(a, a.Scheduler.Submit, time.Now)
	var calls atomic.Int32
	release := make(chan struct{})
	job := jobs.Job{Kind: jobs.KindFetch, Operation: domain.OperationFetch, RepositoryID: "r", StoreID: "s",
		Repositories: []domain.RepositoryID{"r"},
		Run: func(ctx context.Context) (any, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("network down")
			}
			select {
			case <-release:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}

	if _, ok := o.Retry(); ok {
		t.Fatal("nothing to retry without a batch")
	}
	o.start(domain.OperationFetch, []jobs.Job{job}, nil)
	if _, ok := o.Retry(); ok {
		t.Fatal("retry must wait until the batch is finished")
	}
	drain(o)
	if s := o.Batch().Rows[0].Result.Status; s != domain.StatusFailed {
		t.Fatalf("first run: %s", s)
	}
	if r, ok := o.LastResult("r"); !ok || r.Status != domain.StatusFailed {
		t.Fatal("the latest result must be kept per repository")
	}
	first := o.Batch()
	if _, ok := o.Retry(); !ok || o.Batch() == first || len(o.Batch().Rows) != 1 {
		t.Fatal("retry must start a new batch with the failed jobs")
	}
	if update, batch := o.Cancel(); update || !batch {
		t.Fatalf("cancel: update=%v batch=%v", update, batch)
	}
	drain(o)
	if s := o.Batch().Rows[0].Result.Status; s != domain.StatusCancelled {
		t.Fatalf("cancelled batch: %s", s)
	}
	close(release)

	o.Retry()
	drain(o)
	if s := o.Batch().Rows[0].Result.Status; s != domain.StatusSucceeded {
		t.Fatalf("cancelled jobs must be retried too: %s", s)
	}
	if n, ok := o.Retry(); ok || n.Text != "nothing to retry" {
		t.Fatalf("notice = %q", n.Text)
	}
}

func TestHandoverLocksStore(t *testing.T) {
	o := offlineOperations(t)
	if _, ok := o.BeginHandover("s"); !ok {
		t.Fatal("a free store must be handed over")
	}
	if busy, ok := o.BeginHandover("s"); ok || busy != "an interactive session" || !o.StoreBusy("s") {
		t.Fatalf("a second handover must be refused: %q", busy)
	}
	o.EndHandover("s")
	if o.StoreBusy("s") || !o.app.Scheduler.Locks.TryLock("s") {
		t.Fatal("the end of a handover must release the store")
	}
}

func TestConfirmCleanupListsEveryBranch(t *testing.T) {
	o := offlineOperations(t)
	r := cleanupRepo()
	plan := []CleanupCandidate{
		{Repository: r, Branch: "a", CommitID: "c1"},
		{Repository: r, Branch: "b", CommitID: "c2"},
		{Repository: r, Branch: "p", CommitID: "c3", Reason: "pinned"},
	}
	n, ok := o.ConfirmCleanup([]domain.Repository{r}, plan)
	if !ok || n.Error {
		t.Fatalf("cleanup must start: %+v", n)
	}
	b := o.Batch()
	if b.Op != domain.OperationCleanup || len(b.Rows) != 3 {
		t.Fatalf("batch = %+v", b)
	}
	for i, want := range []struct {
		branch string
		status domain.OperationStatus
	}{{"a", domain.StatusQueued}, {"b", domain.StatusQueued}, {"p", domain.StatusSkipped}} {
		if row := b.Rows[i]; row.Branch != want.branch || row.Result.Status != want.status {
			t.Fatalf("row %d = %q %v", i, row.Branch, row.Result.Status)
		}
	}
	if _, ok := o.ConfirmCleanup([]domain.Repository{r}, plan); ok {
		t.Fatal("a second batch must not start while one is running")
	}
	finish(o, 0, domain.StatusFailed, errors.New("not fully merged"))
	if _, ok := o.LastResult(r.ID); ok {
		t.Fatal("a branch deletion must not change the latest result of the repository")
	}

	o = offlineOperations(t)
	if n, ok := o.ConfirmCleanup([]domain.Repository{r}, plan[2:]); ok || n.Text == "" {
		t.Fatalf("without deletable branches nothing starts: %+v", n)
	}
}
