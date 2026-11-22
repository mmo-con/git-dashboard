package jobs

import (
	"sync"

	"github.com/mmo-con/headquarters/internal/domain"
)

// Event reports a change of a job.
type Event struct {
	BatchID uint64
	JobID   int
	Result  domain.OperationResult
	// Repositories lists every entry affected by the job, for example all
	// worktrees of a fetched store.
	Repositories []domain.RepositoryID
	// Payload carries job-specific data, such as an inspection result.
	Payload any
}

// Queue is an unbounded event queue. Producers never block; a single consumer
// drains batches of events.
type Queue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	events []Event
	closed bool
}

func NewQueue() *Queue {
	q := &Queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push appends an event without blocking.
func (q *Queue) Push(e Event) {
	q.mu.Lock()
	if !q.closed {
		q.events = append(q.events, e)
	}
	q.mu.Unlock()
	q.cond.Signal()
}

// Next waits for events and returns all pending ones. It returns nil after
// Close.
func (q *Queue) Next() []Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.events) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return nil
	}
	out := q.events
	q.events = nil
	return out
}

// Close wakes the consumer and discards further events.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
}
