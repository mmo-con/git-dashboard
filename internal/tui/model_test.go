package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/inventory"
	"github.com/mmo-con/git-dashboard/internal/jobs"
)

func repo(name string, family domain.FamilyID, dirty bool) domain.Repository {
	w := domain.WorktreeState{UntrackedChecked: true}
	if dirty {
		w.Modified = 1
	}
	return domain.Repository{
		ID: domain.RepositoryID("/r/" + name), StoreID: domain.StoreID("/r/" + name + "/.git"),
		Name: name, Path: "/r/" + name, Family: family,
		// Unavailable entries are not inspected, which keeps the test free of
		// Git processes.
		Availability: domain.NotARepository,
		Inspection:   domain.InspectionInfo{FinishedAt: time.Now(), Generation: 1},
		Worktree:     domain.KnownValue(w),
	}
}

func newModel(t *testing.T) *Model {
	t.Helper()
	a := app.New("/none", config.Default(), git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(inventoryMsg{result: inventory.Result{Repositories: []domain.Repository{
		repo("api", "Work", true), repo("frontend", "Work", false), repo("dotfiles", "Personal", false),
	}}})
	return m
}

func keys(m *Model, ks ...string) {
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func names(m *Model) string {
	var out []string
	for _, id := range m.visible {
		out = append(out, m.repo(id).Name)
	}
	return strings.Join(out, " ")
}

func TestFilterFamilyAndSelection(t *testing.T) {
	m := newModel(t)
	if names(m) != "api dotfiles frontend" {
		t.Fatalf("visible = %q", names(m))
	}
	keys(m, "j", "j") // cursor on frontend
	keys(m, "/", "i", "s", ":", "d", "i", "r", "t", "y", "enter")
	if names(m) != "api" || m.mode != modeNormal {
		t.Fatalf("filter: %q", names(m))
	}
	keys(m, "esc")
	if names(m) != "api dotfiles frontend" {
		t.Fatalf("Esc must clear the filter: %q", names(m))
	}
	keys(m, "tab")
	if m.filter.Family != "Personal" || names(m) != "dotfiles" {
		t.Fatalf("family: %q %q", m.filter.Family, names(m))
	}
	keys(m, "tab", "tab")
	if m.filter.Family != "" {
		t.Fatalf("family cycle must return to All: %q", m.filter.Family)
	}
}

func TestMarksTargetsAndHiddenCount(t *testing.T) {
	m := newModel(t)
	keys(m, " ", " ") // mark api and dotfiles
	if len(m.marks) != 2 {
		t.Fatalf("marks = %v", m.marks)
	}
	keys(m, "/", "f", "r", "o", "n", "t", "enter")
	targets, hidden := m.targets()
	if len(targets) != 2 || hidden != 2 {
		t.Fatalf("targets=%d hidden=%d", len(targets), hidden)
	}
	keys(m, "f")
	if m.mode != modeConfirmFetch || m.fetchHidden != 2 {
		t.Fatalf("f with marks must open the confirmation: mode=%v hidden=%d", m.mode, m.fetchHidden)
	}
	if !strings.Contains(m.View(), "2 marked repositories are hidden") {
		t.Fatal("confirmation must mention hidden marks")
	}
	keys(m, "esc", "A")
	if t2, _ := m.targets(); len(t2) != 1 || len(m.marks) != 0 {
		t.Fatalf("without marks the visible rows are targets: %d", len(t2))
	}
}

func TestBatchEventsAreApplied(t *testing.T) {
	m := newModel(t)
	r := m.repos[0]
	m.batch = &batch{id: 7, op: domain.OperationFetch, rows: []opRow{{
		job:    jobs.Job{RepositoryID: r.ID, StoreID: r.StoreID, Operation: domain.OperationFetch},
		result: domain.OperationResult{Status: domain.StatusQueued},
	}}}
	if m.storeBusy(r.StoreID) != "fetch" {
		t.Fatal("queued fetch must mark the store busy")
	}
	m.Update(eventsMsg{events: []jobs.Event{{BatchID: 7, JobID: 0, Repositories: []domain.RepositoryID{r.ID},
		Result: domain.OperationResult{RepositoryID: r.ID, Operation: domain.OperationFetch, Status: domain.StatusFailed}}}})
	if !m.batch.done() || m.storeBusy(r.StoreID) != "" {
		t.Fatal("batch must be finished")
	}
	if got := upstreamText(m.repo(r.ID), m.lastOp(r.ID)); got != "fetch failed" {
		t.Fatalf("upstream column = %q", got)
	}
	if !m.messageErr {
		t.Fatal("failed batch must be reported as an error")
	}
}

func TestStaleInspectionIsIgnored(t *testing.T) {
	m := newModel(t)
	id := m.repos[0].ID
	m.repo(id).Inspection.Generation = 5
	m.Update(eventsMsg{events: []jobs.Event{{Result: domain.OperationResult{RepositoryID: id, Status: domain.StatusSucceeded},
		Payload: app.InspectionPayload{Generation: 4, Inspection: domain.Inspection{
			Worktree: domain.KnownValue(domain.WorktreeState{Conflicts: 9})}}}}})
	if m.repo(id).Worktree.Value.Conflicts == 9 {
		t.Fatal("an older generation overwrote newer state")
	}
}

func TestStatusTexts(t *testing.T) {
	r := repo("x", "", false)
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Operation = domain.KnownValue(domain.Merging)
	r.Worktree = domain.KnownValue(domain.WorktreeState{Conflicts: 1, Untracked: 2})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true, Ahead: 1, Behind: 2})
	r.ExpectedRemote = &domain.ExpectedRemote{URL: "git@a:b.git"}
	r.Remotes = domain.KnownValue([]domain.Remote{{Name: "origin", URL: "git@a:c.git"}})
	if got := statusText(&r); got != "merging, conflict 1, untracked 2 ≠remote" {
		t.Errorf("status = %q", got)
	}
	if got := upstreamText(&r, nil); got != "diverged +1 -2" {
		t.Errorf("upstream = %q", got)
	}
	r.Worktree = domain.Known[domain.WorktreeState]{}
	if got := statusText(&r); got != "unknown" {
		t.Errorf("unknown state must not be shown as clean: %q", got)
	}
	r.Availability = domain.Missing
	if got := statusText(&r); got != "missing" {
		t.Errorf("missing = %q", got)
	}
}

func TestViewRendersAllModes(t *testing.T) {
	m := newModel(t)
	for _, k := range []string{"?", "e", "s", "u", "o"} {
		keys(m, k)
		if v := m.View(); strings.Count(v, "\n") != m.height-1 {
			t.Errorf("mode %q renders %d lines, want %d", k, strings.Count(v, "\n")+1, m.height)
		}
		keys(m, "esc")
		if m.mode != modeNormal {
			t.Errorf("Esc must close mode after %q", k)
		}
	}
}
