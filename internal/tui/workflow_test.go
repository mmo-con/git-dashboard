package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/jobs"
	tu "github.com/mmo-con/git-dashboard/internal/testutil"
)

// pump forwards scheduler events to the model until no inspection, batch, or
// confirmed update is pending, like waitEvents does in the running program.
func pump(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for m.pendingInspect() > 0 || m.batchRunning() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for jobs")
		}
		m.Update(eventsMsg{events: m.app.Scheduler.Events.Next()})
	}
}

func row(t *testing.T, m *Model, id domain.RepositoryID) (int, opRow) {
	t.Helper()
	for i, r := range m.batch.rows {
		if r.job.RepositoryID == id {
			return i, r
		}
	}
	t.Fatalf("%s is not part of the batch", id)
	return 0, opRow{}
}

// TestFetchUpdateAndWorklistWithRealRepositories drives the user interface
// through fetch, safe update, a lazygit visit that resolves a blocker, and
// the continued update.
func TestFetchUpdateAndWorklistWithRealRepositories(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	quiet := tu.InitRepo(t, filepath.Join(base, "quiet-origin"))
	for _, name := range []string{"behind", "dirty"} {
		tu.Git(t, base, "clone", "-q", origin, name)
	}
	tu.Git(t, base, "clone", "-q", quiet, "current")
	behind, dirty, current := filepath.Join(base, "behind"), filepath.Join(base, "dirty"), filepath.Join(base, "current")
	tu.Commit(t, origin, "new.txt", "new\n", "upstream change")
	tu.WriteFile(t, filepath.Join(dirty, "README"), "local edit\n")

	cfg := config.Default()
	for _, p := range []string{behind, dirty, current} {
		cfg.Repositories = append(cfg.Repositories, config.Repository{Path: p, Enabled: true, Family: "Work"})
	}
	cfg.Lazygit.Command = "true" // stands in for lazygit; the handover itself is not executed
	a := app.New("", cfg, git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.Update(inventoryMsg{result: a.LoadInventory(context.Background())})
	pump(t, m)
	ids := map[string]domain.RepositoryID{}
	for _, r := range m.repos {
		ids[filepath.Base(r.Path)] = r.ID
		if !r.IsAvailable() || r.Inspection.Err != nil {
			t.Fatalf("%s: %v %v", r.Path, r.Availability, r.Inspection.Err)
		}
	}

	keys(m, "F")
	if m.mode != modeOperations || m.batch == nil || m.batch.op != domain.OperationFetch || len(m.batch.rows) != 3 {
		t.Fatalf("F must fetch all visible repositories without a confirmation: mode=%v", m.mode)
	}
	pump(t, m)
	for _, r := range m.batch.rows {
		if r.result.Status != domain.StatusSucceeded {
			t.Fatalf("fetch %s: %s %v", r.job.RepositoryID, r.result.Status, r.result.Error)
		}
	}
	if b := m.repo(ids["behind"]).Upstream.Value.Behind; b != 1 {
		t.Fatalf("after fetch the repository must be reinspected: behind=%d", b)
	}

	// The update fetches again, so it applies upstream commits that were not
	// known when it was confirmed.
	tu.Commit(t, origin, "newer.txt", "newer\n", "upstream change after the fetch")
	keys(m, "esc", "U")
	if m.mode != modeUpdatePreview || len(m.updateOK) != 1 || len(m.updateSkip) != 2 {
		t.Fatalf("confirmation: ok=%d skip=%d", len(m.updateOK), len(m.updateSkip))
	}
	if v := m.View(); strings.Contains(v, "git ") || !strings.Contains(v, "Fetch and update 3 repositories?") {
		t.Fatalf("the confirmation must describe the update instead of Git commands:\n%s", v)
	}
	keys(m, "y")
	if m.mode != modeOperations || m.batch.op != domain.OperationFetch || m.refetch == nil {
		t.Fatalf("a confirmed update must fetch first: mode=%v", m.mode)
	}
	pump(t, m)
	if m.batch.op != domain.OperationUpdate {
		t.Fatalf("the update must start after the fetch: %s", m.batch.op)
	}
	if len(m.batch.rows) != 2 {
		t.Fatalf("the worklist must hold the update and the blocked repository, not the current one: %d rows", len(m.batch.rows))
	}
	if _, r := row(t, m, ids["behind"]); r.result.Status != domain.StatusSucceeded {
		t.Fatalf("update: %s %v", r.result.Status, r.result.Error)
	}
	if tu.Git(t, behind, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("behind was not fast-forwarded")
	}
	idx, r := row(t, m, ids["dirty"])
	if r.result.Status != domain.StatusSkipped || !strings.Contains(r.result.Error.Error(), "local changes") {
		t.Fatalf("blocked row: %s %v", r.result.Status, r.result.Error)
	}

	keys(m, "u")
	if m.mode != modeUpdatePreview || len(m.updateOK) != 0 {
		t.Fatalf("an unresolved blocker must stay blocked in the continued preview: ok=%d", len(m.updateOK))
	}
	keys(m, "esc")

	m.overlayIdx = idx
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	store := m.repo(ids["dirty"]).StoreID
	if cmd == nil || m.visit == nil || !m.handovers[store] {
		t.Fatal("Enter must hand the selected row over to lazygit")
	}
	if m.app.Scheduler.Locks.TryLock(store) {
		t.Fatal("the store must be locked during the handover")
	}
	tu.Git(t, dirty, "checkout", "--", "README") // the user discards the change in lazygit
	m.Update(handoverDoneMsg{label: "lazygit", repo: ids["dirty"], store: store})
	pump(t, m)
	if fu, ok := m.followUp(m.batch.rows[idx]); fu != "now eligible" || !ok {
		t.Fatalf("follow-up = %q", fu)
	}

	keys(m, "u", "y")
	pump(t, m)
	if _, r := row(t, m, ids["dirty"]); r.result.Status != domain.StatusSucceeded {
		t.Fatalf("continued update: %s %v", r.result.Status, r.result.Error)
	}
	if tu.Git(t, dirty, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("dirty was not fast-forwarded after the blocker was resolved")
	}
	keys(m, "u")
	if m.message != "nothing left to update" {
		t.Fatalf("message = %q", m.message)
	}
}

func TestRetryAndCancelBatch(t *testing.T) {
	m := newModel(t)
	r := m.repos[0]
	var calls atomic.Int32
	release := make(chan struct{})
	job := jobs.Job{Kind: jobs.KindFetch, Operation: domain.OperationFetch, RepositoryID: r.ID, StoreID: r.StoreID,
		Repositories: []domain.RepositoryID{r.ID},
		Run: func(ctx context.Context) (any, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("network down")
			}
			select {
			case <-release:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}}

	keys(m, "x")
	m.submitBatch(domain.OperationFetch, []jobs.Job{job}, nil, true)
	keys(m, "x")
	if calls.Load() > 1 {
		t.Fatal("retry must wait until the batch is finished")
	}
	pump(t, m)
	if m.batch.rows[0].result.Status != domain.StatusFailed {
		t.Fatalf("first run: %s", m.batch.rows[0].result.Status)
	}
	if !strings.Contains(m.View(), "network down") {
		t.Fatal("operations view must show the failure")
	}
	first := m.batch
	keys(m, "x")
	if m.batch == first || len(m.batch.rows) != 1 {
		t.Fatal("x must start a new batch with the failed jobs")
	}
	keys(m, "c")
	pump(t, m)
	if s := m.batch.rows[0].result.Status; s != domain.StatusCancelled {
		t.Fatalf("cancelled batch: %s", s)
	}
	close(release)

	keys(m, "x")
	pump(t, m)
	if s := m.batch.rows[0].result.Status; s != domain.StatusSucceeded {
		t.Fatalf("cancelled jobs must be retried too: %s", s)
	}
	keys(m, "x")
	if m.message != "nothing to retry" {
		t.Fatalf("message = %q", m.message)
	}
}

func TestActionsOnUnavailableRepositories(t *testing.T) {
	m := newModel(t)
	cases := []struct {
		keys []string
		want string
	}{
		{[]string{"enter"}, "api is not a repository"},
		{[]string{"o", "enter"}, "api is not a repository"},
		{[]string{"f"}, "no available repositories to fetch"},
	}
	for _, tc := range cases {
		m.mode, m.message = modeNormal, ""
		keys(m, tc.keys...)
		if m.message != tc.want || !m.messageErr {
			t.Errorf("%v: message = %q", tc.keys, m.message)
		}
	}
	if len(m.handovers) != 0 || m.batch != nil {
		t.Fatal("nothing may be started for unavailable repositories")
	}
}

// TestConfirmedUpdateFetchesFirst checks that a confirmed update fetches its
// repositories, updates with the fetched state without opening a view, and
// skips a repository whose fetch failed even though it looked eligible.
func TestConfirmedUpdateFetchesFirst(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Commit(t, origin, "second.txt", "second\n", "second")
	for _, name := range []string{"behind", "broken"} {
		tu.Git(t, base, "clone", "-q", origin, name)
	}
	behind, broken := filepath.Join(base, "behind"), filepath.Join(base, "broken")
	tu.Git(t, broken, "reset", "-q", "--hard", "HEAD~1")
	tu.Git(t, broken, "remote", "set-url", "origin", filepath.Join(base, "gone"))
	tu.Commit(t, origin, "third.txt", "third\n", "third")
	brokenHead := tu.Git(t, broken, "rev-parse", "HEAD")

	cfg := config.Default()
	for _, p := range []string{behind, broken} {
		cfg.Repositories = append(cfg.Repositories, config.Repository{Path: p, Enabled: true})
	}
	a := app.New("", cfg, git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.Update(inventoryMsg{result: a.LoadInventory(context.Background())})
	pump(t, m)

	keys(m, "U")
	if len(m.updateOK) != 1 || m.updateOK[0].Repository.Path != broken {
		t.Fatalf("before the fetch only broken is known to be behind: ok=%+v", m.updateOK)
	}
	keys(m, "y")
	if m.mode != modeOperations || m.batch.op != domain.OperationFetch || m.refetch == nil {
		t.Fatalf("the update must fetch first: mode=%v", m.mode)
	}
	// Leaving the operations view must keep it closed when the update
	// starts, so that no key meant for another view acts on it.
	keys(m, "esc")
	pump(t, m)
	if m.mode != modeNormal || m.batch.op != domain.OperationUpdate {
		t.Fatalf("the update must start on its own without opening a view: mode=%v op=%s", m.mode, m.batch.op)
	}
	if tu.Git(t, behind, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("behind must be fast-forwarded to the fetched state")
	}
	_, r := row(t, m, m.updateOK[0].Repository.ID)
	if r.result.Status != domain.StatusSkipped || !strings.HasPrefix(r.result.Error.Error(), "fetch failed") {
		t.Fatalf("a failed fetch must skip the repository: %s %v", r.result.Status, r.result.Error)
	}
	if tu.Git(t, broken, "rev-parse", "HEAD") != brokenHead {
		t.Fatal("a repository whose fetch failed must not be updated")
	}
}

// TestDetailPanelShowsRecentCommits checks that the recent commits of the
// selected repository are read in the background, cached for its HEAD, and
// shortened oldest first when the panel is small.
func TestDetailPanelShowsRecentCommits(t *testing.T) {
	tu.IsolateGit(t)
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	for _, s := range []string{"first", "second", "third", "fourth"} {
		tu.Commit(t, repo, s+".txt", s+"\n", "commit "+s)
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{Path: repo, Enabled: true}}
	a := app.New("", cfg, git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(inventoryMsg{result: a.LoadInventory(context.Background())})
	pump(t, m)
	if m.historyPending == nil && len(m.history) == 0 {
		t.Fatal("the selected repository's history must be requested")
	}
	for m.historyPending != nil {
		m.Update(eventsMsg{events: m.app.Scheduler.Events.Next()})
	}
	r := m.current()
	v := strings.Join(m.detailLines(r, 80).fit(100), "\n")
	for _, want := range []string{"RECENT COMMITS", "commit fourth", "commit third", "commit second"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the detail panel must show %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "commit first") {
		t.Fatalf("the detail panel must show only the configured number of commits:\n%s", v)
	}

	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if m.historyPending != nil {
		t.Fatal("a cached history must not be read again while HEAD is unchanged")
	}

	full := len(m.detailLines(r, 80).fit(100))
	small := strings.Join(m.detailLines(r, 80).fit(full-1), "\n")
	if strings.Contains(small, "commit second") || !strings.Contains(small, "commit third") || !strings.Contains(small, "── BRANCH") {
		t.Fatalf("the oldest commit must be dropped first:\n%s", small)
	}

	m.app.Config.UI.DetailCommits = 1
	m.history = map[domain.RepositoryID]historyEntry{}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if m.historyPending != nil {
		t.Fatal("one commit comes from the inspection and needs no Git process")
	}
	if v := strings.Join(m.detailLines(r, 80).fit(100), "\n"); !strings.Contains(v, "LAST COMMIT") || !strings.Contains(v, "commit fourth") {
		t.Fatalf("one commit is the latest commit:\n%s", v)
	}
}
