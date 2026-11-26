package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// Batch is a fetch or update batch. Repositories that the update excluded are
// kept as skipped rows that never run, so that the batch lists every
// repository that still needs attention.
type Batch struct {
	ID   uint64
	Op   domain.OperationType
	Rows []BatchRow
}

// BatchRow is one repository of a batch.
type BatchRow struct {
	Job    jobs.Job
	Result domain.OperationResult
	// RevisitedAt is the time the user returned from lazygit after opening
	// the repository from this row.
	RevisitedAt time.Time
	// Incoming is the range an update row was planned with, read after the
	// fetch; it stays fixed when the fast-forward moves HEAD.
	Incoming IncomingRange
}

// Done reports whether every row has a final status.
func (b *Batch) Done() bool {
	for _, r := range b.Rows {
		if !r.Result.Status.Final() {
			return false
		}
	}
	return true
}

// Notice is a message for the user; an empty text means nothing to report.
type Notice struct {
	Text  string
	Error bool
}

func info(format string, args ...any) Notice {
	return Notice{Text: fmt.Sprintf(format, args...)}
}

func failure(format string, args ...any) Notice {
	return Notice{Text: fmt.Sprintf(format, args...), Error: true}
}

// pendingUpdate is a confirmed update whose targets are fetched first. The
// update is planned only after the fetch and the inspections that follow it,
// so that it applies the fetched upstream state.
type pendingUpdate struct {
	fetch       *Batch
	targets     []domain.RepositoryID
	inspections []uint64
}

// Operations runs the fetch and update batches of a session: at most one
// batch at a time, the safe update that fetches before it fast-forwards, the
// latest result per repository, and the stores locked by interactive
// handovers. It is not safe for concurrent use; the user interface calls it
// from its update loop, and repos is always its current list of entries.
type Operations struct {
	app *Application
	// submit starts the jobs of a batch; the caller delivers their events to
	// Handle.
	submit    func([]jobs.Job) uint64
	now       func() time.Time
	batch     *Batch
	update    *pendingUpdate
	last      map[domain.RepositoryID]domain.OperationResult
	handovers map[domain.StoreID]bool
}

// NewOperations returns the operations of a, which start their jobs with
// submit.
func NewOperations(a *Application, submit func([]jobs.Job) uint64, now func() time.Time) *Operations {
	return &Operations{
		app: a, submit: submit, now: now,
		last:      map[domain.RepositoryID]domain.OperationResult{},
		handovers: map[domain.StoreID]bool{},
	}
}

// Batch returns the current or latest batch, or nil.
func (o *Operations) Batch() *Batch { return o.batch }

// Running reports whether the current batch has unfinished jobs or a
// confirmed update still waits for its fetch.
func (o *Operations) Running() bool {
	return (o.batch != nil && !o.batch.Done()) || o.update != nil
}

// UpdatePending reports whether a confirmed update waits for its fetch.
func (o *Operations) UpdatePending() bool { return o.update != nil }

// FetchForUpdate reports whether the current batch is the fetch of a
// confirmed update.
func (o *Operations) FetchForUpdate() bool {
	return o.update != nil && o.batch != nil && o.update.fetch == o.batch
}

// LastResult returns the result of the latest operation of a repository.
func (o *Operations) LastResult(id domain.RepositoryID) (domain.OperationResult, bool) {
	r, ok := o.last[id]
	return r, ok
}

// Health classifies r with its latest operation result (see HealthOf).
func (o *Operations) Health(r *domain.Repository, now time.Time) Health {
	if last, ok := o.last[r.ID]; ok {
		return HealthOf(r, &last, now)
	}
	return HealthOf(r, nil, now)
}

// ForgetResults drops the latest results of ids, for example after the user
// fetched them in the terminal.
func (o *Operations) ForgetResults(ids []domain.RepositoryID) {
	for _, id := range ids {
		delete(o.last, id)
	}
}

// StoreBusy reports whether a mutating job or a handover is active or queued
// for the store.
func (o *Operations) StoreBusy(id domain.StoreID) bool { return o.StoreActivity(id) != "" }

// StoreActivity names the mutating job or handover that is active or queued
// for the store, or returns "".
func (o *Operations) StoreActivity(id domain.StoreID) string {
	if o.handovers[id] {
		return "an interactive session"
	}
	if o.batch != nil {
		for _, row := range o.batch.Rows {
			if row.Job.StoreID == id && !row.Result.Status.Final() {
				return string(row.Job.Operation)
			}
		}
	}
	return ""
}

// BeginHandover locks the store for an interactive session. If the store is
// busy, it returns the activity that blocks it and false.
func (o *Operations) BeginHandover(id domain.StoreID) (string, bool) {
	if busy := o.StoreActivity(id); busy != "" {
		return busy, false
	}
	if !o.app.Scheduler.Locks.TryLock(id) {
		return "another operation", false
	}
	o.handovers[id] = true
	return "", true
}

