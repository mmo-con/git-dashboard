package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

func loaded(r *domain.Repository) bool {
	return !r.Inspection.FinishedAt.IsZero()
}

// statusText describes the worktree. Color is never the only carrier of
// information.
func statusText(r *domain.Repository) string {
	if !r.IsAvailable() {
		return r.Availability.String()
	}
	if !loaded(r) {
		return "…"
	}
	if !r.Worktree.Valid {
		return "unknown"
	}
	var parts []string
	if r.Operation.Valid && r.Operation.Value != domain.NoOperation {
		parts = append(parts, r.Operation.Value.String())
	}
	w := r.Worktree.Value
	add := func(label string, n int) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", label, n))
		}
	}
	add("conflict", w.Conflicts)
	add("staged", w.Staged)
	add("modified", w.Modified)
	add("deleted", w.Deleted)
	add("untracked", w.Untracked)
	if len(parts) == 0 {
		parts = append(parts, "clean")
	}
	s := strings.Join(parts, ", ")
	if r.RemoteMismatch() {
		s += " ≠remote"
	}
	return s
}

func upstreamText(r *domain.Repository, last *domain.OperationResult) string {
	if last != nil && last.Operation == domain.OperationFetch && last.Status == domain.StatusFailed {
		return "fetch failed"
	}
	if !r.IsAvailable() {
		return "–"
	}
	if !loaded(r) {
		return "…"
	}
	if !r.Head.Valid || !r.Upstream.Valid {
		return "unknown"
	}
	h, u := r.Head.Value, r.Upstream.Value
	switch {
	case h.Unborn:
		return "no commits"
	case h.Detached:
		return "detached"
	case !u.Configured:
		return "no upstream"
	case u.Gone:
		return "upstream gone"
	case u.Diverged():
		return fmt.Sprintf("diverged +%d -%d", u.Ahead, u.Behind)
	case u.Behind > 0:
		return fmt.Sprintf("behind %d", u.Behind)
	case u.Ahead > 0:
		return fmt.Sprintf("ahead %d", u.Ahead)
	}
	return "up to date"
}

func branchText(r *domain.Repository) string {
	if !r.IsAvailable() || !r.Head.Valid {
		return "–"
	}
	h := r.Head.Value
	if h.Detached {
		if len(h.CommitID) >= 7 {
			return "(" + h.CommitID[:7] + ")"
		}
		return "(detached)"
	}
	return h.Branch
}

func severity(r *domain.Repository, last *domain.OperationResult) int {
	switch {
	case !r.IsAvailable(), r.Inspection.Err != nil,
		last != nil && last.Status == domain.StatusFailed,
		r.Worktree.Valid && r.Worktree.Value.Conflicts > 0:
		return 2
	case r.Worktree.Valid && !r.Worktree.Value.Clean(),
		r.Upstream.Valid && (r.Upstream.Value.Behind > 0 || r.Upstream.Value.Ahead > 0),
		r.RemoteMismatch():
		return 1
	}
	return 0
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

func shortAgo(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/24/365))
}

func fetchAge(now time.Time, r *domain.Repository) string {
	if !r.LastFetch.Valid {
		return "unknown"
	}
	if r.LastFetch.Value == nil {
		return "never fetched"
	}
	return "fetched " + ago(now, *r.LastFetch.Value)
}
