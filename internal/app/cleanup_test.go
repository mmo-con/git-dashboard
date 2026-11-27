package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
)

func cleanupRepo() domain.Repository {
	return domain.Repository{
		ID: "/w/r", StoreID: "/w/r/.git", Name: "r", Availability: domain.Available,
		Location:      domain.Location{WorktreeRoot: "/w/r"},
		PrimaryRemote: &domain.Remote{Name: "origin"},
		BranchPolicy:  domain.BranchPolicy{Pinned: []string{"release"}},
	}
}

// cleanupState is a store whose default branch origin/main is at commit d0
// and whose worktree /w/r has main checked out.
func cleanupState(branches ...git.CleanupBranch) git.BranchCleanup {
	main := git.CleanupBranch{Name: "main", CommitID: "d0", Upstream: "origin/main", Worktree: "/w/r", MergedDefault: true, MergedHead: true}
	return git.BranchCleanup{Remote: "origin", Default: "refs/remotes/origin/main", DefaultID: "d0",
		Branches: append([]git.CleanupBranch{main}, branches...)}
}

// planned returns branch: reason for every row; "delete" marks deletable rows.
func planned(rows []CleanupCandidate) map[string]string {
	out := map[string]string{}
	for _, c := range rows {
		if c.Deletable() {
			out[c.Branch] = "delete"
		} else {
			out[c.Branch] = c.Reason
		}
	}
	return out
}

func TestPlanCleanup(t *testing.T) {
	merged := func(name string) git.CleanupBranch {
		return git.CleanupBranch{Name: name, CommitID: "c-" + name, MergedDefault: true, MergedHead: true}
	}
	tests := []struct {
		name   string
		branch git.CleanupBranch
		want   string // "delete", a reason substring, or "" for not listed
	}{
		{"merged without upstream", merged("a"), "delete"},
		{"merged with gone upstream", func() git.CleanupBranch {
			b := merged("a")
			b.Upstream, b.Gone = "origin/a", true
			return b
		}(), "delete"},
		{"merged into its existing upstream", func() git.CleanupBranch {
			b := merged("a")
			b.Upstream, b.MergedHead = "origin/a", false
			return b
		}(), "delete"},
		{"checked out here", func() git.CleanupBranch { b := merged("a"); b.Worktree = "/w/r"; return b }(), "checked out"},
		{"checked out in another worktree", func() git.CleanupBranch { b := merged("a"); b.Worktree = "/w/other"; return b }(), "checked out in /w/other"},
		{"pinned", merged("release"), "pinned"},
		{"gone but not merged", git.CleanupBranch{Name: "a", CommitID: "x", Upstream: "origin/a", Gone: true}, "not merged into origin/main"},
		{"merged but not into HEAD", func() git.CleanupBranch {
			b := merged("a")
			b.Upstream, b.Gone, b.MergedHead = "origin/a", true, false
			return b
		}(), "update main first"},
		{"merged but ahead of its upstream", func() git.CleanupBranch {
			b := merged("a")
			b.Upstream, b.Ahead = "origin/a", 2
			return b
		}(), "not pushed to origin/a"},
		{"no commits of its own", func() git.CleanupBranch { b := merged("a"); b.CommitID = "d0"; return b }(), "no commits of its own"},
		{"not merged and active", git.CleanupBranch{Name: "a", CommitID: "x", Upstream: "origin/a"}, ""},
		{"not merged without upstream", git.CleanupBranch{Name: "a", CommitID: "x"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := planned(PlanCleanup(cleanupRepo(), cleanupState(tc.branch)))
			if _, listed := got["main"]; listed {
				t.Fatalf("the default branch must not be listed: %v", got)
			}
			reason, listed := got[tc.branch.Name]
			switch {
			case tc.want == "" && listed:
				t.Fatalf("must not be listed: %q", reason)
			case tc.want != "" && !strings.Contains(reason, tc.want):
				t.Fatalf("got %q, want %q", reason, tc.want)
			}
		})
	}
}