// EndHandover releases the store of an interactive session.
func (o *Operations) EndHandover(id domain.StoreID) {
	o.app.Scheduler.Locks.Unlock(id)
	delete(o.handovers, id)
}

// Plan splits targets into eligible and skipped repositories with their
// current state.
func (o *Operations) Plan(targets []domain.Repository) (eligible, skipped []UpdateCandidate) {
	return PlanUpdate(targets, o.StoreBusy)
}

// Fetch starts a fetch of targets. It reports whether a batch started.
func (o *Operations) Fetch(repos, targets []domain.Repository) (Notice, bool) {
	if o.Running() {
		return failure("another batch is still running"), false
	}
	js := o.fetchJobs(repos, targets)
	if len(js) == 0 {
		return failure("no available repositories to fetch"), false
	}
	return o.start(domain.OperationFetch, js, nil), true
}

// ConfirmUpdate starts a confirmed update of targets: it fetches their stores
// first, and Advance plans and starts the update once the fetch and the
// following inspections are finished. It reports whether a batch started.
func (o *Operations) ConfirmUpdate(repos []domain.Repository, targets []domain.RepositoryID) (Notice, bool) {
	if o.Running() {
		return failure("another batch is still running"), false
	}
	o.update = &pendingUpdate{targets: targets}
	js := o.fetchJobs(repos, lookup(repos, targets))
	if len(js) == 0 {
		return o.runUpdate(repos, nil)
	}
	o.start(domain.OperationFetch, js, nil)
	o.update.fetch = o.batch
	return info("fetching %d repository stores before the update", len(js)), true
}

// Handle records an event of the current batch. It returns the repositories
// that a finished job changed, which must be inspected again, and a summary
// once the batch is finished.
func (o *Operations) Handle(ev jobs.Event) (reinspect []domain.RepositoryID, summary Notice) {
	b := o.batch
	if b == nil || ev.BatchID != b.ID || ev.JobID >= len(b.Rows) {
		return nil, Notice{}
	}
	res := ev.Result
	b.Rows[ev.JobID].Result = res
	if !res.Status.Final() {
		return nil, Notice{}
	}
	for _, id := range ev.Repositories {
		o.last[id] = res
	}
	if b.Done() {
		summary = b.summary()
	}
	return ev.Repositories, summary
}

// AwaitInspection makes a confirmed update wait for the inspection batch id,
// which inspects repositories changed by its fetch.
func (o *Operations) AwaitInspection(id uint64) {
	if o.FetchForUpdate() && id != 0 {
		o.update.inspections = append(o.update.inspections, id)
	}
}

// Advance starts a confirmed update once its fetch and the inspections that
// follow it are finished; inspecting reports whether an inspection batch is
// still running. It plans the update with the fetched state, so the update applies
// exactly the upstream commits read after the fetch. A cancelled fetch
// cancels the update. It reports whether a batch started.
func (o *Operations) Advance(repos []domain.Repository, inspecting func(batch uint64) bool) (Notice, bool) {
	u := o.update
	if u == nil || u.fetch == nil {
		return Notice{}, false
	}
	if u.fetch != o.batch {
		o.update = nil
		return Notice{}, false
	}
	if !u.fetch.Done() {
		return Notice{}, false
	}
	for _, id := range u.inspections {
		if inspecting(id) {
			return Notice{}, false
		}
	}
	failed := map[domain.StoreID]string{}
	for _, row := range u.fetch.Rows {
		switch row.Result.Status {
		case domain.StatusCancelled:
			o.update = nil
			return info("update cancelled during the fetch; press u to try again"), false
		case domain.StatusFailed:
			reason := "fetch failed"
			if row.Result.Error != nil {
				reason += ": " + firstLine(row.Result.Error.Error())
			}
			failed[row.Job.StoreID] = reason
		}
	}
	return o.runUpdate(repos, failed)
}

// DropUpdate forgets a confirmed update that waits for its fetch, for
// example because the inventory was replaced.
func (o *Operations) DropUpdate() { o.update = nil }

// Cancel stops the current batch and a confirmed update that waits for it.
// It reports whether it dropped such an update and whether the batch was
// still running.
func (o *Operations) Cancel() (update, batch bool) {
	if o.update != nil {
		o.update = nil
		update = true
	}
	if o.batch != nil && !o.batch.Done() {
		o.app.Scheduler.Cancel(o.batch.ID)
		batch = true
	}
	return update, batch
}

// Retry starts the failed and cancelled jobs of the finished batch again. It
// reports whether a batch started.
func (o *Operations) Retry() (Notice, bool) {
	if o.batch == nil || o.Running() {
		return Notice{}, false
	}
	var js []jobs.Job
	planned := map[domain.RepositoryID]IncomingRange{}
	for _, r := range o.batch.Rows {
		if r.Result.Status == domain.StatusFailed || r.Result.Status == domain.StatusCancelled {
			js = append(js, r.Job)
			planned[r.Job.RepositoryID] = r.Incoming
		}
	}
	if len(js) == 0 {
		return info("nothing to retry"), false
	}
	n := o.start(o.batch.Op, js, nil)
	for i := range o.batch.Rows {
		o.batch.Rows[i].Incoming = planned[o.batch.Rows[i].Job.RepositoryID]
	}
	return n, true
}

