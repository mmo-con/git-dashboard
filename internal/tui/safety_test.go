package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/headquarters/internal/app"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/inventory"
	"github.com/mmo-con/headquarters/internal/jobs"
	tu "github.com/mmo-con/headquarters/internal/testutil"
)

func writeConfig(t *testing.T, m *Model, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("version = 1\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	m.app.ConfigPath = p
	return p
}

func TestReloadActivatesConfigurationInUpdateLoop(t *testing.T) {
	m := newModel(t)
	p := writeConfig(t, m, "[git]\nfetch_parallelism = 1\n[lazygit]\nenabled = false\n")
	keys(m, "R")
	if !m.loadingInventory {
		t.Fatal("R must start reloading")
	}
	keys(m, "R")
	if !strings.Contains(m.message, "still loading") {
		t.Fatalf("a second R while loading must be refused: %q", m.message)
	}

	// The reload command runs concurrently with rendering, as in Bubble Tea;
	// the race detector reports any shared access.
	msgs := make(chan tea.Msg)
	go func() { msgs <- m.reload()() }()
	var msg tea.Msg
	for msg == nil {
		select {
		case msg = <-msgs:
		default:
			_ = m.View()
		}
	}
	if m.app.Config.File == p {
		t.Fatal("the configuration must not change before the update loop applies it")
	}
	m.Update(msg)
	if m.app.Config.File != p || m.app.Config.Git.FetchParallelism != 1 || m.app.Lazygit.Available() {
		t.Fatalf("reloaded configuration not active: %+v", m.app.Config.Git)
	}
	if m.loadingInventory || len(m.repos) != 0 {
		t.Fatalf("the inventory of the reloaded configuration must replace the old one: %d", len(m.repos))
	}
}

func TestReloadKeepsScanPath(t *testing.T) {
	tu.IsolateGit(t)
	m := newModel(t)
	base := tu.Canonical(t, t.TempDir())
	configured := tu.InitRepo(t, filepath.Join(base, "configured"))
	scanned := tu.InitRepo(t, filepath.Join(base, "scan", "api"))
	writeConfig(t, m, "[git]\nfetch_parallelism = 1\n[[repositories]]\npath = \""+configured+"\"\n")
	m.app.SetScanRoot(filepath.Join(base, "scan"))

	m.Update(m.reload()())
	if len(m.repos) != 1 || m.repos[0].Path != scanned {
		t.Fatalf("reload must rescan the path instead of the configured repositories: %+v", m.repos)
	}
	if m.app.Config.Git.FetchParallelism != 1 {
		t.Fatal("settings of the configuration file must apply in scan mode")
	}
}

func TestReloadWithUnusableGitKeepsConfiguration(t *testing.T) {
	m := newModel(t)
	writeConfig(t, m, "[git]\nexecutable = \"/nonexistent/git\"\n")
	before := m.app.Config
	m.Update(m.reload()())
	if m.app.Config != before || !m.messageErr || !strings.Contains(m.message, "/nonexistent/git") {
		t.Fatalf("config replaced or error not shown: %q", m.message)
	}
}

func TestInspectionsOfEarlierInventoryAreIgnored(t *testing.T) {
	m := newModel(t)
	id := m.repos[0].ID
	// An inspection batch that is still running for the previous inventory;
	// its jobs never run.
	m.inspections = app.NewInspections(m.app, func([]jobs.Job) uint64 {
		m.routes[42] = m.applyInspection
		return 42
	})
	old := m.repos[0]
	old.Availability = domain.Available
	m.inspections.Start([]domain.Repository{old})
	m.Update(inventoryMsg{result: inventory.Result{Repositories: []domain.Repository{repo("api", "Work", false)}}})
	if m.pendingInspect() != 0 {
		t.Fatalf("pending inspections of the old inventory must be forgotten: %d", m.pendingInspect())
	}
	m.Update(eventsMsg{events: []jobs.Event{{BatchID: 42,
		Result: domain.OperationResult{RepositoryID: id, Status: domain.StatusSucceeded},
		Payload: app.InspectionPayload{Generation: 9, Inspection: domain.Inspection{
			Worktree: domain.KnownValue(domain.WorktreeState{Conflicts: 9})}}}}})
	if m.repo(id).Worktree.Value.Conflicts == 9 || m.pendingInspect() != 0 {
		t.Fatal("an inspection of the earlier inventory was applied")
	}
}

func TestExternalTextCannotInjectEscapeSequences(t *testing.T) {
	const evil = "\x1b]0;pwned\x07\x1b[2J\x1bc\u009b1m"
	m := newModel(t)
	r := repo("x"+evil, domain.FamilyID("fam"+evil), true)
	r.Path = "/r/x" + evil
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Operation = domain.KnownValue(domain.NoOperation)
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true, Ref: "origin/main"})
	r.Remotes = domain.KnownValue([]domain.Remote{{Name: "origin", URL: "https://h/" + evil}})
	r.LastCommit = domain.KnownValue(&domain.CommitSummary{ShortID: "abc", Author: "a" + evil, Subject: "s" + evil, Time: time.Now()})
	r.Inspected.Err = errors.New("inspection " + evil + "\nsecond line " + evil)
	m.Update(inventoryMsg{result: inventory.Result{Repositories: []domain.Repository{r}, Warnings: []string{"warn " + evil}}})
	b := fetchBatch(t, m, r.ID)
	m.ops.Handle(jobs.Event{BatchID: b.ID, Repositories: []domain.RepositoryID{r.ID}, Result: domain.OperationResult{
		RepositoryID: r.ID, Operation: domain.OperationFetch, Status: domain.StatusFailed,
		Error: errors.New("fetch " + evil), Stderr: "fatal: " + evil + "\r\nmore " + evil}})
	m.setError("toast " + evil)

	for _, k := range []string{"", "e", "b", "/", "t"} {
		if k != "" {
			keys(m, k)
		}
		v := m.View()
		for _, bad := range []string{"\x07", "\x1b]", "\x1b[2J", "\x1bc", "\u009b", "\r"} {
			if strings.Contains(v, bad) {
				t.Errorf("mode %q: output contains %q", k, bad)
			}
		}
		keys(m, "esc")
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitAsksWhileBatchRuns(t *testing.T) {
	m := newModel(t)
	if !isQuit(m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})) {
		t.Fatal("without a batch, q must quit at once")
	}
	r := m.repos[0]
	fetchBatch(t, m, r.ID)
	setRow(m, 0, domain.StatusRunning, nil)
	m.mode = modeOperations
	if cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd != nil || m.mode != modeNormal {
		t.Fatal("q in the operations view closes the view")
	}
	if isQuit(m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})) || m.mode != modeConfirmQuit {
		t.Fatal("q must ask while a batch runs")
	}
	if !strings.Contains(m.View(), "1 unfinished") {
		t.Fatal("the confirmation must explain what quitting cancels")
	}
	keys(m, "esc")
	if m.mode != modeNormal {
		t.Fatal("Esc must keep the program running")
	}
	if isQuit(m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})) || m.mode != modeConfirmQuit {
		t.Fatal("Ctrl+C must ask while a batch runs")
	}
	if !isQuit(m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})) {
		t.Fatal("a second Ctrl+C must quit")
	}
}