func TestPlanCleanupKeepsCommitAndRepository(t *testing.T) {
	r := cleanupRepo()
	rows := PlanCleanup(r, cleanupState(git.CleanupBranch{Name: "a", CommitID: "c1", MergedDefault: true, MergedHead: true}))
	if len(rows) != 1 || rows[0].CommitID != "c1" || rows[0].Repository.ID != r.ID {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestPlanCleanupWithoutDefaultBranch(t *testing.T) {
	st := cleanupState(git.CleanupBranch{Name: "a", CommitID: "c1"})
	st.Default, st.DefaultID = "", ""
	rows := PlanCleanup(cleanupRepo(), st)
	if len(rows) != 1 || rows[0].Branch != "" || rows[0].Deletable() ||
		!strings.Contains(rows[0].Reason, "git remote set-head origin --auto") {
		t.Fatalf("rows = %+v", rows)
	}

	r := cleanupRepo()
	r.PrimaryRemote = nil
	st.Remote = ""
	if rows := PlanCleanup(r, st); len(rows) != 1 || !strings.Contains(rows[0].Reason, "no remote") {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestCleanupTargetsReadEachStoreOnce(t *testing.T) {
	a, linked, b, missing := cleanupRepo(), cleanupRepo(), cleanupRepo(), cleanupRepo()
	linked.ID = "/w/linked"
	b.ID, b.StoreID = "/w/b", "/w/b/.git"
	missing.ID, missing.StoreID, missing.Availability = "/w/m", "/w/m/.git", domain.Missing
	all := []domain.Repository{a, linked, b, missing}
	read, blocked := CleanupTargets(all, all)
	if len(read) != 2 || read[0].ID != a.ID || read[1].ID != b.ID {
		t.Fatalf("read = %+v", read)
	}
	if len(blocked) != 1 || blocked[0].Repository.ID != missing.ID || !strings.Contains(blocked[0].Reason, "missing") {
		t.Fatalf("blocked = %+v", blocked)
	}
}

func TestCleanupJobRechecksBeforeDeleting(t *testing.T) {
	r := cleanupRepo()
	a := git.CleanupBranch{Name: "a", CommitID: "c1", MergedDefault: true, MergedHead: true}
	planned := []CleanupCandidate{{Repository: r, Branch: "a", CommitID: "c1"}}
	tests := []struct {
		name   string
		state  git.BranchCleanup
		err    error
		status domain.OperationStatus
		reason string
	}{
		{"deleted", cleanupState(a), nil, domain.StatusSucceeded, ""},
		{"gone", cleanupState(), nil, domain.StatusSkipped, "no longer exists"},
		{"moved", cleanupState(func() git.CleanupBranch { b := a; b.CommitID = "c2"; return b }()), nil, domain.StatusSkipped, "moved since the preview"},
		{"checked out meanwhile", cleanupState(func() git.CleanupBranch { b := a; b.Worktree = "/w/other"; return b }()), nil, domain.StatusSkipped, "checked out in /w/other"},
		{"refused by Git", cleanupState(a), errors.New("not fully merged"), domain.StatusFailed, "not fully merged"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGit{cleanup: tc.state, deleteErr: tc.err}
			js := CleanupJobs(f, []domain.Repository{r}, planned, time.Second)
			if len(js) != 1 || !js[0].Mutating || js[0].StoreID != r.StoreID || js[0].Operation != domain.OperationCleanup {
				t.Fatalf("jobs = %+v", js)
			}
			ev := runJobs(t, js)[0].Result
			if ev.Status != tc.status || (tc.reason != "" && (ev.Error == nil || !strings.Contains(ev.Error.Error(), tc.reason))) {
				t.Fatalf("result = %v %v", ev.Status, ev.Error)
			}
			if deleted := len(f.deleted) == 1 && f.deleted[0] == "a"; deleted != (tc.status == domain.StatusSucceeded) {
				t.Fatalf("deleted = %v", f.deleted)
			}
		})
	}
}

// TestCleanupTargetsKeepPinsOfTheWholeStore checks that a branch pinned by
// any entry of a store is kept, also when another entry reads the store.
func TestCleanupTargetsKeepPinsOfTheWholeStore(t *testing.T) {
	a, linked := cleanupRepo(), cleanupRepo()
	a.BranchPolicy.Pinned = []string{"main"}
	linked.ID, linked.BranchPolicy.Pinned = "/w/linked", []string{"keep"}
	read, _ := CleanupTargets([]domain.Repository{a, linked}, []domain.Repository{a})
	if len(read) != 1 {
		t.Fatalf("read = %+v", read)
	}
	rows := PlanCleanup(read[0], cleanupState(git.CleanupBranch{Name: "keep", CommitID: "c1", MergedDefault: true, MergedHead: true}))
	if got := planned(rows)["keep"]; got != "pinned" {
		t.Fatalf("keep = %q", got)
	}
}
