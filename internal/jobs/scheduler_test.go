package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// collect drains events until n final events have arrived.
func collect(t *testing.T, s *Scheduler, n int) []Event {
	t.Helper()
	var final []Event
	deadline := time.After(10 * time.Second)
	got := make(chan []Event)
	for len(final) < n {
		go func() { got <- s.Events.Next() }()
		select {
		case evs := <-got:
			for _, e := range evs {
				if e.Result.Status.Final() {
					final = append(final, e)
				}
			}
		case <-deadline:
			t.Fatalf("only %d of %d jobs finished", len(final), n)
		}
	}
	return final
}

func TestParallelismLimit(t *testing.T) {
	s := NewScheduler(map[Kind]int{KindFetch: 2})
	if peak := runConcurrent(t, s, 8); peak > 2 {
		t.Fatalf("peak parallelism %d exceeds limit 2", peak)
	}
	s.SetLimits(map[Kind]int{KindFetch: 1})
	if peak := runConcurrent(t, s, 4); peak != 1 {
		t.Fatalf("peak parallelism %d after lowering the limit to 1", peak)
	}
}

// TestJobsUseTheirParallelism verifies that jobs actually run concurrently up
// to the limit: each job waits until the limit is reached, so a scheduler that
// serialized them would time out.
func TestJobsUseTheirParallelism(t *testing.T) {
	const limit = 3
	s := NewScheduler(map[Kind]int{KindInspect: limit})
	var running atomic.Int32
	full := make(chan struct{})
	var js []Job
	for i := 0; i < 2*limit; i++ {
		js = append(js, Job{Kind: KindInspect, RepositoryID: domain.RepositoryID(rune('a' + i)),
			Run: func(context.Context) (any, error) {
				if running.Add(1) == limit {
					close(full)
				}
				select {
				case <-full:
				case <-time.After(5 * time.Second):
					return nil, errors.New("jobs did not run concurrently")
				}
				return nil, nil
			}})
	}
	s.Submit(js)
	for _, e := range collect(t, s, len(js)) {
		if e.Result.Error != nil {
			t.Fatal(e.Result.Error)
		}
	}
}

// runConcurrent runs n fetch jobs of distinct stores and returns the peak
// number of jobs that ran at the same time.
func runConcurrent(t *testing.T, s *Scheduler, n int) int32 {
	t.Helper()
	var running, peak atomic.Int32
	var js []Job
	for i := 0; i < n; i++ {
		js = append(js, Job{Kind: KindFetch, StoreID: domain.StoreID(rune('a' + i)), Mutating: true,
			Run: func(context.Context) (any, error) {
				n := running.Add(1)
				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				running.Add(-1)
				return nil, nil
			}})
	}
	s.Submit(js)
	collect(t, s, n)
	return peak.Load()
}

func TestStoreLockSerializesMutatingJobs(t *testing.T) {
	s := NewScheduler(map[Kind]int{KindFetch: 4})
	var running, peak atomic.Int32
	job := Job{Kind: KindFetch, StoreID: "same", Mutating: true, Run: func(context.Context) (any, error) {
		if n := running.Add(1); n > peak.Load() {
			peak.Store(n)
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return nil, nil
	}}
	s.Submit([]Job{job, job, job})
	collect(t, s, 3)
	if peak.Load() != 1 {
		t.Fatalf("jobs of the same store ran concurrently: %d", peak.Load())
	}
}

func TestHandoverLockBlocksJobs(t *testing.T) {
	s := NewScheduler(map[Kind]int{KindFetch: 1})
	if !s.Locks.TryLock("store") {
		t.Fatal("lock must be free")
	}
	var ran atomic.Bool
	s.Submit([]Job{{Kind: KindFetch, StoreID: "store", Mutating: true, Run: func(context.Context) (any, error) {
		ran.Store(true)
		return nil, nil
	}}})
	time.Sleep(50 * time.Millisecond)
	if ran.Load() {
		t.Fatal("job ran during the handover")
	}
	if s.Locks.TryLock("store") {
		t.Fatal("TryLock must fail while the store is locked")
	}
	s.Locks.Unlock("store")
	collect(t, s, 1)
	if !ran.Load() {
		t.Fatal("job did not run after the handover")
	}
}

func TestCancelTimeoutSkipAndFailure(t *testing.T) {
	s := NewScheduler(map[Kind]int{KindFetch: 1})
	block := Job{Kind: KindFetch, Run: func(ctx context.Context) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	id := s.Submit([]Job{block, block})
	time.Sleep(20 * time.Millisecond)
	s.Cancel(id)
	for _, e := range collect(t, s, 2) {
		if e.Result.Status != domain.StatusCancelled {
			t.Errorf("status = %s, want cancelled", e.Result.Status)
		}
	}

	timeout := block
	timeout.Timeout = 30 * time.Millisecond
	skip := Job{Kind: KindFetch, Run: func(context.Context) (any, error) { return nil, &SkipError{Reason: "no"} }}
	fail := Job{Kind: KindFetch, Run: func(context.Context) (any, error) { return nil, errors.New("boom") }}
	s.Submit([]Job{timeout, skip, fail})
	got := map[int]domain.OperationStatus{}
	for _, e := range collect(t, s, 3) {
		got[e.JobID] = e.Result.Status
		if e.JobID == 0 && (e.Result.Error == nil || e.Result.Error.Error() != "timed out after 30ms") {
			t.Errorf("timeout error: %v", e.Result.Error)
		}
	}
	if got[0] != domain.StatusFailed || got[1] != domain.StatusSkipped || got[2] != domain.StatusFailed {
		t.Fatalf("statuses = %v", got)
	}
}

func TestQueueNeverBlocksProducers(t *testing.T) {
	q := NewQueue()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			q.Push(Event{JobID: i})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("producer blocked without a consumer")
	}
	if n := len(q.Next()); n != 10000 {
		t.Fatalf("got %d events", n)
	}
	q.Close()
	if q.Next() != nil {
		t.Fatal("Next must return nil after Close")
	}
}
