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

// pump forwards scheduler events to the model until no inspection or batch
// is pending, like waitEvents does in the running program.
func pump(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for m.pendingInspect() > 0 || (m.batch != nil && !m.batch.done()) {
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
	if m.mode != modeConfirmFetch || len(m.fetchTargets) != 3 {
		t.Fatalf("F must confirm all visible repositories: mode=%v targets=%d", m.mode, len(m.fetchTargets))
	}
	keys(m, "y")
	pump(t, m)
	for _, r := range m.batch.rows {
		if r.result.Status != domain.StatusSucceeded {
			t.Fatalf("fetch %s: %s %v", r.job.RepositoryID, r.result.Status, r.result.Error)
		}
	}
	if b := m.repo(ids["behind"]).Upstream.Value.Behind; b != 1 {
		t.Fatalf("after fetch the repository must be reinspected: behind=%d", b)
	}

	keys(m, "esc", "u")
	if m.mode != modeUpdatePreview || len(m.updateOK) != 1 || len(m.updateSkip) != 2 {
		t.Fatalf("preview: ok=%d skip=%d", len(m.updateOK), len(m.updateSkip))
	}
	keys(m, "y")
	pump(t, m)
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

func TestUpdateSkipsUpstreamFetchedAfterPreview(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Git(t, base, "clone", "-q", origin, "clone")
	clone := filepath.Join(base, "clone")
	tu.Commit(t, origin, "a.txt", "a\n", "shown in the preview")
	tu.Git(t, clone, "fetch", "-q")

	cfg := config.Default()
	cfg.Repositories = []config.Repository{{Path: clone, Enabled: true}}
	a := app.New("", cfg, git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.Update(inventoryMsg{result: a.LoadInventory(context.Background())})
	pump(t, m)

	keys(m, "u")
	if m.mode != modeUpdatePreview || len(m.updateOK) != 1 {
		t.Fatalf("preview: ok=%d skip=%d", len(m.updateOK), len(m.updateSkip))
	}
	head := tu.Git(t, clone, "rev-parse", "HEAD")
	tu.Commit(t, origin, "b.txt", "b\n", "not shown in the preview")
	tu.Git(t, clone, "fetch", "-q") // another tool fetches while the preview is open
	keys(m, "y")
	pump(t, m)
	if r := m.batch.rows[0]; r.result.Status != domain.StatusSkipped || !strings.Contains(r.result.Error.Error(), "upstream moved") {
		t.Fatalf("update after an external fetch: %s %v", r.result.Status, r.result.Error)
	}
	if tu.Git(t, clone, "rev-parse", "HEAD") != head {
		t.Fatal("HEAD moved although the update was skipped")
	}

	keys(m, "u")
	if m.mode != modeUpdatePreview || len(m.updateOK) != 1 {
		t.Fatalf("the new preview must offer the update again: ok=%d", len(m.updateOK))
	}
	keys(m, "y")
	pump(t, m)
	if tu.Git(t, clone, "rev-parse", "HEAD") != tu.Git(t, origin, "rev-parse", "HEAD") {
		t.Fatal("the confirmed new preview must fast-forward to the fetched state")
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
	m.submitBatch(domain.OperationFetch, []jobs.Job{job}, nil)
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

// TestFetchFromUpdatePreviewRefreshesIt fetches the preview's repositories
// from the preview and checks that u opens the preview again with the
// fetched state, skips a failed fetch, and does not update on its own.
func TestFetchFromUpdatePreviewRefreshesIt(t *testing.T) {
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
	head := tu.Git(t, behind, "rev-parse", "HEAD")

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

	keys(m, "u")
	if len(m.updateOK) != 1 || m.updateOK[0].Repository.Path != broken {
		t.Fatalf("before the fetch only broken is known to be behind: ok=%+v", m.updateOK)
	}
	keys(m, "f")
	if m.mode != modeOperations || m.batch.op != domain.OperationFetch || m.refetch == nil {
		t.Fatalf("f must fetch the preview's repositories: mode=%v", m.mode)
	}
	// Leaving the operations view must not let the preview open on its own,
	// where the next Enter would confirm an update.
	keys(m, "esc")
	pump(t, m)
	if m.mode != modeNormal || m.refetch == nil || !m.refetch.ready {
		t.Fatalf("the preview must wait for u after the fetch: mode=%v", m.mode)
	}
	keys(m, "b", "u")
	if m.mode != modeUpdatePreview || m.refetch != nil {
		t.Fatalf("u in the operations view must open the preview: mode=%v", m.mode)
	}
	if len(m.updateOK) != 1 || m.updateOK[0].Repository.Path != behind {
		t.Fatalf("the preview must use the fetched state: ok=%+v", m.updateOK)
	}
	if len(m.updateSkip) != 1 || !strings.HasPrefix(m.updateSkip[0].SkipReason, "fetch failed") {
		t.Fatalf("a failed fetch must be a skip reason: %+v", m.updateSkip)
	}
	if m.batch.op != domain.OperationFetch || tu.Git(t, behind, "rev-parse", "HEAD") != head {
		t.Fatal("fetching from the preview must not update")
	}
	keys(m, "esc")
	if m.mode != modeOperations {
		t.Fatal("cancelling the refreshed preview must show the fetch results")
	}
}
