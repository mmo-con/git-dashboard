package app

import (
	"sort"
	"strings"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// Filter selects visible repositories.
type Filter struct {
	Text   string
	Family domain.FamilyID // empty means all families
}

// StateKeywords lists the supported "is:" filter tokens.
var StateKeywords = []string{
	"is:dirty", "is:clean", "is:behind", "is:ahead", "is:diverged", "is:noupstream",
	"is:error", "is:unavailable", "is:mismatch", "is:marked", "is:branches", "is:unpublished",
}

// Matches reports whether r passes the filter. Text tokens are combined with
// AND; "is:" tokens filter by state, other tokens match name or path
// case-insensitively.
func (f Filter) Matches(r *domain.Repository, marked bool) bool {
	if f.Family != "" && r.Family != f.Family {
		return false
	}
	for _, tok := range strings.Fields(strings.ToLower(f.Text)) {
		if strings.HasPrefix(tok, "is:") {
			if !matchState(tok, r, marked) {
				return false
			}
			continue
		}
		if !strings.Contains(strings.ToLower(r.Name), tok) && !strings.Contains(strings.ToLower(r.Path), tok) {
			return false
		}
	}
	return true
}

func matchState(tok string, r *domain.Repository, marked bool) bool {
	w, u := r.Worktree, r.Upstream
	switch tok {
	case "is:dirty":
		return w.Valid && !w.Value.Clean()
	case "is:clean":
		return w.Valid && w.Value.Clean()
	case "is:behind":
		return u.Valid && u.Value.Behind > 0
	case "is:ahead":
		return u.Valid && u.Value.Ahead > 0
	case "is:diverged":
		return u.Valid && u.Value.Diverged()
	case "is:noupstream":
		return u.Valid && (!u.Value.Configured || u.Value.Gone)
	case "is:error":
		return r.EntryError != nil || r.Inspection.Err != nil
	case "is:unavailable":
		return !r.IsAvailable()
	case "is:mismatch":
		return r.RemoteMismatch()
	case "is:marked":
		return marked
	case "is:branches":
		return len(r.Branches.Value) > 0
	case "is:unpublished":
		return r.UnpublishedBranches() > 0
	}
	return false
}

type SortKey int

const (
	SortName SortKey = iota
	SortFamily
	SortCommitTime
	SortWorktree
	SortUpstream
)

var SortKeys = []SortKey{SortName, SortFamily, SortCommitTime, SortWorktree, SortUpstream}

func (k SortKey) String() string {
	switch k {
	case SortFamily:
		return "family"
	case SortCommitTime:
		return "latest commit"
	case SortWorktree:
		return "worktree state"
	case SortUpstream:
		return "upstream state"
	}
	return "name"
}

// Sort orders repositories in place. Name and path break ties so that the
// order is stable.
func Sort(repos []*domain.Repository, key SortKey) {
	sort.SliceStable(repos, func(i, j int) bool {
		a, b := repos[i], repos[j]
		switch key {
		case SortFamily:
			if a.Family != b.Family {
				return a.Family < b.Family
			}
		case SortCommitTime:
			ta, tb := commitUnix(a), commitUnix(b)
			if ta != tb {
				return ta > tb
			}
		case SortWorktree:
			ra, rb := worktreeRank(a), worktreeRank(b)
			if ra != rb {
				return ra < rb
			}
		case SortUpstream:
			ra, rb := upstreamRank(a), upstreamRank(b)
			if ra != rb {
				return ra < rb
			}
		}
		na, nb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if na != nb {
			return na < nb
		}
		return a.Path < b.Path
	})
}

func commitUnix(r *domain.Repository) int64 {
	if r.LastCommit.Valid && r.LastCommit.Value != nil {
		return r.LastCommit.Value.Time.Unix()
	}
	return 0
}

// worktreeRank orders problems first: errors, conflicts, operations in
// progress, local changes, untracked files, clean.
func worktreeRank(r *domain.Repository) int {
	switch {
	case !r.IsAvailable() || !r.Worktree.Valid:
		return 0
	case r.Worktree.Value.Conflicts > 0:
		return 1
	case r.Operation.Valid && r.Operation.Value != domain.NoOperation:
		return 2
	case r.Worktree.Value.HasTrackedChanges():
		return 3
	case r.Worktree.Value.Untracked > 0:
		return 4
	}
	return 5
}

// upstreamRank orders diverged, behind, ahead, missing upstream, up to date.
func upstreamRank(r *domain.Repository) int {
	if !r.IsAvailable() || !r.Upstream.Valid {
		return 0
	}
	u := r.Upstream.Value
	switch {
	case u.Diverged():
		return 1
	case u.Behind > 0:
		return 2
	case u.Ahead > 0:
		return 3
	case !u.Configured || u.Gone:
		return 4
	}
	return 5
}