// NotUpdated returns every repository of the finished update batch that has
// not been updated, for a new update preview.
func (o *Operations) NotUpdated(repos []domain.Repository) ([]domain.Repository, Notice) {
	b := o.batch
	if b == nil || b.Op != domain.OperationUpdate || !b.Done() {
		return nil, failure("continue is available for a finished update batch")
	}
	var ids []domain.RepositoryID
	seen := map[domain.RepositoryID]bool{}
	for _, row := range b.Rows {
		id := row.Job.RepositoryID
		if row.Result.Status != domain.StatusSucceeded && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	targets := lookup(repos, ids)
	if len(targets) == 0 {
		return nil, info("nothing left to update")
	}
	return targets, Notice{}
}

// runUpdate plans the update of the pending targets with their current state
// and starts it. Fetch failures take precedence over every other reason,
// because the state of these repositories is not fresh.
func (o *Operations) runUpdate(repos []domain.Repository, failed map[domain.StoreID]string) (Notice, bool) {
	targets := lookup(repos, o.update.targets)
	o.update = nil
	ok, skip := o.Plan(targets)
	var eligible, blocked []UpdateCandidate
	for _, c := range append(ok, skip...) {
		if reason, bad := failed[c.Repository.StoreID]; bad {
			c.Verdict = Blocked(reason)
			blocked = append(blocked, c)
		} else if c.Verdict.Eligible() {
			eligible = append(eligible, c)
		} else if !c.Verdict.UpToDate() {
			blocked = append(blocked, c)
		}
	}
	if len(eligible) == 0 && len(blocked) == 0 {
		return info("all %d repositories are up to date", len(targets)), false
	}
	js := UpdateJobs(o.app.Git, repos, eligible, o.app.Config.Git.FetchTimeout)
	n := o.start(domain.OperationUpdate, js, blocked)
	planned := map[domain.RepositoryID]IncomingRange{}
	for _, c := range append(eligible, blocked...) {
		planned[c.Repository.ID], _ = IncomingOf(&c.Repository)
	}
	for i := range o.batch.Rows {
		o.batch.Rows[i].Incoming = planned[o.batch.Rows[i].Job.RepositoryID]
	}
	return n, true
}

// start makes js the current batch. skipped lists repositories that the
// update excluded; they are added as skipped rows.
func (o *Operations) start(op domain.OperationType, js []jobs.Job, skipped []UpdateCandidate) Notice {
	b := &Batch{Op: op, Rows: make([]BatchRow, len(js), len(js)+len(skipped))}
	for i, j := range js {
		b.Rows[i] = BatchRow{Job: j, Result: domain.OperationResult{
			RepositoryID: j.RepositoryID, StoreID: j.StoreID, Operation: op, Status: domain.StatusQueued,
		}}
	}
	now := o.now()
	for _, c := range skipped {
		r := c.Repository
		b.Rows = append(b.Rows, BatchRow{
			Job: jobs.Job{RepositoryID: r.ID, StoreID: r.StoreID, Operation: op},
			Result: domain.OperationResult{
				RepositoryID: r.ID, StoreID: r.StoreID, Operation: op, Status: domain.StatusSkipped,
				FinishedAt: now, Error: errors.New(c.Verdict.String()),
			},
		})
	}
	o.batch = b
	if len(js) == 0 {
		return info("nothing to %s; %d blocked repositories listed", op, len(skipped))
	}
	b.ID = o.submit(js)
	return info("%s started for %d repositories", op, len(js))
}

func (o *Operations) fetchJobs(repos, targets []domain.Repository) []jobs.Job {
	return FetchJobs(o.app.Git, repos, targets, o.app.FetchOptions(), o.app.Config.Git.FetchTimeout)
}

func (b *Batch) summary() Notice {
	counts := map[domain.OperationStatus]int{}
	for _, r := range b.Rows {
		counts[r.Result.Status]++
	}
	msg := fmt.Sprintf("%s finished: %d succeeded", b.Op, counts[domain.StatusSucceeded])
	for _, s := range []domain.OperationStatus{domain.StatusFailed, domain.StatusSkipped, domain.StatusCancelled} {
		if counts[s] > 0 {
			msg += fmt.Sprintf(", %d %s", counts[s], s)
		}
	}
	if counts[domain.StatusFailed] > 0 {
		return failure("%s (press b for details)", msg)
	}
	return Notice{Text: msg}
}

// lookup returns the entries of repos with the given IDs in the order of ids;
// IDs without an entry are left out.
func lookup(repos []domain.Repository, ids []domain.RepositoryID) []domain.Repository {
	index := make(map[domain.RepositoryID]int, len(repos))
	for i, r := range repos {
		index[r.ID] = i
	}
	var out []domain.Repository
	for _, id := range ids {
		if i, ok := index[id]; ok {
			out = append(out, repos[i])
		}
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
