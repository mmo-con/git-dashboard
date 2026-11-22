package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/jobs"
)

func behindRepo() domain.Repository {
	return domain.Repository{
		ID: "r", StoreID: "s", Name: "r", Availability: domain.Available,
		Inspection: domain.InspectionInfo{FinishedAt: time.Now()},
		Head:       domain.KnownValue(domain.HeadState{Branch: "main", CommitID: "abc"}),
		Operation:  domain.KnownValue(domain.NoOperation),
		Worktree:   domain.KnownValue(domain.WorktreeState{Untracked: 3, UntrackedChecked: true}),
		Upstream:   domain.KnownValue(domain.UpstreamState{Configured: true, Ref: "origin/main", CommitID: "up1", Behind: 2}),
	}
}

func TestEligibility(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(r *domain.Repository)
		busy   bool
		want   string
	}{
		{"eligible with untracked files", func(*domain.Repository) {}, false, ""},
		{"unavailable", func(r *domain.Repository) { r.Availability = domain.Missing }, false, "repository is missing"},
		{"unknown", func(r *domain.Repository) { r.Upstream = domain.Known[domain.UpstreamState]{} }, false, "state is unknown"},
		{"inspection error", func(r *domain.Repository) { r.Inspection.Err = errors.New("x") }, false, "state is unknown"},
		{"rebase", func(r *domain.Repository) { r.Operation.Value = domain.Rebasing }, false, "rebasing in progress"},
		{"dirty", func(r *domain.Repository) { r.Worktree.Value.Modified = 1 }, false, "worktree contains local changes"},
		{"staged", func(r *domain.Repository) { r.Worktree.Value.Staged = 1 }, false, "worktree contains local changes"},
		{"detached", func(r *domain.Repository) { r.Head.Value.Detached = true }, false, "detached HEAD"},
		{"unborn", func(r *domain.Repository) { r.Head.Value.Unborn = true }, false, "branch has no commits"},
		{"no upstream", func(r *domain.Repository) { r.Upstream.Value.Configured = false }, false, "no upstream"},
		{"gone", func(r *domain.Repository) { r.Upstream.Value.Gone = true }, false, "upstream gone"},
		{"diverged", func(r *domain.Repository) { r.Upstream.Value.Ahead = 1 }, false, "diverged from upstream"},
		{"ahead", func(r *domain.Repository) { r.Upstream.Value = domain.UpstreamState{Configured: true, Ahead: 1} }, false, "ahead of upstream"},
		{"up to date", func(r *domain.Repository) { r.Upstream.Value.Behind = 0 }, false, "already up to date"},
		{"upstream commit unknown", func(r *domain.Repository) { r.Upstream.Value.CommitID = "" }, false, "upstream commit is unknown"},
		{"busy", func(*domain.Repository) {}, true, "another operation or lazygit session is active"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := behindRepo()
			tc.mutate(&r)
			if got := Eligibility(r, tc.busy); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeGit is called concurrently by scheduler workers.
type fakeGit struct {
	git.Interface
	mu         sync.Mutex
	inspection domain.Inspection
	merged     int
	target     string
	fetched    []string
}

func (f *fakeGit) Inspect(context.Context, domain.Location, git.InspectOptions) (domain.Inspection, error) {
	return f.inspection, nil
}

func (f *fakeGit) MergeFastForwardOnly(_ context.Context, _ domain.Location, target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged++
	f.target = target
	return nil
}

func (f *fakeGit) Fetch(_ context.Context, loc domain.Location, _ git.FetchOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, loc.WorktreeRoot)
	return nil
}

func runJobs(t *testing.T, js []jobs.Job) []jobs.Event {
	t.Helper()
	s := jobs.NewScheduler(map[jobs.Kind]int{jobs.KindFetch: 2, jobs.KindUpdate: 2})
	s.Submit(js)
	var final []jobs.Event
	for len(final) < len(js) {
		for _, e := range s.Events.Next() {
			if e.Result.Status.Final() {
				final = append(final, e)
			}
		}
	}
	return final
}

func TestUpdateJobRechecksBeforeMerge(t *testing.T) {
	r := behindRepo()
	f := &fakeGit{inspection: domain.Inspection{
		Head: r.Head, Operation: r.Operation, Upstream: r.Upstream,
		Worktree: domain.KnownValue(domain.WorktreeState{Modified: 1}),
	}}
	ev := runJobs(t, UpdateJobs(f, []domain.Repository{r}, []UpdateCandidate{{Repository: r}}, time.Second))
	if ev[0].Result.Status != domain.StatusSkipped || f.merged != 0 {
		t.Fatalf("changed repository must be skipped: %+v merged=%d", ev[0].Result, f.merged)
	}

	f.inspection.Worktree = r.Worktree
	ev = runJobs(t, UpdateJobs(f, []domain.Repository{r}, []UpdateCandidate{{Repository: r}}, time.Second))
	if ev[0].Result.Status != domain.StatusSucceeded || f.merged != 1 || f.target != "up1" {
		t.Fatalf("eligible repository must be merged to the previewed upstream commit: %+v merged=%d target=%q", ev[0].Result, f.merged, f.target)
	}
}

func TestUpdateJobSkipsStateChangedSincePlanning(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(in *domain.Inspection)
		want   string
	}{
		{"upstream moved", func(in *domain.Inspection) { in.Upstream.Value.CommitID, in.Upstream.Value.Behind = "up2", 3 }, "upstream moved"},
		{"upstream rewritten with the same distance", func(in *domain.Inspection) { in.Upstream.Value.CommitID = "up2" }, "upstream moved"},
		{"branch switched", func(in *domain.Inspection) { in.Head.Value.Branch = "release" }, "branch changed"},
		{"HEAD moved", func(in *domain.Inspection) { in.Head.Value.CommitID = "def" }, "HEAD moved"},
		{"upstream changed", func(in *domain.Inspection) { in.Upstream.Value.Ref = "fork/main" }, "upstream changed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := behindRepo()
			f := &fakeGit{inspection: domain.Inspection{Head: r.Head, Operation: r.Operation, Upstream: r.Upstream, Worktree: r.Worktree}}
			tc.mutate(&f.inspection)
			ev := runJobs(t, UpdateJobs(f, []domain.Repository{r}, []UpdateCandidate{{Repository: r}}, time.Second))
			if ev[0].Result.Status != domain.StatusSkipped || f.merged != 0 ||
				!strings.Contains(ev[0].Result.Error.Error(), tc.want+" since the update was planned") {
				t.Fatalf("%+v merged=%d", ev[0].Result, f.merged)
			}
		})
	}
}

