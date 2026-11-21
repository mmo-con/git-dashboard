package domain

import (
	"sort"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// BranchState is the state of one local branch other than the one checked out
// in the worktree of the entry.
type BranchState struct {
	Name       string
	Upstream   string // empty if no upstream is configured
	Gone       bool
	Ahead      int
	Behind     int
	CommitTime time.Time
	// Current is set for the branch checked out in this worktree; Worktree
	// holds the path of another worktree that has the branch checked out.
	Current  bool
	Worktree string
	// Pinned and Missing are set by BranchPolicy.Select.
	Pinned  bool
	Missing bool
}

// Unpublished reports whether the branch has commits or a state that exists
// only locally: no upstream, a gone upstream, or commits ahead of it.
func (b BranchState) Unpublished() bool {
	return b.Upstream == "" || b.Gone || b.Ahead > 0
}

// NeedsAttention reports whether the branch differs from its upstream or has
// no usable upstream.
func (b BranchState) NeedsAttention() bool {
	return b.Missing || b.Unpublished() || b.Behind > 0
}

// BranchPolicy decides which local branches other than HEAD are reported.
type BranchPolicy struct {
	Enabled bool
	// Pinned branches are always evaluated, regardless of Ignore and MaxAge.
	Pinned []string
	// ReportMissing reports pinned branches that do not exist. It is set only
	// for per-repository lists, so that a global default such as
	// ["main", "master"] does not produce warnings everywhere.
	ReportMissing bool
	Ignore        []string // doublestar patterns matched against branch names
	MaxAge        time.Duration
}

// Select returns the branches that need attention: pinned branches first in
// configuration order, then the others by latest commit, newest first.
// Branches checked out in this or another worktree are never reported because
// they are shown by the row of their worktree.
func (p BranchPolicy) Select(all []BranchState, now time.Time) []BranchState {
	if !p.Enabled {
		return nil
	}
	rank := map[string]int{}
	for i, name := range p.Pinned {
		if _, ok := rank[name]; !ok {
			rank[name] = i
		}
	}
	exists := map[string]bool{}
	var out []BranchState
	for _, b := range all {
		exists[b.Name] = true
		if b.Current || b.Worktree != "" {
			continue
		}
		_, b.Pinned = rank[b.Name]
		if !b.NeedsAttention() {
			continue
		}
		if !b.Pinned && (p.ignored(b.Name) || (p.MaxAge > 0 && now.Sub(b.CommitTime) > p.MaxAge)) {
			continue
		}
		out = append(out, b)
	}
	if p.ReportMissing {
		for name := range rank {
			if !exists[name] {
				out = append(out, BranchState{Name: name, Pinned: true, Missing: true})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.Pinned {
			return rank[a.Name] < rank[b.Name]
		}
		if !a.CommitTime.Equal(b.CommitTime) {
			return a.CommitTime.After(b.CommitTime)
		}
		return a.Name < b.Name
	})
	return out
}

func (p BranchPolicy) ignored(name string) bool {
	for _, pat := range p.Ignore {
		if ok, _ := doublestar.Match(pat, name); ok {
			return true
		}
	}
	return false
}
