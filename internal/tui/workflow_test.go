package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/headquarters/internal/app"
	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	tu "github.com/mmo-con/headquarters/internal/testutil"
)

// pump forwards scheduler events to the model until no inspection, batch, or
// confirmed update is pending, like waitEvents does in the running program.
func pump(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for m.pendingInspect() > 0 || m.ops.Running() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for jobs")
		}
		m.Update(eventsMsg{events: m.app.Scheduler.Events.Next()})
	}
}

func row(t *testing.T, m *Model, id domain.RepositoryID) (int, app.BatchRow) {
	t.Helper()
	for i, r := range m.ops.Batch().Rows {
		if r.Job.RepositoryID == id {
			return i, r
		}
	}
	t.Fatalf("%s is not part of the batch", id)
	return 0, app.BatchRow{}
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
		if !r.IsAvailable() || r.Inspected.Err != nil {
			t.Fatalf("%s: %v %v", r.Path, r.Availability, r.Inspected.Err)
		}
	}

	keys(m, "F")
	if m.mode != modeOperations || m.ops.Batch() == nil || m.ops.Batch().Op != domain.OperationFetch || len(m.ops.Batch().Rows) != 3 {
		t.Fatalf("F must fetch all visible repositories without a confirmation: mode=%v", m.mode)
	}
	pump(t, m)
	for _, r := range m.ops.Batch().Rows {
		if r.Result.Status != domain.StatusSucceeded {
			t.Fatalf("fetch %s: %s %v", r.Job.RepositoryID, r.Result.Status, r.Result.Error)
		}
	}
	if b := m.repo(ids["behind"]).Upstream.Value.Behind; b != 1 {
		t.Fatalf("after fetch the repository must be reinspected: behind=%d", b)
	}

	// The update fetches again, so it applies upstream commits that were not
	// known when it was confirmed.
	tu.Commit(t, origin, "newer.txt", "newer\n", "upstream change after the fetch")
	keys(m, "esc", "U")
	if m.mode != modeUpdatePreview || len(m.preview.ok) != 1 || len(m.preview.skip) != 2 {
		t.Fatalf("confirmation: ok=%d skip=%d", len(m.preview.ok), len(m.preview.skip))
	}
	if v := m.View(); strings.Contains(v, "git ") || !strings.Contains(v, "Fetch and update 3 repositories?") {
		t.Fatalf("the confirmation must describe the update instead of Git commands:\n%s", v)
	}
	keys(m, "y")
	if m.mode != modeOperations || m.ops.Batch().Op != domain.OperationFetch || !m.ops.UpdatePending() {
		t.Fatalf("a confirmed update must fetch first: mode=%v", m.mode)
	}
	pump(t, m)
	if m.ops.Batch().Op != domain.OperationUpdate {
		t.Fatalf("the update must start after the fetch: %s", m.ops.Batch().Op)
	}
	if len(m.ops.Batch().Rows) != 2 {
		t.Fatalf("the worklist must hold the update and the blocked repository, not the current one: %d rows", len(m.ops.Batch().Rows))
	}
	if _, r := row(t, m, ids["behind"]); r.Result.Status != domain.StatusSucceeded {
		t.Fatalf("update: %s %v", r.Result.Status, r.Result.Error)
	}
	if tu.Git(t, behind, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("behind was not fast-forwarded")
	}
	idx, r := row(t, m, ids["dirty"])
	if r.Result.Status != domain.StatusSkipped || !strings.Contains(r.Result.Error.Error(), "local changes") {
		t.Fatalf("blocked row: %s %v", r.Result.Status, r.Result.Error)
	}

	keys(m, "u")
	if m.mode != modeUpdatePreview || len(m.preview.ok) != 0 {
		t.Fatalf("an unresolved blocker must stay blocked in the continued preview: ok=%d", len(m.preview.ok))
	}
	keys(m, "esc")

	m.opSelected = idx
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	store := m.repo(ids["dirty"]).StoreID
	if cmd == nil || m.visit == nil || m.ops.StoreActivity(store) != "an interactive session" {
		t.Fatal("Enter must hand the selected row over to lazygit")
	}
	if m.app.Scheduler.Locks.TryLock(store) {
		t.Fatal("the store must be locked during the handover")
	}
	tu.Git(t, dirty, "checkout", "--", "README") // the user discards the change in lazygit
	m.Update(handoverDoneMsg{label: "lazygit", repo: ids["dirty"], store: store})
	pump(t, m)
	if fu, ok := m.followUp(m.ops.Batch().Rows[idx]); fu != "now eligible" || !ok {
		t.Fatalf("follow-up = %q", fu)
	}

	keys(m, "u", "y")
	pump(t, m)
	if _, r := row(t, m, ids["dirty"]); r.Result.Status != domain.StatusSucceeded {
		t.Fatalf("continued update: %s %v", r.Result.Status, r.Result.Error)
	}
	if tu.Git(t, dirty, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("dirty was not fast-forwarded after the blocker was resolved")
	}
	keys(m, "u")
	if m.message != "nothing left to update" {
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
	for _, r := range m.repos {
		if m.ops.StoreBusy(r.StoreID) {
			t.Fatalf("store of %s must stay free", r.Name)
		}
	}
	if m.ops.Batch() != nil {
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
	if len(m.preview.ok) != 1 || m.preview.ok[0].Repository.Path != broken {
		t.Fatalf("before the fetch only broken is known to be behind: ok=%+v", m.preview.ok)
	}
	brokenID := m.preview.ok[0].Repository.ID
	keys(m, "y")
	if m.mode != modeOperations || m.preview != nil || m.ops.Batch().Op != domain.OperationFetch || !m.ops.UpdatePending() {
		t.Fatalf("the update must fetch first: mode=%v", m.mode)
	}
	// Leaving the operations view must keep it closed when the update
	// starts, so that no key meant for another view acts on it.
	keys(m, "esc")
	pump(t, m)
	if m.mode != modeNormal || m.ops.Batch().Op != domain.OperationUpdate {
		t.Fatalf("the update must start on its own without opening a view: mode=%v op=%s", m.mode, m.ops.Batch().Op)
	}
	if tu.Git(t, behind, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("behind must be fast-forwarded to the fetched state")
	}
	_, r := row(t, m, brokenID)
	if r.Result.Status != domain.StatusSkipped || !strings.HasPrefix(r.Result.Error.Error(), "fetch failed") {
		t.Fatalf("a failed fetch must skip the repository: %s %v", r.Result.Status, r.Result.Error)
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
	if strings.Contains(small, "commit second") || !strings.Contains(small, "commit third") || !strings.Contains(small, "── STATUS") {
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