func TestFetchJobsDeduplicateStores(t *testing.T) {
	all := []domain.Repository{
		{ID: "main", StoreID: "s1", Availability: domain.Available, Location: domain.Location{WorktreeRoot: "/main"}},
		{ID: "wt", StoreID: "s1", Availability: domain.Available, Location: domain.Location{WorktreeRoot: "/wt"}},
		{ID: "other", StoreID: "s2", Availability: domain.Available, Location: domain.Location{WorktreeRoot: "/other"}},
		{ID: "missing", Availability: domain.Missing},
	}
	f := &fakeGit{}
	js := FetchJobs(f, all, []domain.Repository{all[1], all[0], all[2], all[3]}, git.FetchOptions{}, time.Second)
	if len(js) != 2 {
		t.Fatalf("got %d jobs, want 2", len(js))
	}
	if len(js[0].Repositories) != 2 || !js[0].Mutating {
		t.Fatalf("job must cover every worktree of the store: %+v", js[0].Repositories)
	}
	runJobs(t, js)
	if len(f.fetched) != 2 {
		t.Fatalf("fetched %v", f.fetched)
	}
}

func TestRefreshStoreFetchTimes(t *testing.T) {
	old, recent := time.Unix(100, 0), time.Unix(200, 0)
	repos := []domain.Repository{
		{StoreID: "s", FetchHead: domain.KnownValue(&old)},
		{StoreID: "s", FetchHead: domain.KnownValue(&recent)},
		{StoreID: "t", FetchHead: domain.KnownValue[*time.Time](nil)},
		{StoreID: "u"},
	}
	RefreshStoreFetchTimes(repos)
	if !repos[0].LastFetch.Value.Equal(recent) || !repos[1].LastFetch.Value.Equal(recent) {
		t.Fatal("newest FETCH_HEAD of the store must win")
	}
	if !repos[2].LastFetch.Valid || repos[2].LastFetch.Value != nil {
		t.Fatal("never fetched")
	}
	if repos[3].LastFetch.Valid {
		t.Fatal("unknown must stay unknown")
	}
}

