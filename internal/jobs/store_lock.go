package jobs

import (
	"context"
	"sync"

	"github.com/mmo-con/headquarters/internal/domain"
)

// StoreLocks serializes mutating work per repository store. Interactive
// handovers take the same lock so that no job runs while lazygit is open.
type StoreLocks struct {
	mu    sync.Mutex
	locks map[domain.StoreID]chan struct{}
}

func NewStoreLocks() *StoreLocks {
	return &StoreLocks{locks: map[domain.StoreID]chan struct{}{}}
}

func (l *StoreLocks) get(id domain.StoreID) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch, ok := l.locks[id]
	if !ok {
		ch = make(chan struct{}, 1)
		l.locks[id] = ch
	}
	return ch
}

// Lock waits until the store is free or ctx is done.
func (l *StoreLocks) Lock(ctx context.Context, id domain.StoreID) error {
	select {
	case l.get(id) <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryLock acquires the lock only if the store is free.
func (l *StoreLocks) TryLock(id domain.StoreID) bool {
	select {
	case l.get(id) <- struct{}{}:
		return true
	default:
		return false
	}
}

// Unlock releases a lock acquired with Lock or TryLock.
func (l *StoreLocks) Unlock(id domain.StoreID) {
	<-l.get(id)
}
