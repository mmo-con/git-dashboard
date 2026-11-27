package app

import (
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
)

// Health is the most important state of a repository, ordered by priority.
// The zero value is unknown, so a repository is never healthy by default.
type Health int

const (
	HealthUnknown Health = iota
	HealthError
	HealthCritical
	HealthDirty
	HealthBehind
	HealthAhead
	HealthAttention
	HealthOK
)

// HealthOf classifies r: errors and unavailable entries first, then
// conflicts, operations in progress, and divergence, a dirty worktree,
// behind, ahead, and other attention such as a remote mismatch, other
// branches, stash entries, or a stale fetch. Repositories that are not
// inspected yet are unknown. last is the latest operation result of r, or
// nil; now decides whether the last fetch is stale.
func HealthOf(r *domain.Repository, last *domain.OperationResult, now time.Time) Health {
	if !r.IsAvailable() || r.EntryError != nil || r.Inspected.Err != nil || (last != nil && last.Error != nil) {
		return HealthError
	}
	if r.Inspected.FinishedAt.IsZero() || !r.Worktree.Valid || !r.Head.Valid || !r.Upstream.Valid {
		return HealthUnknown
	}
	w, h, u := r.Worktree.Value, r.Head.Value, r.Upstream.Value
	switch {
	case w.Conflicts > 0 || (r.Operation.Valid && r.Operation.Value != domain.NoOperation) || u.Diverged():
		return HealthCritical
	case !w.Clean():
		return HealthDirty
	case u.Behind > 0:
		return HealthBehind
	case u.Ahead > 0:
		return HealthAhead
	case len(r.Branches.Value) > 0 || r.RemoteMismatch() || h.Detached || u.Gone ||
		(r.Stashes.Valid && r.Stashes.Value > 0) || r.FetchStale(now):
		return HealthAttention
	}
	return HealthOK
}

// healthRank orders health levels most urgent first; unknown state ranks
// above healthy repositories.
func healthRank(h Health) int {
	switch h {
	case HealthUnknown:
		return int(HealthAttention) + 1
	case HealthOK:
		return int(HealthAttention) + 2
	}
	return int(h)
}

// String returns the stable name of h, as used in the status report.
func (h Health) String() string {
	switch h {
	case HealthError:
		return "error"
	case HealthCritical:
		return "critical"
	case HealthDirty:
		return "dirty"
	case HealthBehind:
		return "behind"
	case HealthAhead:
		return "ahead"
	case HealthAttention:
		return "attention"
	case HealthOK:
		return "ok"
	}
	return "unknown"
}