func TestStateTokens(t *testing.T) {
	up := func(u domain.UpstreamState) *domain.Repository {
		return &domain.Repository{Availability: domain.Available, Upstream: domain.KnownValue(u),
			Worktree: domain.KnownValue(domain.WorktreeState{})}
	}
	cases := []struct {
		token string
		match *domain.Repository
		miss  *domain.Repository
	}{
		{"is:clean", up(domain.UpstreamState{}), &domain.Repository{Worktree: domain.KnownValue(domain.WorktreeState{Untracked: 1})}},
		{"is:ahead", up(domain.UpstreamState{Configured: true, Ahead: 1}), up(domain.UpstreamState{Configured: true, Behind: 1})},
		{"is:diverged", up(domain.UpstreamState{Configured: true, Ahead: 1, Behind: 1}), up(domain.UpstreamState{Configured: true, Ahead: 1})},
		{"is:noupstream", up(domain.UpstreamState{Configured: true, Gone: true}), up(domain.UpstreamState{Configured: true})},
		{"is:error", &domain.Repository{Inspection: domain.InspectionInfo{Err: errors.New("x")}}, up(domain.UpstreamState{})},
		{"is:mismatch", &domain.Repository{ExpectedRemote: &domain.ExpectedRemote{URL: "a"},
			Remotes: domain.KnownValue([]domain.Remote{{Name: "origin", URL: "b"}})}, up(domain.UpstreamState{})},
		{"is:stashed", &domain.Repository{Stashes: domain.KnownValue(1)}, &domain.Repository{Stashes: domain.KnownValue(0)}},
		{"is:unknown-token", nil, up(domain.UpstreamState{})},
	}
	for _, tc := range cases {
		f := Filter{Text: tc.token}
		if tc.match != nil && !f.Matches(tc.match, false) {
			t.Errorf("%s must match", tc.token)
		}
		if f.Matches(tc.miss, false) {
			t.Errorf("%s must not match", tc.token)
		}
	}
	if (Filter{Text: "is:behind"}).Matches(&domain.Repository{}, false) {
		t.Error("unknown state must not match a state token")
	}
}

func TestFilterAndSort(t *testing.T) {
	now := time.Now()
	older := now.Add(-time.Hour)
	repos := []*domain.Repository{
		{Name: "beta", Path: "/w/beta", Family: "Work", Availability: domain.Available,
			Worktree:   domain.KnownValue(domain.WorktreeState{Modified: 1}),
			Upstream:   domain.KnownValue(domain.UpstreamState{Configured: true}),
			LastCommit: domain.KnownValue(&domain.CommitSummary{Time: older})},
		{Name: "Alpha", Path: "/p/alpha", Family: "Personal", Availability: domain.Available,
			Worktree:   domain.KnownValue(domain.WorktreeState{}),
			Upstream:   domain.KnownValue(domain.UpstreamState{Configured: true, Behind: 2}),
			LastCommit: domain.KnownValue(&domain.CommitSummary{Time: now})},
		{Name: "gamma", Path: "/w/gamma", Family: "Work", Availability: domain.Missing},
	}
	match := func(f Filter) []string {
		var out []string
		for _, r := range repos {
			if f.Matches(r, r.Name == "gamma") {
				out = append(out, r.Name)
			}
		}
		return out
	}
	cases := []struct {
		filter Filter
		want   string
	}{
		{Filter{Text: "is:dirty"}, "beta"},
		{Filter{Text: "is:behind"}, "Alpha"},
		{Filter{Text: "is:unavailable"}, "gamma"},
		{Filter{Family: "Work"}, "beta gamma"},
		{Filter{Text: "A"}, "beta Alpha gamma"},
		{Filter{Text: "alp"}, "Alpha"},
		{Filter{Text: "/w/ is:dirty"}, "beta"},
		{Filter{Text: "is:marked"}, "gamma"},
		{Filter{Text: "is:clean is:dirty"}, ""},
	}
	for _, tc := range cases {
		if got := strings.Join(match(tc.filter), " "); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.filter, got, tc.want)
		}
	}

	order := func(k SortKey) string {
		list := append([]*domain.Repository{}, repos...)
		Sort(list, k)
		s := ""
		for _, r := range list {
			s += r.Name + " "
		}
		return s
	}
	for k, want := range map[SortKey]string{
		SortName:       "Alpha beta gamma ",
		SortFamily:     "Alpha beta gamma ",
		SortCommitTime: "Alpha beta gamma ",
		SortWorktree:   "gamma beta Alpha ",
		SortUpstream:   "gamma Alpha beta ",
	} {
		if got := order(k); got != want {
			t.Errorf("sort %s: got %q, want %q", k, got, want)
		}
	}
}
