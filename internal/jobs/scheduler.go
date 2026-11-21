// Package jobs runs bounded, cancellable work for repositories.
package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// Kind selects the concurrency pool of a job.
type Kind int

const (
	KindInspect Kind = iota
	KindFetch
	KindUpdate
)

// Job describes one unit of work.
type Job struct {
	Kind         Kind
	Operation    domain.OperationType
	RepositoryID domain.RepositoryID
	Repositories []domain.RepositoryID
	StoreID      domain.StoreID
	// Mutating jobs hold the store lock while they run.
	Mutating bool
	Timeout  time.Duration
	// Run performs the work. A returned SkipError marks the job as skipped.
	Run func(ctx context.Context) (payload any, err error)
}

// SkipError reports that a job was intentionally not executed.
type SkipError struct{ Reason string }

func (e *SkipError) Error() string { return e.Reason }

// Scheduler executes jobs with per-kind parallelism and per-store locks.
type Scheduler struct {
	Events *Queue
	Locks  *StoreLocks
	// Detail extracts exit code and stderr from job errors.
	Detail func(error) (*int, string)

	pools   map[Kind]chan struct{}
	nextID  atomic.Uint64
	mu      sync.Mutex
	batches map[uint64]context.CancelFunc
	wg      sync.WaitGroup
	clock   func() time.Time
}

// NewScheduler creates a scheduler with the given limits per kind.
func NewScheduler(limits map[Kind]int) *Scheduler {
	s := &Scheduler{
		Events:  NewQueue(),
		Locks:   NewStoreLocks(),
		pools:   map[Kind]chan struct{}{},
		batches: map[uint64]context.CancelFunc{},
		clock:   time.Now,
	}
	for k, n := range limits {
		if n < 1 {
			n = 1
		}
		s.pools[k] = make(chan struct{}, n)
	}
	return s
}

// SetLimits replaces the parallelism limits. Running jobs keep their slots in
// the previous pools, so the new limits are fully in effect once they finish.
func (s *Scheduler) SetLimits(limits map[Kind]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, n := range limits {
		if n < 1 {
			n = 1
		}
		if cur := s.pools[k]; cur == nil || cap(cur) != n {
			s.pools[k] = make(chan struct{}, n)
		}
	}
}

func (s *Scheduler) pool(k Kind) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pools[k]
}

// Submit starts a batch and returns its ID. Events for every job are pushed
// to Events: queued, running, and a final status.
func (s *Scheduler) Submit(jobs []Job) uint64 {
	id := s.nextID.Add(1)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.batches[id] = cancel
	s.mu.Unlock()

	var batchWG sync.WaitGroup
	for i, j := range jobs {
		s.emit(id, i, j, domain.StatusQueued, time.Time{}, nil, nil)
		batchWG.Add(1)
		s.wg.Add(1)
		go func(i int, j Job) {
			defer s.wg.Done()
			defer batchWG.Done()
			s.execute(ctx, id, i, j)
		}(i, j)
	}
	go func() {
		batchWG.Wait()
		s.mu.Lock()
		delete(s.batches, id)
		s.mu.Unlock()
		cancel()
	}()
	return id
}

// Cancel stops a batch: queued jobs are cancelled and running commands are
// terminated.
func (s *Scheduler) Cancel(batchID uint64) {
	s.mu.Lock()
	cancel := s.batches[batchID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// CancelAll cancels every batch and waits for all jobs to finish.
func (s *Scheduler) CancelAll() {
	s.mu.Lock()
	for _, c := range s.batches {
		c()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) execute(ctx context.Context, batch uint64, i int, j Job) {
	if j.Mutating && j.StoreID != "" {
		if err := s.Locks.Lock(ctx, j.StoreID); err != nil {
			s.emit(batch, i, j, domain.StatusCancelled, time.Time{}, err, nil)
			return
		}
		defer s.Locks.Unlock(j.StoreID)
	}
	pool := s.pool(j.Kind)
	if pool != nil {
		select {
		case pool <- struct{}{}:
			defer func() { <-pool }()
		case <-ctx.Done():
			s.emit(batch, i, j, domain.StatusCancelled, time.Time{}, ctx.Err(), nil)
			return
		}
	}

	started := s.clock()
	s.emit(batch, i, j, domain.StatusRunning, started, nil, nil)
	jctx := ctx
	if j.Timeout > 0 {
		var cancel context.CancelFunc
		jctx, cancel = context.WithTimeout(ctx, j.Timeout)
		defer cancel()
	}
	payload, err := j.Run(jctx)
	status := domain.StatusSucceeded
	var skip *SkipError
	switch {
	case errors.As(err, &skip):
		status = domain.StatusSkipped
	case err != nil && ctx.Err() != nil:
		status = domain.StatusCancelled
	case err != nil && errors.Is(jctx.Err(), context.DeadlineExceeded):
		status = domain.StatusFailed
		err = &timeoutError{after: j.Timeout, cause: err}
	case err != nil:
		status = domain.StatusFailed
	}
	s.emit(batch, i, j, status, started, err, payload)
}

type timeoutError struct {
	after time.Duration
	cause error
}

func (e *timeoutError) Error() string { return "timed out after " + e.after.String() }
func (e *timeoutError) Unwrap() error { return e.cause }

func (s *Scheduler) emit(batch uint64, i int, j Job, status domain.OperationStatus, started time.Time, err error, payload any) {
	res := domain.OperationResult{
		RepositoryID: j.RepositoryID,
		StoreID:      j.StoreID,
		Operation:    j.Operation,
		Status:       status,
		StartedAt:    started,
		Error:        err,
	}
	if status.Final() {
		res.FinishedAt = s.clock()
		if err != nil && s.Detail != nil {
			res.ExitCode, res.Stderr = s.Detail(err)
		}
	}
	repos := j.Repositories
	if len(repos) == 0 && j.RepositoryID != "" {
		repos = []domain.RepositoryID{j.RepositoryID}
	}
	s.Events.Push(Event{BatchID: batch, JobID: i, Result: res, Repositories: repos, Payload: payload})
}
